package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type fakeCapabilityAPI struct {
	groups       []app.Group
	nodes        []app.Node
	subscription app.Subscription
	route        app.RouteDiagnosis
	logs         []app.LogLine
	stream       []app.NodeTestEvent
	selectGroup  string
	selectNode   string
	replaced     []byte
	modeStatus   app.RoutingModeStatus
	setMode      app.SetRoutingModeRequest
	err          error
}

func (f *fakeCapabilityAPI) ModeStatus(context.Context, string) (app.RoutingModeStatus, error) {
	return f.modeStatus, f.err
}
func (f *fakeCapabilityAPI) SetMode(_ context.Context, request app.SetRoutingModeRequest) (app.RoutingModeStatus, error) {
	f.setMode = request
	return f.modeStatus, f.err
}

func (f *fakeCapabilityAPI) CoreStatus(context.Context, string) (app.CoreStatus, error) {
	return app.CoreStatus{Type: domain.CoreTypeMihomo, State: domain.CoreStateRunning, ProfileID: "legacy-mihomo", PID: 42}, f.err
}
func (f *fakeCapabilityAPI) CoreAction(context.Context, string, string) error { return f.err }
func (f *fakeCapabilityAPI) ValidateConfig(context.Context, string) error     { return f.err }
func (f *fakeCapabilityAPI) Groups(context.Context, string) ([]app.Group, error) {
	return f.groups, f.err
}
func (f *fakeCapabilityAPI) Group(context.Context, string, string) (app.Group, error) {
	if len(f.groups) == 0 {
		return app.Group{}, f.err
	}
	return f.groups[0], f.err
}
func (f *fakeCapabilityAPI) SelectGroupNode(_ context.Context, _, group, node string) error {
	f.selectGroup, f.selectNode = group, node
	return f.err
}
func (f *fakeCapabilityAPI) Nodes(context.Context, string, string) ([]app.Node, error) {
	return f.nodes, f.err
}
func (f *fakeCapabilityAPI) TestNodes(context.Context, app.NodeTestRequest) <-chan app.NodeTestEvent {
	result := make(chan app.NodeTestEvent, len(f.stream))
	for _, event := range f.stream {
		result <- event
	}
	close(result)
	return result
}
func (f *fakeCapabilityAPI) Subscription(context.Context, string) (app.Subscription, error) {
	return f.subscription, f.err
}
func (f *fakeCapabilityAPI) SetSubscription(context.Context, string, string) error { return f.err }
func (f *fakeCapabilityAPI) UpdateSubscription(context.Context, string) error      { return f.err }
func (f *fakeCapabilityAPI) Whitelist(context.Context, string) ([]string, error) {
	return []string{"example.com"}, f.err
}
func (f *fakeCapabilityAPI) AddWhitelist(context.Context, string, string) error    { return f.err }
func (f *fakeCapabilityAPI) RemoveWhitelist(context.Context, string, string) error { return f.err }
func (f *fakeCapabilityAPI) EditWhitelist(context.Context, string, string, string) error {
	return f.err
}
func (f *fakeCapabilityAPI) ApplyRoutePreset(context.Context, string, string) error { return f.err }
func (f *fakeCapabilityAPI) DiagnoseRoute(context.Context, string, string) (app.RouteDiagnosis, error) {
	return f.route, f.err
}
func (f *fakeCapabilityAPI) ConfigBackup(context.Context, string) error  { return f.err }
func (f *fakeCapabilityAPI) ConfigRestore(context.Context, string) error { return f.err }
func (f *fakeCapabilityAPI) ReadConfig(context.Context, string) (app.ConfigDocument, error) {
	return app.ConfigDocument{Content: []byte("mode: rule\n"), SHA256: strings.Repeat("a", 64)}, f.err
}
func (f *fakeCapabilityAPI) ReplaceConfig(_ context.Context, _, _ string, content []byte) error {
	f.replaced = append([]byte(nil), content...)
	return f.err
}
func (f *fakeCapabilityAPI) TailLogs(context.Context, app.LogRequest) ([]app.LogLine, error) {
	return f.logs, f.err
}
func (f *fakeCapabilityAPI) FollowLogs(context.Context, app.LogRequest) <-chan app.LogEvent {
	result := make(chan app.LogEvent, 2)
	for _, line := range f.logs {
		line := line
		result <- app.LogEvent{Line: &line}
	}
	result <- app.LogEvent{Finished: true}
	close(result)
	return result
}

