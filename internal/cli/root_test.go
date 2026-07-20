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
