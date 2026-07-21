package legacy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/store"
)

type fakeRoutingRuntime struct {
	mode              domain.RoutingMode
	calls             []string
	reloads           int
	closeErr          error
	failTargetSet     bool
	failRestoreReload bool
	badRules          bool
}

type failingExpectationRepository struct {
	*store.Store
	failures int
}

func (r *failingExpectationRepository) UpdateLegacyFileExpectations(ctx context.Context, migration domain.LegacyMigration) error {
	if r.failures > 0 {
		r.failures--
		return errors.New("expectation update failed")
	}
	return r.Store.UpdateLegacyFileExpectations(ctx, migration)
}

func (r *fakeRoutingRuntime) Mode(context.Context) (domain.RoutingMode, error) {
	r.calls = append(r.calls, "mode")
	return r.mode, nil
}

func (r *fakeRoutingRuntime) SetMode(_ context.Context, mode domain.RoutingMode) error {
	r.calls = append(r.calls, "set:"+mode.String())
	if r.failTargetSet && mode == domain.RoutingModeRule {
		r.failTargetSet = false
		return errors.New("set target failed")
	}
	r.mode = mode
	return nil
}

func (r *fakeRoutingRuntime) Reload(context.Context, string) error {
	r.reloads++
	r.calls = append(r.calls, "reload")
	if r.failRestoreReload && r.reloads > 1 {
		return errors.New("restore reload failed")
	}
	return nil
}

func (r *fakeRoutingRuntime) LoadedRules(context.Context) ([]mihomo.RuntimeRule, error) {
	r.calls = append(r.calls, "rules")
	if r.badRules {
		return []mihomo.RuntimeRule{{Type: "Match", Proxy: "DIRECT"}}, nil
	}
	return []mihomo.RuntimeRule{
		{Type: "RuleSet", Payload: mihomo.CNDomainProviderName, Proxy: "DIRECT"},
		{Type: "RuleSet", Payload: mihomo.CNIPProviderName, Proxy: "DIRECT"},
		{Type: "Match", Proxy: mihomo.ProxyGroupName},
	}, nil
}

func (r *fakeRoutingRuntime) ConnectionCount(context.Context) (int, error) {
	r.calls = append(r.calls, "connections")
	return 3, nil
}

func (r *fakeRoutingRuntime) CloseConnections(context.Context) error {
	r.calls = append(r.calls, "close")
	return r.closeErr
}

func (r *fakeRoutingRuntime) SelectedProxy(_ context.Context, group string) (string, error) {
	r.calls = append(r.calls, "selected:"+group)
	return "node-a", nil
}

func TestSetRoutingModeStoppedRoundTripAndInvalidInput(t *testing.T) {
	for _, mode := range []domain.RoutingMode{domain.RoutingModeGlobal, domain.RoutingModeRule, domain.RoutingModeDirect} {
		t.Run(mode.String(), func(t *testing.T) {
			compat, migration, _, configFile, original := setupModeCompatibility(t)
			status, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{Mode: mode, CoreState: domain.CoreStateStopped})
			if err != nil {
				t.Fatal(err)
			}
			if status.ConfigMode != mode || !status.NextStart || status.RuntimeAvailable || status.CoreState != domain.CoreStateStopped {
				t.Fatalf("unexpected stopped status: %+v", status)
			}
			got, err := compat.RoutingModeStatus(context.Background(), migration.ID, domain.CoreStateStopped, nil)
			if err != nil || got.ConfigMode != mode {
				t.Fatalf("round trip status=%+v err=%v", got, err)
			}
			if mode == domain.RoutingModeGlobal && string(mustReadFile(t, configFile)) == string(original) {
				t.Fatal("mode transaction did not synthesize the M2 routing policy")
			}
		})
	}

	compat, migration, _, configFile, original := setupModeCompatibility(t)
	if _, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{Mode: "invalid", CoreState: domain.CoreStateStopped}); !errors.Is(err, ErrInvalidRoutingMode) {
		t.Fatalf("invalid mode error=%v", err)
	}
	if got := mustReadFile(t, configFile); string(got) != string(original) {
		t.Fatal("invalid mode changed the configuration")
	}
}

