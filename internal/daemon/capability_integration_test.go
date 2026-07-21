//go:build linux

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
)

func TestCapabilityRoutesLegacyThroughUnixIPC(t *testing.T) {
	runtimeAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/proxies":
			_, _ = w.Write([]byte(`{"proxies":{"GLOBAL":{"type":"Selector","now":"node-a","all":["node-a"]},"node-a":{"type":"VLESS"}}}`))
		case r.URL.Path == "/proxies/GLOBAL" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"now":"node-a","all":["node-a"]}`))
		case r.URL.Path == "/proxies/node-a/delay":
			_, _ = w.Write([]byte(`{"delay":25}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer runtimeAPI.Close()
	parsed, err := url.Parse(runtimeAPI.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := parsed.Port()
	root := t.TempDir()
	configDir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "config.yaml")
	content := fmt.Sprintf("mixed-port: 7890\nexternal-controller: 127.0.0.1:%s\nproxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n", port)
	if err := os.WriteFile(configFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "subscription.url"), []byte("https://example.invalid/redacted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "whitelist.yaml"), []byte("domains:\n  - example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeCore := filepath.Join(root, "mihomo")
	if err := os.WriteFile(fakeCore, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_DIR", configDir)
	t.Setenv("MIHOMO_BIN", fakeCore)
	t.Setenv("MIHOMO_API_PORT", port)

	paths := testPaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.CoreLog), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.CoreLog, []byte("level=info token=private-value ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	server := New(Options{Paths: paths, CoreAdapter: &managerFakeAdapter{}})
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	waitForSocket(t, paths.Socket)
	client := ipc.NewClient(paths.Socket)

	var migration map[string]any
	if err := client.Do(context.Background(), http.MethodPost, "/v1/migrations/apply", "apply-integration", nil, &migration); err != nil {
		cancel()
		t.Fatal(err)
	}
	var modeStatus ModeStatus
	modeRequest := map[string]any{"mode": "direct", "closeConnections": false}
	if err := client.Do(context.Background(), http.MethodPut, "/v1/mode", "mode-integration", modeRequest, &modeStatus); err != nil {
		cancel()
		t.Fatal(err)
	}
	if modeStatus.ConfigMode != domain.RoutingModeDirect || !modeStatus.NextStart || modeStatus.RuntimeAvailable || modeStatus.OperationID == "" {
		cancel()
		t.Fatalf("unexpected stopped mode status: %+v", modeStatus)
	}
	backupPattern := filepath.Join(configDir, "config.yaml.mode-*.bak")
	backups, _ := filepath.Glob(backupPattern)
	var replay ModeStatus
	if err := client.Do(context.Background(), http.MethodPut, "/v1/mode", "mode-integration", modeRequest, &replay); err != nil {
		cancel()
		t.Fatal(err)
	}
	replayedBackups, _ := filepath.Glob(backupPattern)
	if replay.OperationID != modeStatus.OperationID || len(replayedBackups) != len(backups) {
		cancel()
		t.Fatalf("mode replay executed twice: first=%+v replay=%+v backups=%d/%d", modeStatus, replay, len(backups), len(replayedBackups))
	}
	assertIPCCode := func(err error, code string) {
		t.Helper()
		var ipcErr *ipc.Error
		got := ""
		if errors.As(err, &ipcErr) {
			got = ipcErr.Body.Code
		}
		if got != code {
			cancel()
			t.Fatalf("error=%v code=%q, want %q", err, got, code)
		}
	}
	assertIPCCode(client.Do(context.Background(), http.MethodPut, "/v1/mode", "mode-integration", map[string]any{"mode": "rule"}, &modeStatus), "REQUEST_ID_CONFLICT")
	assertIPCCode(client.Do(context.Background(), http.MethodPut, "/v1/mode", "", modeRequest, &modeStatus), "REQUEST_ID_REQUIRED")
	assertIPCCode(client.Do(context.Background(), http.MethodPut, "/v1/mode", "mode-invalid", map[string]any{"mode": "invalid"}, &modeStatus), "INVALID_ROUTING_MODE")
	if err := client.Do(context.Background(), http.MethodGet, "/v1/mode", "", nil, &modeStatus); err != nil || modeStatus.ConfigMode != domain.RoutingModeDirect {
		cancel()
		t.Fatalf("mode GET status=%+v err=%v", modeStatus, err)
	}
	var subscription SubscriptionInfo
	if err := client.Do(context.Background(), http.MethodGet, "/v1/subscription", "", nil, &subscription); err != nil {
		cancel()
		t.Fatal(err)
	}
	if subscription.ID != legacySubscriptionID || !strings.Contains(subscription.URL, "example.invalid") {
		cancel()
		t.Fatalf("unexpected subscription: %+v", subscription)
	}
	var groups []GroupInfo
	if err := client.Do(context.Background(), http.MethodGet, "/v1/groups", "", nil, &groups); err != nil {
		cancel()
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].ID != "GLOBAL" || groups[0].SelectedNode != "node-a" {
		cancel()
		t.Fatalf("unexpected groups: %+v", groups)
	}
	var validated map[string]any
	if err := client.Do(context.Background(), http.MethodPost, "/v1/config/validate", "validate-integration", map[string]string{}, &validated); err != nil {
		cancel()
		t.Fatal(err)
	}
	var document ConfigDocument
	if err := client.Do(context.Background(), http.MethodGet, "/v1/config/edit", "", nil, &document); err != nil || len(document.Content) == 0 || len(document.SHA256) != 64 {
		cancel()
		t.Fatalf("unexpected config document: value=%+v err=%v", document, err)
	}
	updatedConfig := append(append([]byte(nil), document.Content...), []byte("# edited by integration\n")...)
	var editResult map[string]any
	if err := client.Do(context.Background(), http.MethodPut, "/v1/config/edit", "edit-integration", map[string]any{"expectedSha256": document.SHA256, "content": updatedConfig}, &editResult); err != nil {
		cancel()
		t.Fatal(err)
	}
	var coreAction map[string]any
	if err := client.Do(context.Background(), http.MethodPost, "/v1/core/start", "start-integration", map[string]string{}, &coreAction); err != nil {
		cancel()
		t.Fatal(err)
	}
	var coreStatus CoreStatus
	if err := client.Do(context.Background(), http.MethodGet, "/v1/core/status", "", nil, &coreStatus); err != nil || coreStatus.State != domain.CoreStateRunning || coreStatus.ProfileID != "legacy-mihomo" {
		cancel()
		t.Fatalf("unexpected core status: value=%+v err=%v", coreStatus, err)
	}
	stream, err := client.OpenStream(context.Background(), http.MethodPost, "/v1/nodes/test", "test-integration", map[string]any{"concurrency": 1, "limit": 1})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	decoder := ipc.NewStreamDecoder(stream)
	first, err := decoder.Next(context.Background())
	if err != nil || first.Kind != "event" || !strings.Contains(string(first.Data), `"delayMs":25`) {
		_ = stream.Close()
		cancel()
		t.Fatalf("unexpected node event: event=%+v err=%v", first, err)
	}
	last, err := decoder.Next(context.Background())
	_ = stream.Close()
	if err != nil || last.Kind != "done" {
		cancel()
		t.Fatalf("unexpected terminal event: event=%+v err=%v", last, err)
	}
	var logs map[string]string
	if err := client.Do(context.Background(), http.MethodGet, "/v1/logs?lines=5", "", nil, &logs); err != nil || !strings.Contains(logs["content"], "ready") {
		cancel()
		t.Fatalf("unexpected logs: value=%v err=%v", logs, err)
	}
	if err := client.Do(context.Background(), http.MethodPost, "/v1/core/stop", "stop-integration", map[string]string{}, &coreAction); err != nil {
		cancel()
		t.Fatal(err)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
