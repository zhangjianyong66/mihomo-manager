package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func TestCoreManager_ActivateSwitchesOnlyAfterReady(t *testing.T) {
	adapter := &managerFakeAdapter{}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("new")}
	repository := &managerFakeRepository{}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("old")); err != nil {
		t.Fatal(err)
	}
	oldProcess := adapter.processes[0]
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	if err := manager.Activate(context.Background(), newSnapshot("new")); err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if status.State != domain.CoreStateRunning || status.ProfileID != "new" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if oldProcess.stopCalls != 1 {
		t.Fatalf("old process stop calls = %d", oldProcess.stopCalls)
	}
	if repository.active != "new" || configs.active.ProfileID != "new" {
		t.Fatalf("activation was not committed: repo=%s config=%+v", repository.active, configs.active)
	}
	operation := repository.lastOperation(t)
	if operation.State != domain.OperationStateSucceeded || operation.Phase != "commit" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
}

func TestCoreManager_ValidationFailureLeavesOldProcessRunning(t *testing.T) {
	adapter := &managerFakeAdapter{}
	configs := &managerFakeConfigs{prepareErr: errors.New("invalid candidate")}
	repository := &managerFakeRepository{}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("old")); err != nil {
		t.Fatal(err)
	}
	oldProcess := adapter.processes[0]
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	if err := manager.Activate(context.Background(), newSnapshot("new")); err == nil {
		t.Fatal("expected validation failure")
	}
	if oldProcess.stopCalls != 0 || manager.Status().ProfileID != "old" {
		t.Fatalf("old process was changed: calls=%d status=%+v", oldProcess.stopCalls, manager.Status())
	}
	if operation := repository.lastOperation(t); operation.State != domain.OperationStateFailed || operation.Phase != "validate" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
}

func TestCoreManager_ReadinessFailureRestoresOldRuntime(t *testing.T) {
	adapter := &managerFakeAdapter{readyErrors: []error{nil, errors.New("not ready"), nil}}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("new")}
	repository := &managerFakeRepository{}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("old")); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	if err := manager.Activate(context.Background(), newSnapshot("new")); err == nil {
		t.Fatal("expected activation failure")
	}
	status := manager.Status()
	if status.State != domain.CoreStateRunning || status.ProfileID != "old" {
		t.Fatalf("old runtime was not restored: %+v", status)
	}
	if len(adapter.processes) != 3 || adapter.processes[1].stopCalls != 1 {
		t.Fatalf("unexpected process history: %+v", adapter.processes)
	}
	operation := repository.lastOperation(t)
	if operation.State != domain.OperationStateRolledBack || operation.ErrorCode != "ACTIVATION_FAILED" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
}

func TestCoreManager_ActivatePortConflictRestoresOldRuntimeAndPreservesConflicts(t *testing.T) {
	conflict := &core.PortConflictError{Conflicts: []core.PortConflict{{Field: "mixed-port", Network: "tcp", Host: "127.0.0.1", Port: 17890}}}
	adapter := &managerFakeAdapter{startErrors: []error{nil, conflict, nil}}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("new")}
	repository := &managerFakeRepository{active: "old", hasActive: true}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("old")); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	if err := manager.Activate(context.Background(), newSnapshot("new")); !errors.Is(err, core.ErrPortConflict) {
		t.Fatalf("expected port conflict, got %v", err)
	}
	status := manager.Status()
	if status.State != domain.CoreStateRunning || status.ProfileID != "old" || len(status.PortConflicts) != 1 {
		t.Fatalf("old runtime/conflicts not preserved: %+v", status)
	}
}

