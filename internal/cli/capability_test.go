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
	groups           []app.Group
	nodes            []app.Node
	subscription     app.Subscription
	route            app.RouteDiagnosis
	logs             []app.LogLine
	stream           []app.NodeTestEvent
	testRequest      app.NodeTestRequest
	selectGroup      string
	selectNode       string
	replaced         []byte
	modeStatus       app.RoutingModeStatus
	connections      []app.Connection
	connectionEvents []app.ConnectionEvent
	setMode          app.SetRoutingModeRequest
	portStatus       app.ListenerPortStatus
	setPort          app.SetListenerPortRequest
	proxyStatus      app.ProxyConfigStatus
	proxyRequest     app.ProxyRequest
	err              error
}

func (f *fakeCapabilityAPI) Connections(context.Context, app.ConnectionRequest) ([]app.Connection, error) {
	return append([]app.Connection(nil), f.connections...), f.err
}

func (f *fakeCapabilityAPI) FollowConnections(context.Context, app.ConnectionRequest) <-chan app.ConnectionEvent {
	result := make(chan app.ConnectionEvent, len(f.connectionEvents))
	for _, event := range f.connectionEvents {
		result <- event
	}
	close(result)
	return result
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
func (f *fakeCapabilityAPI) TestNodes(_ context.Context, request app.NodeTestRequest) <-chan app.NodeTestEvent {
	f.testRequest = request
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
func (f *fakeCapabilityAPI) ListenerPorts(context.Context, string) (app.ListenerPortStatus, error) {
	return f.portStatus, f.err
}
func (f *fakeCapabilityAPI) SetListenerPort(_ context.Context, request app.SetListenerPortRequest) (app.ListenerPortStatus, error) {
	f.setPort = request
	return f.portStatus, f.err
}
func (f *fakeCapabilityAPI) ProxyStatus(context.Context, string, string) (app.ProxyConfigStatus, error) {
	return f.proxyStatus, f.err
}
func (f *fakeCapabilityAPI) SetProxy(_ context.Context, request app.ProxyRequest) (app.ProxyConfigStatus, error) {
	f.proxyRequest = request
	return f.proxyStatus, f.err
}
func (f *fakeCapabilityAPI) RestoreProxy(context.Context, app.ProxyRequest) (app.ProxyConfigStatus, error) {
	return app.ProxyConfigStatus{}, f.err
}

func TestProxyCLISetUsesTypedCapabilityAndValidatesArguments(t *testing.T) {
	fake := &fakeCapabilityAPI{proxyStatus: app.ProxyConfigStatus{Layer: "env", Managed: true, Endpoints: []app.ProxyEndpointStatus{{Target: "http", Scheme: "http", Host: "127.0.0.1", Port: 7890, State: "matched"}}}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"proxy", "env", "set", "http", "127.0.0.1", "7890", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 || fake.proxyRequest.Layer != "env" || fake.proxyRequest.Target != "http" || fake.proxyRequest.Port != 7890 {
		t.Fatalf("exit=%d request=%+v stderr=%q", code, fake.proxyRequest, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"kind":"ProxyConfigSet"`) || !strings.Contains(stdout.String(), `"managed":true`) {
		t.Fatalf("stdout=%q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"proxy", "system", "set", "all", "example.com", "7890"}, nil, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("domain input should fail: stdout=%q", stdout.String())
	}
}
func (f *fakeCapabilityAPI) DisableProxy(context.Context, app.ProxyRequest) (app.ProxyConfigStatus, error) {
	return app.ProxyConfigStatus{}, f.err
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
	testedAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	fake := &fakeCapabilityAPI{stream: []app.NodeTestEvent{
		{Done: 1, Total: 1, Result: &app.NodeDelay{NodeID: "node-a", Status: app.NodeTestStatusSuccess, Delay: 123 * time.Millisecond, TestedAt: testedAt}},
		{Done: 1, Total: 1, Finished: true},
	}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"node", "test", "--output", "ndjson"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"kind":"NodeTestEvent"`) || !strings.Contains(lines[0], `"status":"success"`) || !strings.Contains(lines[0], `"testedAt":"2026-07-22T12:00:00Z"`) || !strings.Contains(lines[1], `"kind":"NodeTestComplete"`) {
		t.Fatalf("unexpected NDJSON: %q", stdout.String())
	}
}

func TestCapabilityCLINodeTestSingleUsesNodeAndOptionalGroup(t *testing.T) {
	fake := &fakeCapabilityAPI{stream: []app.NodeTestEvent{
		{Done: 1, Total: 1, Result: &app.NodeDelay{NodeID: "node-a", Status: app.NodeTestStatusFailed, TestedAt: time.Now()}},
		{Done: 1, Total: 1, Finished: true},
	}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"node", "test", "--node", "node-a", "--group", "GLOBAL"}, nil, &stdout, &stderr)
	if code != 0 || fake.testRequest.NodeID != "node-a" || fake.testRequest.GroupID != "GLOBAL" || !strings.Contains(stdout.String(), "失败") {
		t.Fatalf("unexpected single test: code=%d request=%+v stdout=%q stderr=%q", code, fake.testRequest, stdout.String(), stderr.String())
	}
}

func TestCapabilityCLINodeTestSingleRejectsBatchFlags(t *testing.T) {
	fake := &fakeCapabilityAPI{}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"node", "test", "--node", "node-a", "--limit", "1"}, nil, &stdout, &stderr)
	if code != ExitInvalidArgument || fake.testRequest.NodeID != "" || !strings.Contains(stderr.String(), "不能同时指定") {
		t.Fatalf("unexpected conflict result: code=%d request=%+v stderr=%q", code, fake.testRequest, stderr.String())
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

func TestCapabilityCLIListenerPortsTableJSONAndSet(t *testing.T) {
	status := app.ListenerPortStatus{
		ProfileID: "legacy-mihomo", CoreState: domain.CoreStateStopped, NextStart: true,
		Ports: []app.ListenerPort{
			{Field: app.ListenerPortFieldMixed, Host: "127.0.0.1", Port: 7890, Enabled: true, Networks: []string{"tcp", "udp"}},
			{Field: app.ListenerPortFieldSocks, Host: "127.0.0.1", Port: 0, Networks: []string{"tcp", "udp"}},
		},
		PortConflicts: []app.PortConflict{{Field: app.ListenerPortFieldMixed, Network: "tcp", Host: "127.0.0.1", Port: 7890}},
	}
	fake := &fakeCapabilityAPI{portStatus: status}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"config", "ports", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("ports failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{`"kind":"ListenerPortStatus"`, `"field":"mixed-port"`, `"portConflicts"`, `"enabled":false`} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("ports JSON missing %q: %s", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	status.Restarted, status.NextStart = true, false
	fake.portStatus = status
	code = Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"config", "port", "set", "mixed-port", "17890"}, nil, &stdout, &stderr)
	if code != 0 || fake.setPort.Field != "mixed-port" || fake.setPort.Port != 17890 || !strings.Contains(stdout.String(), "已重启并生效") {
		t.Fatalf("set failed: code=%d request=%+v stdout=%q stderr=%q", code, fake.setPort, stdout.String(), stderr.String())
	}
}

func TestCapabilityCLIListenerPortRejectsInvalidInputAndMapsConflict(t *testing.T) {
	fake := &fakeCapabilityAPI{}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"config", "port", "set", "unknown", "7890"}, nil, &stdout, &stderr)
	if code != ExitInvalidArgument || fake.setPort.Field != "" {
		t.Fatalf("invalid field: code=%d request=%+v stderr=%q", code, fake.setPort, stderr.String())
	}

	fake.err = &app.Error{Category: app.ErrorCategoryConflict, Code: app.ErrorCode("PORT_CONFLICT"), Message: "端口已被占用"}
	stdout.Reset()
	stderr.Reset()
	code = Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"config", "port", "set", "mixed-port", "7890"}, nil, &stdout, &stderr)
	if code != ExitConflict || !strings.Contains(stderr.String(), "端口已被占用") {
		t.Fatalf("conflict: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
