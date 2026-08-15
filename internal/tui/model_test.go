package tui

import (
	"context"
	"errors"
	"reflect"
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
	testContexts     []context.Context
	testStreams      []chan app.NodeTestEvent
	selectedGroup    string
	selectedNode     string
	restartCalls     int
	restartProgress  []app.DaemonRestartProgress
	restartResult    app.DaemonRestartResult
	restartErr       error
	coreActions      []string
}

func (f *fakeTUIService) RestartDaemon(_ context.Context, progress app.DaemonRestartProgressFunc) (app.DaemonRestartResult, error) {
	f.restartCalls++
	for _, event := range f.restartProgress {
		if progress != nil {
			progress(event)
		}
	}
	return f.restartResult, f.restartErr
}

func (f *fakeTUIService) CoreAction(_ context.Context, _ string, action string) error {
	f.coreActions = append(f.coreActions, action)
	return nil
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
	f.testContexts = append(f.testContexts, ctx)
	if index := f.testCalls - 1; index < len(f.testStreams) {
		return f.testStreams[index]
	}
	result := make(chan app.NodeTestEvent, len(f.testEvents))
	for _, event := range f.testEvents {
		result <- event
	}
	close(result)
	return result
}

var _ Capabilities = (*fakeTUIService)(nil)

func TestServiceMenuSeparatesCoreAndFullRestart(t *testing.T) {
	model := New(&fakeTUIService{})
	model.mainIndex = 2
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	view := model.View()
	if !strings.Contains(view, "重启 Core") || !strings.Contains(view, "重启全部") || strings.Contains(view, "> 重启\n") {
		t.Fatalf("restart actions are ambiguous: %s", view)
	}
}

func TestServiceCoreRestartDoesNotInvokeDaemonRestart(t *testing.T) {
	fake := &fakeTUIService{}
	model := New(fake)
	model.mainIndex = 2
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model.actionIndex = 3
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if command == nil || fake.restartCalls != 0 || len(fake.coreActions) != 0 {
		t.Fatalf("core restart was not deferred or touched daemon: command=%v restart=%d core=%v", command != nil, fake.restartCalls, fake.coreActions)
	}
	next, _ = model.Update(command())
	model = next.(Model)
	if fake.restartCalls != 0 || !reflect.DeepEqual(fake.coreActions, []string{"restart"}) || !strings.Contains(model.View(), "Core 已重启") {
		t.Fatalf("unexpected core restart: restart=%d core=%v view=%s", fake.restartCalls, fake.coreActions, model.View())
	}
}

func TestDaemonRestartConfirmationCancelsWithoutSideEffects(t *testing.T) {
	fake := &fakeTUIService{}
	model := New(fake)
	model.mainIndex = 2
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	model.actionIndex = 4
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if command != nil || fake.restartCalls != 0 || model.actionCtx != "daemon_restart_confirm" {
		t.Fatalf("confirmation should be side-effect free: command=%v calls=%d model=%+v", command != nil, fake.restartCalls, model)
	}
	for _, want := range []string{"代理连接会短暂中断", "测速历史", "Enter 确认重启", "Esc 取消"} {
		if !strings.Contains(model.View(), want) {
			t.Fatalf("confirmation missing %q: %s", want, model.View())
		}
	}
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if command != nil || fake.restartCalls != 0 || model.actionCtx != "" || !reflect.DeepEqual(model.actionItems, menuActions("服务管理")) {
		t.Fatalf("escape did not cancel cleanly: calls=%d model=%+v", fake.restartCalls, model)
	}
}