func TestCoreManager_RestoreFailureEntersFailedState(t *testing.T) {
	adapter := &managerFakeAdapter{
		readyErrors: []error{nil, errors.New("not ready")},
		startErrors: []error{nil, nil, errors.New("restore failed")},
	}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("new")}
	repository := &managerFakeRepository{}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("old")); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	if err := manager.Activate(context.Background(), newSnapshot("new")); err == nil {
		t.Fatal("expected restore failure")
	}
	if status := manager.Status(); status.State != domain.CoreStateFailed || status.ProfileID != "old" {
		t.Fatalf("expected failed old runtime status, got %+v", status)
	}
	if operation := repository.lastOperation(t); operation.State != domain.OperationStateFailed || operation.ErrorCode != "RESTORE_FAILED" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
}

func TestCoreManager_ReconfigureRestartsRunningCore(t *testing.T) {
	adapter := &managerFakeAdapter{}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("profile")}
	repository := &managerFakeRepository{active: "profile", hasActive: true}
	supervisor := NewSupervisor(adapter, time.Second)
	oldSpec := newRuntimeSpec("profile")
	oldSpec.SourceSHA256 = "old"
	if err := supervisor.Start(context.Background(), oldSpec); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	applyCalls := 0
	restoreCalls := 0
	restarted, err := manager.Reconfigure(context.Background(), newSnapshot("profile"), func(context.Context) (func(context.Context) error, error) {
		applyCalls++
		return func(context.Context) error { restoreCalls++; return nil }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !restarted || applyCalls != 1 || restoreCalls != 0 || len(adapter.processes) != 2 {
		t.Fatalf("restarted=%v apply=%d restore=%d processes=%d", restarted, applyCalls, restoreCalls, len(adapter.processes))
	}
	if status := manager.Status(); status.State != domain.CoreStateRunning || status.ProfileID != "profile" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if operation := repository.lastOperation(t); operation.State != domain.OperationStateSucceeded || operation.Kind != "core.reconfigure" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
}

func TestCoreManager_ReconfigureStoppedOnlyMutatesConfig(t *testing.T) {
	adapter := &managerFakeAdapter{}
	manager := newTestCoreManager(adapter, &managerFakeConfigs{}, &managerFakeRepository{}, NewSupervisor(adapter, time.Second))
	applyCalls := 0
	restarted, err := manager.Reconfigure(context.Background(), newSnapshot("profile"), func(context.Context) (func(context.Context) error, error) {
		applyCalls++
		return func(context.Context) error { return nil }, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if restarted || applyCalls != 1 || len(adapter.processes) != 0 || manager.Status().State != domain.CoreStateStopped {
		t.Fatalf("restarted=%v apply=%d processes=%d status=%+v", restarted, applyCalls, len(adapter.processes), manager.Status())
	}
}

func TestCoreManager_ReconfigureStartFailureRestoresConfigAndOldRuntime(t *testing.T) {
	conflict := &core.PortConflictError{Conflicts: []core.PortConflict{{Field: "mixed-port", Network: "tcp", Host: "127.0.0.1", Port: 17890}}}
	adapter := &managerFakeAdapter{startErrors: []error{nil, conflict, nil}}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("profile")}
	repository := &managerFakeRepository{active: "profile", hasActive: true}
	supervisor := NewSupervisor(adapter, time.Second)
	oldSpec := newRuntimeSpec("profile")
	oldSpec.SourceSHA256 = "old"
	if err := supervisor.Start(context.Background(), oldSpec); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	restoreCalls := 0
	restarted, err := manager.Reconfigure(context.Background(), newSnapshot("profile"), func(context.Context) (func(context.Context) error, error) {
		return func(context.Context) error { restoreCalls++; return nil }, nil
	})
	if restarted || !errors.Is(err, core.ErrPortConflict) || restoreCalls != 1 {
		t.Fatalf("restarted=%v err=%v restore=%d", restarted, err, restoreCalls)
	}
	status := manager.Status()
	if status.State != domain.CoreStateRunning || status.ProfileID != "profile" || len(status.PortConflicts) != 1 {
		t.Fatalf("old runtime/conflicts not preserved: %+v", status)
	}
	if operation := repository.lastOperation(t); operation.State != domain.OperationStateRolledBack || operation.ErrorCode != "RECONFIGURE_FAILED" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
}

func TestCoreManager_ReconfigureRestoreFailureMarksCoreFailed(t *testing.T) {
	adapter := &managerFakeAdapter{}
	configs := &managerFakeConfigs{prepareErr: errors.New("candidate invalid")}
	repository := &managerFakeRepository{active: "profile", hasActive: true}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("profile")); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	_, err := manager.Reconfigure(context.Background(), newSnapshot("profile"), func(context.Context) (func(context.Context) error, error) {
		return func(context.Context) error { return errors.New("restore config failed") }, nil
	})
	if !errors.Is(err, ErrReconfigureRestoreFailed) {
		t.Fatalf("expected restore failure, got %v", err)
	}
	if status := manager.Status(); status.State != domain.CoreStateFailed || status.ErrorCode != "RESTORE_FAILED" {
		t.Fatalf("unexpected failed status: %+v", status)
	}
	if operation := repository.lastOperation(t); operation.State != domain.OperationStateFailed || operation.ErrorCode != "RESTORE_FAILED" {
		t.Fatalf("unexpected operation: %+v", operation)
	}
}

func TestCoreManager_ReconfigureStopFailureDoesNotStartDuplicateProcess(t *testing.T) {
	stopErr := errors.New("old core stop failed")
	adapter := &managerFakeAdapter{stopErrors: []error{stopErr}}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("profile")}
	repository := &managerFakeRepository{active: "profile", hasActive: true}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("profile")); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	restoreCalls := 0
	_, err := manager.Reconfigure(context.Background(), newSnapshot("profile"), func(context.Context) (func(context.Context) error, error) {
		return func(context.Context) error { restoreCalls++; return nil }, nil
	})
	if !errors.Is(err, ErrReconfigureRestoreFailed) || !errors.Is(err, stopErr) {
		t.Fatalf("expected uncertain stop restore failure, got %v", err)
	}
	if restoreCalls != 1 || adapter.starts != 1 || len(adapter.processes) != 1 {
		t.Fatalf("restore=%d starts=%d processes=%d", restoreCalls, adapter.starts, len(adapter.processes))
	}
	if status := manager.Status(); status.State != domain.CoreStateFailed || status.ErrorCode != "RESTORE_FAILED" {
		t.Fatalf("unexpected failed status: %+v", status)
	}
}

