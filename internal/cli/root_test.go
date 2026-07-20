package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

type fakeTUIRunner struct {
	calls   int
	err     error
	streams IOStreams
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
	for _, future := range []string{"daemon", "profile", "subscription"} {
		if strings.Contains(help, future) {
			t.Fatalf("help contains future command %q: %q", future, help)
		}
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
