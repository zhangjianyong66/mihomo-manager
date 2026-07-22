package mihomo

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
)

const (
	PortFieldMixedPort          = "mixed-port"
	PortFieldHTTPPort           = "port"
	PortFieldSocksPort          = "socks-port"
	PortFieldRedirPort          = "redir-port"
	PortFieldTProxyPort         = "tproxy-port"
	PortFieldExternalController = "external-controller"
)

type ListenerPort struct {
	Field    string
	Host     string
	Port     int
	Required bool
	Networks []string
}

type portDefinition struct {
	field    string
	required bool
	networks []string
	value    func(staticConfig) int
}

var portDefinitions = []portDefinition{
	{field: PortFieldMixedPort, networks: []string{"tcp", "udp"}, value: func(config staticConfig) int { return config.MixedPort }},
	{field: PortFieldHTTPPort, networks: []string{"tcp"}, value: func(config staticConfig) int { return config.Port }},
	{field: PortFieldSocksPort, networks: []string{"tcp", "udp"}, value: func(config staticConfig) int { return config.SocksPort }},
	{field: PortFieldRedirPort, networks: []string{"tcp"}, value: func(config staticConfig) int { return config.RedirPort }},
	{field: PortFieldTProxyPort, networks: []string{"tcp", "udp"}, value: func(config staticConfig) int { return config.TProxyPort }},
	{field: PortFieldExternalController, required: true, networks: []string{"tcp"}},
}

func ReadListenerPorts(path string) ([]ListenerPort, error) {
	content, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	return ParseListenerPorts(content)
}

func ReadControllerEndpoint(path string) (string, error) {
	content, err := readConfig(path)
	if err != nil {
		return "", err
	}
	var config staticConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return "", fmt.Errorf("decode mihomo controller endpoint: %w", core.ErrInvalidConfig)
	}
	endpoint, _, err := parseControllerEndpoint(config.ExternalController)
	return endpoint, err
}

func ParseListenerPorts(content []byte) ([]ListenerPort, error) {
	var config staticConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("decode mihomo listener ports: %w", core.ErrInvalidConfig)
	}
	_, controller, err := parseControllerEndpoint(config.ExternalController)
	if err != nil {
		return nil, err
	}
	controllerHost, controllerPortText, _ := net.SplitHostPort(controller)
	controllerPort, _ := strconv.Atoi(controllerPortText)
	proxyHost := effectiveProxyBindHost(config.BindAddress, config.AllowLAN)
	result := make([]ListenerPort, 0, len(portDefinitions))
	for _, definition := range portDefinitions {
		port := controllerPort
		host := controllerHost
		if definition.field != PortFieldExternalController {
			port = definition.value(config)
			host = proxyHost
		}
		result = append(result, ListenerPort{
			Field: definition.field, Host: host, Port: port, Required: definition.required,
			Networks: append([]string(nil), definition.networks...),
		})
	}
	return result, nil
}

func UpdateListenerPort(content []byte, field string, port int) ([]byte, error) {
	field = strings.TrimSpace(field)
	if err := ValidateListenerPort(field, port); err != nil {
		return nil, err
	}
	var config map[string]any
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("decode mihomo config: %w", core.ErrInvalidConfig)
	}
	if field == PortFieldExternalController {
		value, _ := config[field].(string)
		replaced, err := replaceControllerPort(value, port)
		if err != nil {
			return nil, err
		}
		config[field] = replaced
	} else {
		config[field] = port
	}
	updated, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode mihomo config: %w", err)
	}
	if err := validateStatic(updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func ValidateListenerPort(field string, port int) error {
	field = strings.TrimSpace(field)
	definition, ok := listenerPortDefinition(field)
	if !ok {
		return fmt.Errorf("unknown listener port field %q: %w", field, core.ErrInvalidConfig)
	}
	if port < 0 || port > 65535 || (definition.required && port == 0) {
		return fmt.Errorf("%s port is out of range: %w", field, core.ErrInvalidConfig)
	}
	return nil
}

func preflightListenerPorts(path string) error {
	ports, err := ReadListenerPorts(path)
	if err != nil {
		return err
	}
	conflicts := make([]core.PortConflict, 0)
	for _, listener := range ports {
		if listener.Port == 0 {
			continue
		}
		address := net.JoinHostPort(listener.Host, strconv.Itoa(listener.Port))
		for _, network := range listener.Networks {
			if err := probePort(network, address); err != nil {
				if errors.Is(err, syscall.EADDRINUSE) {
					conflicts = append(conflicts, core.PortConflict{
						Field: listener.Field, Network: network, Host: listener.Host, Port: listener.Port,
					})
					continue
				}
				return fmt.Errorf("probe %s %s: %w", network, address, err)
			}
		}
	}
	if len(conflicts) > 0 {
		return &core.PortConflictError{Conflicts: conflicts}
	}
	return nil
}

func probePort(network, address string) error {
	if network == "udp" {
		listener, err := net.ListenPacket(network, address)
		if err != nil {
			return err
		}
		return listener.Close()
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return err
	}
	return listener.Close()
}

func listenerPortDefinition(field string) (portDefinition, bool) {
	for _, definition := range portDefinitions {
		if definition.field == field {
			return definition, true
		}
	}
	return portDefinition{}, false
}

func effectiveProxyBindHost(value string, allowLAN bool) string {
	host := strings.Trim(strings.TrimSpace(value), "[]")
	if host == "" || host == "*" {
		if allowLAN {
			return "0.0.0.0"
		}
		return "127.0.0.1"
	}
	return host
}

func replaceControllerPort(value string, port int) (string, error) {
	if _, _, err := parseControllerEndpoint(value); err != nil {
		return "", err
	}
	hasScheme := strings.Contains(value, "://")
	parsed, err := url.Parse(value)
	if !hasScheme {
		parsed, err = url.Parse("http://" + value)
	}
	if err != nil {
		return "", fmt.Errorf("external-controller endpoint is invalid: %w", core.ErrInvalidConfig)
	}
	parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(port))
	if hasScheme {
		return parsed.String(), nil
	}
	return parsed.Host, nil
}
