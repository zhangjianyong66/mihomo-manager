package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type fakeTUIService struct {
	app.CapabilityAPI
	status           app.RoutingModeStatus
	statusErr        error
	setErr           error
	statusCalls      int
	setCalls         int
	request          app.SetRoutingModeRequest
	editOld          string
	editNew          string
	connectionEvents []app.ConnectionEvent
	connectionCalls  int
	portStatus       app.ListenerPortStatus
	portCalls        int
	setPortCalls     int
	setPortRequest   app.SetListenerPortRequest
	group            app.Group
	testEvents       []app.NodeTestEvent
	testCalls        int
	testRequests     []app.NodeTestRequest
	testContext      context.Context
	selectedGroup    string
	selectedNode     string
}

func (f *fakeTUIService) ModeStatus(context.Context, string) (app.RoutingModeStatus, error) {
	f.statusCalls++
	return f.status, f.statusErr
}

func (f *fakeTUIService) SetMode(_ context.Context, request app.SetRoutingModeRequest) (app.RoutingModeStatus, error) {
	f.setCalls++
	f.request = request
	status := f.status
	status.ConfigMode = request.Mode
	return status, f.setErr
}

func (*fakeTUIService) EditConfig(context.Context, string) error { return nil }

func (f *fakeTUIService) FollowConnections(context.Context, app.ConnectionRequest) <-chan app.ConnectionEvent {
	f.connectionCalls++
	result := make(chan app.ConnectionEvent, len(f.connectionEvents))
	for _, event := range f.connectionEvents {
		result <- event
	}
	close(result)
	return result
}

func (f *fakeTUIService) EditWhitelist(_ context.Context, _, oldValue, newValue string) error {
	f.editOld, f.editNew = oldValue, newValue
	return nil
}

func (f *fakeTUIService) Whitelist(context.Context, string) ([]string, error) {
	return []string{f.editNew}, nil
}

func (f *fakeTUIService) ListenerPorts(context.Context, string) (app.ListenerPortStatus, error) {
	f.portCalls++
	return f.portStatus, nil
}

func (f *fakeTUIService) SetListenerPort(_ context.Context, request app.SetListenerPortRequest) (app.ListenerPortStatus, error) {
	f.setPortCalls++
	f.setPortRequest = request
	return f.portStatus, nil
}

func (f *fakeTUIService) Groups(context.Context, string) ([]app.Group, error) {
	return []app.Group{f.group}, nil
}

func (f *fakeTUIService) Group(context.Context, string, string) (app.Group, error) {
	return f.group, nil
}

func (f *fakeTUIService) SelectGroupNode(_ context.Context, _, group, node string) error {
	f.selectedGroup, f.selectedNode = group, node
	return nil
}

func (f *fakeTUIService) TestNodes(ctx context.Context, request app.NodeTestRequest) <-chan app.NodeTestEvent {
	f.testCalls++
	f.testRequests = append(f.testRequests, request)
	f.testContext = ctx
	result := make(chan app.NodeTestEvent, len(f.testEvents))
	for _, event := range f.testEvents {
		result <- event
	}
	close(result)
	return result
}

var _ Capabilities = (*fakeTUIService)(nil)

func TestNodeListLoadsHistoryWithoutAutomaticTest(t *testing.T) {
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	fake := &fakeTUIService{}
	model := New(fake)
	model.now = func() time.Time { return now }
	group := app.Group{
		ID: "GLOBAL", Name: "GLOBAL", SelectedNodeID: "node-a", NodeIDs: []domain.NodeID{"node-a", "node-b"},
		NodeStates: []app.GroupNodeState{
			{NodeID: "node-a", Testable: true, Latest: &app.NodeDelay{NodeID: "node-a", Status: app.NodeTestStatusSuccess, Delay: 238 * time.Millisecond, TestedAt: now.Add(-9 * time.Minute)}},
			{NodeID: "node-b", Testable: true},
		},
	}
	next, command := model.enterLoadedGroup(group)
	model = next.(Model)
	view := model.View()
	if command != nil || fake.testCalls != 0 || model.nodeTestActive {
		t.Fatalf("entering group started a test: command=%v calls=%d model=%+v", command != nil, fake.testCalls, model)
	}
	for _, want := range []string{"238ms · 9分钟前", "未测速", "t 单测", "a 批量"} {
		if !strings.Contains(view, want) {
			t.Fatalf("node view missing %q: %s", want, view)
		}
	}
}

