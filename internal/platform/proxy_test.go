package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

func TestGNOMEProxyConfiguratorApplyRestoreAndConflict(t *testing.T) {
	values := defaultGNOMEProxyValues()
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		key := args[1] + "/" + args[2]
		switch args[0] {
		case "get":
			value, ok := values[key]
			if !ok {
				return nil, errors.New("missing key")
			}
			return []byte(value), nil
		case "set":
			values[key] = args[3]
			return nil, nil
		default:
			return nil, errors.New("unexpected operation")
		}
	}
	configurator := &GNOMEProxyConfigurator{run: runner}
	original, err := configurator.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := configurator.Apply(context.Background(), original, map[ProxyTarget]ProxyConfigEndpoint{
		ProxyTargetHTTP: {Host: "127.0.0.1", Port: 7890}, ProxyTargetHTTPS: {Host: "127.0.0.1", Port: 7890}, ProxyTargetSocks: {Host: "::1", Port: 7891},
	})
	if err != nil {
		t.Fatal(err)
	}
	if expected.Values["mode"] != "'manual'" || !strings.Contains(expected.Values["ignore-hosts"], "127.0.0.1") || !strings.Contains(expected.Values["ignore-hosts"], "::1") {
		t.Fatalf("expected=%+v", expected)
	}
	values["org.gnome.system.proxy.http/port"] = "10808"
	if err := configurator.Restore(context.Background(), expected, original); !errors.Is(err, ErrProxyConflict) {
		t.Fatalf("restore conflict err=%v", err)
	}
	values["org.gnome.system.proxy.http/port"] = "7890"
	if err := configurator.Restore(context.Background(), expected, original); err != nil {
		t.Fatal(err)
	}
	if values["org.gnome.system.proxy/mode"] != "'none'" || values["org.gnome.system.proxy.http/port"] != "0" {
		t.Fatalf("values=%+v", values)
	}
}

func TestGNOMEProxyConfiguratorRejectsAuthenticationBeforeWrite(t *testing.T) {
	values := defaultGNOMEProxyValues()
	values["org.gnome.system.proxy.http/use-authentication"] = "true"
	writes := 0
	configurator := &GNOMEProxyConfigurator{run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "set" {
			writes++
			return nil, nil
		}
		return []byte(values[args[1]+"/"+args[2]]), nil
	}}
	snapshot, err := configurator.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = configurator.Apply(context.Background(), snapshot, map[ProxyTarget]ProxyConfigEndpoint{ProxyTargetHTTP: {Host: "127.0.0.1", Port: 7890}})
	if !errors.Is(err, ErrProxyAuthUnsupported) || writes != 0 {
		t.Fatalf("err=%v writes=%d", err, writes)
	}
}

func TestBashProxyConfiguratorSetIsIdempotentAndDisablePreservesUserContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	original := "alias ll='ls -l'\nexport NO_PROXY='corp.example,localhost'\n"
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	configurator := NewBashProxyConfigurator()
	endpoints := map[ProxyTarget]ProxyConfigEndpoint{
		ProxyTargetHTTP: {Host: "127.0.0.1", Port: 7890}, ProxyTargetHTTPS: {Host: "127.0.0.1", Port: 7890}, ProxyTargetSocks: {Host: "::1", Port: 7891},
	}
	hash, err := configurator.Set(path, "", endpoints)
	if err != nil {
		t.Fatal(err)
	}
	content, _, currentHash, err := configurator.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{"export HTTP_PROXY='http://127.0.0.1:7890'", "export http_proxy='http://127.0.0.1:7890'", "export ALL_PROXY='socks5://[::1]:7891'", "_mm_proxy_item", "'localhost'", "'127.0.0.1'", "'::1'"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if hash != currentHash || strings.Count(text, proxyBlockStart) != 1 {
		t.Fatalf("hash=%q current=%q content=%s", hash, currentHash, text)
	}
	if output, err := exec.Command("bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("generated bashrc syntax: %v: %s", err, output)
	}
	output, err := exec.Command("bash", "--noprofile", "--norc", "-c", `unset no_proxy; source "$1"; printf '%s' "$NO_PROXY"`, "bash", path).CombinedOutput()
	if err != nil {
		t.Fatalf("source generated bashrc: %v: %s", err, output)
	}
	for _, want := range []string{"corp.example", "localhost", "127.0.0.1", "::1"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("NO_PROXY=%q missing %q", output, want)
		}
	}
	hash, err = configurator.Set(path, hash, endpoints)
	if err != nil {
		t.Fatal(err)
	}
	content, _, _, _ = configurator.Read(path)
	if strings.Count(string(content), proxyBlockStart) != 1 {
		t.Fatalf("duplicate block: %s", content)
	}
	if err := configurator.Disable(path, hash); err != nil {
		t.Fatal(err)
	}
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(final), proxyBlockStart) || !strings.Contains(string(final), "corp.example") || !strings.Contains(string(final), "alias ll") {
		t.Fatalf("final=%s", final)
	}
}

func TestBashProxyConfiguratorRejectsDamagedOrExternallyChangedBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(path, []byte(proxyBlockStart+"\nexport HTTP_PROXY=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configurator := NewBashProxyConfigurator()
	if _, _, _, err := configurator.Read(path); !errors.Is(err, ErrProxyConflict) {
		t.Fatalf("damaged block err=%v", err)
	}
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := configurator.Set(path, "", map[ProxyTarget]ProxyConfigEndpoint{ProxyTargetHTTP: {Host: "127.0.0.1", Port: 7890}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(content), "7890", "10808", 1)
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configurator.Disable(path, hash); !errors.Is(err, ErrProxyConflict) {
		t.Fatalf("external change err=%v", err)
	}
	if err := os.WriteFile(path, []byte("# block removed externally\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := configurator.Disable(path, hash); !errors.Is(err, ErrProxyConflict) {
		t.Fatalf("missing managed block err=%v", err)
	}
}

func TestBashProxyConfiguratorRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, ".bashrc")
	if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	_, err := NewBashProxyConfigurator().Set(path, "", map[ProxyTarget]ProxyConfigEndpoint{ProxyTargetHTTP: {Host: "127.0.0.1", Port: 7890}})
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("symlink err=%v", err)
	}
	content, readErr := os.ReadFile(target)
	if readErr != nil || string(content) != "keep\n" {
		t.Fatalf("target changed: %q err=%v", content, readErr)
	}
}

func defaultGNOMEProxyValues() map[string]string {
	return map[string]string{
		"org.gnome.system.proxy/mode": "'none'", "org.gnome.system.proxy/use-same-proxy": "true", "org.gnome.system.proxy/ignore-hosts": "['corp.example']",
		"org.gnome.system.proxy.http/host": "''", "org.gnome.system.proxy.http/port": "0", "org.gnome.system.proxy.http/use-authentication": "false",
		"org.gnome.system.proxy.https/host": "''", "org.gnome.system.proxy.https/port": "0",
		"org.gnome.system.proxy.socks/host": "''", "org.gnome.system.proxy.socks/port": "0",
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
