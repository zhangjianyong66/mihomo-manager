package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	proxyBlockStart = "# >>> mihomo-manager proxy >>>"
	proxyBlockEnd   = "# <<< mihomo-manager proxy <<<"
)

var ErrProxyAuthUnsupported = errors.New("GNOME 代理认证暂不支持")
var ErrProxySnapshotNotFound = errors.New("GNOME 代理没有可恢复快照")
var ErrProxyConflict = errors.New("代理配置已被外部修改")
var ErrProxyRestoreFailed = errors.New("代理配置回滚失败")
var ErrProxyEndpointInvalid = errors.New("代理端点无效")

type ProxyTarget string

const (
	ProxyTargetHTTP  ProxyTarget = "http"
	ProxyTargetHTTPS ProxyTarget = "https"
	ProxyTargetSocks ProxyTarget = "socks"
)

func (t ProxyTarget) Valid() bool {
	return t == ProxyTargetHTTP || t == ProxyTargetHTTPS || t == ProxyTargetSocks
}

type ProxyConfigEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func ValidateProxyEndpoint(host string, port int) (ProxyConfigEndpoint, error) {
	host = normalizeProxyHost(host)
	if host == "" || net.ParseIP(host) == nil {
		return ProxyConfigEndpoint{}, fmt.Errorf("%w: 地址必须是 IPv4 或 IPv6 字面量", ErrProxyEndpointInvalid)
	}
	if port < 1 || port > 65535 {
		return ProxyConfigEndpoint{}, fmt.Errorf("%w: 端口必须在 1 到 65535 之间", ErrProxyEndpointInvalid)
	}
	return ProxyConfigEndpoint{Host: host, Port: port}, nil
}

func ProxyURL(scheme string, endpoint ProxyConfigEndpoint) string {
	return scheme + "://" + net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
}

type GNOMEProxySnapshot struct {
	Values map[string]string `json:"values"`
}

func (s GNOMEProxySnapshot) clone() GNOMEProxySnapshot {
	values := make(map[string]string, len(s.Values))
	for key, value := range s.Values {
		values[key] = value
	}
	return GNOMEProxySnapshot{Values: values}
}

type GNOMEProxyConfigurator struct{ run commandRunner }

type SystemProxyConfigurator interface {
	Read(context.Context) (GNOMEProxySnapshot, error)
	Apply(context.Context, GNOMEProxySnapshot, map[ProxyTarget]ProxyConfigEndpoint) (GNOMEProxySnapshot, error)
	Restore(context.Context, GNOMEProxySnapshot, GNOMEProxySnapshot) error
}

func NewGNOMEProxyConfigurator() *GNOMEProxyConfigurator {
	return &GNOMEProxyConfigurator{run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}}
}

func (c *GNOMEProxyConfigurator) get(ctx context.Context, schema, key string) (string, error) {
	if c == nil || c.run == nil {
		return "", errors.New("GNOME 代理配置器未配置")
	}
	value, err := c.run(ctx, "gsettings", "get", schema, key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(value)), nil
}

func (c *GNOMEProxyConfigurator) set(ctx context.Context, schema, key, value string) error {
	if c == nil || c.run == nil {
		return errors.New("GNOME 代理配置器未配置")
	}
	_, err := c.run(ctx, "gsettings", "set", schema, key, value)
	return err
}

var gnomeProxyKeys = []struct{ schema, key, name string }{
	{"org.gnome.system.proxy", "mode", "mode"},
	{"org.gnome.system.proxy", "use-same-proxy", "use-same-proxy"},
	{"org.gnome.system.proxy", "ignore-hosts", "ignore-hosts"},
	{"org.gnome.system.proxy.http", "host", "http.host"},
	{"org.gnome.system.proxy.http", "port", "http.port"},
	{"org.gnome.system.proxy.http", "use-authentication", "http.use-authentication"},
	{"org.gnome.system.proxy.https", "host", "https.host"},
	{"org.gnome.system.proxy.https", "port", "https.port"},
	{"org.gnome.system.proxy.socks", "host", "socks.host"},
	{"org.gnome.system.proxy.socks", "port", "socks.port"},
}

