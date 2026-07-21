package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

func TestParseOutputFormat(t *testing.T) {
	for _, value := range []OutputFormat{OutputTable, OutputJSON} {
		got, err := ParseOutputFormat(value.String())
		if err != nil {
			t.Fatalf("parse %q: %v", value, err)
		}
		if got != value {
			t.Fatalf("got %q want %q", got, value)
		}
	}

	_, err := ParseOutputFormat("yaml")
	var appErr *app.Error
	if !errors.As(err, &appErr) || appErr.Code != app.ErrorCodeInvalidArgument {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestBindOutputOptions(t *testing.T) {
	cmd := &cobra.Command{Use: "fixture"}
	var options OutputOptions
	BindOutputOptions(cmd, &options)
	if err := cmd.ParseFlags([]string{"--output", "json", "--show-secrets"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if options.Format != OutputJSON || !options.ShowSecrets {
		t.Fatalf("unexpected options: %#v", options)
	}
}

func TestBindOutputOptionsClassifiesInvalidFormat(t *testing.T) {
	cmd := &cobra.Command{Use: "fixture", RunE: func(*cobra.Command, []string) error { return nil }}
	var options OutputOptions
	BindOutputOptions(cmd, &options)
	cmd.SetArgs([]string{"--output", "yaml"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	err := cmd.Execute()
	var appErr *app.Error
	if !errors.As(err, &appErr) || appErr.Classification() != app.ErrorCategoryInvalidArgument {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestPresenterWritesTable(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	presenter := NewPresenter(&stdout, &stderr, OutputOptions{Format: OutputTable})
	err := presenter.WriteResult(Result{
		Kind: "Fixture",
		Table: func(w io.Writer, _ bool) error {
			_, err := fmt.Fprintln(w, "VALUE")
			return err
		},
		Warnings: []string{"需要注意"},
	})
	if err != nil {
		t.Fatalf("write result: %v", err)
	}
	if got := stdout.String(); got != "VALUE\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if got := stderr.String(); got != "警告: 需要注意\n" {
		t.Fatalf("unexpected stderr: %q", got)
	}
}

func TestPresenterWritesJSONEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	presenter := NewPresenter(&stdout, &stderr, OutputOptions{Format: OutputJSON})
	err := presenter.WriteResult(Result{
		Kind: "CoreStatus",
		Data: func(bool) any { return map[string]any{"state": "running"} },
	})
	if err != nil {
		t.Fatalf("write result: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	var got struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Data       map[string]any `json:"data"`
		Warnings   []string       `json:"warnings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode stdout: %v; output=%q", err, stdout.String())
	}
	if got.APIVersion != APIVersion || got.Kind != "CoreStatus" || got.Data["state"] != "running" {
		t.Fatalf("unexpected envelope: %#v", got)
	}
	if got.Warnings == nil || len(got.Warnings) != 0 {
		t.Fatalf("warnings must be an empty array: %#v", got.Warnings)
	}
}

func TestPresenterRejectsUnknownFormat(t *testing.T) {
	presenter := NewPresenter(nil, nil, OutputOptions{Format: "yaml"})
	err := presenter.WriteResult(Result{Kind: "Fixture"})
	var appErr *app.Error
	if !errors.As(err, &appErr) || appErr.Code != app.ErrorCodeInvalidArgument {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestSecretIsRedactedUnlessExplicitlyShown(t *testing.T) {
	secretValue := "token-1234"
	secret := NewSecret(secretValue)
	if got := secret.String(); got != Redacted {
		t.Fatalf("unexpected default display: %q", got)
	}
	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal secret: %v", err)
	}
	if strings.Contains(string(encoded), secretValue) {
		t.Fatalf("marshal leaked secret: %q", encoded)
	}
	if got := secret.Display(true); got != secretValue {
		t.Fatalf("unexpected revealed value: %q", got)
	}
}

func TestPresenterControlsSecretsInSuccessOutput(t *testing.T) {
	const value = "uuid-1111-2222"
	secret := NewSecret(value)

	tests := []struct {
		name        string
		format      OutputFormat
		showSecrets bool
	}{
		{name: "table redacted", format: OutputTable},
		{name: "table revealed", format: OutputTable, showSecrets: true},
		{name: "json redacted", format: OutputJSON},
		{name: "json revealed", format: OutputJSON, showSecrets: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			presenter := NewPresenter(&stdout, nil, OutputOptions{
				Format:      tt.format,
				ShowSecrets: tt.showSecrets,
			})
			err := presenter.WriteResult(Result{
				Kind: "SecretFixture",
				Data: func(show bool) any {
					return map[string]any{"credential": secret.Display(show)}
				},
				Table: func(w io.Writer, show bool) error {
					_, err := fmt.Fprintln(w, secret.Display(show))
					return err
				},
			})
			if err != nil {
				t.Fatalf("write result: %v", err)
			}
			containsSecret := strings.Contains(stdout.String(), value)
			if containsSecret != tt.showSecrets {
				t.Fatalf("secret visibility mismatch: show=%v output=%q", tt.showSecrets, stdout.String())
			}
		})
	}
}

func TestURLSecretRedactsCredentialsAndLocation(t *testing.T) {
	raw := "https://user:password@example.com/subscription/token?access_token=abc#private"
	secret := NewURLSecret(raw)
	got := secret.String()
	if got != "https://example.com/redacted" {
		t.Fatalf("unexpected redacted URL: %q", got)
	}
	for _, sensitive := range []string{"user", "password", "subscription", "token", "abc", "private"} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("redacted URL contains %q: %q", sensitive, got)
		}
	}
	if got := secret.Display(true); got != raw {
		t.Fatalf("unexpected revealed URL: %q", got)
	}
}

func TestRedactTextHidesProxyURIsUUIDsAndCredentials(t *testing.T) {
	raw := "connect vless://user@example.com:443?token=abc https://example.com/sub/private?access=abc uuid=550e8400-e29b-41d4-a716-446655440000 password=hunter2"
	redacted := RedactText(raw)
	for _, secret := range []string{"user@example.com", "token=abc", "/sub/private", "access=abc", "550e8400-e29b-41d4-a716-446655440000", "hunter2"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted text contains %q: %q", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "connect") || !strings.Contains(redacted, Redacted) {
		t.Fatalf("redaction removed useful context: %q", redacted)
	}
}