func TestNodeListSingleAndBatchTestsUseExplicitRequests(t *testing.T) {
	fake := &fakeTUIService{testEvents: []app.NodeTestEvent{{Done: 1, Total: 1, Finished: true}}}
	model := New(fake)
	next, _ := model.enterLoadedGroup(app.Group{
		ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a"},
		NodeStates: []app.GroupNodeState{{NodeID: "node-a", Testable: true}},
	})
	model = next.(Model)

	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	if command == nil || fake.testCalls != 0 || !model.nodeTestActive {
		t.Fatalf("single test was not deferred: command=%v calls=%d active=%v", command != nil, fake.testCalls, model.nodeTestActive)
	}
	next, _ = model.Update(command())
	model = next.(Model)
	if fake.testCalls != 1 || fake.testRequests[0].NodeID != "node-a" || fake.testRequests[0].GroupID != "GLOBAL" {
		t.Fatalf("unexpected single request: %+v", fake.testRequests)
	}

	next, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	model = next.(Model)
	if command == nil {
		t.Fatal("batch test command is nil")
	}
	_, _ = model.Update(command())
	if fake.testCalls != 2 || fake.testRequests[1].NodeID != "" || fake.testRequests[1].Concurrency != 5 || fake.testRequests[1].Limit != 0 {
		t.Fatalf("unexpected batch request: %+v", fake.testRequests)
	}
}

