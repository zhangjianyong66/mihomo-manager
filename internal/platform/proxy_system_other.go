//go:build !linux && !darwin

package platform

func NewSystemProxyInspector() ProxyInspector {
	return unsupportedSystemProxy{warning: "当前平台不支持系统代理检测"}
}

func NewSystemProxyConfigurator() SystemProxyConfigurator { return unsupportedSystemProxy{} }