func (c *GNOMEProxyConfigurator) Read(ctx context.Context) (GNOMEProxySnapshot, error) {
	result := GNOMEProxySnapshot{Values: make(map[string]string, len(gnomeProxyKeys))}
	for _, item := range gnomeProxyKeys {
		value, err := c.get(ctx, item.schema, item.key)
		if err != nil {
			return GNOMEProxySnapshot{}, fmt.Errorf("读取 GNOME %s: %w", item.name, err)
		}
		result.Values[item.name] = value
	}
	return result, nil
}

func (c *GNOMEProxyConfigurator) Apply(ctx context.Context, current GNOMEProxySnapshot, endpoints map[ProxyTarget]ProxyConfigEndpoint) (GNOMEProxySnapshot, error) {
	if strings.EqualFold(parseGSettingsScalar(current.Values["http.use-authentication"]), "true") {
		return GNOMEProxySnapshot{}, ErrProxyAuthUnsupported
	}
	previous := current.clone()
	expected := current.clone()
	expected.Values["mode"] = "'manual'"
	expected.Values["use-same-proxy"] = "false"
	ignore := parseGSettingsStringList(current.Values["ignore-hosts"])
	ignore = mergeProxyIgnoreHosts(ignore)
	expected.Values["ignore-hosts"] = formatGSettingsStringList(ignore)
	for target, endpoint := range endpoints {
		if !target.Valid() {
			return GNOMEProxySnapshot{}, fmt.Errorf("不支持的 GNOME 代理类型 %q", target)
		}
		if _, err := ValidateProxyEndpoint(endpoint.Host, endpoint.Port); err != nil {
			return GNOMEProxySnapshot{}, err
		}
		expected.Values[string(target)+".host"] = quoteGVariantString(endpoint.Host)
		expected.Values[string(target)+".port"] = strconv.Itoa(endpoint.Port)
	}
	changed := []struct{ schema, key, value string }{
		{"org.gnome.system.proxy", "mode", expected.Values["mode"]},
		{"org.gnome.system.proxy", "use-same-proxy", expected.Values["use-same-proxy"]},
		{"org.gnome.system.proxy", "ignore-hosts", expected.Values["ignore-hosts"]},
	}
	for _, target := range []ProxyTarget{ProxyTargetHTTP, ProxyTargetHTTPS, ProxyTargetSocks} {
		if _, ok := endpoints[target]; ok {
			changed = append(changed,
				struct{ schema, key, value string }{"org.gnome.system.proxy." + string(target), "host", expected.Values[string(target)+".host"]},
				struct{ schema, key, value string }{"org.gnome.system.proxy." + string(target), "port", expected.Values[string(target)+".port"]},
			)
		}
	}
	for index, item := range changed {
		if err := c.set(ctx, item.schema, item.key, item.value); err != nil {
			if restoreErr := c.restoreValues(ctx, changed[:index], previous); restoreErr != nil {
				return GNOMEProxySnapshot{}, fmt.Errorf("%w: %v; rollback: %v", ErrProxyRestoreFailed, err, restoreErr)
			}
			return GNOMEProxySnapshot{}, err
		}
	}
	verified, err := c.Read(ctx)
	if err != nil {
		if restoreErr := c.restoreValues(ctx, changed, previous); restoreErr != nil {
			return GNOMEProxySnapshot{}, fmt.Errorf("%w: %v; rollback: %v", ErrProxyRestoreFailed, err, restoreErr)
		}
		return GNOMEProxySnapshot{}, err
	}
	for _, item := range changed {
		name := gnomeKeyName(item.schema, item.key)
		if strings.TrimSpace(verified.Values[name]) != strings.TrimSpace(expected.Values[name]) {
			if restoreErr := c.restoreValues(ctx, changed, previous); restoreErr != nil {
				return GNOMEProxySnapshot{}, fmt.Errorf("%w: GNOME %s; rollback: %v", ErrProxyRestoreFailed, name, restoreErr)
			}
			return GNOMEProxySnapshot{}, fmt.Errorf("%w: GNOME %s 核对失败", ErrProxyConflict, name)
		}
	}
	return expected, nil
}

