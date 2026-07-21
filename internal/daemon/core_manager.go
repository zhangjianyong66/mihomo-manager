package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type CoreStatus struct {
	State        domain.CoreState `json:"state"`
	ProfileID    domain.ProfileID `json:"profileId,omitempty"`
	GenerationID string           `json:"generationId,omitempty"`
	PID          int              `json:"pid,omitempty"`
	ErrorCode    string           `json:"errorCode,omitempty"`
}

type ConfigStore interface {
	Prepare(context.Context, core.ProfileSnapshot, core.Adapter) (core.RuntimeSpec, error)
	CheckSource(core.RuntimeSpec) error
	MarkActive(core.RuntimeSpec) error
}

type CoreRepository interface {
	CreateOperation(context.Context, domain.Operation) error
	UpdateOperation(context.Context, domain.Operation) error
	SetActiveProfile(context.Context, domain.ProfileID, time.Time) error
	ActiveProfileID(context.Context) (domain.ProfileID, bool, error)
	RestoreActiveProfile(context.Context, *domain.ProfileID, time.Time) error
}

type Supervisor struct {
	adapter      core.Adapter
	readyTimeout time.Duration
	mu           sync.RWMutex
	opMu         sync.Mutex
	status       CoreStatus
	process      core.Process
	spec         core.RuntimeSpec
}

func NewSupervisor(adapter core.Adapter, readyTimeout time.Duration) *Supervisor {
	if readyTimeout <= 0 {
		readyTimeout = 10 * time.Second
	}
	return &Supervisor{adapter: adapter, readyTimeout: readyTimeout, status: CoreStatus{State: domain.CoreStateStopped}}
}

func (s *Supervisor) Status() CoreStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Supervisor) Current() (core.RuntimeSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.spec, s.process != nil && s.status.State == domain.CoreStateRunning
}

func (s *Supervisor) Start(ctx context.Context, spec core.RuntimeSpec) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.start(ctx, spec, nil)
}

func (s *Supervisor) startWithHook(ctx context.Context, spec core.RuntimeSpec, started func() error) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.start(ctx, spec, started)
}

func (s *Supervisor) start(ctx context.Context, spec core.RuntimeSpec, started func() error) error {
	s.mu.RLock()
	alreadyRunning := s.process != nil
	s.mu.RUnlock()
	if alreadyRunning {
		return errors.New("a core process is already managed")
	}
	s.setStatus(CoreStatus{State: domain.CoreStateStarting, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID})
	process, err := s.adapter.Start(ctx, spec)
	if err != nil {
		s.setStatus(CoreStatus{State: domain.CoreStateFailed, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, ErrorCode: "START_FAILED"})
		return err
	}
	runtimeClient, err := s.adapter.Runtime(spec.ControllerEndpoint)
	if err != nil {
		_ = process.Stop(context.Background())
		s.setStatus(CoreStatus{State: domain.CoreStateFailed, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, ErrorCode: "RUNTIME_CLIENT_FAILED"})
		return err
	}
	if started != nil {
		if err := started(); err != nil {
			_ = process.Stop(context.Background())
			s.setStatus(CoreStatus{State: domain.CoreStateFailed, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, ErrorCode: "STATE_UPDATE_FAILED"})
			return err
		}
	}
	readyCtx, cancel := context.WithTimeout(ctx, s.readyTimeout)
	defer cancel()
	ready := make(chan error, 1)
	go func() { ready <- runtimeClient.Ready(readyCtx) }()
	select {
	case err := <-ready:
		if err != nil {
			_ = process.Stop(context.Background())
			code := "READINESS_FAILED"
			if errors.Is(err, context.DeadlineExceeded) {
				code = "READINESS_TIMEOUT"
				err = errors.Join(core.ErrReadinessTimeout, err)
			}
			s.setStatus(CoreStatus{State: domain.CoreStateFailed, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, ErrorCode: code})
			return err
		}
		select {
		case exitErr := <-process.Done():
			s.setStatus(CoreStatus{State: domain.CoreStateFailed, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, ErrorCode: "PROCESS_EXITED"})
			return errors.Join(core.ErrProcessExited, exitErr)
		default:
		}
	case err := <-process.Done():
		cancel()
		s.setStatus(CoreStatus{State: domain.CoreStateFailed, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, ErrorCode: "PROCESS_EXITED"})
		return errors.Join(core.ErrProcessExited, err)
	case <-readyCtx.Done():
		_ = process.Stop(context.Background())
		s.setStatus(CoreStatus{State: domain.CoreStateFailed, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, ErrorCode: "READINESS_TIMEOUT"})
		return errors.Join(core.ErrReadinessTimeout, readyCtx.Err())
	}
	s.mu.Lock()
	s.process = process
	s.spec = spec
	s.status = CoreStatus{State: domain.CoreStateRunning, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, PID: process.PID()}
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) Stop(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.stop(ctx)
}