func TestSetRoutingModeRunningVerifiesAndClosesOnlyWhenRequested(t *testing.T) {
	for _, closeConnections := range []bool{false, true} {
		name := "keep-connections"
		if closeConnections {
			name = "close-connections"
		}
		t.Run(name, func(t *testing.T) {
			compat, migration, stateStore, _, _ := setupModeCompatibility(t)
			runtime := &fakeRoutingRuntime{mode: domain.RoutingModeGlobal}
			status, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{
				Mode: domain.RoutingModeRule, CoreState: domain.CoreStateRunning,
				Runtime: runtime, CloseConnections: closeConnections,
			})
			if err != nil {
				t.Fatal(err)
			}
			if status.RuntimeMode == nil || *status.RuntimeMode != domain.RoutingModeRule || status.EffectiveGroup != mihomo.ProxyGroupName || status.EffectiveNode != "node-a" {
				t.Fatalf("unexpected running status: %+v", status)
			}
			if status.ActiveConnections != map[bool]int{false: 3, true: 0}[closeConnections] || status.ConnectionsClosed != closeConnections {
				t.Fatalf("unexpected connection status: %+v", status)
			}
			closed := false
			for _, call := range runtime.calls {
				closed = closed || call == "close"
			}
			if closed != closeConnections {
				t.Fatalf("runtime calls=%v", runtime.calls)
			}
			operation, err := stateStore.GetOperation(context.Background(), status.OperationID)
			if err != nil || operation.State != domain.OperationStateSucceeded || operation.Phase != "succeeded" {
				t.Fatalf("operation=%+v err=%v", operation, err)
			}
		})
	}
}

func TestSetRoutingModeRestoresFileAndRuntimeOnFailure(t *testing.T) {
	compat, migration, stateStore, configFile, original := setupModeCompatibility(t)
	runtime := &fakeRoutingRuntime{mode: domain.RoutingModeGlobal, failTargetSet: true}
	status, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{
		Mode: domain.RoutingModeRule, CoreState: domain.CoreStateRunning, Runtime: runtime,
	})
	if err == nil || errors.Is(err, ErrRestoreFailed) {
		t.Fatalf("expected restored mutation error, got status=%+v err=%v", status, err)
	}
	if got := mustReadFile(t, configFile); string(got) != string(original) {
		t.Fatalf("config was not restored:\n%s", got)
	}
	if runtime.mode != domain.RoutingModeGlobal {
		t.Fatalf("runtime mode=%s", runtime.mode)
	}
	operation, getErr := stateStore.GetOperation(context.Background(), status.OperationID)
	if getErr != nil || operation.State != domain.OperationStateRolledBack || operation.Phase != "restored" {
		t.Fatalf("operation=%+v err=%v", operation, getErr)
	}
}

func TestSetRoutingModeRestoresOnRuntimeVerificationFailure(t *testing.T) {
	compat, migration, _, configFile, original := setupModeCompatibility(t)
	runtime := &fakeRoutingRuntime{mode: domain.RoutingModeGlobal, badRules: true}
	status, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{
		Mode: domain.RoutingModeRule, CoreState: domain.CoreStateRunning, Runtime: runtime,
	})
	if !errors.Is(err, ErrModeRuntimeMismatch) || errors.Is(err, ErrRestoreFailed) || status.OperationPhase != "restored" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if got := mustReadFile(t, configFile); string(got) != string(original) || runtime.mode != domain.RoutingModeGlobal {
		t.Fatalf("verification failure was not restored: mode=%s config=%s", runtime.mode, got)
	}
}

func TestSetRoutingModeReportsRestoreFailure(t *testing.T) {
	compat, migration, stateStore, _, _ := setupModeCompatibility(t)
	runtime := &fakeRoutingRuntime{mode: domain.RoutingModeGlobal, failTargetSet: true, failRestoreReload: true}
	status, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{
		Mode: domain.RoutingModeRule, CoreState: domain.CoreStateRunning, Runtime: runtime,
	})
	if !errors.Is(err, ErrRestoreFailed) || status.CoreState != domain.CoreStateFailed || status.OperationPhase != "restore_failed" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	operation, getErr := stateStore.GetOperation(context.Background(), status.OperationID)
	if getErr != nil || operation.State != domain.OperationStateFailed || operation.ErrorCode != "RESTORE_FAILED" {
		t.Fatalf("operation=%+v err=%v", operation, getErr)
	}
}

func TestSetRoutingModeConnectionCloseFailureDoesNotRollback(t *testing.T) {
	compat, migration, _, configFile, original := setupModeCompatibility(t)
	runtime := &fakeRoutingRuntime{mode: domain.RoutingModeGlobal, closeErr: errors.New("close failed")}
	status, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{
		Mode: domain.RoutingModeRule, CoreState: domain.CoreStateRunning, Runtime: runtime, CloseConnections: true,
	})
	if !errors.Is(err, ErrConnectionCloseFailed) || status.OperationPhase != "connections_close_failed" || runtime.mode != domain.RoutingModeRule {
		t.Fatalf("status=%+v err=%v calls=%v", status, err, runtime.calls)
	}
	if got := mustReadFile(t, configFile); string(got) == string(original) {
		t.Fatal("connection-close failure rolled back the applied mode")
	}
}