func TestNodeListEscapeCancelsBeforeLeavingAndStaleEventsAreIgnored(t *testing.T) {
	fake := &fakeTUIService{}
	model := New(fake)
	next, _ := model.enterLoadedGroup(app.Group{ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a"}, NodeStates: []app.GroupNodeState{{NodeID: "node-a", Testable: true}}})
	model = next.(Model)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	started := command().(nodeTestStartedMsg)
	next, _ = model.Update(started)
	model = next.(Model)
	oldGeneration := model.switchTestGeneration

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.actionCtx != "group_nodes" || model.nodeTestActive || model.switchTestGeneration == oldGeneration {
		t.Fatalf("first escape should only cancel: %+v", model)
	}
	select {
	case <-fake.testContext.Done():
	default:
		t.Fatal("test context was not cancelled")
	}
	stale := app.NodeDelay{NodeID: "node-a", Status: app.NodeTestStatusSuccess, Delay: time.Millisecond}
	next, _ = model.Update(switchNodeTestMsg{generation: oldGeneration, event: app.NodeTestEvent{Done: 1, Total: 1, Result: &stale}, ok: true})
	model = next.(Model)
	if _, exists := model.nodeResults["node-a"]; exists {
		t.Fatal("stale generation polluted node results")
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.actionCtx != "group_list" {
		t.Fatalf("second escape did not leave node list: %+v", model)
	}
}

func TestNodeSelectionDoesNotClearHistoryOrStartTest(t *testing.T) {
	fake := &fakeTUIService{}
	model := New(fake)
	history := app.NodeDelay{NodeID: "node-a", Status: app.NodeTestStatusSuccess, Delay: 20 * time.Millisecond, TestedAt: time.Now()}
	next, _ := model.enterLoadedGroup(app.Group{ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a"}, NodeStates: []app.GroupNodeState{{NodeID: "node-a", Testable: true, Latest: &history}}})
	model = next.(Model)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if command == nil {
		t.Fatal("selection command is nil")
	}
	next, _ = model.Update(command())
	model = next.(Model)
	if fake.selectedGroup != "GLOBAL" || fake.selectedNode != "node-a" || fake.testCalls != 0 || model.nodeResults["node-a"].Delay != 20*time.Millisecond {
		t.Fatalf("selection changed test state: group=%q node=%q calls=%d results=%+v", fake.selectedGroup, fake.selectedNode, fake.testCalls, model.nodeResults)
	}
}

func TestNodeListNarrowRowsKeepStatusWithinTerminalWidth(t *testing.T) {
	model := New(&fakeTUIService{})
	model.width = 32
	model.currentNodeName = "这是一个非常长的中文节点名称"
	model.nodeResults = map[string]app.NodeDelay{
		model.currentNodeName: {NodeID: domain.NodeID(model.currentNodeName), Status: app.NodeTestStatusSuccess, Delay: 238 * time.Millisecond, TestedAt: time.Now()},
	}
	row := model.renderSwitchNodeItem(model.currentNodeName)
	if lipgloss.Width(row) > model.width-4 || !strings.Contains(row, "[238ms · 刚刚]") {
		t.Fatalf("narrow row overflowed or lost status: width=%d row=%q", lipgloss.Width(row), row)
	}
}

func TestModePageLoadsAsynchronouslyAndShowsStoppedState(t *testing.T) {
	fake := &fakeTUIService{status: app.RoutingModeStatus{
		ProfileID: "legacy-mihomo", ConfigMode: domain.RoutingModeRule,
		CoreState: domain.CoreStateStopped, EffectiveGroup: "🌐 代理", NextStart: true,
		RuleSets: []app.RuleSetHealth{{Name: "mm-cn-domain"}, {Name: "mm-cn-ip"}},
	}}
	model := New(fake)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if fake.statusCalls != 0 || command == nil || model.actionCtx != "mode" || !model.busy {
		t.Fatalf("mode load should be deferred: calls=%d command=%v model=%+v", fake.statusCalls, command != nil, model)
	}
	next, _ = model.Update(command())
	model = next.(Model)
	view := model.View()
	for _, want := range []string{"配置模式：规则分流", "Core 已停止", "下次启动生效", "待 Core 启动后核验", "● 规则分流"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q: %s", want, view)
		}
	}
}

func TestModePageSetKeepsSelectionAndPassesCloseOption(t *testing.T) {
	fake := &fakeTUIService{status: app.RoutingModeStatus{
		ProfileID: "legacy-mihomo", ConfigMode: domain.RoutingModeRule,
		CoreState: domain.CoreStateStopped, NextStart: true,
	}}
	model := New(fake)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	next, _ = model.Update(command())
	model = next.(Model)

	model.actionIndex = 2
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model.actionIndex = 3
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model.actionIndex = 4
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if fake.setCalls != 0 || command == nil {
		t.Fatalf("set should be deferred: calls=%d command=%v", fake.setCalls, command != nil)
	}
	next, _ = model.Update(command())
	model = next.(Model)
	if fake.request.Mode != domain.RoutingModeDirect || !fake.request.CloseConnections || model.modeSelected != domain.RoutingModeDirect {
		t.Fatalf("unexpected request/model: request=%+v selected=%s", fake.request, model.modeSelected)
	}
	if !strings.Contains(model.View(), "配置已保存，将在下次启动 mihomo 时生效") {
		t.Fatalf("missing stopped success result: %s", model.View())
	}
}

func TestModePageProvidesExecutableDaemonAndMigrationHints(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "daemon", err: &app.Error{Code: app.ErrorCodeDaemonUnavailable, Message: "daemon 不可用"}, want: "mm daemon start"},
		{name: "migration", err: &app.Error{Code: app.ErrorCodeNotFound, Message: "资源不存在"}, want: "mm migrate apply"},
		{name: "restore", err: &app.Error{Code: app.ErrorCode("RESTORE_FAILED"), Message: "恢复失败"}, want: "状态可能不确定"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeTUIService{statusErr: tt.err}
			model := New(fake)
			next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			model = next.(Model)
			next, _ = model.Update(command())
			model = next.(Model)
			if !strings.Contains(model.View(), tt.want) {
				t.Fatalf("missing hint %q: %s", tt.want, model.View())
			}
		})
	}
}

func TestModePageNarrowViewKeepsControlsOnSeparateLines(t *testing.T) {
	fake := &fakeTUIService{status: app.RoutingModeStatus{ConfigMode: domain.RoutingModeGlobal, CoreState: domain.CoreStateRunning}}
	model := New(fake)
	model.actionCtx = "mode"
	model.page = actionMenu
	model.actionItems = modeActionItems()
	model.modeStatus = fake.status
	model.modeSelected = domain.RoutingModeGlobal
	model.width = 24
	view := model.View()
	for _, want := range []string{"● 全局代理\n", "[ ] 关闭现有连接\n", "应用切换\n", "返回\n"} {
		if !strings.Contains(view, want) {
			t.Fatalf("control layout missing %q: %s", want, view)
		}
	}
}

func TestListenerPortsPageLoadsAsynchronouslyAndShowsAllFields(t *testing.T) {
	fake := &fakeTUIService{portStatus: app.ListenerPortStatus{
		ProfileID: "legacy-mihomo",
		CoreState: domain.CoreStateStopped,
		Ports: []app.ListenerPort{
			{Field: app.ListenerPortFieldMixed, Host: "127.0.0.1", Port: 7890, Enabled: true, Networks: []string{"tcp", "udp"}},
			{Field: app.ListenerPortFieldHTTP, Host: "127.0.0.1", Port: 0, Networks: []string{"tcp"}},
			{Field: app.ListenerPortFieldSocks, Host: "127.0.0.1", Port: 7891, Enabled: true, Networks: []string{"tcp", "udp"}},
			{Field: app.ListenerPortFieldRedir, Host: "127.0.0.1", Port: 7892, Enabled: true, Networks: []string{"tcp"}},
			{Field: app.ListenerPortFieldTProxy, Host: "127.0.0.1", Port: 7893, Enabled: true, Networks: []string{"tcp", "udp"}},
			{Field: app.ListenerPortFieldExternalController, Host: "127.0.0.1", Port: 9090, Required: true, Enabled: true, Networks: []string{"tcp"}},
		},
		PortConflicts: []app.PortConflict{{Field: app.ListenerPortFieldMixed, Network: "tcp", Host: "127.0.0.1", Port: 10808}},
	}}
	model := New(fake)
	model.mainIndex = 7
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if fake.portCalls != 0 || command == nil || !model.busy || model.actionCtx != "listener_ports" {
		t.Fatalf("port load should be deferred: calls=%d command=%v model=%+v", fake.portCalls, command != nil, model)
	}
	next, _ = model.Update(command())
	model = next.(Model)
	view := model.View()
	for _, want := range []string{"mixed-port", "port  禁用", "socks-port", "redir-port", "tproxy-port", "external-controller", "[冲突]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("listener port view does not contain %q: %s", want, view)
		}
	}
}