func (s *Supervisor) stop(ctx context.Context) error {
	s.mu.Lock()
	process := s.process
	spec := s.spec
	if process == nil {
		s.status = CoreStatus{State: domain.CoreStateStopped}
		s.mu.Unlock()
		return nil
	}
	s.status.State = domain.CoreStateStopping
	s.mu.Unlock()
	err := process.Stop(ctx)
	s.mu.Lock()
	s.process = nil
	s.spec = core.RuntimeSpec{}
	if err != nil {
		s.status = CoreStatus{State: domain.CoreStateFailed, ProfileID: spec.ProfileID, GenerationID: spec.GenerationID, ErrorCode: "STOP_FAILED"}
	} else {
		s.status = CoreStatus{State: domain.CoreStateStopped}
	}
	s.mu.Unlock()
	return err
}

func (s *Supervisor) setStatus(status CoreStatus) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

type CoreManagerOptions struct {
	Adapter     core.Adapter
	Configs     ConfigStore
	Repository  CoreRepository
	Coordinator *Coordinator
	Supervisor  *Supervisor
	Clock       func() time.Time
	NewID       func() string
}

type CoreManager struct {
	adapter     core.Adapter
	configs     ConfigStore
	repository  CoreRepository
	coordinator *Coordinator
	supervisor  *Supervisor
	clock       func() time.Time
	newID       func() string
	operationMu sync.Mutex
}