var _ app.CapabilityAPI = (*fakeCapabilityAPI)(nil)

func TestCapabilityCLIGroupListJSONUsesStableFields(t *testing.T) {
	fake := &fakeCapabilityAPI{groups: []app.Group{{ID: "GLOBAL", Name: "GLOBAL", Type: "Selector", SelectedNodeID: "node-a", NodeIDs: []domain.NodeID{"node-a", "node-b"}}}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"group", "list", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"selectedNodeId":"node-a"`) || strings.Contains(stdout.String(), "SelectedNodeID") {
		t.Fatalf("unexpected group JSON: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestCapabilityCLISubscriptionRedactsURL(t *testing.T) {
	const secret = "access-token-123"
	fake := &fakeCapabilityAPI{subscription: app.Subscription{ID: "legacy-subscription", Name: "legacy", URL: "https://user:pass@example.com/sub/" + secret + "?token=" + secret, Enabled: true}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"subscription", "show", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 || strings.Contains(stdout.String(), secret) {
		t.Fatalf("secret leaked or command failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "https://example.com/redacted") {
		t.Fatalf("expected redacted URL: %q", stdout.String())
	}
}

func TestCapabilityCLINodeTestNDJSON(t *testing.T) {
	fake := &fakeCapabilityAPI{stream: []app.NodeTestEvent{
		{Done: 1, Total: 1, Result: &app.NodeDelay{NodeID: "node-a", Delay: 123 * time.Millisecond}},
		{Done: 1, Total: 1, Finished: true},
	}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"node", "test", "--output", "ndjson"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"kind":"NodeTestEvent"`) || !strings.Contains(lines[1], `"kind":"NodeTestComplete"`) {
		t.Fatalf("unexpected NDJSON: %q", stdout.String())
	}
}

func TestCapabilityCLINodeSelectCallsSharedService(t *testing.T) {
	fake := &fakeCapabilityAPI{}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"node", "select", "GLOBAL", "node-a"}, nil, &stdout, &stderr)
	if code != 0 || fake.selectGroup != "GLOBAL" || fake.selectNode != "node-a" {
		t.Fatalf("unexpected select: code=%d group=%q node=%q stderr=%q", code, fake.selectGroup, fake.selectNode, stderr.String())
	}
}

func TestCapabilityCLIMapsDaemonErrorExitCode(t *testing.T) {
	fake := &fakeCapabilityAPI{err: &app.Error{Code: app.ErrorCodeDaemonUnavailable, Message: "daemon 不可用"}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"core", "status"}, nil, &stdout, &stderr)
	if code != ExitDaemonUnavailable || stdout.Len() != 0 || !strings.Contains(stderr.String(), "daemon 不可用") {
		t.Fatalf("unexpected error mapping: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCapabilityCLIStreamCancellationIsSuccessful(t *testing.T) {
	fake := &fakeCapabilityAPI{stream: []app.NodeTestEvent{{Err: context.Canceled, Finished: true}}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"node", "test"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cancellation should be graceful: code=%d stderr=%q", code, stderr.String())
	}
}

func TestCapabilityCLIConfigEditUsesLocalEditorAndDigest(t *testing.T) {
	root := t.TempDir()
	editor := filepath.Join(root, "editor.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\nprintf 'mode: rule\\n# edited\\n' > \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)
	fake := &fakeCapabilityAPI{}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"config", "edit"}, nil, &stdout, &stderr)
	if code != 0 || !strings.Contains(string(fake.replaced), "# edited") {
		t.Fatalf("config edit failed: code=%d replacement=%q stderr=%q", code, fake.replaced, stderr.String())
	}
}