func TestSetRoutingModeRestoresWhenExpectedDigestRefreshFails(t *testing.T) {
	compat, migration, stateStore, configFile, original := setupModeCompatibility(t)
	wrapped := &failingExpectationRepository{Store: stateStore, failures: 1}
	compat.service.repository = wrapped
	status, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{
		Mode: domain.RoutingModeDirect, CoreState: domain.CoreStateStopped,
	})
	if err == nil || errors.Is(err, ErrRestoreFailed) || status.OperationPhase != "restored" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if got := mustReadFile(t, configFile); string(got) != string(original) {
		t.Fatalf("config was not restored after expectation failure:\n%s", got)
	}
	operation, getErr := stateStore.GetOperation(context.Background(), status.OperationID)
	if getErr != nil || operation.State != domain.OperationStateRolledBack {
		t.Fatalf("operation=%+v err=%v", operation, getErr)
	}
}

func TestSetRoutingModeNativeValidationFailureHasNoStateChange(t *testing.T) {
	compat, migration, _, configFile, original := setupModeCompatibility(t)
	compat.service.paths.MihomoBin = fakeValidator(t, t.TempDir(), false)
	status, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{
		Mode: domain.RoutingModeDirect, CoreState: domain.CoreStateStopped,
	})
	if !errors.Is(err, ErrValidation) || status.OperationPhase != "" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if got := mustReadFile(t, configFile); string(got) != string(original) {
		t.Fatal("native validation failure changed the configuration")
	}
}

func TestSetRoutingModePublishFailureRestoresBackupAndConfig(t *testing.T) {
	compat, migration, _, configFile, original := setupModeCompatibility(t)
	compat.service.modeWriter = func(string, []byte, os.FileMode) error { return errors.New("publish failed") }
	if _, err := compat.SetRoutingMode(context.Background(), migration.ID, SetModeRequest{
		Mode: domain.RoutingModeDirect, CoreState: domain.CoreStateStopped,
	}); err == nil {
		t.Fatal("expected publish failure")
	}
	if got := mustReadFile(t, configFile); string(got) != string(original) {
		t.Fatal("publish failure changed the configuration")
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(configFile), "config.yaml.mode-*.bak"))
	if err != nil || len(backups) != 0 {
		t.Fatalf("publish failure left backups: %v err=%v", backups, err)
	}
}

func TestVerifyRuntimeRulesRequiresProvidersAndOneFallback(t *testing.T) {
	health := []RuleSetHealth{{Name: mihomo.CNDomainProviderName}, {Name: mihomo.CNIPProviderName}}
	valid := []mihomo.RuntimeRule{
		{Type: "RuleSet", Payload: mihomo.CNDomainProviderName, Proxy: "DIRECT"},
		{Type: "RuleSet", Payload: mihomo.CNIPProviderName, Proxy: "DIRECT"},
		{Type: "Match", Proxy: mihomo.ProxyGroupName},
	}
	if err := verifyRuntimeRules(valid, health); err != nil {
		t.Fatal(err)
	}
	for _, rules := range [][]mihomo.RuntimeRule{
		valid[:2],
		append(append([]mihomo.RuntimeRule(nil), valid...), mihomo.RuntimeRule{Type: "MATCH", Proxy: "DIRECT"}),
		{{Type: "RuleSet", Payload: mihomo.CNDomainProviderName, Proxy: "DIRECT"}, {Type: "Match", Proxy: mihomo.ProxyGroupName}},
	} {
		if err := verifyRuntimeRules(rules, health); !errors.Is(err, ErrModeRuntimeMismatch) {
			t.Fatalf("rules=%+v err=%v", rules, err)
		}
	}
}

func TestInspectModeRuntimeEffectiveTargets(t *testing.T) {
	for _, test := range []struct {
		mode  domain.RoutingMode
		group string
		node  string
	}{
		{mode: domain.RoutingModeGlobal, group: "GLOBAL", node: "node-a"},
		{mode: domain.RoutingModeRule, group: mihomo.ProxyGroupName, node: "node-a"},
		{mode: domain.RoutingModeDirect, group: "DIRECT", node: "DIRECT"},
	} {
		runtime := &fakeRoutingRuntime{mode: test.mode}
		status, err := inspectModeRuntime(context.Background(), test.mode, domain.CoreStateRunning, runtime)
		if err != nil || status.EffectiveGroup != test.group || status.EffectiveNode != test.node {
			t.Fatalf("mode=%s status=%+v err=%v", test.mode, status, err)
		}
	}
}

func setupModeCompatibility(t *testing.T) (*Compatibility, domain.LegacyMigration, *store.Store, string, []byte) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "config.yaml")
	original := []byte("mode: global\nexternal-controller: 127.0.0.1:9090\nproxies:\n  - {name: node-a, type: socks5, server: 127.0.0.1, port: 1081}\nproxy-groups:\n  - {name: '🌐 代理', type: select, proxies: [node-a]}\nrules:\n  - MATCH,GLOBAL\n")
	if err := os.WriteFile(configFile, original, 0o640); err != nil {
		t.Fatal(err)
	}
	service, stateStore := testService(t, root, configDir, fakeValidator(t, root, true))
	result, err := service.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return service.Compatibility(), result.Migration, stateStore, configFile, original
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
