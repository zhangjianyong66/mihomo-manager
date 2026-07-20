package mihomo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func TestAdapter_RenderManagedGoldenAndDeterministic(t *testing.T) {
	snapshot := core.ProfileSnapshot{
		ProfileID: "profile-one", Revision: 1, Mode: domain.ProfileModeManaged,
		ControllerEndpoint: "http://127.0.0.1:19090", MixedPort: 17890, SocksPort: 17891,
		Proxies: []core.Proxy{{
			ID: "node-one", Name: "node one", Protocol: "socks5",
			Spec: json.RawMessage(`{"server":"127.0.0.1","port":1080,"username":"demo","password":"secret","udp":true}`),
		}},
	}
	adapter := NewAdapter(AdapterOptions{})
	first, err := adapter.Render(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Render(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "managed.golden.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Content) != string(want) {
		t.Fatalf("rendered config differs from golden:\n%s", first.Content)
	}
	if first.SHA256 != second.SHA256 || string(first.Content) != string(second.Content) {
		t.Fatal("managed rendering is not deterministic")
	}
}

func TestValidateStatic_RejectsUnsafeControllerConflictsAndUnknownTargets(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{"non-loopback", "mixed-port: 7890\nexternal-controller: 0.0.0.0:9090\nproxies: [{name: a}]\nrules: [MATCH,DIRECT]\n"},
		{"port-conflict", "mixed-port: 7890\nsocks-port: 7890\nexternal-controller: 127.0.0.1:9090\nproxies: [{name: a}]\nrules: [MATCH,DIRECT]\n"},
		{"unknown-group-target", "mixed-port: 7890\nexternal-controller: 127.0.0.1:9090\nproxies: [{name: a}]\nproxy-groups: [{name: GLOBAL, proxies: [missing]}]\nrules: [MATCH,GLOBAL]\n"},
		{"no-proxy-source", "mixed-port: 7890\nexternal-controller: 127.0.0.1:9090\nrules: [MATCH,DIRECT]\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateStatic([]byte(test.config)); !errors.Is(err, core.ErrInvalidConfig) {
				t.Fatalf("expected invalid config, got %v", err)
			}
		})
	}
}

func TestValidateStatic_AllowsProxyProviderSource(t *testing.T) {
	config := "mixed-port: 17890\nexternal-controller: 127.0.0.1:19090\nproxy-providers:\n  cloud:\n    type: http\n    url: https://example.invalid/nodes\n    path: ./cloud.yaml\nproxy-groups:\n  - name: GLOBAL\n    type: select\n    use: [cloud]\nrules:\n  - MATCH,GLOBAL\n"
	if err := validateStatic([]byte(config)); err != nil {
		t.Fatalf("proxy provider config should validate: %v", err)
	}
}

func TestAdapter_ValidateNativeArgumentsFailureAndTimeout(t *testing.T) {
	root := t.TempDir()
	argsFile := filepath.Join(root, "args")
	binary := writeScript(t, root, "validator", `#!/bin/sh
printf '%s\n' "$@" > "$MM_TEST_ARGS"
case "$MM_TEST_MODE" in
  fail) exit 7 ;;
  slow) sleep 5 ;;
esac
`)
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(validStaticConfig()), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := core.RuntimeSpec{ConfigDir: root, ConfigPath: configPath, ControllerEndpoint: "http://127.0.0.1:19090"}
	t.Setenv("MM_TEST_ARGS", argsFile)
	adapter := NewAdapter(AdapterOptions{Binary: binary, ValidationTimeout: 80 * time.Millisecond})
	if err := adapter.Validate(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := fmt.Sprintf("-t\n-d\n%s\n-f\n%s\n", root, configPath)
	if string(args) != wantArgs {
		t.Fatalf("unexpected validation args: %q", args)
	}
	t.Setenv("MM_TEST_MODE", "fail")
	if err := adapter.Validate(context.Background(), spec); !errors.Is(err, core.ErrValidationFailed) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unexpected validation failure: %v", err)
	}
	t.Setenv("MM_TEST_MODE", "slow")
	if err := adapter.Validate(context.Background(), spec); !errors.Is(err, core.ErrValidationTimeout) {
		t.Fatalf("expected validation timeout, got %v", err)
	}
}

