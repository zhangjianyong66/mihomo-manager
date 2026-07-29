package platform

import "context"

type unsupportedSystemProxy struct{ warning string }

func (p unsupportedSystemProxy) Inspect(context.Context) []ProxySource {
	warning := p.warning
	if warning == "" {
		warning = "当前平台不支持系统代理检测"
	}
	return withProxyWarning(gnomeProxySources(ProxyStateUnknown), warning)
}

func (unsupportedSystemProxy) Read(context.Context) (GNOMEProxySnapshot, error) {
	return GNOMEProxySnapshot{}, ErrUnsupported
}

func (unsupportedSystemProxy) Apply(context.Context, GNOMEProxySnapshot, map[ProxyTarget]ProxyConfigEndpoint) (GNOMEProxySnapshot, error) {
	return GNOMEProxySnapshot{}, ErrUnsupported
}

func (unsupportedSystemProxy) Restore(context.Context, GNOMEProxySnapshot, GNOMEProxySnapshot) error {
	return ErrUnsupported
}