func TestDaemonRestartStreamsProgressLocksKeysAndShowsResult(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	fake := &fakeTUIService{
		restartProgress: []app.DaemonRestartProgress{
			{Phase: app.DaemonRestartPhasePreflight, Step: 1, Total: 6, Message: "检查状态"},
			{Phase: app.DaemonRestartPhaseRestartDaemon, Step: 3, Total: 6, Message: "重启服务"},
			{Phase: app.DaemonRestartPhaseWaitDaemon, Step: 4, Total: 6, Message: "等待握手"},
			{Phase: app.DaemonRestartPhaseVerify, Step: 6, Total: 6, Message: "最终验证"},
		},
		restartResult: app.DaemonRestartResult{
			PreviousDaemonPID: 100, DaemonPID: 200, PreviousStartedAt: oldTime, StartedAt: newTime,
			PreviousCoreState: domain.CoreStateStopped, CoreState: domain.CoreStateStopped, DaemonRestarted: true,
		},
	}
	model := New(fake)
	model.page = actionMenu
	model.actionCtx = "daemon_restart_confirm"
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(Model)
	if command == nil || fake.restartCalls != 0 || !model.busy || model.actionCtx != "daemon_restart_progress" {
		t.Fatalf("restart was not deferred: command=%v calls=%d model=%+v", command != nil, fake.restartCalls, model)
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune("q")}, {Type: tea.KeyCtrlC}} {
		next, blocked := model.Update(key)
		model = next.(Model)
		if blocked != nil || !model.busy || model.actionCtx != "daemon_restart_progress" {
			t.Fatalf("key %q interrupted restart: command=%v model=%+v", key.String(), blocked != nil, model)
		}
	}

	next, command = model.Update(command())
	model = next.(Model)
	for command != nil {
		next, command = model.Update(command())
		model = next.(Model)
	}
	if fake.restartCalls != 1 || model.page != resultView || model.busy {
		t.Fatalf("restart did not finish: calls=%d model=%+v", fake.restartCalls, model)
	}
	view := model.View()
	for _, want := range []string{"重启全部完成", "daemon PID: 100 -> 200", "Core 状态: stopped -> stopped", "daemon 已更换: 是"} {
		if !strings.Contains(view, want) {
			t.Fatalf("result missing %q: %s", want, view)
		}
	}
}

func TestDaemonRestartRecoveryResultDoesNotClaimFullSuccess(t *testing.T) {
	model := New(&fakeTUIService{})
	result := app.DaemonRestartResult{
		PreviousDaemonPID: 100, DaemonPID: 200,
		PreviousCoreState: domain.CoreStateRunning, CoreState: domain.CoreStateRunning,
		DaemonRestarted: true, CoreRestored: true, RecoveryAttempted: true, RecoverySucceeded: true,
		FailurePhase: app.DaemonRestartPhaseRestoreCore,
	}
	next, _ := model.Update(daemonRestartEvent{done: true, result: result, err: &app.Error{Code: app.ErrorCodeUpstreamFailure, Message: "恢复阶段失败"}})
	model = next.(Model)
	view := model.View()
	for _, want := range []string{"组合重启异常，服务已恢复", "自动恢复: 成功", "失败阶段: 恢复 Core"} {
		if !strings.Contains(view, want) {
			t.Fatalf("recovery result missing %q: %s", want, view)
		}
	}
}

