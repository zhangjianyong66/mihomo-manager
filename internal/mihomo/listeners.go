package mihomo

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type ProxyListener struct {
	Protocol string
	Host     string
	Port     int
}

func ReadProxyListeners(path string) ([]ProxyListener, error) {
	content, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	var config struct {
		BindAddress string `yaml:"bind-address"`
		MixedPort   int    `yaml:"mixed-port"`
		HTTPPort    int    `yaml:"port"`
		SocksPort   int    `yaml:"socks-port"`
	}
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("decode mihomo listeners: %w", err)
	}
	host := strings.Trim(strings.TrimSpace(config.BindAddress), "[]")
	if host == "" || host == "*" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	definitions := []struct {
		protocol string
		port     int
	}{{"mixed", config.MixedPort}, {"http", config.HTTPPort}, {"socks", config.SocksPort}}
	result := make([]ProxyListener, 0, len(definitions))
	for _, definition := range definitions {
		if definition.port == 0 {
			continue
		}
		if err := validatePort(definition.port); err != nil {
			return nil, fmt.Errorf("%s listener: %w", definition.protocol, err)
		}
		result = append(result, ProxyListener{Protocol: definition.protocol, Host: host, Port: definition.port})
	}
	return result, nil
}
