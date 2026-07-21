package legacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
)

var (
	ErrInvalidRoutingMode    = errors.New("invalid routing mode")
	ErrConfigChanged         = errors.New("legacy config changed")
	ErrModeRuntimeMismatch   = errors.New("runtime routing mode mismatch")
	ErrRestoreFailed         = errors.New("routing mode restore failed")
	ErrConnectionCloseFailed = errors.New("mihomo connection close failed")
)

type RuleSetHealth struct {
	Name      string
	Available bool
	Loaded    bool
}

type ModeStatus struct {
	ConfigMode           domain.RoutingMode
	RuntimeMode          *domain.RoutingMode
	RuntimeAvailable     bool
	CoreState            domain.CoreState
	EffectiveGroup       string
	EffectiveNode        string
	RuleSets             []RuleSetHealth
	ActiveConnections    int
	ConnectionsAvailable bool
	ConnectionsClosed    bool
	NextStart            bool
	OperationID          domain.OperationID
	OperationPhase       string
	Warnings             []string
}

type SetModeRequest struct {
	Mode             domain.RoutingMode
	CloseConnections bool
	CoreState        domain.CoreState
	Runtime          mihomo.RoutingRuntime
}

type modeOperationRepository interface {
	CreateOperation(context.Context, domain.Operation) error
	UpdateOperation(context.Context, domain.Operation) error
}

func (c *Compatibility) RoutingModeStatus(ctx context.Context, id domain.RestorePointID, coreState domain.CoreState, runtime mihomo.RoutingRuntime) (ModeStatus, error) {
	if err := ctx.Err(); err != nil {
		return ModeStatus{}, err
	}
	migration, err := c.load(ctx, id)
	if err != nil {
		return ModeStatus{}, err
	}
	if err := checkExpected(migration); err != nil {
		return ModeStatus{}, err
	}
	policy, err := mihomo.New(c.service.pathsOrDefault()).ReadRoutingPolicy()
	if err != nil {
		return ModeStatus{}, err
	}
	status, err := inspectModeRuntime(ctx, policy.Mode, coreState, runtime)
	status.Warnings = append(status.Warnings, policy.Warnings...)
	return status, err
}