func TestCoreManager_ReconfigureRollbackStopFailureDoesNotRestoreOldRuntime(t *testing.T) {
	stopErr := errors.New("candidate core stop failed")
	adapter := &managerFakeAdapter{stopErrors: []error{nil, stopErr}}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("profile"), markErr: errors.New("mark candidate failed")}
	repository := &managerFakeRepository{active: "profile", hasActive: true}
	supervisor := NewSupervisor(adapter, time.Second)
	oldSpec := newRuntimeSpec("profile")
	oldSpec.SourceSHA256 = "old"
	if err := supervisor.Start(context.Background(), oldSpec); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, configs, repository, supervisor)
	_, err := manager.Reconfigure(context.Background(), newSnapshot("profile"), func(context.Context) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	})
	if !errors.Is(err, ErrReconfigureRestoreFailed) || !errors.Is(err, stopErr) {
		t.Fatalf("expected rollback stop failure, got %v", err)
	}
	if adapter.starts != 2 || len(adapter.processes) != 2 {
		t.Fatalf("old runtime was started after uncertain stop: starts=%d processes=%d", adapter.starts, len(adapter.processes))
	}
	if status := manager.Status(); status.State != domain.CoreStateFailed || status.ErrorCode != "RESTORE_FAILED" {
		t.Fatalf("unexpected failed status: %+v", status)
	}
}

func TestCoreManager_CloseStopsOnlyManagedProcess(t *testing.T) {
	adapter := &managerFakeAdapter{}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("one")); err != nil {
		t.Fatal(err)
	}
	manager := newTestCoreManager(adapter, &managerFakeConfigs{}, &managerFakeRepository{}, supervisor)
	if err := manager.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if adapter.processes[0].stopCalls != 1 || manager.Status().State != domain.CoreStateStopped {
		t.Fatalf("managed process was not stopped: %+v", manager.Status())
	}
}

