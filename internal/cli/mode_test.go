package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func TestModeStatusJSONUsesStableEnvelopeAndWarnings(t *testing.T) {
	fake := &fakeCapabilityAPI{modeStatus: app.RoutingModeStatus{
		ProfileID: "legacy-mihomo", ConfigMode: domain.RoutingModeRule,
		CoreState: domain.CoreStateStopped, EffectiveGroup: "🌐 代理", NextStart: true,
		RuleSets: []app.RuleSetHealth{{Name: "mm-cn-domain"}, {Name: "mm-cn-ip"}},
		Warnings: []string{"mihomo core 未运行，配置将在下次启动时生效"},
	}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"mode", "status", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var envelope struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Data       map[string]any `json:"data"`
		Warnings   []string       `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &envelope); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if envelope.APIVersion != "mm/v1" || envelope.Kind != "RoutingModeStatus" || envelope.Data["configMode"] != "rule" || len(envelope.Warnings) != 1 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestModeSetValidatesModeAndPassesCloseConnections(t *testing.T) {
	fake := &fakeCapabilityAPI{modeStatus: app.RoutingModeStatus{
		ProfileID: "legacy-mihomo", ConfigMode: domain.RoutingModeDirect,
		CoreState: domain.CoreStateStopped, NextStart: true,
	}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"mode", "set", "DIRECT", "--profile", "legacy-mihomo", "--close-connections", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 || fake.setMode.Mode != domain.RoutingModeDirect || fake.setMode.ProfileID != "legacy-mihomo" || !fake.setMode.CloseConnections {
		t.Fatalf("unexpected set: code=%d request=%+v stderr=%q", code, fake.setMode, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"kind":"RoutingModeChange"`) {
		t.Fatalf("unexpected output: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"mode", "set", "invalid"}, nil, &stdout, &stderr)
	if code != ExitInvalidArgument || stdout.Len() != 0 || !strings.Contains(stderr.String(), "global、rule 或 direct") {
		t.Fatalf("unexpected invalid result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestModeStatusTableDistinguishesStoppedRuntime(t *testing.T) {
	fake := &fakeCapabilityAPI{modeStatus: app.RoutingModeStatus{
		ProfileID: "legacy-mihomo", ConfigMode: domain.RoutingModeRule,
		CoreState: domain.CoreStateStopped, EffectiveGroup: "🌐 代理", NextStart: true,
		RuleSets: []app.RuleSetHealth{{Name: "mm-cn-domain"}},
		Warnings: []string{"下次启动生效"},
	}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"mode", "status"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "运行模式: 不可用 (Core 已停止)") || !strings.Contains(stdout.String(), "待 Core 启动后核验") || !strings.Contains(stdout.String(), "警告: 下次启动生效") {
		t.Fatalf("unexpected table: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestModeErrorUsesStableExitCode(t *testing.T) {
	fake := &fakeCapabilityAPI{err: &app.Error{Category: app.ErrorCategoryConflict, Code: app.ErrorCode("PROFILE_MODE_UNSUPPORTED"), Message: "当前档案不支持此操作"}}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"mode", "status", "--output=json"}, nil, &stdout, &stderr)
	if code != ExitConflict || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"PROFILE_MODE_UNSUPPORTED"`) {
		t.Fatalf("unexpected error: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestModePartialFailureKeepsSuccessfulStatusInErrorDetails(t *testing.T) {
	fake := &fakeCapabilityAPI{
		modeStatus: app.RoutingModeStatus{
			ProfileID: "legacy-mihomo", ConfigMode: domain.RoutingModeDirect,
			CoreState: domain.CoreStateRunning, OperationPhase: "connections_close_failed",
			Warnings: []string{"模式已生效，但关闭现有连接失败"},
		},
		err: &app.Error{Category: app.ErrorCategoryUpstreamFailure, Code: app.ErrorCode("CONNECTION_CLOSE_FAILED"), Message: "模式已生效，但关闭活动连接失败"},
	}
	var stdout, stderr strings.Builder
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"mode", "set", "direct", "--close-connections", "--output=json"}, nil, &stdout, &stderr)
	if code != ExitUpstreamFailure || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"configMode":"direct"`) || !strings.Contains(stderr.String(), `"operationPhase":"connections_close_failed"`) {
		t.Fatalf("unexpected partial failure: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestModeErrorsIncludeExecutableRecoveryHints(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{err: &app.Error{Code: app.ErrorCodeDaemonUnavailable, Message: "daemon 不可用"}, want: "mm daemon start"},
		{err: &app.Error{Code: app.ErrorCodeNotFound, Message: "资源不存在"}, want: "mm migrate plan"},
	}
	for _, tt := range tests {
		fake := &fakeCapabilityAPI{err: tt.err}
		var stdout, stderr strings.Builder
		_ = Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"mode", "status"}, nil, &stdout, &stderr)
		if !strings.Contains(stderr.String(), tt.want) {
			t.Fatalf("missing hint %q: %q", tt.want, stderr.String())
		}
	}
}

func TestModeHelpDoesNotExposeSecretFlag(t *testing.T) {
	fake := &fakeCapabilityAPI{}
	for _, args := range [][]string{{"mode", "status", "--help"}, {"mode", "set", "--help"}} {
		var stdout, stderr strings.Builder
		code := Execute(context.Background(), Dependencies{Capabilities: fake}, args, nil, &stdout, &stderr)
		if code != 0 || stderr.Len() != 0 || strings.Contains(stdout.String(), "show-secrets") {
			t.Fatalf("unexpected help for %v: code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
