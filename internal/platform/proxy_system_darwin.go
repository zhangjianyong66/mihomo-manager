//go:build darwin

package platform

func NewSystemProxyInspector() ProxyInspector {
	return unsupportedSystemProxy{warning: "macOS 系统代理暂不支持"}
}

func NewSystemProxyConfigurator() SystemProxyConfigurator { return unsupportedSystemProxy{} }
