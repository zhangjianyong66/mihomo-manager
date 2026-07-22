package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type fakeTUIRunner struct {
	calls   int
	err     error
	streams IOStreams
}

type fakeDaemonClient struct {
	status app.DaemonStatus
	err    error
}

func (f fakeDaemonClient) Status(context.Context) (app.DaemonStatus, error) { return f.status, f.err }

type sequenceDaemonClient struct {
	statuses []app.DaemonStatus
	index    int
}

func (f *sequenceDaemonClient) Status(context.Context) (app.DaemonStatus, error) {
	if len(f.statuses) == 0 {
		return app.DaemonStatus{}, errors.New("missing daemon status fixture")
	}
	index := f.index
	if index >= len(f.statuses) {
		index = len(f.statuses) - 1
	}
	f.index++
	return f.statuses[index], nil
}

type fakeDaemonCore struct{ actions []string }

func (f *fakeDaemonCore) CoreAction(_ context.Context, _ string, action string) error {
	f.actions = append(f.actions, action)
	return nil
}

type fakeDaemonRunner struct{ calls int }

func (f *fakeDaemonRunner) Run(_ context.Context, diagnostics io.Writer) error {
	f.calls++
	_, _ = io.WriteString(diagnostics, "daemon diagnostic\n")
	return nil
}

type fakeDaemonController struct{ result app.DaemonControlResult }

func (f fakeDaemonController) Status(context.Context) (app.DaemonControlResult, error) {
	return f.result, nil
}
func (f fakeDaemonController) Enable(context.Context) (app.DaemonControlResult, error) {
	return f.result, nil
}
func (f fakeDaemonController) Disable(context.Context) (app.DaemonControlResult, error) {
	return f.result, nil
}
func (f fakeDaemonController) Start(context.Context) (app.DaemonControlResult, error) {
	return f.result, nil
}
func (f fakeDaemonController) Stop(context.Context) (app.DaemonControlResult, error) {
	return f.result, nil
}
func (f fakeDaemonController) Restart(context.Context) (app.DaemonControlResult, error) {
	return f.result, nil
}

func (r *fakeTUIRunner) Run(_ context.Context, streams IOStreams) error {
	r.calls++
	r.streams = streams
	return r.err
}

func TestExecuteRunsTUIFromRootAndExplicitCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"tui"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			runner := &fakeTUIRunner{}
			stdin := strings.NewReader("input")
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			code := Execute(context.Background(), Dependencies{TUI: runner}, args, stdin, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("unexpected exit code: %d; stderr=%q", code, stderr.String())
			}
			if runner.calls != 1 {
				t.Fatalf("runner calls: got %d want 1", runner.calls)
			}
			if runner.streams.In != stdin || runner.streams.Out != &stdout || runner.streams.ErrOut != &stderr {
				t.Fatalf("streams were not injected: %#v", runner.streams)
			}
		})
	}
}