func NewCoreManager(options CoreManagerOptions) *CoreManager {
	if options.Coordinator == nil {
		options.Coordinator = NewCoordinator()
	}
	if options.Supervisor == nil {
		options.Supervisor = NewSupervisor(options.Adapter, 10*time.Second)
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.NewID == nil {
		options.NewID = func() string { return fmt.Sprintf("core-switch-%d", time.Now().UnixNano()) }
	}
	return &CoreManager{
		adapter: options.Adapter, configs: options.Configs, repository: options.Repository,
		coordinator: options.Coordinator, supervisor: options.Supervisor,
		clock: options.Clock, newID: options.NewID,
	}
}

func (m *CoreManager) Status() CoreStatus { return m.supervisor.Status() }

func (m *CoreManager) Runtime() (core.RuntimeClient, bool, error) {
	if m == nil || m.adapter == nil || m.supervisor == nil {
		return nil, false, errors.New("core manager is not fully configured")
	}
	spec, running := m.supervisor.Current()
	if !running {
		return nil, false, nil
	}
	client, err := m.adapter.Runtime(spec.ControllerEndpoint)
	if err != nil {
		return nil, true, err
	}
	return client, true, nil
}

func (m *CoreManager) markFailed(profileID domain.ProfileID, code string) {
	if m == nil || m.supervisor == nil {
		return
	}
	status := m.supervisor.Status()
	status.State = domain.CoreStateFailed
	status.ProfileID = profileID
	status.ErrorCode = code
	m.supervisor.setStatus(status)
}

func (m *CoreManager) Stop(ctx context.Context) error {
	if m == nil || m.supervisor == nil {
		return errors.New("core manager is not fully configured")
	}
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	release, err := m.coordinator.TryAcquire(ctx, m.newID(), "core.stop")
	if err != nil {
		return err
	}
	defer release()
	return m.supervisor.Stop(ctx)
}

func (m *CoreManager) Activate(ctx context.Context, snapshot core.ProfileSnapshot) error {
	if m.adapter == nil || m.configs == nil || m.repository == nil || m.supervisor == nil {
		return errors.New("core manager is not fully configured")
	}
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	opID := m.newID()
	release, err := m.coordinator.TryAcquire(ctx, opID, "core.activate")
	if err != nil {
		return err
	}
	defer release()
	oldSpec, oldRunning := m.supervisor.Current()
	previousActive, hasPreviousActive, err := m.repository.ActiveProfileID(ctx)
	if err != nil {
		return err
	}
	previousActivePtr := activeProfilePointer(previousActive, hasPreviousActive)
	recovery, err := json.Marshal(struct {
		WasRunning bool              `json:"wasRunning"`
		Runtime    core.RuntimeSpec  `json:"runtime"`
		Active     *domain.ProfileID `json:"activeProfileId,omitempty"`
	}{WasRunning: oldRunning, Runtime: oldSpec, Active: previousActivePtr})
	if err != nil {
		return fmt.Errorf("encode core recovery state: %w", err)
	}
	now := m.clock().UTC()
	profileID := snapshot.ProfileID
	operation := domain.Operation{
		ID: domain.OperationID(opID), ProfileID: &profileID, Kind: "core.activate",
		State: domain.OperationStatePending, Phase: "prepare", Attempt: 1,
		Recovery: recovery, CreatedAt: now, UpdatedAt: now,
	}
	if err := m.repository.CreateOperation(ctx, operation); err != nil {
		return err
	}
	update := func(state domain.OperationState, phase, code string) error {
		operation.State = state
		operation.Phase = phase
		operation.ErrorCode = code
		operation.UpdatedAt = m.clock().UTC()
		return m.repository.UpdateOperation(ctx, operation)
	}
	if err := update(domain.OperationStateRunning, "validate", ""); err != nil {
		return err
	}
	newSpec, err := m.configs.Prepare(ctx, snapshot, m.adapter)
	if err != nil {
		return errors.Join(err, update(domain.OperationStateFailed, "validate", "CONFIG_PREPARE_FAILED"))
	}
	if err := m.configs.CheckSource(newSpec); err != nil {
		return errors.Join(err, update(domain.OperationStateFailed, "validate", "CONFIG_CHANGED"))
	}
	if oldRunning {
		if err := update(domain.OperationStateRunning, "stop-old", ""); err != nil {
			return err
		}
		if err := m.supervisor.Stop(ctx); err != nil {
			return errors.Join(err, update(domain.OperationStateFailed, "stop-old", "STOP_OLD_FAILED"))
		}
	}
	if err := update(domain.OperationStateRunning, "start-new", ""); err != nil {
		return m.rollback(ctx, &operation, oldSpec, previousActivePtr, oldRunning, false, err)
	}
	if err := m.configs.CheckSource(newSpec); err != nil {
		return m.rollback(ctx, &operation, oldSpec, previousActivePtr, oldRunning, false, err)
	}
	if err := m.supervisor.startWithHook(ctx, newSpec, func() error {
		return update(domain.OperationStateRunning, "ready", "")
	}); err != nil {
		return m.rollback(ctx, &operation, oldSpec, previousActivePtr, oldRunning, false, err)
	}
	if err := update(domain.OperationStateRunning, "commit", ""); err != nil {
		return m.rollback(ctx, &operation, oldSpec, previousActivePtr, oldRunning, false, err)
	}
	if err := m.repository.SetActiveProfile(ctx, snapshot.ProfileID, m.clock().UTC()); err != nil {
		return m.rollback(ctx, &operation, oldSpec, previousActivePtr, oldRunning, false, err)
	}
	if err := m.configs.MarkActive(newSpec); err != nil {
		return m.rollback(ctx, &operation, oldSpec, previousActivePtr, oldRunning, true, err)
	}
	return update(domain.OperationStateSucceeded, "commit", "")
}

func (m *CoreManager) rollback(ctx context.Context, operation *domain.Operation, oldSpec core.RuntimeSpec, previousActive *domain.ProfileID, oldRunning, activeChanged bool, cause error) error {
	operation.State = domain.OperationStateRollingBack
	operation.Phase = "rollback"
	operation.ErrorCode = "ACTIVATION_FAILED"
	operation.UpdatedAt = m.clock().UTC()
	updateErr := m.repository.UpdateOperation(ctx, *operation)
	failedStatus := m.supervisor.Status()
	stopErr := m.supervisor.Stop(ctx)
	if !oldRunning {
		if failedStatus.State != domain.CoreStateFailed {
			failedStatus = CoreStatus{State: domain.CoreStateFailed, ProfileID: *operation.ProfileID, ErrorCode: "START_FAILED"}
		}
		m.supervisor.setStatus(failedStatus)
		if activeChanged {
			_ = m.repository.RestoreActiveProfile(ctx, previousActive, m.clock().UTC())
		}
		operation.State = domain.OperationStateFailed
		operation.ErrorCode = "START_FAILED"
		operation.UpdatedAt = m.clock().UTC()
		return errors.Join(cause, updateErr, stopErr, m.repository.UpdateOperation(ctx, *operation))
	}
	checkErr := m.configs.CheckSource(oldSpec)
	startErr := error(nil)
	if checkErr == nil {
		startErr = m.supervisor.Start(ctx, oldSpec)
	}
	if checkErr == nil && startErr == nil {
		var activeErr error
		if activeChanged {
			activeErr = m.repository.RestoreActiveProfile(ctx, previousActive, m.clock().UTC())
		}
		markErr := m.configs.MarkActive(oldSpec)
		if activeErr == nil && markErr == nil {
			operation.State = domain.OperationStateRolledBack
			operation.ErrorCode = "ACTIVATION_FAILED"
			operation.UpdatedAt = m.clock().UTC()
			return errors.Join(cause, updateErr, stopErr, m.repository.UpdateOperation(ctx, *operation))
		}
		startErr = errors.Join(activeErr, markErr)
	}
	operation.State = domain.OperationStateFailed
	operation.ErrorCode = "RESTORE_FAILED"
	operation.UpdatedAt = m.clock().UTC()
	return errors.Join(cause, updateErr, stopErr, checkErr, startErr, m.repository.UpdateOperation(ctx, *operation))
}

func activeProfilePointer(id domain.ProfileID, ok bool) *domain.ProfileID {
	if !ok {
		return nil
	}
	return &id
}

func (m *CoreManager) Close(ctx context.Context) error {
	if m == nil || m.supervisor == nil {
		return nil
	}
	m.operationMu.Lock()
	defer m.operationMu.Unlock()
	return m.supervisor.Stop(ctx)
}