func TestListenerPortsPageEditsFieldAndShowsActivationResult(t *testing.T) {
	tests := []struct {
		name       string
		status     app.ListenerPortStatus
		wantResult string
	}{
		{
			name:       "running",
			status:     app.ListenerPortStatus{ProfileID: "legacy-mihomo", CoreState: domain.CoreStateRunning, Restarted: true},
			wantResult: "mihomo 已重启并生效",
		},
		{
			name:       "stopped",
			status:     app.ListenerPortStatus{ProfileID: "legacy-mihomo", CoreState: domain.CoreStateStopped, NextStart: true},
			wantResult: "下次启动 mihomo 时生效",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.status.Ports = []app.ListenerPort{{Field: app.ListenerPortFieldSocks, Host: "127.0.0.1", Port: 7891, Enabled: true, Networks: []string{"tcp", "udp"}}}
			fake := &fakeTUIService{portStatus: tt.status}
			model := New(fake)
			model.page = actionMenu
			model.actionCtx = "listener_ports"
			model.listenerPortStatus = tt.status
			model.actionItems = listenerPortActionItems(tt.status.Ports)

			next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			model = next.(Model)
			if model.page != inputView || model.actionCtx != "listener_port_input" {
				t.Fatalf("listener port input was not opened: %+v", model)
			}
			model.input.SetValue("17891")
			next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			model = next.(Model)
			if fake.setPortCalls != 0 || command == nil {
				t.Fatalf("port set should be deferred: calls=%d command=%v", fake.setPortCalls, command != nil)
			}
			next, _ = model.Update(command())
			model = next.(Model)
			if fake.setPortRequest.Field != app.ListenerPortFieldSocks || fake.setPortRequest.Port != 17891 {
				t.Fatalf("unexpected port request: %+v", fake.setPortRequest)
			}
			if !strings.Contains(model.View(), tt.wantResult) {
				t.Fatalf("missing activation result %q: %s", tt.wantResult, model.View())
			}
		})
	}
}