func (c *GNOMEProxyConfigurator) Restore(ctx context.Context, expected, original GNOMEProxySnapshot) error {
	if len(original.Values) == 0 || len(expected.Values) == 0 {
		return ErrProxySnapshotNotFound
	}
	current, err := c.Read(ctx)
	if err != nil {
		return err
	}
	if strings.EqualFold(parseGSettingsScalar(current.Values["http.use-authentication"]), "true") {
		return ErrProxyAuthUnsupported
	}
	for key, value := range expected.Values {
		if strings.TrimSpace(current.Values[key]) != strings.TrimSpace(value) {
			return fmt.Errorf("%w: GNOME %s", ErrProxyConflict, key)
		}
	}
	changed := make([]struct{ schema, key, value string }, 0, len(gnomeProxyKeys))
	for _, item := range gnomeProxyKeys {
		if value, ok := original.Values[item.name]; ok {
			changed = append(changed, struct{ schema, key, value string }{item.schema, item.key, value})
		}
	}
	for index, item := range changed {
		if err := c.set(ctx, item.schema, item.key, item.value); err != nil {
			if restoreErr := c.restoreValues(ctx, changed[:index], current); restoreErr != nil {
				return fmt.Errorf("%w: %v", ErrProxyRestoreFailed, restoreErr)
			}
			return err
		}
	}
	return nil
}

