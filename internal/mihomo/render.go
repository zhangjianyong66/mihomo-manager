package mihomo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
)

type managedConfig struct {
	MixedPort          int            `yaml:"mixed-port"`
	SocksPort          int            `yaml:"socks-port"`
	Mode               string         `yaml:"mode"`
	ExternalController string         `yaml:"external-controller"`
	Proxies            []managedProxy `yaml:"proxies"`
	ProxyGroups        []managedGroup `yaml:"proxy-groups"`
	Rules              []string       `yaml:"rules"`
}

type managedProxy struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Server   string `yaml:"server"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	UDP      bool   `yaml:"udp,omitempty"`
}

type managedGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

type proxySpec struct {
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	UDP      bool   `json:"udp"`
}

func renderManaged(snapshot core.ProfileSnapshot) ([]byte, string, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, "", err
	}
	baseURL, controller, err := parseControllerEndpoint(snapshot.ControllerEndpoint)
	if err != nil {
		return nil, "", err
	}
	if err := validatePort(snapshot.MixedPort); err != nil {
		return nil, "", fmt.Errorf("mixed port: %w", err)
	}
	if err := validatePort(snapshot.SocksPort); err != nil {
		return nil, "", fmt.Errorf("socks port: %w", err)
	}
	if snapshot.MixedPort == snapshot.SocksPort {
		return nil, "", fmt.Errorf("proxy ports conflict: %w", core.ErrInvalidConfig)
	}
	_, controllerPortText, _ := net.SplitHostPort(controller)
	controllerPort, _ := strconv.Atoi(controllerPortText)
	if controllerPort == snapshot.MixedPort || controllerPort == snapshot.SocksPort {
		return nil, "", fmt.Errorf("controller port conflicts with proxy port: %w", core.ErrInvalidConfig)
	}

	proxies := append([]core.Proxy(nil), snapshot.Proxies...)
	sort.SliceStable(proxies, func(i, j int) bool { return proxies[i].ID.String() < proxies[j].ID.String() })
	config := managedConfig{
		MixedPort: snapshot.MixedPort, SocksPort: snapshot.SocksPort, Mode: "rule",
		ExternalController: controller,
	}
	for _, proxy := range proxies {
		var spec proxySpec
		if err := json.Unmarshal(proxy.Spec, &spec); err != nil {
			return nil, "", fmt.Errorf("decode proxy %s: %w", proxy.ID, core.ErrInvalidConfig)
		}
		if spec.Server == "" || spec.Port < 1 || spec.Port > 65535 {
			return nil, "", fmt.Errorf("proxy %s endpoint: %w", proxy.ID, core.ErrInvalidConfig)
		}
		protocol := strings.ToLower(strings.TrimSpace(proxy.Protocol))
		if protocol != "socks5" && protocol != "http" {
			return nil, "", fmt.Errorf("proxy %s protocol is not supported by the minimal renderer", proxy.ID)
		}
		name := deterministicProxyName(proxy.Name, proxy.ID.String())
		config.Proxies = append(config.Proxies, managedProxy{
			Name: name, Type: protocol, Server: spec.Server, Port: spec.Port,
			Username: spec.Username, Password: spec.Password, UDP: spec.UDP,
		})
	}
	names := make([]string, 0, len(config.Proxies))
	for _, proxy := range config.Proxies {
		names = append(names, proxy.Name)
	}
	config.ProxyGroups = []managedGroup{{Name: "GLOBAL", Type: "select", Proxies: names}}
	config.Rules = []string{"MATCH,GLOBAL"}
	content, err := yaml.Marshal(config)
	if err != nil {
		return nil, "", fmt.Errorf("encode mihomo config: %w", err)
	}
	if err := validateStatic(content); err != nil {
		return nil, "", err
	}
	return content, baseURL, nil
}

func deterministicProxyName(name, id string) string {
	name = strings.TrimSpace(name)
	sum := sha256.Sum256([]byte(id))
	return name + "-" + hex.EncodeToString(sum[:4])
}

type staticConfig struct {
	MixedPort          int    `yaml:"mixed-port"`
	SocksPort          int    `yaml:"socks-port"`
	Port               int    `yaml:"port"`
	RedirPort          int    `yaml:"redir-port"`
	TProxyPort         int    `yaml:"tproxy-port"`
	ExternalController string `yaml:"external-controller"`
	Proxies            []struct {
		Name string `yaml:"name"`
	} `yaml:"proxies"`
	ProxyGroups []struct {
		Name    string   `yaml:"name"`
		Proxies []string `yaml:"proxies"`
		Use     []string `yaml:"use"`
	} `yaml:"proxy-groups"`
	ProxyProviders map[string]yaml.Node `yaml:"proxy-providers"`
	Rules          []string             `yaml:"rules"`
}

func validateStatic(content []byte) error {
	_, err := inspectStatic(content)
	return err
}

func inspectStatic(content []byte) (string, error) {
	if len(content) == 0 {
		return "", fmt.Errorf("mihomo config is empty: %w", core.ErrInvalidConfig)
	}
	var config staticConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return "", fmt.Errorf("decode mihomo config: %w", core.ErrInvalidConfig)
	}
	baseURL, controller, err := parseControllerEndpoint(config.ExternalController)
	if err != nil {
		return "", err
	}
	ports := map[int]string{}
	for name, port := range map[string]int{
		"mixed-port": config.MixedPort, "socks-port": config.SocksPort, "port": config.Port,
		"redir-port": config.RedirPort, "tproxy-port": config.TProxyPort,
	} {
		if port == 0 {
			continue
		}
		if err := validatePort(port); err != nil {
			return "", fmt.Errorf("%s: %w", name, core.ErrInvalidConfig)
		}
		if prior, exists := ports[port]; exists {
			return "", fmt.Errorf("%s conflicts with %s: %w", name, prior, core.ErrInvalidConfig)
		}
		ports[port] = name
	}
	_, controllerPortText, _ := net.SplitHostPort(controller)
	controllerPort, _ := strconv.Atoi(controllerPortText)
	if prior, exists := ports[controllerPort]; exists {
		return "", fmt.Errorf("external-controller conflicts with %s: %w", prior, core.ErrInvalidConfig)
	}

	known := map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true}
	for _, proxy := range config.Proxies {
		if strings.TrimSpace(proxy.Name) != "" {
			known[proxy.Name] = true
		}
	}
	for _, group := range config.ProxyGroups {
		if strings.TrimSpace(group.Name) != "" {
			known[group.Name] = true
		}
	}
	usable := len(config.Proxies) > 0 || len(config.ProxyProviders) > 0
	for _, group := range config.ProxyGroups {
		if len(group.Use) > 0 {
			usable = true
		}
		for _, target := range group.Proxies {
			if !known[target] {
				return "", fmt.Errorf("proxy group references an unknown target: %w", core.ErrInvalidConfig)
			}
		}
	}
	if !usable {
		return "", fmt.Errorf("mihomo config has no usable proxy source: %w", core.ErrInvalidConfig)
	}
	if len(config.Rules) == 0 {
		return "", fmt.Errorf("mihomo config has no rules: %w", core.ErrInvalidConfig)
	}
	last := strings.Split(config.Rules[len(config.Rules)-1], ",")
	if len(last) < 2 || !known[strings.TrimSpace(last[len(last)-1])] {
		return "", fmt.Errorf("final rule references an unknown target: %w", core.ErrInvalidConfig)
	}
	return baseURL, nil
}

func parseControllerEndpoint(value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", fmt.Errorf("external-controller is required: %w", core.ErrInvalidConfig)
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("external-controller endpoint is invalid: %w", core.ErrInvalidConfig)
	}
	host := net.ParseIP(parsed.Hostname())
	if host == nil || !host.IsLoopback() {
		return "", "", fmt.Errorf("external-controller must use a loopback IP: %w", core.ErrInvalidConfig)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || validatePort(port) != nil {
		return "", "", fmt.Errorf("external-controller port is invalid: %w", core.ErrInvalidConfig)
	}
	hostPort := net.JoinHostPort(host.String(), strconv.Itoa(port))
	return "http://" + hostPort, hostPort, nil
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}
