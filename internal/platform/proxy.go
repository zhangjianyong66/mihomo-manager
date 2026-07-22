package platform

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
)

type ProxyState string

const (
	ProxyStateMatched    ProxyState = "matched"
	ProxyStateMismatched ProxyState = "mismatched"
	ProxyStateDisabled   ProxyState = "disabled"
	ProxyStateUnknown    ProxyState = "unknown"
)

type ProxyEndpoint struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

type ProxyListener struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type ProxySource struct {
	Source           string         `json:"source"`
	ExpectedProtocol string         `json:"expectedProtocol"`
	State            ProxyState     `json:"state"`
	Endpoint         *ProxyEndpoint `json:"endpoint,omitempty"`
	Warning          string         `json:"warning,omitempty"`
}

type ProxyInspector interface {
	Inspect(context.Context) []ProxySource
}

type commandRunner func(context.Context, string, ...string) ([]byte, error)

type GNOMEProxyInspector struct{ run commandRunner }

func NewGNOMEProxyInspector() *GNOMEProxyInspector {
	return &GNOMEProxyInspector{run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	}}
}

func (i *GNOMEProxyInspector) Inspect(ctx context.Context) []ProxySource {
	sources := gnomeProxySources(ProxyStateUnknown)
	if i == nil || i.run == nil {
		return sources
	}
	mode, err := i.gsettings(ctx, "org.gnome.system.proxy", "mode")
	if err != nil {
		return withProxyWarning(sources, "无法读取 GNOME 系统代理，入口状态未知")
	}
	switch strings.ToLower(mode) {
	case "none":
		return gnomeProxySources(ProxyStateDisabled)
	case "manual":
	default:
		return withProxyWarning(sources, "GNOME 系统代理模式无法识别，入口状态未知")
	}
	for index, item := range []struct {
		schema string
		scheme string
	}{
		{"org.gnome.system.proxy.http", "http"},
		{"org.gnome.system.proxy.https", "http"},
		{"org.gnome.system.proxy.socks", "socks"},
	} {
		host, hostErr := i.gsettings(ctx, item.schema, "host")
		portText, portErr := i.gsettings(ctx, item.schema, "port")
		port, parseErr := strconv.Atoi(strings.TrimSpace(portText))
		if hostErr != nil || portErr != nil || parseErr != nil || port < 0 || port > 65535 {
			sources[index].Warning = "无法读取 GNOME 代理端点，入口状态未知"
			continue
		}
		host = normalizeProxyHost(host)
		if host == "" || port == 0 {
			sources[index].State = ProxyStateDisabled
			continue
		}
		sources[index].Endpoint = &ProxyEndpoint{Scheme: item.scheme, Host: host, Port: port}
		sources[index].State = ""
	}
	return sources
}

func (i *GNOMEProxyInspector) gsettings(ctx context.Context, schema, key string) (string, error) {
	value, err := i.run(ctx, "gsettings", "get", schema, key)
	if err != nil {
		return "", err
	}
	return parseGSettingsScalar(string(value)), nil
}

func InspectProxyEnvironment(lookup func(string) (string, bool)) []ProxySource {
	definitions := []struct {
		name, lower, protocol, scheme string
	}{
		{"HTTP_PROXY", "http_proxy", "http", "http"},
		{"HTTPS_PROXY", "https_proxy", "http", "http"},
		{"ALL_PROXY", "all_proxy", "socks", "socks"},
	}
	result := make([]ProxySource, 0, len(definitions))
	for _, definition := range definitions {
		source := ProxySource{Source: "env." + definition.name, ExpectedProtocol: definition.protocol, State: ProxyStateDisabled}
		value := ""
		if lookup != nil {
			value, _ = lookup(definition.name)
			if strings.TrimSpace(value) == "" {
				value, _ = lookup(definition.lower)
			}
		}
		if strings.TrimSpace(value) == "" {
			result = append(result, source)
			continue
		}
		endpoint, err := parseProxyEndpoint(value, definition.scheme)
		if err != nil {
			source.State = ProxyStateUnknown
			source.Warning = definition.name + " 无法解析，入口状态未知"
			result = append(result, source)
			continue
		}
		source.State = ""
		source.Endpoint = &endpoint
		result = append(result, source)
	}
	return result
}

func DiagnoseProxySources(listeners []ProxyListener, sources []ProxySource) []ProxySource {
	result := make([]ProxySource, len(sources))
	copy(result, sources)
	for index := range result {
		source := &result[index]
		if source.State == ProxyStateDisabled || source.State == ProxyStateUnknown {
			continue
		}
		if source.Endpoint == nil {
			source.State = ProxyStateUnknown
			source.Warning = "代理端点缺失，入口状态未知"
			continue
		}
		matched := false
		for _, listener := range listeners {
			if listenerMatches(listener, source.ExpectedProtocol, *source.Endpoint) {
				matched = true
				break
			}
		}
		if matched {
			source.State = ProxyStateMatched
			source.Warning = ""
		} else {
			source.State = ProxyStateMismatched
			source.Warning = fmt.Sprintf("%s 指向 %s，但未匹配 mihomo %s/mixed listener；普通应用流量不会进入 mihomo", source.Source, formatProxyEndpoint(*source.Endpoint), source.ExpectedProtocol)
		}
	}
	return result
}

func listenerMatches(listener ProxyListener, expected string, endpoint ProxyEndpoint) bool {
	protocol := strings.ToLower(strings.TrimSpace(listener.Protocol))
	if protocol != "mixed" && protocol != expected {
		return false
	}
	return listener.Port == endpoint.Port && equivalentLoopback(listener.Host, endpoint.Host)
}

func equivalentLoopback(left, right string) bool {
	return isLoopbackHost(left) && isLoopbackHost(right)
}

func isLoopbackHost(value string) bool {
	value = normalizeProxyHost(value)
	if strings.EqualFold(value, "localhost") {
		return true
	}
	ip := net.ParseIP(value)
	return ip != nil && ip.IsLoopback()
}

func parseProxyEndpoint(value, fallbackScheme string) (ProxyEndpoint, error) {
	trimmed := strings.TrimSpace(value)
	if !strings.Contains(trimmed, "://") {
		trimmed = fallbackScheme + "://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Hostname() == "" {
		return ProxyEndpoint{}, fmt.Errorf("invalid proxy endpoint")
	}
	portText := parsed.Port()
	if portText == "" {
		return ProxyEndpoint{}, fmt.Errorf("proxy port is required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return ProxyEndpoint{}, fmt.Errorf("invalid proxy port")
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "socks5" || scheme == "socks5h" {
		scheme = "socks"
	}
	return ProxyEndpoint{Scheme: scheme, Host: normalizeProxyHost(parsed.Hostname()), Port: port}, nil
}

func parseGSettingsScalar(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@s ")
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		value = value[1 : len(value)-1]
	}
	return strings.TrimSpace(value)
}

func normalizeProxyHost(value string) string {
	return strings.Trim(strings.TrimSpace(value), "[]")
}

func gnomeProxySources(state ProxyState) []ProxySource {
	return []ProxySource{
		{Source: "gnome.http", ExpectedProtocol: "http", State: state},
		{Source: "gnome.https", ExpectedProtocol: "http", State: state},
		{Source: "gnome.socks", ExpectedProtocol: "socks", State: state},
	}
}

func withProxyWarning(values []ProxySource, warning string) []ProxySource {
	for index := range values {
		values[index].Warning = warning
	}
	return values
}

func formatProxyEndpoint(value ProxyEndpoint) string {
	return value.Scheme + "://" + net.JoinHostPort(value.Host, strconv.Itoa(value.Port))
}