func (c *GNOMEProxyConfigurator) restoreValues(ctx context.Context, changed []struct{ schema, key, value string }, snapshot GNOMEProxySnapshot) error {
	var first error
	for index := len(changed) - 1; index >= 0; index-- {
		item := changed[index]
		name := gnomeKeyName(item.schema, item.key)
		if value, ok := snapshot.Values[name]; ok {
			if err := c.set(ctx, item.schema, item.key, value); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func gnomeKeyName(schema, key string) string {
	if schema == "org.gnome.system.proxy" {
		return key
	}
	return strings.TrimPrefix(schema, "org.gnome.system.proxy.") + "." + key
}

type BashProxyConfigurator struct{}

func NewBashProxyConfigurator() *BashProxyConfigurator { return &BashProxyConfigurator{} }

func (c *BashProxyConfigurator) Read(path string) (content []byte, block string, hash string, err error) {
	content, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, "", "", nil
	}
	if err != nil {
		return nil, "", "", err
	}
	block, count, err := extractProxyBlock(string(content))
	if err != nil {
		return nil, "", "", err
	}
	if count == 0 {
		return content, "", "", nil
	}
	sum := sha256.Sum256([]byte(block))
	return content, block, hex.EncodeToString(sum[:]), nil
}

func (c *BashProxyConfigurator) Set(path, expectedHash string, endpoints map[ProxyTarget]ProxyConfigEndpoint) (string, error) {
	content, block, hash, err := c.Read(path)
	if err != nil {
		return "", err
	}
	if hash != "" && hash != expectedHash {
		return "", ErrProxyConflict
	}
	if hash == "" && expectedHash != "" {
		return "", ErrProxyConflict
	}
	for target, endpoint := range endpoints {
		if !target.Valid() {
			return "", fmt.Errorf("不支持的环境代理类型 %q", target)
		}
		if _, err := ValidateProxyEndpoint(endpoint.Host, endpoint.Port); err != nil {
			return "", err
		}
	}
	base := string(content)
	if block != "" {
		base = strings.Replace(base, block, "", 1)
	}
	noProxy := mergeProxyIgnoreHosts(nil)
	newBlock := renderBashProxyBlock(endpoints, noProxy)
	updated := strings.TrimRight(base, "\n") + "\n\n" + newBlock + "\n"
	if err := atomicProxyFile(path, []byte(updated)); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(newBlock))
	return hex.EncodeToString(sum[:]), nil
}

func (c *BashProxyConfigurator) Disable(path, expectedHash string) error {
	content, block, hash, err := c.Read(path)
	if err != nil {
		return err
	}
	if block == "" {
		if expectedHash != "" {
			return ErrProxyConflict
		}
		return nil
	}
	if hash != expectedHash {
		return ErrProxyConflict
	}
	updated := strings.Replace(string(content), block, "", 1)
	updated = strings.TrimRight(updated, "\n") + "\n"
	return atomicProxyFile(path, []byte(updated))
}

func (c *BashProxyConfigurator) Restore(path string, content []byte) error {
	return atomicProxyFile(path, content)
}

func GNOMEProxySourcesFromSnapshot(snapshot GNOMEProxySnapshot) []ProxySource {
	mode := strings.ToLower(parseGSettingsScalar(snapshot.Values["mode"]))
	if mode == "none" {
		return gnomeProxySources(ProxyStateDisabled)
	}
	if mode != "manual" {
		return withProxyWarning(gnomeProxySources(ProxyStateUnknown), "GNOME 系统代理模式无法识别，入口状态未知")
	}
	result := gnomeProxySources("")
	for index, target := range []ProxyTarget{ProxyTargetHTTP, ProxyTargetHTTPS, ProxyTargetSocks} {
		host := normalizeProxyHost(parseGSettingsScalar(snapshot.Values[string(target)+".host"]))
		port, err := strconv.Atoi(parseGSettingsScalar(snapshot.Values[string(target)+".port"]))
		if err != nil || port < 0 || port > 65535 {
			result[index].State = ProxyStateUnknown
			result[index].Warning = "GNOME 代理端点无法解析"
			continue
		}
		if host == "" || port == 0 {
			result[index].State = ProxyStateDisabled
			continue
		}
		if net.ParseIP(host) == nil {
			result[index].State = ProxyStateUnknown
			result[index].Warning = "GNOME 代理地址不是 IPv4/IPv6 字面量"
			continue
		}
		scheme := "http"
		if target == ProxyTargetSocks {
			scheme = "socks"
		}
		result[index].Endpoint = &ProxyEndpoint{Scheme: scheme, Host: host, Port: port}
	}
	return result
}

func BashProxySources(block string) []ProxySource {
	definitions := []struct {
		name, protocol, scheme string
	}{
		{"HTTP_PROXY", "http", "http"},
		{"HTTPS_PROXY", "http", "http"},
		{"ALL_PROXY", "socks", "socks"},
	}
	values := make(map[string]string)
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		if ok {
			values[strings.TrimSpace(name)] = strings.Trim(strings.TrimSpace(value), "'\"")
		}
	}
	result := make([]ProxySource, 0, len(definitions))
	for _, definition := range definitions {
		source := ProxySource{Source: "bash." + definition.name, ExpectedProtocol: definition.protocol, State: ProxyStateDisabled}
		value := values[definition.name]
		if value == "" {
			result = append(result, source)
			continue
		}
		endpoint, err := parseProxyEndpoint(value, definition.scheme)
		if err != nil {
			source.State = ProxyStateUnknown
			source.Warning = definition.name + " 无法解析"
		} else if net.ParseIP(endpoint.Host) == nil {
			source.State = ProxyStateUnknown
			source.Warning = definition.name + " 地址不是 IPv4/IPv6 字面量"
		} else {
			source.State = ""
			source.Endpoint = &endpoint
		}
		result = append(result, source)
	}
	return result
}

