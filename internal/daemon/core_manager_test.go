package daemon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

type managerFakeAdapter struct {
	mu          sync.Mutex
	readyErrors []error
	startErrors []error
	exitErrors  []error
	processes   []*managerFakeProcess
	starts      int
	runtimes    int
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
	pid       int
	done      chan error
	stopCalls int
	stopOnce  sync.Once
}

func (p *managerFakeProcess) PID() int           { return p.pid }
func (p *managerFakeProcess) Done() <-chan error { return p.done }
func (p *managerFakeProcess) Stop(context.Context) error {
	p.stopCalls++
	p.stopOnce.Do(func() { p.done <- nil; close(p.done) })
	return nil
}

type managerFakeConfigs struct {
	spec       core.RuntimeSpec
	prepareErr error
	checkErr   error
	markErr    error
	active     core.RuntimeSpec
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