func TestAdapter_ValidateRejectsControllerMismatch(t *testing.T) {
	root := t.TempDir()
	binary := writeScript(t, root, "validator", "#!/bin/sh\nexit 0\n")
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte(validStaticConfig()), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(AdapterOptions{Binary: binary})
	err := adapter.Validate(context.Background(), core.RuntimeSpec{
		ConfigDir: root, ConfigPath: configPath, ControllerEndpoint: "http://127.0.0.1:19091",
	})
	if !errors.Is(err, core.ErrInvalidConfig) {
		t.Fatalf("expected controller mismatch, got %v", err)
	}
}

func TestRuntimeClient_ReadyAndResponseBoundaries(t *testing.T) {
	server := loopbackServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"version":"1.19.28"}`))
		case "/configs":
			_, _ = w.Write([]byte(`{"mode":"rule"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := newRuntimeClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://0.0.0.0:9090", "http://localhost:9090", "https://127.0.0.1:9090"} {
		if _, err := newRuntimeClient(endpoint, http.DefaultClient); err == nil {
			t.Fatalf("expected endpoint rejection: %s", endpoint)
		}
	}
}

func TestRuntimeClient_RejectsStatusAndOversizedResponse(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
	}{
		{"status", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", http.StatusBadGateway) })},
		{"oversized", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"version":"` + strings.Repeat("x", maxRuntimeResponseBytes) + `"}`))
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := loopbackServer(t, test.handler)
			defer server.Close()
			client, err := newRuntimeClient(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := client.probe(context.Background()); err == nil {
				t.Fatal("expected runtime response rejection")
			}
		})
	}
}

func TestManagedProcess_SetsidStopsOnlyOwnedProcessAndEscalates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process contract")
	}
	root := t.TempDir()
	signalFile := filepath.Join(root, "signal")
	readyFile := filepath.Join(root, "ready")
	binary := writeScript(t, root, "core", `#!/bin/sh
trap 'printf term > "$MM_TEST_SIGNAL"; exit 0' TERM
printf ready > "$MM_TEST_READY"
while :; do sleep 0.05; done
`)
	t.Setenv("MM_TEST_SIGNAL", signalFile)
	t.Setenv("MM_TEST_READY", readyFile)
	spec := core.RuntimeSpec{ConfigDir: root, ConfigPath: filepath.Join(root, "config.yaml"), LogPath: filepath.Join(root, "state", "core.log")}
	process, err := startProcess(binary, spec, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, readyFile)
	unrelated := exec.Command("sleep", "30")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() }()
	sid, err := unix.Getsid(process.PID())
	if err != nil || sid != process.PID() {
		t.Fatalf("mihomo process is not a session leader: sid=%d pid=%d err=%v", sid, process.PID(), err)
	}
	if err := process.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(signalFile); err != nil {
		t.Fatalf("SIGTERM was not observed: %v", err)
	}
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated process was affected: %v", err)
	}

	ignoreReady := filepath.Join(root, "ignore-ready")
	t.Setenv("MM_TEST_READY", ignoreReady)
	ignoreTerm := writeScript(t, root, "ignore-term", "#!/bin/sh\ntrap '' TERM\nprintf ready > \"$MM_TEST_READY\"\nwhile :; do sleep 0.05; done\n")
	forced, err := startProcess(ignoreTerm, spec, 40*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, ignoreReady)
	started := time.Now()
	if err := forced.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 35*time.Millisecond {
		t.Fatal("process was not given the SIGTERM grace period")
	}
}

func validStaticConfig() string {
	return "mixed-port: 17890\nsocks-port: 17891\nexternal-controller: 127.0.0.1:19090\nproxies:\n  - name: demo\n    type: socks5\n    server: 127.0.0.1\n    port: 1080\nproxy-groups:\n  - name: GLOBAL\n    type: select\n    proxies: [demo]\nrules:\n  - MATCH,GLOBAL\n"
}

func writeScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func loopbackServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("file did not appear: %s", path)
}