func extractProxyBlock(content string) (string, int, error) {
	start := strings.Index(content, proxyBlockStart)
	end := strings.Index(content, proxyBlockEnd)
	count := strings.Count(content, proxyBlockStart)
	count += strings.Count(content, proxyBlockEnd)
	if count == 0 {
		return "", 0, nil
	}
	if strings.Count(content, proxyBlockStart) != 1 || strings.Count(content, proxyBlockEnd) != 1 || start < 0 || end < start {
		return "", 0, ErrProxyConflict
	}
	endLine := strings.Index(content[end:], "\n")
	if endLine < 0 {
		endLine = len(content) - end
	}
	return content[start : end+endLine], 1, nil
}

func renderBashProxyBlock(endpoints map[ProxyTarget]ProxyConfigEndpoint, noProxy []string) string {
	get := func(target ProxyTarget, scheme string) string {
		if endpoint, ok := endpoints[target]; ok {
			return ProxyURL(scheme, endpoint)
		}
		return ""
	}
	values := map[string]string{
		"HTTP_PROXY": get(ProxyTargetHTTP, "http"), "HTTPS_PROXY": get(ProxyTargetHTTPS, "http"), "ALL_PROXY": get(ProxyTargetSocks, "socks5"),
	}
	var b strings.Builder
	b.WriteString(proxyBlockStart + "\n")
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"} {
		value := values[name]
		b.WriteString("export " + name + "=" + shellQuote(value) + "\n")
		b.WriteString("export " + strings.ToLower(name) + "=" + shellQuote(value) + "\n")
	}
	b.WriteString("_mm_proxy_no_proxy=${NO_PROXY:-${no_proxy:-}}\n")
	b.WriteString("for _mm_proxy_item in")
	for _, item := range noProxy {
		b.WriteString(" " + shellQuote(item))
	}
	b.WriteString("; do\n")
	b.WriteString("  case ,${_mm_proxy_no_proxy}, in\n")
	b.WriteString("    *,${_mm_proxy_item},*) ;;\n")
	b.WriteString("    *) _mm_proxy_no_proxy=${_mm_proxy_no_proxy:+${_mm_proxy_no_proxy},}${_mm_proxy_item} ;;\n")
	b.WriteString("  esac\n")
	b.WriteString("done\n")
	b.WriteString("export NO_PROXY=${_mm_proxy_no_proxy}\n")
	b.WriteString("export no_proxy=${_mm_proxy_no_proxy}\n")
	b.WriteString("unset _mm_proxy_no_proxy _mm_proxy_item\n")
	b.WriteString(proxyBlockEnd)
	return b.String()
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func mergeProxyIgnoreHosts(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values)+3)
	for _, value := range append(values, "localhost", "127.0.0.1", "::1") {
		value = strings.TrimSpace(value)
		if value == "" || seen[strings.ToLower(value)] {
			continue
		}
		seen[strings.ToLower(value)] = true
		result = append(result, value)
	}
	return result
}

func atomicProxyFile(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(dir); err != nil {
		return err
	} else if info.Mode().Perm()&0o002 != 0 || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: 代理配置目录权限不安全", os.ErrPermission)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: .bashrc 不允许为符号链接", os.ErrPermission)
		}
		if info.Mode().Perm()&0o002 != 0 {
			return fmt.Errorf("%w: .bashrc 权限不安全", os.ErrPermission)
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.CreateTemp(dir, ".mihomo-manager-proxy-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func parseGSettingsStringList(value string) []string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	var result []string
	var current strings.Builder
	quoted := false
	for _, r := range value {
		switch {
		case r == '\'' || r == '"':
			quoted = !quoted
		case r == ',' && !quoted:
			if item := strings.TrimSpace(current.String()); item != "" {
				result = append(result, strings.Trim(item, "'\""))
			}
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if item := strings.TrimSpace(current.String()); item != "" {
		result = append(result, strings.Trim(item, "'\""))
	}
	return result
}

func formatGSettingsStringList(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, quoteGVariantString(value))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func quoteGVariantString(value string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "'", "\\'") + "'"
}