func TestCorePortConflictPointsToListenerPortsPage(t *testing.T) {
	model := New(&fakeTUIService{})
	next, _ := model.Update(actionDoneMsg{
		result: "启动失败",
		err:    &app.Error{Category: app.ErrorCategoryConflict, Code: app.ErrorCode("PORT_CONFLICT"), Message: "监听端口冲突"},
	})
	model = next.(Model)
	if !strings.Contains(model.View(), "配置管理 > 监听端口") {
		t.Fatalf("missing listener port hint: %s", model.View())
	}
}

func TestModeAndStreamPagesCancelOnEscape(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Model, context.CancelFunc)
	}{
		{name: "mode", set: func(model *Model, cancel context.CancelFunc) {
			model.actionCtx, model.page, model.busy, model.modeCancel = "mode", actionMenu, true, cancel
		}},
		{name: "logs", set: func(model *Model, cancel context.CancelFunc) {
			model.actionCtx, model.page, model.logFollowCancel = "log_live", actionMenu, cancel
		}},
		{name: "connections", set: func(model *Model, cancel context.CancelFunc) {
			model.actionCtx, model.page, model.connectionFollowCancel = "connections_live", actionMenu, cancel
		}},
		{name: "node test", set: func(model *Model, cancel context.CancelFunc) {
			model.actionCtx, model.page, model.switchTestCancel = "group_nodes", actionMenu, cancel
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := New(&fakeTUIService{})
			ctx, cancel := context.WithCancel(context.Background())
			tt.set(&model, cancel)
			_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
			select {
			case <-ctx.Done():
			default:
				t.Fatal("escape did not cancel active operation")
			}
		})
	}
}

func TestConnectionsPageStartsDeferredReducesEventsAndRendersStableRows(t *testing.T) {
	connection := app.Connection{
		ID: "conn-a", Host: "example.com", DestinationPort: 443, Network: "tcp",
		Rule: "RuleSet", RulePayload: "mm-cn-domain", Chains: []string{"node-a", "GLOBAL"}, FinalNode: "node-a", Upload: 1024, Download: 2048,
	}
	fake := &fakeTUIService{connectionEvents: []app.ConnectionEvent{
		{Seq: 1, Action: app.ConnectionActionOpen, Connection: &connection},
		{Seq: 2, Action: app.ConnectionActionUpdate, Connection: &connection},
		{Seq: 3, Action: app.ConnectionActionClosed, Connection: &connection},
		{Seq: 4, Finished: true},
	}}
	model := New(fake)
	model.mainIndex = 1
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if fake.connectionCalls != 0 || command == nil || model.actionCtx != "connections_live" {
		t.Fatalf("follow must be deferred: calls=%d command=%v ctx=%s", fake.connectionCalls, command != nil, model.actionCtx)
	}
	next, command = model.Update(command())
	model = next.(Model)
	if fake.connectionCalls != 1 || command == nil {
		t.Fatalf("stream did not start: calls=%d command=%v", fake.connectionCalls, command != nil)
	}
	for index := 0; index < 4; index++ {
		next, command = model.Update(command())
		model = next.(Model)
		if index < 3 && command == nil {
			t.Fatalf("event %d did not schedule next read", index)
		}
	}
	view := model.View()
	if len(model.connections) != 0 || len(model.connectionClosed) != 1 || !model.connectionFinished {
		t.Fatalf("model did not reduce stream: %+v", model)
	}
	for _, expected := range []string{"实时连接", "最近关闭：example.com:443", "连接流已结束"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view missing %q: %s", expected, view)
		}
	}
}