func (c *Compatibility) SetRoutingMode(ctx context.Context, id domain.RestorePointID, request SetModeRequest) (ModeStatus, error) {
	if err := request.Mode.Validate(); err != nil {
		return ModeStatus{}, fmt.Errorf("%w: %v", ErrInvalidRoutingMode, err)
	}
	c.service.opMu.Lock()
	defer c.service.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ModeStatus{}, err
	}
	migration, err := c.load(ctx, id)
	if err != nil {
		return ModeStatus{}, err
	}
	if err := checkExpected(migration); err != nil {
		return ModeStatus{}, fmt.Errorf("%w: %v", ErrConfigChanged, err)
	}
	if request.CoreState == domain.CoreStateRunning && request.Runtime == nil {
		return ModeStatus{}, errors.New("running core requires a routing runtime client")
	}
	if request.CoreState != domain.CoreStateRunning {
		request.Runtime = nil
	}

	paths := c.service.pathsOrDefault()
	client := mihomo.New(paths)
	oldPolicy, err := client.ReadRoutingPolicy()
	if err != nil {
		return ModeStatus{}, err
	}
	oldDigest, err := fileSHA256(paths.ConfigFile)
	if err != nil {
		return ModeStatus{}, err
	}
	candidate, policy, err := client.RoutingModeCandidate(request.Mode)
	if err != nil {
		return ModeStatus{}, err
	}

	operation, operationRepo, err := c.beginModeOperation(ctx, migration.ProfileID, oldPolicy.Mode, request)
	if err != nil {
		return ModeStatus{}, err
	}
	updateOperation := func(state domain.OperationState, phase, code string) error {
		if operationRepo == nil {
			return nil
		}
		operation.State = state
		operation.Phase = phase
		operation.ErrorCode = code
		operation.UpdatedAt = c.service.now()
		return operationRepo.UpdateOperation(ctx, operation)
	}
	failBeforePublish := func(cause error, phase, code string) (ModeStatus, error) {
		return ModeStatus{}, errors.Join(cause, updateOperation(domain.OperationStateFailed, phase, code))
	}

	if err := validateModeCandidate(ctx, paths.ConfigDir, paths.MihomoBin, candidate); err != nil {
		return failBeforePublish(fmt.Errorf("%w: %v", ErrValidation, err), "candidate_validated", "VALIDATION_FAILED")
	}
	if err := updateOperation(domain.OperationStateRunning, "candidate_validated", ""); err != nil {
		return ModeStatus{}, err
	}
	currentDigest, err := fileSHA256(paths.ConfigFile)
	if err != nil || !strings.EqualFold(currentDigest, oldDigest) {
		if err == nil {
			err = ErrConfigChanged
		}
		return failBeforePublish(err, "candidate_validated", "CONFIG_CHANGED")
	}

	backupRelative := "config.yaml." + c.service.makeID("mode") + ".bak"
	relatives := mutationRelatives(migration, backupRelative)
	before, err := captureSource(paths.ConfigDir, relatives)
	if err != nil {
		return failBeforePublish(err, "candidate_validated", "CAPTURE_FAILED")
	}
	backupPath, err := safeJoin(paths.ConfigDir, backupRelative)
	if err != nil {
		return failBeforePublish(err, "candidate_validated", "BACKUP_FAILED")
	}
	configMode := os.FileMode(0o600)
	if state := before["config.yaml"]; state.exists {
		configMode = state.mode
	}
	if err := copyFile(paths.ConfigFile, backupPath, 0o600); err != nil {
		return failBeforePublish(err, "candidate_validated", "BACKUP_FAILED")
	}
	modeWriter := c.service.modeWriter
	if modeWriter == nil {
		modeWriter = writeAtomic
	}
	if err := modeWriter(paths.ConfigFile, candidate, configMode); err != nil {
		_ = restoreSource(paths.ConfigDir, before)
		return failBeforePublish(err, "file_published", "PUBLISH_FAILED")
	}
	if err := secureMutationFiles(paths.ConfigDir, relatives); err != nil {
		return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
	}
	if err := updateOperation(domain.OperationStateRunning, "file_published", ""); err != nil {
		return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
	}
	if err := verifyPublishedMode(paths, request.Mode, candidate); err != nil {
		return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
	}

	if request.Runtime != nil {
		if err := request.Runtime.Reload(ctx, paths.ConfigFile); err != nil {
			return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
		}
		if err := request.Runtime.SetMode(ctx, request.Mode); err != nil {
			return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
		}
		if err := updateOperation(domain.OperationStateRunning, "runtime_updated", ""); err != nil {
			return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
		}
	}

	status, err := inspectModeRuntime(ctx, policy.Mode, request.CoreState, request.Runtime)
	if err != nil {
		return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
	}
	status.OperationID = operation.ID
	status.OperationPhase = "runtime_verified"
	status.Warnings = append(status.Warnings, policy.Warnings...)
	if err := updateOperation(domain.OperationStateRunning, "runtime_verified", ""); err != nil {
		return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
	}
	if err := refreshExpected(&migration); err != nil {
		return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
	}
	if err := c.service.repository.UpdateLegacyFileExpectations(ctx, migration); err != nil {
		return c.restoreMode(ctx, migration, request, oldPolicy.Mode, before, operation, operationRepo, err)
	}

	status.OperationPhase = "succeeded"
	if request.CloseConnections && request.Runtime != nil {
		if err := request.Runtime.CloseConnections(ctx); err != nil {
			status.OperationPhase = "connections_close_failed"
			status.Warnings = append(status.Warnings, "模式已生效，但关闭 mihomo 活动连接失败")
			return status, errors.Join(fmt.Errorf("%w: %v", ErrConnectionCloseFailed, err), updateOperation(domain.OperationStateSucceeded, status.OperationPhase, "CONNECTION_CLOSE_FAILED"))
		}
		status.ConnectionsClosed = true
		status.ActiveConnections = 0
	}
	if err := updateOperation(domain.OperationStateSucceeded, "succeeded", ""); err != nil {
		status.Warnings = append(status.Warnings, "模式已生效，但 operation 最终状态写入失败")
		return status, err
	}
	return status, nil
}

