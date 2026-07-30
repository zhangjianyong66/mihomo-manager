package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/ruleset"
)

type fakeRuleSetCapabilities struct {
	*fakeCapabilityAPI
	status app.RuleSetStatus
}

func (f *fakeRuleSetCapabilities) RuleSetStatus(context.Context, string) (app.RuleSetStatus, error) {
	return f.status, nil
}
func (f *fakeRuleSetCapabilities) InstallRuleSets(context.Context, string) <-chan app.RuleSetInstallEvent {
	ch := make(chan app.RuleSetInstallEvent, 2)
	ch <- app.RuleSetInstallEvent{Phase: "publishing"}
	ch <- app.RuleSetInstallEvent{Phase: "succeeded", Status: &f.status, Done: true}
	close(ch)
	return ch
}

func TestRuleSetCommands(t *testing.T) {
	status := app.RuleSetStatus{State: ruleset.StateInstalled, Target: ruleset.Target{Source: "https://example.invalid/rules", Ref: "fixed"}, Domain: ruleset.FileStatus{Path: "/tmp/domain", DigestValid: true}, IP: ruleset.FileStatus{Path: "/tmp/ip", DigestValid: true}}
	service := &fakeRuleSetCapabilities{fakeCapabilityAPI: &fakeCapabilityAPI{}, status: status}
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), Dependencies{Capabilities: service}, []string{"ruleset", "status", "--output", "table"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("status exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "installed") || !strings.Contains(stdout.String(), "fixed") {
		t.Fatalf("status output=%q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Execute(context.Background(), Dependencies{Capabilities: service}, []string{"ruleset", "install", "--output", "json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("install exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"kind":"RuleSetInstall"`) || strings.Contains(stdout.String(), "阶段") {
		t.Fatalf("install output=%q", stdout.String())
	}
}