func TestConnectionsViewUsesFixedWideColumnsAndNarrowRows(t *testing.T) {
	model := New(&fakeTUIService{})
	model.actionCtx, model.page = "connections_live", actionMenu
	model.connections = map[string]app.Connection{"a": {ID: "a", DestinationIP: "2001:db8::1", DestinationPort: 53, Network: "udp", Rule: "Match", Chains: []string{"DIRECT"}, FinalNode: "DIRECT", Upload: 1, Download: 2}}
	model.connectionLastAction = map[string]app.ConnectionAction{"a": app.ConnectionActionOpen}
	model.width, model.height = 120, 24
	wide := model.View()
	for _, expected := range []string{"事件", "目标", "网络", "规则", "最终节点", "[2001:db8::1]:53"} {
		if !strings.Contains(wide, expected) {
			t.Fatalf("wide view missing %q: %s", expected, wide)
		}
	}
	model.width = 48
	narrow := model.View()
	if !strings.Contains(narrow, "open  [2001:db8::1]:53  udp\n") || !strings.Contains(narrow, "Match | DIRECT | 1B/2B") {
		t.Fatalf("narrow view is not stable: %s", narrow)
	}
}

func TestModeViewShowsProxyEntrypointSummary(t *testing.T) {
	model := New(&fakeTUIService{})
	model.actionCtx, model.page = "mode", actionMenu
	model.actionItems = modeActionItems()
	model.modeStatus = app.RoutingModeStatus{
		ConfigMode: domain.RoutingModeRule, CoreState: domain.CoreStateRunning,
		Listeners:        []app.ProxyListener{{Protocol: "mixed", Host: "127.0.0.1", Port: 7890}},
		SystemProxy:      []app.ProxySourceStatus{{Source: "gnome.http", State: "mismatched"}},
		EnvironmentProxy: []app.ProxySourceStatus{{Source: "env.HTTP_PROXY", State: "matched"}},
	}
	view := model.View()
	for _, expected := range []string{"监听入口：mixed 127.0.0.1:7890", "GNOME 代理：gnome.http=mismatched", "CLI 环境代理：env.HTTP_PROXY=matched"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("mode view missing %q: %s", expected, view)
		}
	}
}

func TestWhitelistEditUsesAtomicDaemonCapability(t *testing.T) {
	fake := &fakeTUIService{}
	message := mutateWhitelistCmd(context.Background(), fake, "edit", "old.example", "new.example")()
	result, ok := message.(whitelistLoadedMsg)
	if !ok || result.err != nil || fake.editOld != "old.example" || fake.editNew != "new.example" {
		t.Fatalf("unexpected edit: message=%#v old=%q new=%q", message, fake.editOld, fake.editNew)
	}
}

func TestModePartialFailureKeepsAppliedModeVisible(t *testing.T) {
	fake := &fakeTUIService{
		status: app.RoutingModeStatus{
			ProfileID: "legacy-mihomo", ConfigMode: domain.RoutingModeRule,
			CoreState: domain.CoreStateRunning, RuntimeAvailable: true,
		},
		setErr: &app.Error{Code: app.ErrorCode("CONNECTION_CLOSE_FAILED"), Message: "关闭连接失败"},
	}
	model := New(fake)
	model.page = actionMenu
	model.actionCtx = "mode"
	model.actionItems = modeActionItems()
	message := setModeCmd(context.Background(), fake, "legacy-mihomo", domain.RoutingModeDirect, true)()
	next, _ := model.Update(message)
	model = next.(Model)
	if model.modeStatus.ConfigMode != domain.RoutingModeDirect || model.modeSelected != domain.RoutingModeDirect || !strings.Contains(model.View(), "模式已生效，但关闭现有连接失败") {
		t.Fatalf("partial status was not preserved: status=%+v selected=%s view=%s", model.modeStatus, model.modeSelected, model.View())
	}
}