func (c *Compatibility) beginModeOperation(ctx context.Context, profileID domain.ProfileID, oldMode domain.RoutingMode, request SetModeRequest) (domain.Operation, modeOperationRepository, error) {
	repo, _ := c.service.repository.(modeOperationRepository)
	now := c.service.now()
	recovery, err := json.Marshal(map[string]any{"oldMode": oldMode, "wasRunning": request.Runtime != nil})
	if err != nil {
		return domain.Operation{}, nil, err
	}
	operation := domain.Operation{
		ID: domain.OperationID(c.service.makeID("mode")), ProfileID: &profileID,
		Kind: "routing.mode.set", State: domain.OperationStateRunning, Phase: "captured", Attempt: 1,
		Recovery: recovery, CreatedAt: now, UpdatedAt: now,
	}
	if repo != nil {
		if err := repo.CreateOperation(ctx, operation); err != nil {
			return domain.Operation{}, nil, err
		}
	}
	return operation, repo, nil
}

func (c *Compatibility) restoreMode(ctx context.Context, migration domain.LegacyMigration, request SetModeRequest, oldMode domain.RoutingMode, before map[string]sourceState, operation domain.Operation, operationRepo modeOperationRepository, cause error) (ModeStatus, error) {
	restoreCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	update := func(state domain.OperationState, phase, code string) error {
		if operationRepo == nil {
			return nil
		}
		operation.State, operation.Phase, operation.ErrorCode = state, phase, code
		operation.UpdatedAt = c.service.now()
		return operationRepo.UpdateOperation(restoreCtx, operation)
	}
	updateErr := update(domain.OperationStateRollingBack, "restoring", "MODE_SET_FAILED")
	fileErr := restoreSource(c.service.pathsOrDefault().ConfigDir, before)
	var runtimeErr error
	if request.Runtime != nil {
		paths := c.service.pathsOrDefault()
		if err := request.Runtime.Reload(restoreCtx, paths.ConfigFile); err != nil {
			runtimeErr = err
		} else if err := request.Runtime.SetMode(restoreCtx, oldMode); err != nil {
			runtimeErr = err
		} else if restored, err := request.Runtime.Mode(restoreCtx); err != nil || restored != oldMode {
			if err != nil {
				runtimeErr = err
			} else {
				runtimeErr = fmt.Errorf("%w: got %s want %s", ErrModeRuntimeMismatch, restored, oldMode)
			}
		}
	}
	expectationErr := error(nil)
	if fileErr == nil {
		expectationErr = c.service.repository.UpdateLegacyFileExpectations(restoreCtx, migration)
	}
	if fileErr != nil || runtimeErr != nil || expectationErr != nil {
		finalUpdateErr := update(domain.OperationStateFailed, "restore_failed", "RESTORE_FAILED")
		return ModeStatus{CoreState: domain.CoreStateFailed, OperationID: operation.ID, OperationPhase: "restore_failed"}, errors.Join(ErrRestoreFailed, cause, updateErr, fileErr, runtimeErr, expectationErr, finalUpdateErr)
	}
	finalUpdateErr := update(domain.OperationStateRolledBack, "restored", "MODE_SET_FAILED")
	return ModeStatus{ConfigMode: oldMode, CoreState: request.CoreState, OperationID: operation.ID, OperationPhase: "restored"}, errors.Join(cause, updateErr, finalUpdateErr)
}