func TestDaemonRestartConfirmationWrapsOnNarrowTerminal(t *testing.T) {
	model := New(&fakeTUIService{})
	model.page = actionMenu
	model.actionCtx = "daemon_restart_confirm"
	model.width = 32
	view := model.View()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "重启全部会先停止") || strings.Contains(line, "代理连接会短暂") || strings.Contains(line, "Core 内存状态") {
			if lipgloss.Width(line) > model.width-4 {
				t.Fatalf("confirmation line overflowed: width=%d line=%q", lipgloss.Width(line), line)
			}
		}
	}
}

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
	if command != nil || fake.testCalls != 0 || model.nodeTestMode != nodeTestIdle {
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
	if command == nil || fake.testCalls != 0 || model.nodeTestMode != nodeTestSingle {
		t.Fatalf("single test was not deferred: command=%v calls=%d mode=%v", command != nil, fake.testCalls, model.nodeTestMode)
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

func TestNodeListBatchShowsKnownTotalBeforeFirstResult(t *testing.T) {
	streams := []chan app.NodeTestEvent{
		make(chan app.NodeTestEvent, 1),
		make(chan app.NodeTestEvent, 1),
	}
	fake := &fakeTUIService{testStreams: streams}
	model := New(fake)
	next, _ := model.enterLoadedGroup(app.Group{
		ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a", "node-b", "DIRECT"},
		NodeStates: []app.GroupNodeState{
			{NodeID: "node-a", Testable: true},
			{NodeID: "node-b", Testable: true},
			{NodeID: "DIRECT", Testable: false},
		},
	})
	model = next.(Model)

	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	model = next.(Model)
	if command == nil || model.nodeTestMode != nodeTestBatch || model.switchTestDone != 0 || model.switchTestTotal != 2 {
		t.Fatalf("batch total was not initialized before command execution: command=%v model=%+v", command != nil, model)
	}
	if view := model.View(); !strings.Contains(view, "批量测速 0/2") || strings.Contains(view, "批量测速 0/0") {
		t.Fatalf("batch start view does not show the known total: %s", view)
	}
	started := command().(nodeTestStartedMsg)
	next, _ = model.Update(started)
	model = next.(Model)

	next, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	model = next.(Model)
	if command == nil || model.switchTestDone != 0 || model.switchTestTotal != 2 {
		t.Fatalf("restarted batch total was not initialized: command=%v done=%d total=%d", command != nil, model.switchTestDone, model.switchTestTotal)
	}
	select {
	case <-fake.testContexts[0].Done():
	default:
		t.Fatal("restarting batch did not cancel the previous context")
	}
	started = command().(nodeTestStartedMsg)
	next, wait := model.Update(started)
	model = next.(Model)
	result := app.NodeDelay{NodeID: "node-a", Status: app.NodeTestStatusSuccess, Delay: time.Millisecond}
	streams[1] <- app.NodeTestEvent{Done: 1, Total: 3, Result: &result}
	next, _ = model.Update(wait())
	model = next.(Model)
	if model.switchTestDone != 1 || model.switchTestTotal != 3 {
		t.Fatalf("stream progress did not replace the initialized total: done=%d total=%d", model.switchTestDone, model.switchTestTotal)
	}
}

func TestNodeListQueuesSingleTestsWithoutCancellingActiveStream(t *testing.T) {
	streams := []chan app.NodeTestEvent{
		make(chan app.NodeTestEvent, 2),
		make(chan app.NodeTestEvent, 2),
		make(chan app.NodeTestEvent, 1),
	}
	fake := &fakeTUIService{testStreams: streams}
	model := New(fake)
	next, _ := model.enterLoadedGroup(app.Group{
		ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a", "node-b", "node-c"},
		NodeStates: []app.GroupNodeState{
			{NodeID: "node-a", Testable: true},
			{NodeID: "node-b", Testable: true},
			{NodeID: "node-c", Testable: true},
		},
	})
	model = next.(Model)

	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	started := command().(nodeTestStartedMsg)
	next, wait := model.Update(started)
	model = next.(Model)
	if wait == nil || fake.testCalls != 1 || model.nodeTestCurrent != "node-a" {
		t.Fatalf("first single test did not start: calls=%d current=%q", fake.testCalls, model.nodeTestCurrent)
	}
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	if command != nil || len(model.nodeTestQueue) != 0 || !strings.Contains(model.View(), "该节点正在测速") {
		t.Fatalf("active node was queued again or feedback missing: queue=%v view=%s", model.nodeTestQueue, model.View())
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	if command != nil || !reflect.DeepEqual(model.nodeTestQueue, []domain.NodeID{"node-b"}) {
		t.Fatalf("second node was not queued: command=%v queue=%v", command != nil, model.nodeTestQueue)
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	if len(model.nodeTestQueue) != 1 || !strings.Contains(model.View(), "该节点已在队列中") {
		t.Fatalf("queued node was duplicated or feedback missing: queue=%v view=%s", model.nodeTestQueue, model.View())
	}
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	if !reflect.DeepEqual(model.nodeTestQueue, []domain.NodeID{"node-b", "node-c"}) || !strings.Contains(model.View(), "待测 2") || !strings.Contains(model.View(), "已加入队列: node-c") {
		t.Fatalf("single queue state is not visible: queue=%v view=%s", model.nodeTestQueue, model.View())
	}
	select {
	case <-fake.testContexts[0].Done():
		t.Fatal("queueing another node cancelled the active single test")
	default:
	}

	streams[0] <- app.NodeTestEvent{Done: 1, Total: 1, Finished: true}
	next, command = model.Update(wait())
	model = next.(Model)
	if command == nil || model.nodeTestCurrent != "node-b" || !reflect.DeepEqual(model.nodeTestQueue, []domain.NodeID{"node-c"}) {
		t.Fatalf("queue did not advance to node-b: current=%q queue=%v", model.nodeTestCurrent, model.nodeTestQueue)
	}
	started = command().(nodeTestStartedMsg)
	next, wait = model.Update(started)
	model = next.(Model)
	failed := app.NodeDelay{NodeID: "node-b", Status: app.NodeTestStatusFailed}
	streams[1] <- app.NodeTestEvent{Done: 1, Total: 1, Result: &failed}
	next, wait = model.Update(wait())
	model = next.(Model)
	streams[1] <- app.NodeTestEvent{Done: 1, Total: 1, Finished: true}
	next, command = model.Update(wait())
	model = next.(Model)
	if command == nil || model.nodeTestCurrent != "node-c" || model.nodeResults["node-b"].Status != app.NodeTestStatusFailed {
		t.Fatalf("failed single test did not continue: current=%q result=%+v", model.nodeTestCurrent, model.nodeResults["node-b"])
	}
	_ = command().(nodeTestStartedMsg)
	if got := []domain.NodeID{fake.testRequests[0].NodeID, fake.testRequests[1].NodeID, fake.testRequests[2].NodeID}; !reflect.DeepEqual(got, []domain.NodeID{"node-a", "node-b", "node-c"}) {
		t.Fatalf("single tests ran out of order: %v", got)
	}
}

func TestNodeListSystemStreamErrorStopsSingleQueue(t *testing.T) {
	stream := make(chan app.NodeTestEvent, 1)
	fake := &fakeTUIService{testStreams: []chan app.NodeTestEvent{stream}}
	model := New(fake)
	next, _ := model.enterLoadedGroup(app.Group{
		ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a", "node-b"},
		NodeStates: []app.GroupNodeState{{NodeID: "node-a", Testable: true}, {NodeID: "node-b", Testable: true}},
	})
	model = next.(Model)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	next, wait := model.Update(command())
	model = next.(Model)
	model.actionIndex = 1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	stream <- app.NodeTestEvent{Err: errors.New("stream broken"), Finished: true}
	next, command = model.Update(wait())
	model = next.(Model)
	if command != nil || model.nodeTestMode != nodeTestIdle || len(model.nodeTestQueue) != 0 || !strings.Contains(model.switchTestMessage, "stream broken") {
		t.Fatalf("system stream error did not stop queue: command=%v model=%+v", command != nil, model)
	}
}

func TestNodeListUnexpectedSingleStreamCloseContinuesQueue(t *testing.T) {
	first := make(chan app.NodeTestEvent)
	second := make(chan app.NodeTestEvent)
	fake := &fakeTUIService{testStreams: []chan app.NodeTestEvent{first, second}}
	model := New(fake)
	next, _ := model.enterLoadedGroup(app.Group{
		ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a", "node-b"},
		NodeStates: []app.GroupNodeState{{NodeID: "node-a", Testable: true}, {NodeID: "node-b", Testable: true}},
	})
	model = next.(Model)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	next, wait := model.Update(command())
	model = next.(Model)
	model.actionIndex = 1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	close(first)
	next, command = model.Update(wait())
	model = next.(Model)
	if command == nil || model.nodeTestCurrent != "node-b" || model.nodeTestMode != nodeTestSingle {
		t.Fatalf("closed stream did not advance queue: command=%v model=%+v", command != nil, model)
	}
	_ = command().(nodeTestStartedMsg)
}

func TestNodeListSingleAndBatchTestsPreemptEachOther(t *testing.T) {
	streams := []chan app.NodeTestEvent{
		make(chan app.NodeTestEvent),
		make(chan app.NodeTestEvent),
		make(chan app.NodeTestEvent),
		make(chan app.NodeTestEvent),
	}
	fake := &fakeTUIService{testStreams: streams}
	model := New(fake)
	next, _ := model.enterLoadedGroup(app.Group{
		ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a", "node-b"},
		NodeStates: []app.GroupNodeState{{NodeID: "node-a", Testable: true}, {NodeID: "node-b", Testable: true}},
	})
	model = next.(Model)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	oldGeneration := model.switchTestGeneration
	_ = command().(nodeTestStartedMsg)
	model.actionIndex = 1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)

	next, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	model = next.(Model)
	if command == nil || model.nodeTestMode != nodeTestBatch || len(model.nodeTestQueue) != 0 {
		t.Fatalf("batch did not preempt single queue: command=%v model=%+v", command != nil, model)
	}
	select {
	case <-fake.testContexts[0].Done():
	default:
		t.Fatal("batch did not cancel active single context")
	}
	_ = command().(nodeTestStartedMsg)
	stale := app.NodeDelay{NodeID: "node-a", Status: app.NodeTestStatusSuccess, Delay: time.Millisecond}
	next, _ = model.Update(switchNodeTestMsg{generation: oldGeneration, event: app.NodeTestEvent{Done: 1, Total: 1, Result: &stale, Finished: true}, ok: true})
	model = next.(Model)
	if model.nodeTestMode != nodeTestBatch {
		t.Fatalf("stale single completion changed batch state: %+v", model)
	}
	next, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	model = next.(Model)
	if command == nil || model.nodeTestMode != nodeTestBatch {
		t.Fatalf("batch restart did not start: command=%v model=%+v", command != nil, model)
	}
	select {
	case <-fake.testContexts[1].Done():
	default:
		t.Fatal("batch restart did not cancel previous batch context")
	}
	_ = command().(nodeTestStartedMsg)

	next, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	if command == nil || model.nodeTestMode != nodeTestSingle || model.nodeTestCurrent != "node-b" {
		t.Fatalf("single did not preempt batch: command=%v model=%+v", command != nil, model)
	}
	select {
	case <-fake.testContexts[2].Done():
	default:
		t.Fatal("single did not cancel active batch context")
	}
	_ = command().(nodeTestStartedMsg)
	if fake.testRequests[1].NodeID != "" || fake.testRequests[1].Concurrency != 5 || fake.testRequests[1].Limit != 0 || fake.testRequests[2].NodeID != "" || fake.testRequests[3].NodeID != "node-b" {
		t.Fatalf("unexpected preemption requests: %+v", fake.testRequests)
	}
}

func TestNodeListEscapeCancelsBeforeLeavingAndStaleEventsAreIgnored(t *testing.T) {
	fake := &fakeTUIService{}
	model := New(fake)
	next, _ := model.enterLoadedGroup(app.Group{
		ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"node-a", "node-b"},
		NodeStates: []app.GroupNodeState{{NodeID: "node-a", Testable: true}, {NodeID: "node-b", Testable: true}},
	})
	model = next.(Model)
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	started := command().(nodeTestStartedMsg)
	next, _ = model.Update(started)
	model = next.(Model)
	model.actionIndex = 1
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	oldGeneration := model.switchTestGeneration

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = next.(Model)
	if model.actionCtx != "group_nodes" || model.nodeTestMode != nodeTestIdle || len(model.nodeTestQueue) != 0 || model.switchTestGeneration == oldGeneration {
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

func TestNodeListCursorRowUsesDedicatedFullWidthHighlight(t *testing.T) {
	model := New(&fakeTUIService{})
	model.width = 80
	model.currentNodeName = "node-a"

	selected := model.renderNodeListRow("node-a", true)
	plain := model.renderNodeListRow("node-b", false)
	if got := nodeCursorStyle.GetBackground(); got != lipgloss.Color("#0B7285") {
		t.Fatalf("cursor background = %v, want deep cyan", got)
	}
	if got := nodeCursorStyle.GetForeground(); got != lipgloss.Color("#FFFFFF") {
		t.Fatalf("cursor foreground = %v, want white", got)
	}
	if lipgloss.Width(selected) != model.width-2 || lipgloss.Width(plain) != model.width-2 {
		t.Fatalf("node row widths = %d, %d, want %d", lipgloss.Width(selected), lipgloss.Width(plain), model.width-2)
	}
}

func TestNodeListCursorHighlightMovesWithActionIndex(t *testing.T) {
	model := New(&fakeTUIService{})
	model.width = 80
	next, _ := model.enterLoadedGroup(app.Group{
		ID:             "GLOBAL",
		Name:           "GLOBAL",
		SelectedNodeID: "node-a",
		NodeIDs:        []domain.NodeID{"node-a", "node-b"},
		NodeStates:     []app.GroupNodeState{{NodeID: "node-a", Testable: true}, {NodeID: "node-b", Testable: true}},
	})
	model = next.(Model)
	if got := model.renderNodeListRow(model.actionItems[model.actionIndex], true); lipgloss.Width(got) != model.width-2 {
		t.Fatalf("initial cursor row width = %d, want %d", lipgloss.Width(got), model.width-2)
	}

	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = next.(Model)
	if model.actionIndex != 1 {
		t.Fatalf("cursor index = %d, want 1", model.actionIndex)
	}
	if got := model.renderNodeListRow(model.actionItems[0], false); lipgloss.Width(got) != model.width-2 {
		t.Fatalf("previous row width = %d, want %d", lipgloss.Width(got), model.width-2)
	}
	if got := model.renderNodeListRow(model.actionItems[model.actionIndex], true); lipgloss.Width(got) != model.width-2 {
		t.Fatalf("new cursor row width = %d, want %d", lipgloss.Width(got), model.width-2)
	}
}

func TestNodeListNarrowActiveFooterKeepsModeVisibleWithinTerminalWidth(t *testing.T) {
	model := New(&fakeTUIService{})
	model.width = 32
	next, _ := model.enterLoadedGroup(app.Group{
		ID: "GLOBAL", Name: "GLOBAL", NodeIDs: []domain.NodeID{"这是一个非常长的中文节点名称"},
		NodeStates: []app.GroupNodeState{{NodeID: "这是一个非常长的中文节点名称", Testable: true}},
	})
	model = next.(Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	model = next.(Model)
	lines := strings.Split(model.View(), "\n")
	found := false
	for _, line := range lines {
		if !strings.Contains(line, "单测") {
			continue
		}
		found = true
		if lipgloss.Width(line) > model.width-2 {
			t.Fatalf("active footer overflowed: width=%d line=%q", lipgloss.Width(line), line)
		}
	}
	if !found {
		t.Fatalf("narrow active footer lost test mode: %s", model.View())
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
	for _, expected := range []string{"监听入口：mixed 127.0.0.1:7890", "系统代理：gnome.http=mismatched", "CLI 环境代理：env.HTTP_PROXY=matched"} {
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

func TestConfigMenuIncludesRuleSetEntry(t *testing.T) {
	items := menuActions("配置管理")
	for _, item := range items {
		if item == "CN 规则集" {
			return
		}
	}
	t.Fatalf("CN 规则集 entry missing: %v", items)
}
