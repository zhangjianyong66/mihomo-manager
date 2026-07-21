package mihomo

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
)

type Client struct {
	paths config.Paths
	http  *http.Client
}

func New(paths config.Paths) *Client {
	return &Client{paths: paths, http: &http.Client{Timeout: 8 * time.Second}}
}

func (c *Client) ServiceStatus() string {
	out, err := exec.Command("pgrep", "-f", "mihomo.*-f.*config.yaml").Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return "running"
	}
	return "stopped"
}

func (c *Client) Start() error {
	cmd := c.startCommand()
	logFile, err := os.OpenFile(c.paths.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	return cmd.Start()
}

func (c *Client) startCommand() *exec.Cmd {
	cmd := exec.Command(c.paths.MihomoBin, "-d", c.paths.ConfigDir, "-f", c.paths.ConfigFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

func (c *Client) Stop() error {
	return exec.Command("pkill", "-f", "mihomo.*-f.*config.yaml").Run()
}

func (c *Client) Restart() error {
	_ = c.Stop()
	time.Sleep(500 * time.Millisecond)
	return c.Start()
}

func (c *Client) Reload() error {
	_, err := c.call(http.MethodPut, "/configs?force=true", bytes.NewBufferString(`{"path":"`+c.paths.ConfigFile+`"}`))
	return err
}

func (c *Client) TestConfig() error {
	return exec.Command(c.paths.MihomoBin, "-t", "-f", c.paths.ConfigFile).Run()
}

func (c *Client) Proxies() (map[string]any, error) {
	body, err := c.call(http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	err = json.Unmarshal(body, &data)
	return data, err
}

func (c *Client) GlobalNodes() ([]string, error) {
	body, err := c.call(http.MethodGet, "/proxies/GLOBAL", nil)
	if err != nil {
		return nil, err
	}
	var v struct {
		All []string `json:"all"`
	}
	if err = json.Unmarshal(body, &v); err != nil {
		return nil, err
	}

	// 过滤掉代理组名称，只保留实际节点
	proxies, _ := c.Proxies()
	pmap := map[string]any{}
	if proxies != nil {
		if ps, ok := proxies["proxies"].(map[string]any); ok {
			pmap = ps
		}
	}
	groupTypes := map[string]bool{
		"Selector": true, "URLTest": true, "Fallback": true, "LoadBalance": true,
		"Direct": true, "Reject": true, "RejectDrop": true, "Pass": true, "Compatible": true,
	}
	out := make([]string, 0, len(v.All))
	for _, name := range v.All {
		if raw, ok := pmap[name].(map[string]any); ok {
			if t, ok2 := raw["type"].(string); ok2 && groupTypes[t] {
				continue
			}
		}
		out = append(out, name)
	}
	return out, nil
}

func (c *Client) CurrentNode() (string, error) {
	body, err := c.call(http.MethodGet, "/proxies/GLOBAL", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Now string `json:"now"`
	}
	if err = json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	return v.Now, nil
}

func (c *Client) SwitchNode(name string) error {
	payload := map[string]string{"name": name}
	b, _ := json.Marshal(payload)
	_, err := c.call(http.MethodPut, "/proxies/GLOBAL", bytes.NewBuffer(b))
	return err
}

type NodeDelay struct {
	Name  string
	Delay int
}

type ProxyGroup struct {
	Name string
	Type string
	Now  string
	All  []string
}

type NodeTestEvent struct {
	Done     int
	Total    int
	Result   *NodeDelay
	Err      error
	Finished bool
}

type LogEvent struct {
	Line     string
	Err      error
	Finished bool
}

type RouteDiagnosisResult struct {
	Input       string
	Host        string
	MatchedRule string
	Target      string
	CurrentNode string
	Confidence  string
	Note        string
}

func (c *Client) ListSelectableGroups() ([]ProxyGroup, error) {
	proxies, err := c.Proxies()
	if err != nil {
		return nil, err
	}
	pmap, _ := proxies["proxies"].(map[string]any)
	if pmap == nil {
		return []ProxyGroup{}, nil
	}
	allowed := map[string]bool{
		"Selector": true, "URLTest": true, "Fallback": true, "LoadBalance": true, "Compatible": true, "Pass": true,
	}
	out := make([]ProxyGroup, 0)
	for name, raw := range pmap {
		pm, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := pm["type"].(string)
		if !allowed[typ] {
			continue
		}
		now, _ := pm["now"].(string)
		all := anyToStrings(pm["all"])
		if len(all) == 0 {
			continue
		}
		out = append(out, ProxyGroup{Name: name, Type: typ, Now: now, All: all})
	}
	sort.Slice(out, func(i, j int) bool {
		ai := strings.EqualFold(out[i].Name, "GLOBAL")
		aj := strings.EqualFold(out[j].Name, "GLOBAL")
		if ai != aj {
			return ai
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (c *Client) DiagnoseRoute(input string) (RouteDiagnosisResult, error) {
	host := normalizeDomain(input)
	if strings.Contains(input, "://") {
		if u, err := url.Parse(strings.TrimSpace(input)); err == nil && u.Host != "" {
			host = u.Hostname()
		}
	}
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return RouteDiagnosisResult{}, fmt.Errorf("invalid input: %s", input)
	}
	cfg, err := c.readConfigMap()
	if err != nil {
		return RouteDiagnosisResult{}, err
	}
	rules := anyToStrings(cfg["rules"])
	res := RouteDiagnosisResult{Input: input, Host: host, Confidence: "high"}
	for _, r := range rules {
		rule := strings.TrimSpace(strings.Trim(r, "\""))
		parts := strings.Split(rule, ",")
		if len(parts) < 2 {
			continue
		}
		tp := strings.ToUpper(strings.TrimSpace(parts[0]))
		switch tp {
		case "DOMAIN":
			if len(parts) >= 3 && strings.EqualFold(strings.TrimSpace(parts[1]), host) {
				res.MatchedRule, res.Target = rule, strings.TrimSpace(parts[2])
			}
		case "DOMAIN-SUFFIX":
			if len(parts) >= 3 {
				suffix := strings.ToLower(strings.Trim(strings.TrimSpace(parts[1]), "."))
				if host == suffix || strings.HasSuffix(host, "."+suffix) {
					res.MatchedRule, res.Target = rule, strings.TrimSpace(parts[2])
				}
			}
		case "DOMAIN-WILDCARD":
			if len(parts) >= 3 && wildcardDomainMatch(strings.TrimSpace(parts[1]), host) {
				res.MatchedRule, res.Target = rule, strings.TrimSpace(parts[2])
			}
		case "GEOSITE":
			// v1: conservative CN heuristic
			if len(parts) >= 3 && strings.EqualFold(strings.TrimSpace(parts[1]), "CN") && isCNLikeDomain(host) {
				res.MatchedRule, res.Target = rule, strings.TrimSpace(parts[2])
				res.Confidence = "medium"
				res.Note = "GEOSITE,CN uses heuristic in v1"
			}
		case "GEOIP":
			// domain-level diagnosis can't resolve GEOIP accurately without DNS result
			if len(parts) >= 3 && strings.EqualFold(strings.TrimSpace(parts[1]), "CN") && isCNLikeDomain(host) {
				res.MatchedRule, res.Target = rule, strings.TrimSpace(parts[2])
				res.Confidence = "low"
				res.Note = "GEOIP,CN diagnosis is approximate without DNS resolution"
			}
		case "MATCH":
			if len(parts) >= 2 {
				res.MatchedRule, res.Target = rule, strings.TrimSpace(parts[1])
			}
		}
		if res.Target != "" {
			break
		}
	}
	if res.Target == "" {
		return RouteDiagnosisResult{
			Input: input, Host: host, Confidence: "low",
			Note: "No supported rule matched (v1 supports DOMAIN/DOMAIN-SUFFIX/DOMAIN-WILDCARD/GEOSITE,GEOIP,MATCH)",
		}, nil
	}
	if up := strings.ToUpper(res.Target); up == "DIRECT" || up == "REJECT" {
		res.CurrentNode = up
		return res, nil
	}
	if now, err := c.CurrentNodeForTarget(res.Target); err == nil {
		res.CurrentNode = now
	} else {
		res.Note = strings.TrimSpace(strings.TrimSpace(res.Note) + " failed to query current node")
	}
	return res, nil
}

func (c *Client) CurrentNodeForTarget(target string) (string, error) {
	body, err := c.call(http.MethodGet, "/proxies/"+url.PathEscape(target), nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Now string `json:"now"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", err
	}
	if strings.TrimSpace(v.Now) == "" {
		return target, nil
	}
	return v.Now, nil
}

func (c *Client) GroupNodes(group string) ([]string, string, error) {
	body, err := c.call(http.MethodGet, "/proxies/"+url.PathEscape(group), nil)
	if err != nil {
		return nil, "", err
	}
	var v struct {
		Now string `json:"now"`
		All []any  `json:"all"`
	}
	if err = json.Unmarshal(body, &v); err != nil {
		return nil, "", err
	}
	nodes := dedupNonEmpty(anyToStrings(v.All))
	return nodes, strings.TrimSpace(v.Now), nil
}

func (c *Client) SwitchNodeInGroup(group, node string) error {
	payload := map[string]string{"name": node}
	b, _ := json.Marshal(payload)
	_, err := c.call(http.MethodPut, "/proxies/"+url.PathEscape(group), bytes.NewBuffer(b))
	return err
}

func (c *Client) TestNodes(concurrency int) ([]NodeDelay, error) {
	nodes, err := c.GlobalNodes()
	if err != nil {
		return nil, err
	}
	proxies, _ := c.Proxies()
	validNodes := filterTestableNodes(nodes, proxies)
	const maxProbeNodes = 120
	if len(validNodes) > maxProbeNodes {
		validNodes = validNodes[:maxProbeNodes]
	}
	if concurrency < 1 {
		concurrency = 1
	}
	jobs := make(chan string)
	out := make(chan NodeDelay)
	wg := sync.WaitGroup{}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				if n == "DIRECT" || n == "REJECT" {
					continue
				}
				enc := url.PathEscape(n)
				b, err := c.call(http.MethodGet, "/proxies/"+enc+"/delay?timeout=3000&url=http://www.gstatic.com/generate_204", nil)
				if err != nil {
					continue
				}
				var d struct {
					Delay int `json:"delay"`
				}
				if json.Unmarshal(b, &d) == nil && d.Delay > 0 {
					out <- NodeDelay{Name: n, Delay: d.Delay}
				}
			}
		}()
	}

	go func() {
		for _, n := range validNodes {
			jobs <- n
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()

	res := make([]NodeDelay, 0)
	for d := range out {
		res = append(res, d)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Delay < res[j].Delay })
	return res, nil
}

func (c *Client) TestNodesStream(concurrency, maxProbeNodes int) <-chan NodeTestEvent {
	return c.TestNodesStreamWithStop(concurrency, maxProbeNodes, nil)
}

func (c *Client) TestNodesStreamWithStop(concurrency, maxProbeNodes int, stop <-chan struct{}) <-chan NodeTestEvent {
	ch := make(chan NodeTestEvent, 32)
	go func() {
		defer close(ch)
		nodes, err := c.GlobalNodes()
		if err != nil {
			ch <- NodeTestEvent{Err: err, Finished: true}
			return
		}
		proxies, _ := c.Proxies()
		validNodes := filterTestableNodes(nodes, proxies)
		if maxProbeNodes > 0 && len(validNodes) > maxProbeNodes {
			validNodes = validNodes[:maxProbeNodes]
		}
		total := len(validNodes)
		if total == 0 {
			ch <- NodeTestEvent{Done: 0, Total: 0, Finished: true}
			return
		}
		if concurrency < 1 {
			concurrency = 1
		}

		jobs := make(chan string)
		doneCh := make(chan NodeDelay, total)
		wg := sync.WaitGroup{}

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for n := range jobs {
					if isStopped(stop) {
						return
					}
					enc := url.PathEscape(n)
					b, callErr := c.call(http.MethodGet, "/proxies/"+enc+"/delay?timeout=3000&url=http://www.gstatic.com/generate_204", nil)
					if callErr != nil {
						if !sendNodeDelay(stop, doneCh, NodeDelay{Name: n, Delay: -1}) {
							return
						}
						continue
					}
					var d struct {
						Delay int `json:"delay"`
					}
					if json.Unmarshal(b, &d) != nil || d.Delay <= 0 {
						if !sendNodeDelay(stop, doneCh, NodeDelay{Name: n, Delay: -1}) {
							return
						}
						continue
					}
					if !sendNodeDelay(stop, doneCh, NodeDelay{Name: n, Delay: d.Delay}) {
						return
					}
				}
			}()
		}

		go func() {
			for _, n := range validNodes {
				if isStopped(stop) {
					break
				}
				select {
				case jobs <- n:
				case <-stop:
					close(jobs)
					wg.Wait()
					close(doneCh)
					return
				}
			}
			close(jobs)
			wg.Wait()
			close(doneCh)
		}()

		done := 0
		for nd := range doneCh {
			if isStopped(stop) {
				break
			}
			done++
			copyNd := nd
			res := &copyNd
			ch <- NodeTestEvent{Done: done, Total: total, Result: res}
		}
		ch <- NodeTestEvent{Done: done, Total: total, Finished: true}
	}()
	return ch
}

func (c *Client) TestGroupNodesStreamWithStop(group string, concurrency, maxProbeNodes int, stop <-chan struct{}) <-chan NodeTestEvent {
	ch := make(chan NodeTestEvent, 32)
	go func() {
		defer close(ch)
		nodes, _, err := c.GroupNodes(group)
		if err != nil {
			ch <- NodeTestEvent{Err: err, Finished: true}
			return
		}
		validNodes := filterDisplayNodes(nodes)
		if maxProbeNodes > 0 && len(validNodes) > maxProbeNodes {
			validNodes = validNodes[:maxProbeNodes]
		}
		total := len(validNodes)
		if total == 0 {
			ch <- NodeTestEvent{Done: 0, Total: 0, Finished: true}
			return
		}
		if concurrency < 1 {
			concurrency = 1
		}
		jobs := make(chan string)
		doneCh := make(chan NodeDelay, total)
		wg := sync.WaitGroup{}

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for n := range jobs {
					if isStopped(stop) {
						return
					}
					enc := url.PathEscape(n)
					b, callErr := c.call(http.MethodGet, "/proxies/"+enc+"/delay?timeout=3000&url=http://www.gstatic.com/generate_204", nil)
					if callErr != nil {
						if !sendNodeDelay(stop, doneCh, NodeDelay{Name: n, Delay: -1}) {
							return
						}
						continue
					}
					var d struct {
						Delay int `json:"delay"`
					}
					if json.Unmarshal(b, &d) != nil || d.Delay <= 0 {
						if !sendNodeDelay(stop, doneCh, NodeDelay{Name: n, Delay: -1}) {
							return
						}
						continue
					}
					if !sendNodeDelay(stop, doneCh, NodeDelay{Name: n, Delay: d.Delay}) {
						return
					}
				}
			}()
		}

		go func() {
			for _, n := range validNodes {
				if isStopped(stop) {
					break
				}
				select {
				case jobs <- n:
				case <-stop:
					close(jobs)
					wg.Wait()
					close(doneCh)
					return
				}
			}
			close(jobs)
			wg.Wait()
			close(doneCh)
		}()

		done := 0
		for nd := range doneCh {
			if isStopped(stop) {
				break
			}
			done++
			copyNd := nd
			ch <- NodeTestEvent{Done: done, Total: total, Result: &copyNd}
		}
		ch <- NodeTestEvent{Done: done, Total: total, Finished: true}
	}()
	return ch
}

func isStopped(stop <-chan struct{}) bool {
	if stop == nil {
		return false
	}
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

func sendNodeDelay(stop <-chan struct{}, output chan<- NodeDelay, value NodeDelay) bool {
	if stop == nil {
		output <- value
		return true
	}
	select {
	case output <- value:
		return true
	case <-stop:
		return false
	}
}

func filterTestableNodes(nodes []string, proxies map[string]any) []string {
	groupTypes := map[string]bool{
		"Selector": true, "URLTest": true, "Fallback": true, "LoadBalance": true,
		"Direct": true, "Reject": true, "RejectDrop": true, "Pass": true, "Compatible": true,
	}
	pmap := map[string]any{}
	if proxies != nil {
		if ps, ok := proxies["proxies"].(map[string]any); ok {
			pmap = ps
		}
	}
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n == "DIRECT" || n == "REJECT" || strings.HasPrefix(n, "官网") || strings.HasPrefix(n, "有效期") {
			continue
		}
		if raw, ok := pmap[n].(map[string]any); ok {
			if t, ok2 := raw["type"].(string); ok2 && groupTypes[t] {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func filterDisplayNodes(nodes []string) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		n = strings.TrimSpace(n)
		if n == "" || n == "DIRECT" || n == "REJECT" || strings.HasPrefix(n, "官网") || strings.HasPrefix(n, "有效期") {
			continue
		}
		out = append(out, n)
	}
	return dedupNonEmpty(out)
}

func dedupNonEmpty(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (c *Client) SaveSubscriptionURL(v string) error {
	return os.WriteFile(c.paths.SubscriptionURL, []byte(strings.TrimSpace(v)+"\n"), 0644)
}

func (c *Client) ReadSubscriptionURL() (string, error) {
	b, err := os.ReadFile(c.paths.SubscriptionURL)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (c *Client) UpdateSubscription() error {
	urlValue, err := c.ReadSubscriptionURL()
	if err != nil {
		return err
	}
	oldCfg, err := c.readConfigMap()
	if err != nil {
		return err
	}
	whitelistDomains, err := c.whitelistDomains(oldCfg)
	if err != nil {
		return err
	}
	body, err := c.downloadSubscriptionWithRetry(urlValue, 3)
	if err != nil {
		return err
	}
	if err := c.BackupConfig(); err != nil {
		return err
	}

	cfg, err := parseSubscriptionConfig(body)
	if err != nil {
		return err
	}

	localMixedPort := anyOrDefault(oldCfg["mixed-port"], 7890)
	localSocksPort := anyOrDefault(oldCfg["socks-port"], 7891)
	localExternalController := anyOrDefault(oldCfg["external-controller"], "127.0.0.1:9090")

	// Remove conflicting port setting, use mixed-port instead.
	delete(cfg, "port")
	cfg["mixed-port"] = localMixedPort
	cfg["socks-port"] = localSocksPort
	cfg["external-controller"] = localExternalController

	// 订阅更新默认不依赖 Geo 数据，避免热重载时因下载 GeoIP/GeoSite 阻塞。
	proxyNames := make([]any, 0)
	if proxies, ok := cfg["proxies"].([]any); ok {
		for _, p := range proxies {
			if pm, ok := p.(map[string]any); ok {
				if name, ok := pm["name"].(string); ok && name != "" && name != "DIRECT" && name != "REJECT" {
					proxyNames = append(proxyNames, name)
				}
			}
		}
	}
	cfg["proxy-groups"] = []any{
		map[string]any{"name": "🌐 代理", "type": "select", "proxies": proxyNames},
		map[string]any{"name": "🎯 直连", "type": "select", "proxies": []string{"DIRECT"}},
	}
	cfg["rules"] = []any{
		"MATCH,🌐 代理",
	}
	injectWhitelistRules(cfg, whitelistDomains)

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config failed: %w", err)
	}

	if err := os.WriteFile(c.paths.ConfigFile, out, 0644); err != nil {
		_ = c.RestoreConfig()
		return err
	}
	return nil
}

func parseSubscriptionConfig(body []byte) (map[string]any, error) {
	if proxies, ok, err := parseURIProxies(body); ok {
		if err != nil {
			return nil, err
		}
		return map[string]any{"proxies": proxies}, nil
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("parse subscription config failed: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

func parseURIProxies(body []byte) ([]any, bool, error) {
	lines := subscriptionLines(string(body))
	foundURI := false
	proxies := make([]any, 0)
	errs := make([]string, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !isProxyURI(line) {
			continue
		}
		foundURI = true
		proxy, err := parseProxyURI(line)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		proxies = append(proxies, proxy)
	}
	if !foundURI {
		return nil, false, nil
	}
	if len(proxies) == 0 {
		return nil, true, fmt.Errorf("parse subscription URI failed: %s", strings.Join(errs, "; "))
	}
	return proxies, true, nil
}

func subscriptionLines(content string) []string {
	raw := strings.TrimSpace(content)
	if decoded, ok := decodeSubscriptionBase64(raw); ok && containsProxyURI(decoded) {
		raw = decoded
	}
	return strings.Split(raw, "\n")
}

func decodeSubscriptionBase64(s string) (string, bool) {
	compact := strings.Join(strings.Fields(s), "")
	if compact == "" {
		return "", false
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		b, err := enc.DecodeString(compact)
		if err == nil {
			return string(b), true
		}
	}
	return "", false
}

func containsProxyURI(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		if isProxyURI(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func isProxyURI(s string) bool {
	return strings.HasPrefix(s, "vless://") ||
		strings.HasPrefix(s, "vmess://") ||
		strings.HasPrefix(s, "trojan://") ||
		strings.HasPrefix(s, "ss://")
}

func parseProxyURI(raw string) (map[string]any, error) {
	switch {
	case strings.HasPrefix(raw, "vless://"):
		return parseVLESSURI(raw)
	case strings.HasPrefix(raw, "vmess://"):
		return parseVMessURI(raw)
	case strings.HasPrefix(raw, "trojan://"):
		return parseTrojanURI(raw)
	case strings.HasPrefix(raw, "ss://"):
		return parseSSURI(raw)
	default:
		return nil, fmt.Errorf("unsupported proxy URI: %s", truncate(raw, 32))
	}
}

func parseVLESSURI(raw string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	port, err := uriPort(u, 443)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	proxy := map[string]any{
		"name":   uriName(u),
		"type":   "vless",
		"server": u.Hostname(),
		"port":   port,
		"uuid":   u.User.Username(),
		"cipher": "auto",
		"udp":    true,
	}
	switch q.Get("type") {
	case "ws":
		proxy["network"] = "ws"
		host := q.Get("host")
		if host == "" {
			host = u.Hostname()
		}
		proxy["ws-opts"] = map[string]any{
			"path":    defaultString(q.Get("path"), "/"),
			"headers": map[string]any{"Host": host},
		}
	case "grpc":
		proxy["network"] = "grpc"
		proxy["grpc-opts"] = map[string]any{"grpc-service-name": defaultString(q.Get("serviceName"), q.Get("path"))}
	case "tcp":
		proxy["network"] = "tcp"
	}
	switch q.Get("security") {
	case "tls":
		proxy["tls"] = true
		proxy["servername"] = defaultString(q.Get("sni"), u.Hostname())
	case "reality":
		proxy["tls"] = true
		proxy["servername"] = defaultString(q.Get("sni"), u.Hostname())
		proxy["reality-opts"] = map[string]any{"public-key": q.Get("pbk"), "short-id": q.Get("sid")}
		proxy["client-fingerprint"] = defaultString(q.Get("fp"), "chrome")
	}
	if flow := q.Get("flow"); flow != "" {
		proxy["flow"] = flow
	}
	return proxy, validateProxy(proxy)
}

func parseVMessURI(raw string) (map[string]any, error) {
	encoded := strings.TrimPrefix(raw, "vmess://")
	decoded, err := decodeBase64Payload(encoded)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(fmt.Sprint(cfg["port"]))
	if err != nil {
		return nil, err
	}
	alterID, _ := strconv.Atoi(defaultString(fmt.Sprint(cfg["aid"]), "0"))
	proxy := map[string]any{
		"name":    defaultString(fmt.Sprint(cfg["ps"]), "未命名节点"),
		"type":    "vmess",
		"server":  fmt.Sprint(cfg["add"]),
		"port":    port,
		"uuid":    fmt.Sprint(cfg["id"]),
		"cipher":  defaultString(fmt.Sprint(cfg["scy"]), "auto"),
		"alterId": alterID,
		"udp":     true,
	}
	if fmt.Sprint(cfg["tls"]) == "tls" {
		proxy["tls"] = true
		proxy["servername"] = defaultString(fmt.Sprint(cfg["host"]), fmt.Sprint(cfg["add"]))
	}
	if fmt.Sprint(cfg["net"]) == "ws" {
		proxy["network"] = "ws"
		proxy["ws-opts"] = map[string]any{
			"path":    defaultString(fmt.Sprint(cfg["path"]), "/"),
			"headers": map[string]any{"Host": defaultString(fmt.Sprint(cfg["host"]), fmt.Sprint(cfg["add"]))},
		}
	}
	return proxy, validateProxy(proxy)
}

func parseTrojanURI(raw string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	port, err := uriPort(u, 443)
	if err != nil {
		return nil, err
	}
	proxy := map[string]any{
		"name":             uriName(u),
		"type":             "trojan",
		"server":           u.Hostname(),
		"port":             port,
		"password":         u.User.Username(),
		"udp":              true,
		"skip-cert-verify": false,
	}
	if sni := u.Query().Get("sni"); sni != "" {
		proxy["sni"] = sni
	}
	return proxy, validateProxy(proxy)
}

func parseSSURI(raw string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	port, err := uriPort(u, 8388)
	if err != nil {
		return nil, err
	}
	user := u.User.Username()
	if password, ok := u.User.Password(); ok {
		user += ":" + password
	}
	if !strings.Contains(user, ":") {
		decoded, err := decodeBase64Payload(user)
		if err != nil {
			return nil, err
		}
		user = string(decoded)
	}
	parts := strings.SplitN(user, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid ss user info: %s", truncate(raw, 32))
	}
	proxy := map[string]any{
		"name":     uriName(u),
		"type":     "ss",
		"server":   u.Hostname(),
		"port":     port,
		"cipher":   parts[0],
		"password": parts[1],
		"udp":      true,
	}
	return proxy, validateProxy(proxy)
}

func decodeBase64Payload(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 payload")
}

func uriPort(u *url.URL, fallback int) (int, error) {
	if u.Hostname() == "" {
		return 0, fmt.Errorf("missing host: %s", truncate(u.String(), 32))
	}
	if u.Port() == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return 0, err
	}
	return port, nil
}

func uriName(u *url.URL) string {
	name := u.Fragment
	if decoded, err := url.QueryUnescape(name); err == nil {
		name = decoded
	}
	if name == "" {
		name = u.Hostname()
	}
	return name
}

func validateProxy(proxy map[string]any) error {
	for _, key := range []string{"name", "type", "server"} {
		if strings.TrimSpace(fmt.Sprint(proxy[key])) == "" {
			return fmt.Errorf("missing %s in proxy", key)
		}
	}
	if port, ok := proxy["port"].(int); !ok || port <= 0 {
		return fmt.Errorf("invalid port in proxy %s", proxy["name"])
	}
	return nil
}

func defaultString(v, fallback string) string {
	if v == "" || v == "<nil>" {
		return fallback
	}
	return v
}

func anyOrDefault(v any, fallback any) any {
	if v == nil {
		return fallback
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
		return fallback
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (c *Client) downloadSubscriptionWithRetry(urlValue string, attempts int) ([]byte, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	var lastData []byte
	noProxyClient := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	for i := 1; i <= attempts; i++ {
		req, reqErr := http.NewRequest(http.MethodGet, urlValue, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("User-Agent", "mihomo-manager/mm")
		resp, err := noProxyClient.Do(req)
		if err != nil {
			lastErr = err
		} else {
			func() {
				defer resp.Body.Close()
				if resp.StatusCode >= 400 {
					lastErr = fmt.Errorf("subscription download failed: %s", resp.Status)
					return
				}
				b, readErr := io.ReadAll(resp.Body)
				if readErr != nil {
					lastErr = readErr
					return
				}
				lastErr = nil
				lastData = b
			}()
			if lastErr == nil {
				return lastData, nil
			}
		}
		if i < attempts {
			time.Sleep(time.Duration(i) * 500 * time.Millisecond)
		}
	}
	return nil, fmt.Errorf("subscription download failed after %d attempts: %w", attempts, lastErr)
}

func (c *Client) BackupConfig() error {
	b, err := os.ReadFile(c.paths.ConfigFile)
	if err != nil {
		return err
	}
	return os.WriteFile(c.paths.BackupFile, b, 0644)
}

func (c *Client) RestoreConfig() error {
	b, err := os.ReadFile(c.paths.BackupFile)
	if err != nil {
		return err
	}
	return os.WriteFile(c.paths.ConfigFile, b, 0644)
}

func (c *Client) TailLogs(lines int) (string, error) {
	if lines <= 0 {
		lines = 50
	}
	b, err := os.ReadFile(c.paths.LogFile)
	if err != nil {
		return "", err
	}
	all := strings.Split(string(b), "\n")
	start := 0
	if len(all) > lines {
		start = len(all) - lines
	}
	return strings.Join(all[start:], "\n"), nil
}

func (c *Client) TailLogsStreamWithStop(initialLines int, stop <-chan struct{}) <-chan LogEvent {
	ch := make(chan LogEvent, 128)
	go func() {
		defer close(ch)
		if initialLines <= 0 {
			initialLines = 50
		}
		initial, offset, err := readTailWithOffset(c.paths.LogFile, initialLines)
		if err != nil {
			ch <- LogEvent{Err: err, Finished: true}
			return
		}
		for _, line := range initial {
			if line == "" {
				continue
			}
			if !sendLogEvent(stop, ch, LogEvent{Line: line}) {
				return
			}
		}
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		for {
			if isStopped(stop) {
				_ = sendLogEvent(stop, ch, LogEvent{Finished: true})
				return
			}
			<-ticker.C
			lines, newOffset, readErr := readAppendedLines(c.paths.LogFile, offset)
			if readErr != nil {
				_ = sendLogEvent(stop, ch, LogEvent{Err: readErr, Finished: true})
				return
			}
			offset = newOffset
			for _, line := range lines {
				if line == "" {
					continue
				}
				if !sendLogEvent(stop, ch, LogEvent{Line: line}) {
					return
				}
			}
		}
	}()
	return ch
}

func sendLogEvent(stop <-chan struct{}, output chan<- LogEvent, value LogEvent) bool {
	if stop == nil {
		output <- value
		return true
	}
	select {
	case output <- value:
		return true
	case <-stop:
		return false
	}
}

func readTailWithOffset(path string, lines int) ([]string, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	all := strings.Split(string(b), "\n")
	start := 0
	if len(all) > lines {
		start = len(all) - lines
	}
	out := all[start:]
	return out, int64(len(b)), nil
}

func readAppendedLines(path string, offset int64) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	// handle log truncation/rotation in-place
	if st.Size() < offset {
		offset = 0
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	sc := bufio.NewScanner(f)
	lines := make([]string, 0, 32)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if sc.Err() != nil {
		return nil, offset, sc.Err()
	}
	return lines, st.Size(), nil
}

func (c *Client) OpenConfigEditor() error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, c.paths.ConfigFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *Client) AddWhitelist(domain string) error {
	cfg, err := c.readConfigMap()
	if err != nil {
		return err
	}
	domains, err := c.whitelistDomains(cfg)
	if err != nil {
		return err
	}
	n := normalizeDomain(domain)
	if n == "" {
		return errors.New("empty whitelist domain")
	}
	domains = append(domains, n)
	domains = normalizeDomainList(domains)
	if err := c.saveWhitelistDomains(domains); err != nil {
		return err
	}
	injectWhitelistRules(cfg, domains)
	return c.writeConfigMap(cfg)
}

func (c *Client) RemoveWhitelist(domain string) error {
	cfg, err := c.readConfigMap()
	if err != nil {
		return err
	}
	n := normalizeDomain(domain)
	domains, err := c.whitelistDomains(cfg)
	if err != nil {
		return err
	}
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		if !strings.EqualFold(d, n) {
			out = append(out, d)
		}
	}
	out = normalizeDomainList(out)
	if err := c.saveWhitelistDomains(out); err != nil {
		return err
	}
	injectWhitelistRules(cfg, out)
	return c.writeConfigMap(cfg)
}

func (c *Client) ListWhitelist() ([]string, error) {
	cfg, err := c.readConfigMap()
	if err != nil {
		return nil, err
	}
	return c.whitelistDomains(cfg)
}

func (c *Client) ApplyRouteCN() error {
	cfg, err := c.readConfigMap()
	if err != nil {
		return err
	}
	whitelistDomains, err := c.whitelistDomains(cfg)
	if err != nil {
		return err
	}
	cfg["geodata-mode"] = true
	cfg["geo-auto-update"] = true
	cfg["geo-update-interval"] = 24
	cfg["geox-url"] = map[string]any{
		"geoip":   "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geoip.dat",
		"geosite": "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/geosite.dat",
		"mmdb":    "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@release/country.mmdb",
	}
	rules := anyToStrings(cfg["rules"])
	cleaned := make([]string, 0, len(rules)+3)
	for _, r := range rules {
		ru := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(r), " ", ""))
		if strings.HasPrefix(ru, "GEOSITE,CN,DIRECT") || strings.HasPrefix(ru, "GEOIP,CN,DIRECT") {
			continue
		}
		if strings.HasPrefix(ru, "MATCH,") {
			continue
		}
		cleaned = append(cleaned, r)
	}
	managed := []string{"GEOSITE,CN,DIRECT", "GEOIP,CN,DIRECT,no-resolve", "MATCH,GLOBAL"}
	cfg["rules"] = append(managed, cleaned...)
	injectWhitelistRules(cfg, whitelistDomains)
	if err := c.writeConfigMap(cfg); err != nil {
		return err
	}
	if err := c.TestConfig(); err != nil {
		_ = c.RestoreConfig()
		return fmt.Errorf("config test failed after applying route rules: %w", err)
	}
	return nil
}

func (c *Client) call(method, path string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, c.paths.APIAddr+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("api error: %s", string(b))
	}
	return b, nil
}

func (c *Client) readConfigMap() (map[string]any, error) {
	b, err := os.ReadFile(c.paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *Client) writeConfigMap(m map[string]any) error {
	if len(m) == 0 {
		return errors.New("empty config")
	}
	if err := c.BackupConfig(); err != nil {
		return err
	}
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.paths.ConfigFile, b, 0644); err != nil {
		_ = c.RestoreConfig()
		return err
	}
	return nil
}

type whitelistConfig struct {
	Domains []string `yaml:"domains"`
}

func (c *Client) whitelistPath() string {
	if c.paths.WhitelistFile != "" {
		return c.paths.WhitelistFile
	}
	if c.paths.ConfigDir != "" {
		return filepath.Join(c.paths.ConfigDir, "whitelist.yaml")
	}
	if c.paths.ConfigFile != "" {
		return filepath.Join(filepath.Dir(c.paths.ConfigFile), "whitelist.yaml")
	}
	return "whitelist.yaml"
}

func (c *Client) whitelistDomains(cfg map[string]any) ([]string, error) {
	domains, err := c.loadWhitelistDomains()
	if err == nil {
		return domains, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	domains = extractWhitelistDomains(anyToStrings(cfg["rules"]))
	if len(domains) == 0 {
		return []string{}, nil
	}
	if err := c.saveWhitelistDomains(domains); err != nil {
		return nil, err
	}
	return domains, nil
}

func (c *Client) loadWhitelistDomains() ([]string, error) {
	b, err := os.ReadFile(c.whitelistPath())
	if err != nil {
		return nil, err
	}
	var cfg whitelistConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return normalizeDomainList(cfg.Domains), nil
}

func (c *Client) saveWhitelistDomains(domains []string) error {
	out := whitelistConfig{Domains: normalizeDomainList(domains)}
	b, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.whitelistPath()), 0755); err != nil {
		return err
	}
	return os.WriteFile(c.whitelistPath(), b, 0644)
}

func extractWhitelistDomains(rules []string) []string {
	out := make([]string, 0)
	for _, r := range rules {
		parts := strings.Split(strings.TrimSpace(strings.Trim(r, "\"")), ",")
		if len(parts) < 3 {
			continue
		}
		tp := strings.ToUpper(strings.TrimSpace(parts[0]))
		if tp != "DOMAIN" && tp != "DOMAIN-SUFFIX" && tp != "DOMAIN-WILDCARD" {
			continue
		}
		target := strings.TrimSpace(parts[2])
		if !isDirectTarget(target) {
			continue
		}
		out = append(out, strings.TrimSpace(parts[1]))
	}
	return normalizeDomainList(out)
}

func injectWhitelistRules(cfg map[string]any, domains []string) {
	rules := anyToStrings(cfg["rules"])
	cleaned := make([]string, 0, len(rules))
	for _, r := range rules {
		if isWhitelistRule(r) {
			continue
		}
		cleaned = append(cleaned, r)
	}
	domains = normalizeDomainList(domains)
	out := make([]string, 0, len(domains)+len(cleaned))
	for _, d := range domains {
		out = append(out, "DOMAIN-SUFFIX,"+d+",DIRECT")
	}
	out = append(out, cleaned...)
	cfg["rules"] = out
}

func isWhitelistRule(rule string) bool {
	parts := strings.Split(strings.TrimSpace(strings.Trim(rule, "\"")), ",")
	if len(parts) < 3 {
		return false
	}
	tp := strings.ToUpper(strings.TrimSpace(parts[0]))
	return (tp == "DOMAIN" || tp == "DOMAIN-SUFFIX" || tp == "DOMAIN-WILDCARD") && isDirectTarget(strings.TrimSpace(parts[2]))
}

func isDirectTarget(target string) bool {
	return strings.EqualFold(target, "DIRECT") || strings.TrimSpace(target) == "🎯 直连"
}

func normalizeDomainList(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, d := range in {
		n := normalizeDomain(d)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func anyToStrings(v any) []string {
	if v == nil {
		return []string{}
	}
	arr, ok := v.([]any)
	if !ok {
		if sarr, ok2 := v.([]string); ok2 {
			return sarr
		}
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func normalizeDomain(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(v, "https://"), "http://"))
	if i := strings.Index(v, "/"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimPrefix(v, "*.")
	return strings.ToLower(strings.Trim(v, "."))
}

func wildcardDomainMatch(pattern, host string) bool {
	pattern = strings.ToLower(strings.Trim(strings.TrimSpace(pattern), "."))
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "."))
	var re strings.Builder
	re.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			re.WriteString(".*")
		case '?':
			re.WriteString(".")
		default:
			re.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	re.WriteString("$")
	ok, err := regexp.MatchString(re.String(), host)
	return err == nil && ok
}

func isCNLikeDomain(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if strings.HasSuffix(host, ".cn") {
		return true
	}
	// common mainland services heuristic
	suffixes := []string{
		"qq.com", "baidu.com", "bilibili.com", "jd.com", "taobao.com", "tmall.com",
		"alicdn.com", "youku.com", "iqiyi.com", "douyin.com", "weibo.com", "163.com",
		"sina.com", "mi.com", "huawei.com", "meituan.com", "pinduoduo.com",
	}
	for _, s := range suffixes {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}
