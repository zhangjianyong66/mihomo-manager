package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: 0},
		{name: "internal", err: &app.Error{Code: app.ErrorCodeInternal}, want: ExitInternal},
		{name: "invalid argument", err: &app.Error{Code: app.ErrorCodeInvalidArgument}, want: ExitInvalidArgument},
		{name: "not found", err: &app.Error{Code: app.ErrorCodeNotFound}, want: ExitNotFound},
		{name: "conflict", err: &app.Error{Code: app.ErrorCodeConflict}, want: ExitConflict},
		{name: "daemon", err: &app.Error{Code: app.ErrorCodeDaemonUnavailable}, want: ExitDaemonUnavailable},
		{name: "validation", err: &app.Error{Code: app.ErrorCodeValidationFailed}, want: ExitValidationFailed},
		{name: "permission", err: &app.Error{Code: app.ErrorCodePermissionDenied}, want: ExitPermissionDenied},
		{name: "upstream", err: &app.Error{Code: app.ErrorCodeUpstreamFailure}, want: ExitUpstreamFailure},
		{name: "wrapped", err: fmt.Errorf("wrapped: %w", &app.Error{Code: app.ErrorCodeNotFound}), want: ExitNotFound},
		{name: "specific conflict code", err: &app.Error{Category: app.ErrorCategoryConflict, Code: app.ErrorCode("PROFILE_CONFLICT")}, want: ExitConflict},
		{name: "unknown", err: errors.New("unknown"), want: ExitInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func TestPresenterWritesTableErrorToStderr(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	presenter := NewPresenter(&stdout, &stderr, OutputOptions{Format: OutputTable})
	err := presenter.WriteError(&app.Error{Code: app.ErrorCodeConflict, Message: "档案正在切换"})
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if got := stderr.String(); got != "档案正在切换\n" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestPresenterWritesJSONErrorWithoutLeakingSecrets(t *testing.T) {
	const token = "token-1234"
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	presenter := NewPresenter(&stdout, &stderr, OutputOptions{Format: OutputJSON})
	err := presenter.WriteError(&app.Error{
		Code:      app.ErrorCodeUpstreamFailure,
		Message:   "订阅更新失败",
		Retryable: true,
		Details: map[string]any{
			"token": NewSecret(token),
			"nested": []any{
				map[string]any{"url": NewURLSecret("https://example.com/sub/token?key=abc")},
			},
		},
	})
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), token) || strings.Contains(stderr.String(), "key=abc") {
		t.Fatalf("stderr leaked secret: %q", stderr.String())
	}
	var got struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Error      struct {
			Code      app.ErrorCode  `json:"code"`
			Message   string         `json:"message"`
			Retryable bool           `json:"retryable"`
			Details   map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("decode stderr: %v; output=%q", err, stderr.String())
	}
	if got.APIVersion != APIVersion || got.Kind != "Error" || got.Error.Code != app.ErrorCodeUpstreamFailure || !got.Error.Retryable {
		t.Fatalf("unexpected envelope: %#v", got)
	}
	if got.Error.Details["token"] != Redacted {
		t.Fatalf("unexpected redacted details: %#v", got.Error.Details)
	}
}

func TestPresenterCanExplicitlyRevealErrorSecrets(t *testing.T) {
	const token = "token-1234"
	var stderr bytes.Buffer
	presenter := NewPresenter(nil, &stderr, OutputOptions{Format: OutputJSON, ShowSecrets: true})
	if err := presenter.WriteError(&app.Error{
		Code:    app.ErrorCodeInvalidArgument,
		Message: "输入错误",
		Details: map[string]any{"token": NewSecret(token)},
	}); err != nil {
		t.Fatalf("write error: %v", err)
	}
	if !strings.Contains(stderr.String(), token) {
		t.Fatalf("expected explicit secret output: %q", stderr.String())
	}
}

func TestPresenterHidesUnknownErrorDetails(t *testing.T) {
	const sensitive = "password-1234"
	var stderr bytes.Buffer
	presenter := NewPresenter(nil, &stderr, OutputOptions{Format: OutputJSON})
	if err := presenter.WriteError(errors.New(sensitive)); err != nil {
		t.Fatalf("write error: %v", err)
	}
	if strings.Contains(stderr.String(), sensitive) {
		t.Fatalf("unknown error leaked details: %q", stderr.String())
	}
}
