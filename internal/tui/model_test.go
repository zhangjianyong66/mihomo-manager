package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type fakeTUIService struct {
	app.CapabilityAPI
	status      app.RoutingModeStatus
	statusErr   error
	setErr      error
	statusCalls int
	setCalls    int
	request     app.SetRoutingModeRequest
	editOld     string
	editNew     string
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

func (f *fakeTUIService) EditWhitelist(_ context.Context, _, oldValue, newValue string) error {
	f.editOld, f.editNew = oldValue, newValue
	return nil
}

func (f *fakeTUIService) Whitelist(context.Context, string) ([]string, error) {
	return []string{f.editNew}, nil
}

var _ Capabilities = (*fakeTUIService)(nil)

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