func TestExecuteHelpDoesNotRunTUI(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "root", args: []string{"--help"}},
		{name: "tui", args: []string{"tui", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeTUIRunner{}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Execute(context.Background(), Dependencies{TUI: runner}, tt.args, nil, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("unexpected exit code: %d; stderr=%q", code, stderr.String())
			}
			if runner.calls != 0 {
				t.Fatalf("help ran TUI %d times", runner.calls)
			}
			if stdout.Len() == 0 || stderr.Len() != 0 {
				t.Fatalf("unexpected help writers: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRootHelpOnlyListsImplementedProductCommand(t *testing.T) {
	runner := &fakeTUIRunner{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{TUI: runner}, []string{"--help"}, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("unexpected exit code: %d", code)
	}
	help := stdout.String()
	if !strings.Contains(help, "tui") {
		t.Fatalf("help does not list tui: %q", help)
	}
	if !strings.Contains(help, "daemon") {
		t.Fatalf("help does not list daemon: %q", help)
	}
	for _, future := range []string{"profile", "subscription"} {
		if strings.Contains(help, future) {
			t.Fatalf("help contains future command %q: %q", future, help)
		}
	}
}

func TestDaemonStatusJSONAndRunWriterSeparation(t *testing.T) {
	runner := &fakeDaemonRunner{}
	service := &app.DaemonService{
		Client:     fakeDaemonClient{status: app.DaemonStatus{ProtocolVersion: 1, State: "running", PID: 42, StartedAt: time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC), SchemaVersion: 2}},
		Runner:     runner,
		Controller: fakeDaemonController{result: app.DaemonControlResult{Installed: true, Message: "ok"}},
	}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Daemon: service}, []string{"daemon", "status", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), `"kind":"DaemonStatus"`) || stderr.Len() != 0 {
		t.Fatalf("unexpected status result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	code = Execute(context.Background(), Dependencies{Daemon: service}, []string{"daemon", "run"}, nil, &stdout, &stderr)
	if code != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "diagnostic") || runner.calls != 1 {
		t.Fatalf("unexpected run result: code=%d stdout=%q stderr=%q calls=%d", code, stdout.String(), stderr.String(), runner.calls)
	}
}

func TestDaemonProtocolErrorUsesJSONAndExitFive(t *testing.T) {
	service := &app.DaemonService{Client: fakeDaemonClient{err: &app.Error{Category: app.ErrorCategoryDaemonUnavailable, Code: app.ErrorCodeDaemonUnavailable, Message: "daemon 协议不兼容"}}}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Daemon: service}, []string{"daemon", "status", "--output=json"}, nil, &stdout, &stderr)
	if code != ExitDaemonUnavailable || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"kind":"Error"`) {
		t.Fatalf("unexpected protocol error: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDaemonRestartTableAndJSONUseSharedResult(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	status := func(pid int, started time.Time) app.DaemonStatus {
		return app.DaemonStatus{ProtocolVersion: 1, State: "running", PID: pid, StartedAt: started, Core: app.DaemonCoreStatus{State: domain.CoreStateStopped.String(), ProfileID: "legacy-mihomo"}}
	}
	for _, format := range []string{"table", "json"} {
		t.Run(format, func(t *testing.T) {
			client := &sequenceDaemonClient{statuses: []app.DaemonStatus{status(100, oldTime), status(100, oldTime), status(200, newTime), status(200, newTime)}}
			service := &app.DaemonService{
				Client: client, Core: &fakeDaemonCore{},
				Controller: fakeDaemonController{result: app.DaemonControlResult{Installed: true, Managed: true, Available: true, Active: true, ServiceActive: true, SocketActive: true}},
			}
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), Dependencies{Daemon: service}, []string{"daemon", "restart", "--output", format}, nil, &stdout, &stderr)
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("unexpected restart result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if format == "json" {
				for _, want := range []string{`"kind":"DaemonRestart"`, `"previousDaemonPid":100`, `"daemonPid":200`, `"daemonRestarted":true`} {
					if !strings.Contains(stdout.String(), want) {
						t.Fatalf("json missing %q: %s", want, stdout.String())
					}
				}
			} else if !strings.Contains(stdout.String(), "daemon PID: 100 -> 200") || !strings.Contains(stdout.String(), "Core 状态: stopped -> stopped") {
				t.Fatalf("unexpected table: %s", stdout.String())
			}
		})
	}
}

func TestDaemonRestartBusyUsesConflictExitAndPartialJSON(t *testing.T) {
	started := time.Date(2026, 7, 22, 1, 2, 3, 0, time.UTC)
	service := &app.DaemonService{
		Client: fakeDaemonClient{status: app.DaemonStatus{PID: 100, StartedAt: started, Core: app.DaemonCoreStatus{State: domain.CoreStateStarting.String()}}},
		Core:   &fakeDaemonCore{},
		Controller: fakeDaemonController{result: app.DaemonControlResult{
			Installed: true, Managed: true, Available: true, ServiceActive: true, SocketActive: true,
		}},
	}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Daemon: service}, []string{"daemon", "restart", "--output=json"}, nil, &stdout, &stderr)
	if code != ExitConflict || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"DAEMON_RESTART_CORE_BUSY"`) || !strings.Contains(stderr.String(), `"restart"`) {
		t.Fatalf("unexpected busy result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDaemonRestartUnavailableUsesExitFiveAndPartialJSON(t *testing.T) {
	service := &app.DaemonService{
		Client: &fakeDaemonClient{err: &app.Error{Category: app.ErrorCategoryDaemonUnavailable, Code: app.ErrorCodeDaemonUnavailable, Message: "daemon 协议不兼容"}},
		Core:   &fakeDaemonCore{},
		Controller: fakeDaemonController{result: app.DaemonControlResult{
			Installed: true, Managed: true, Available: true, ServiceActive: true, SocketActive: true,
		}},
	}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Daemon: service}, []string{"daemon", "restart", "--output=json"}, nil, &stdout, &stderr)
	if code != ExitDaemonUnavailable || stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code":"DAEMON_UNAVAILABLE"`) || !strings.Contains(stderr.String(), `"restart"`) {
		t.Fatalf("unexpected unavailable result: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDaemonRestartHelpDoesNotExecuteService(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{}, []string{"daemon", "restart", "--help"}, nil, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "--output") || stderr.Len() != 0 {
		t.Fatalf("unexpected restart help: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestExecuteInvalidInputReturnsCodeTwoWithoutRunningTUI(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown command", args: []string{"status"}},
		{name: "extra tui argument", args: []string{"tui", "extra"}},
		{name: "unknown flag", args: []string{"--unknown"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeTUIRunner{}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Execute(context.Background(), Dependencies{TUI: runner}, tt.args, nil, &stdout, &stderr)
			if code != ExitInvalidArgument {
				t.Fatalf("got exit code %d want %d; stderr=%q", code, ExitInvalidArgument, stderr.String())
			}
			if runner.calls != 0 {
				t.Fatalf("invalid input ran TUI %d times", runner.calls)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "Usage:") {
				t.Fatalf("unexpected writers: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestExecuteMapsRunnerErrors(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantCode    int
		wantMessage string
		hidden      string
	}{
		{
			name:        "application error",
			err:         &app.Error{Code: app.ErrorCodeConflict, Message: "档案正在切换"},
			wantCode:    ExitConflict,
			wantMessage: "档案正在切换",
		},
		{
			name:        "unknown error",
			err:         errors.New("password-1234"),
			wantCode:    ExitInternal,
			wantMessage: "内部错误",
			hidden:      "password-1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeTUIRunner{err: tt.err}
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			code := Execute(context.Background(), Dependencies{TUI: runner}, nil, nil, &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("got exit code %d want %d", code, tt.wantCode)
			}
			if !strings.Contains(stderr.String(), tt.wantMessage) {
				t.Fatalf("stderr does not contain %q: %q", tt.wantMessage, stderr.String())
			}
			if tt.hidden != "" && strings.Contains(stderr.String(), tt.hidden) {
				t.Fatalf("stderr leaked %q: %q", tt.hidden, stderr.String())
			}
		})
	}
}

func TestNewRootRejectsMissingRunner(t *testing.T) {
	root := NewRoot(Dependencies{})
	root.SetArgs(nil)
	root.SetIn(io.Reader(nil))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.ExecuteContext(context.Background())
	var appErr *app.Error
	if !errors.As(err, &appErr) || appErr.Code != app.ErrorCodeInternal {
		t.Fatalf("unexpected error: %#v", err)
	}
}
