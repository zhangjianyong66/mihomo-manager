package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestGNOMEProxyInspectorManualAndDisabled(t *testing.T) {
	values := map[string]string{
		"org.gnome.system.proxy/mode":      "'manual'",
		"org.gnome.system.proxy.http/host": "'127.0.0.1'", "org.gnome.system.proxy.http/port": "7890",
		"org.gnome.system.proxy.https/host": "'::1'", "org.gnome.system.proxy.https/port": "7890",
		"org.gnome.system.proxy.socks/host": "''", "org.gnome.system.proxy.socks/port": "0",
	}
	inspector := &GNOMEProxyInspector{run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		value, ok := values[args[1]+"/"+args[2]]
		if !ok {
			return nil, errors.New("missing")
		}
		return []byte(value), nil
	}}
	sources := inspector.Inspect(context.Background())
	if sources[0].Endpoint.Host != "127.0.0.1" || sources[1].Endpoint.Host != "::1" || sources[2].State != ProxyStateDisabled {
		t.Fatalf("sources=%+v", sources)
	}

	inspector.run = func(context.Context, string, ...string) ([]byte, error) { return []byte("'none'"), nil }
	for _, source := range inspector.Inspect(context.Background()) {
		if source.State != ProxyStateDisabled || source.Endpoint != nil {
			t.Fatalf("disabled source=%+v", source)
		}
	}
}

func TestProxyEnvironmentDropsCredentialsAndURLDetails(t *testing.T) {
	environment := map[string]string{
		"HTTP_PROXY":  "http://user:secret@127.0.0.1:7890/private?token=secret",
		"HTTPS_PROXY": "http://[::1]:7890/path",
		"ALL_PROXY":   "socks5://localhost:7891/query?secret=yes",
	}
	sources := InspectProxyEnvironment(func(key string) (string, bool) { value, ok := environment[key]; return value, ok })
	encoded := strings.ToLower(strings.TrimSpace(formatProxyEndpoint(*sources[0].Endpoint)))
	if encoded != "http://127.0.0.1:7890" || strings.Contains(encoded, "user") || strings.Contains(encoded, "secret") {
		t.Fatalf("endpoint=%q source=%+v", encoded, sources[0])
	}
	if sources[1].Endpoint.Host != "::1" || sources[2].Endpoint.Scheme != "socks" {
		t.Fatalf("sources=%+v", sources)
	}
}

func TestDiagnoseProxySourcesMatchesProtocolsAndLoopback(t *testing.T) {
	listeners := []ProxyListener{{Protocol: "mixed", Host: "127.0.0.1", Port: 7890}, {Protocol: "socks", Host: "::1", Port: 7891}}
	sources := []ProxySource{
		{Source: "http", ExpectedProtocol: "http", Endpoint: &ProxyEndpoint{Scheme: "http", Host: "::1", Port: 7890}},
		{Source: "socks", ExpectedProtocol: "socks", Endpoint: &ProxyEndpoint{Scheme: "socks", Host: "127.0.0.1", Port: 7891}},
		{Source: "wrong-protocol", ExpectedProtocol: "http", Endpoint: &ProxyEndpoint{Scheme: "http", Host: "127.0.0.1", Port: 7891}},
		{Source: "other-port", ExpectedProtocol: "http", Endpoint: &ProxyEndpoint{Scheme: "http", Host: "127.0.0.1", Port: 10808}},
	}
	got := DiagnoseProxySources(listeners, sources)
	want := []ProxyState{ProxyStateMatched, ProxyStateMatched, ProxyStateMismatched, ProxyStateMismatched}
	states := make([]ProxyState, len(got))
	for index := range got {
		states[index] = got[index].State
	}
	if !reflect.DeepEqual(states, want) || !strings.Contains(got[3].Warning, "普通应用流量不会进入 mihomo") {
		t.Fatalf("sources=%+v", got)
	}
}