func TestCoreManager_BootstrapRunsPrivateCoreThenActivatesFormalConfig(t *testing.T) {
	adapter := &managerFakeAdapter{}
	configs := &managerFakeBootstrapConfigs{
		managerFakeConfigs: managerFakeConfigs{spec: newRuntimeSpec("formal")},
		bootstrapSpec:      newRuntimeSpec("bootstrap"),
	}
	repository := &managerFakeRepository{}
	manager := newTestCoreManager(adapter, configs, repository, NewSupervisor(adapter, time.Second))
	var phases []string
	actionCalled := false
	err := manager.Bootstrap(context.Background(), newSnapshot("formal"), func(content []byte) ([]byte, error) { return content, nil }, func(phase string) {
		phases = append(phases, phase)
	}, func(_ context.Context, spec core.RuntimeSpec) error {
		actionCalled = true
		if spec.ProfileID != "bootstrap" || manager.Status().State != domain.CoreStateRunning {
			t.Fatalf("unexpected bootstrap runtime: %+v %+v", spec, manager.Status())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !actionCalled || !configs.cleaned || adapter.starts != 2 {
		t.Fatalf("bootstrap lifecycle incomplete: action=%t cleaned=%t starts=%d", actionCalled, configs.cleaned, adapter.starts)
	}
	current, running := manager.supervisor.Current()
	if !running || current.ProfileID != "formal" || repository.active != "formal" || configs.active.ProfileID != "formal" {
		t.Fatalf("formal runtime was not activated: running=%t current=%+v active=%s", running, current, repository.active)
	}
	if got := strings.Join(phases, ","); got != "preparing_bootstrap,starting_bootstrap,stopping_bootstrap,starting_core,verifying_core" {
		t.Fatalf("phases = %s", got)
	}
}

func TestCoreManager_BootstrapActionFailureReturnsToStoppedAndCleans(t *testing.T) {
	adapter := &managerFakeAdapter{}
	configs := &managerFakeBootstrapConfigs{
		managerFakeConfigs: managerFakeConfigs{spec: newRuntimeSpec("formal")},
		bootstrapSpec:      newRuntimeSpec("bootstrap"),
	}
	manager := newTestCoreManager(adapter, configs, &managerFakeRepository{}, NewSupervisor(adapter, time.Second))
	want := errors.New("download failed")
	err := manager.Bootstrap(context.Background(), newSnapshot("formal"), func(content []byte) ([]byte, error) { return content, nil }, nil, func(context.Context, core.RuntimeSpec) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("expected action failure, got %v", err)
	}
	if manager.Status().State != domain.CoreStateStopped || !configs.cleaned || adapter.starts != 1 {
		t.Fatalf("bootstrap failure leaked state: status=%+v cleaned=%t starts=%d", manager.Status(), configs.cleaned, adapter.starts)
	}
}

func TestCoreManager_BootstrapFormalStartFailureReturnsToStopped(t *testing.T) {
	want := errors.New("formal start failed")
	adapter := &managerFakeAdapter{startErrors: []error{nil, want}}
	configs := &managerFakeBootstrapConfigs{
		managerFakeConfigs: managerFakeConfigs{spec: newRuntimeSpec("formal")},
		bootstrapSpec:      newRuntimeSpec("bootstrap"),
	}
	manager := newTestCoreManager(adapter, configs, &managerFakeRepository{}, NewSupervisor(adapter, time.Second))
	err := manager.Bootstrap(context.Background(), newSnapshot("formal"), func(content []byte) ([]byte, error) { return content, nil }, nil, func(context.Context, core.RuntimeSpec) error { return nil })
	if !errors.Is(err, want) {
		t.Fatalf("expected formal start failure, got %v", err)
	}
	if manager.Status().State != domain.CoreStateStopped || !configs.cleaned || adapter.starts != 2 {
		t.Fatalf("formal failure leaked state: status=%+v cleaned=%t starts=%d", manager.Status(), configs.cleaned, adapter.starts)
	}
}

func TestCapabilityMutationsShareCoreOperationCoordinator(t *testing.T) {
	coordinator := NewCoordinator()
	manager := NewCoreManager(CoreManagerOptions{Coordinator: coordinator})
	service := NewCapabilityService(nil, manager, nil, config.Paths{})
	if service.coordinator != coordinator || manager.coordinator != coordinator {
		t.Fatal("mode/capability and core operations do not share one coordinator")
	}
	release, err := coordinator.TryAcquire(context.Background(), "core-operation", "core.activate")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	called := false
	err = service.withMutation(context.Background(), "subscription.update", func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrOperationConflict) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestCoreManager_CommitMetadataFailureRestoresInactiveDatabaseState(t *testing.T) {
	adapter := &managerFakeAdapter{}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("new"), markErr: errors.New("metadata write failed")}
	repository := &managerFakeRepository{}
	manager := newTestCoreManager(adapter, configs, repository, NewSupervisor(adapter, time.Second))
	if err := manager.Activate(context.Background(), newSnapshot("new")); err == nil {
		t.Fatal("expected metadata commit failure")
	}
	if repository.hasActive || repository.active != "" {
		t.Fatalf("active profile was not restored: active=%q has=%v", repository.active, repository.hasActive)
	}
	if status := manager.Status(); status.State != domain.CoreStateFailed {
		t.Fatalf("expected failed core state, got %+v", status)
	}
}

func TestCoreManager_CommitMetadataFailureRestoresPreviousStoppedProfile(t *testing.T) {
	adapter := &managerFakeAdapter{}
	configs := &managerFakeConfigs{spec: newRuntimeSpec("new"), markErr: errors.New("metadata write failed")}
	repository := &managerFakeRepository{active: "previous", hasActive: true}
	manager := newTestCoreManager(adapter, configs, repository, NewSupervisor(adapter, time.Second))
	if err := manager.Activate(context.Background(), newSnapshot("new")); err == nil {
		t.Fatal("expected metadata commit failure")
	}
	if !repository.hasActive || repository.active != "previous" {
		t.Fatalf("previous active profile was not restored: active=%q has=%v", repository.active, repository.hasActive)
	}
}

func TestSupervisor_ProcessExitBeforeReadyIsFailure(t *testing.T) {
	adapter := &managerFakeAdapter{exitErrors: []error{errors.New("core exited")}}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("early-exit")); !errors.Is(err, core.ErrProcessExited) {
		t.Fatalf("expected process exit failure, got %v", err)
	}
	if status := supervisor.Status(); status.State != domain.CoreStateFailed {
		t.Fatalf("unexpected supervisor status: %+v", status)
	}
}

func TestSupervisor_ProcessExitAfterReadyClearsManagedRuntime(t *testing.T) {
	adapter := &managerFakeAdapter{}
	supervisor := NewSupervisor(adapter, time.Second)
	spec := newRuntimeSpec("late-exit")
	if err := supervisor.Start(context.Background(), spec); err != nil {
		t.Fatal(err)
	}

	process := adapter.processes[0]
	process.done <- errors.New("core exited")
	close(process.done)

	waitForSupervisorStatus(t, supervisor, func(status CoreStatus) bool {
		return status.State == domain.CoreStateFailed && status.ErrorCode == "PROCESS_EXITED"
	})
	status := supervisor.Status()
	if status.ProfileID != spec.ProfileID || status.GenerationID != spec.GenerationID || status.PID != 0 {
		t.Fatalf("unexpected failed status: %+v", status)
	}
	if current, running := supervisor.Current(); running || current != (core.RuntimeSpec{}) {
		t.Fatalf("exited process is still current: running=%v spec=%+v", running, current)
	}
}

func TestSupervisor_StopIsNotOverwrittenByDelayedProcessExit(t *testing.T) {
	adapter := &managerFakeAdapter{delayStopExit: []bool{true}}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("stopped")); err != nil {
		t.Fatal(err)
	}
	process := adapter.processes[0]
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := supervisor.Status(); status.State != domain.CoreStateStopped {
		t.Fatalf("unexpected status after stop: %+v", status)
	}

	close(process.done)
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if status := supervisor.Status(); status.State != domain.CoreStateStopped || status.ErrorCode != "" {
			t.Fatalf("delayed exit overwrote stopped status: %+v", status)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSupervisor_DelayedOldProcessExitDoesNotOverwriteReplacement(t *testing.T) {
	adapter := &managerFakeAdapter{delayStopExit: []bool{true}}
	supervisor := NewSupervisor(adapter, time.Second)
	if err := supervisor.Start(context.Background(), newRuntimeSpec("old")); err != nil {
		t.Fatal(err)
	}
	oldProcess := adapter.processes[0]
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	replacement := newRuntimeSpec("replacement")
	if err := supervisor.Start(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}

	close(oldProcess.done)
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		status := supervisor.Status()
		if status.State != domain.CoreStateRunning || status.ProfileID != replacement.ProfileID || status.ErrorCode != "" {
			t.Fatalf("old process exit overwrote replacement status: %+v", status)
		}
		time.Sleep(time.Millisecond)
	}
	if current, running := supervisor.Current(); !running || current != replacement {
		t.Fatalf("replacement is no longer current: running=%v spec=%+v", running, current)
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func waitForSupervisorStatus(t *testing.T, supervisor *Supervisor, condition func(CoreStatus) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition(supervisor.Status()) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("supervisor status did not converge: %+v", supervisor.Status())
}

type managerFakeAdapter struct {
	mu            sync.Mutex
	readyErrors   []error
	startErrors   []error
	stopErrors    []error
	exitErrors    []error
	delayStopExit []bool
	processes     []*managerFakeProcess
	starts        int
	runtimes      int
}

func (a *managerFakeAdapter) Type() domain.CoreType { return domain.CoreTypeMihomo }
func (a *managerFakeAdapter) Render(context.Context, core.ProfileSnapshot) (core.RenderedConfig, error) {
	return core.RenderedConfig{}, nil
}
func (a *managerFakeAdapter) Validate(context.Context, core.RuntimeSpec) error { return nil }
func (a *managerFakeAdapter) Start(_ context.Context, _ core.RuntimeSpec) (core.Process, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := a.starts
	a.starts++
	if index < len(a.startErrors) && a.startErrors[index] != nil {
		return nil, a.startErrors[index]
	}
	process := &managerFakeProcess{pid: 1000 + index, done: make(chan error, 1)}
	if index < len(a.stopErrors) {
		process.stopErr = a.stopErrors[index]
	}
	if index < len(a.delayStopExit) {
		process.delayStopExit = a.delayStopExit[index]
	}
	if index < len(a.exitErrors) {
		process.done <- a.exitErrors[index]
		close(process.done)
	}
	a.processes = append(a.processes, process)
	return process, nil
}
func (a *managerFakeAdapter) Runtime(string) (core.RuntimeClient, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	index := a.runtimes
	a.runtimes++
	var err error
	if index < len(a.readyErrors) {
		err = a.readyErrors[index]
	}
	return managerFakeRuntime{err: err}, nil
}

type managerFakeRuntime struct{ err error }

func (r managerFakeRuntime) Ready(context.Context) error { return r.err }

type managerFakeProcess struct {
	pid           int
	done          chan error
	stopCalls     int
	stopOnce      sync.Once
	stopErr       error
	delayStopExit bool
}

func (p *managerFakeProcess) PID() int           { return p.pid }
func (p *managerFakeProcess) Done() <-chan error { return p.done }
func (p *managerFakeProcess) Stop(context.Context) error {
	p.stopCalls++
	if p.stopErr != nil {
		return p.stopErr
	}
	if !p.delayStopExit {
		p.stopOnce.Do(func() { p.done <- nil; close(p.done) })
	}
	return nil
}

type managerFakeConfigs struct {
	spec       core.RuntimeSpec
	prepareErr error
	checkErr   error
	markErr    error
	active     core.RuntimeSpec
}

type managerFakeBootstrapConfigs struct {
	managerFakeConfigs
	bootstrapSpec core.RuntimeSpec
	bootstrapErr  error
	cleaned       bool
}

func (f *managerFakeBootstrapConfigs) PrepareBootstrap(context.Context, core.ProfileSnapshot, core.Adapter, core.BootstrapTransform) (core.RuntimeSpec, func() error, error) {
	if f.bootstrapErr != nil {
		return core.RuntimeSpec{}, nil, f.bootstrapErr
	}
	return f.bootstrapSpec, func() error { f.cleaned = true; return nil }, nil
}

func (f *managerFakeConfigs) Prepare(context.Context, core.ProfileSnapshot, core.Adapter) (core.RuntimeSpec, error) {
	return f.spec, f.prepareErr
}
func (f *managerFakeConfigs) CheckSource(core.RuntimeSpec) error { return f.checkErr }
func (f *managerFakeConfigs) MarkActive(spec core.RuntimeSpec) error {
	if f.markErr == nil {
		f.active = spec
	}
	return f.markErr
}

type managerFakeRepository struct {
	operations []domain.Operation
	active     domain.ProfileID
	hasActive  bool
}

func (r *managerFakeRepository) CreateOperation(_ context.Context, operation domain.Operation) error {
	r.operations = append(r.operations, operation)
	return nil
}
func (r *managerFakeRepository) UpdateOperation(_ context.Context, operation domain.Operation) error {
	r.operations = append(r.operations, operation)
	return nil
}
func (r *managerFakeRepository) SetActiveProfile(_ context.Context, id domain.ProfileID, _ time.Time) error {
	r.active = id
	r.hasActive = true
	return nil
}
func (r *managerFakeRepository) ActiveProfileID(context.Context) (domain.ProfileID, bool, error) {
	return r.active, r.hasActive, nil
}
func (r *managerFakeRepository) RestoreActiveProfile(_ context.Context, id *domain.ProfileID, _ time.Time) error {
	if id == nil {
		r.active = ""
		r.hasActive = false
	} else {
		r.active = *id
		r.hasActive = true
	}
	return nil
}
func (r *managerFakeRepository) lastOperation(t *testing.T) domain.Operation {
	t.Helper()
	if len(r.operations) == 0 {
		t.Fatal("no operation was recorded")
	}
	return r.operations[len(r.operations)-1]
}

func newTestCoreManager(adapter core.Adapter, configs ConfigStore, repository CoreRepository, supervisor *Supervisor) *CoreManager {
	now := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	return NewCoreManager(CoreManagerOptions{
		Adapter: adapter, Configs: configs, Repository: repository, Supervisor: supervisor,
		Clock: func() time.Time { return now }, NewID: func() string { return "operation-one" },
	})
}

func newSnapshot(id domain.ProfileID) core.ProfileSnapshot {
	return core.ProfileSnapshot{ProfileID: id, Revision: 1, Mode: domain.ProfileModeExternal, ExternalConfigPath: "/tmp/config.yaml", ControllerEndpoint: "http://127.0.0.1:19090"}
}

func newRuntimeSpec(id domain.ProfileID) core.RuntimeSpec {
	return core.RuntimeSpec{
		ProfileID: id, Revision: 1, Mode: domain.ProfileModeExternal, GenerationID: id.String(),
		ConfigDir: "/tmp", ConfigPath: "/tmp/" + id.String() + ".yaml", LogPath: "/tmp/core.log",
		ControllerEndpoint: "http://127.0.0.1:19090", SourceSHA256: id.String(),
	}
}