func verifyPublishedMode(paths config.Paths, mode domain.RoutingMode, candidate []byte) error {
	published, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		return err
	}
	if !bytes.Equal(published, candidate) {
		return ErrConfigChanged
	}
	policy, err := mihomo.New(paths).ReadRoutingPolicy()
	if err != nil {
		return err
	}
	if policy.Mode != mode {
		return fmt.Errorf("%w: config=%s want=%s", ErrConfigChanged, policy.Mode, mode)
	}
	return nil
}

func validateModeCandidate(ctx context.Context, configDir, binary string, content []byte) error {
	temporary, err := os.CreateTemp(configDir, ".mode-candidate-*.yaml")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return validateConfig(ctx, configDir, filepath.Clean(name), binary)
}

func inspectModeRuntime(ctx context.Context, configMode domain.RoutingMode, coreState domain.CoreState, runtime mihomo.RoutingRuntime) (ModeStatus, error) {
	status := ModeStatus{
		ConfigMode: configMode, CoreState: coreState, EffectiveGroup: effectiveGroup(configMode),
		RuleSets: []RuleSetHealth{{Name: mihomo.CNDomainProviderName}, {Name: mihomo.CNIPProviderName}},
	}
	if coreState != domain.CoreStateRunning || runtime == nil {
		status.NextStart = true
		status.Warnings = append(status.Warnings, "mihomo core 未运行，配置将在下次启动时生效")
		return status, nil
	}
	status.RuntimeAvailable = true
	runtimeMode, err := runtime.Mode(ctx)
	if err != nil {
		return status, err
	}
	status.RuntimeMode = &runtimeMode
	if runtimeMode != configMode {
		return status, fmt.Errorf("%w: config=%s runtime=%s", ErrModeRuntimeMismatch, configMode, runtimeMode)
	}
	if configMode == domain.RoutingModeRule {
		rules, err := runtime.LoadedRules(ctx)
		if err != nil {
			return status, err
		}
		if err := verifyRuntimeRules(rules, status.RuleSets); err != nil {
			return status, err
		}
		for index := range status.RuleSets {
			status.RuleSets[index].Available = true
			status.RuleSets[index].Loaded = true
		}
	}
	connections, err := runtime.ConnectionCount(ctx)
	if err != nil {
		return status, err
	}
	status.ActiveConnections = connections
	status.ConnectionsAvailable = true
	if status.EffectiveGroup == "DIRECT" {
		status.EffectiveNode = "DIRECT"
	} else {
		selected, err := runtime.SelectedProxy(ctx, status.EffectiveGroup)
		if err != nil {
			return status, err
		}
		status.EffectiveNode = selected
	}
	return status, nil
}

func effectiveGroup(mode domain.RoutingMode) string {
	switch mode {
	case domain.RoutingModeGlobal:
		return "GLOBAL"
	case domain.RoutingModeDirect:
		return "DIRECT"
	default:
		return mihomo.ProxyGroupName
	}
}

func verifyRuntimeRules(rules []mihomo.RuntimeRule, health []RuleSetHealth) error {
	loaded := make(map[string]bool, len(health))
	matchCount := 0
	matchTarget := ""
	for _, rule := range rules {
		kind := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(rule.Type))
		if kind == "ruleset" && rule.Proxy == "DIRECT" {
			loaded[rule.Payload] = true
		}
		if kind == "match" {
			matchCount++
			matchTarget = rule.Proxy
		}
	}
	for _, item := range health {
		if !loaded[item.Name] {
			return fmt.Errorf("%w: ruleset %s is not loaded", ErrModeRuntimeMismatch, item.Name)
		}
	}
	if matchCount != 1 || matchTarget != mihomo.ProxyGroupName {
		return fmt.Errorf("%w: expected one MATCH to %s, got count=%d target=%s", ErrModeRuntimeMismatch, mihomo.ProxyGroupName, matchCount, matchTarget)
	}
	return nil
}
