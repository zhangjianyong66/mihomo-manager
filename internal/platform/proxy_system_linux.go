//go:build linux

package platform

func NewSystemProxyInspector() ProxyInspector { return NewGNOMEProxyInspector() }

func NewSystemProxyConfigurator() SystemProxyConfigurator { return NewGNOMEProxyConfigurator() }
