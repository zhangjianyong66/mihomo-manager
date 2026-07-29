package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type restartStatusResponse struct {
	status DaemonStatus
	err    error
}

type fakeRestartStatusClient struct {
	responses []restartStatusResponse
	calls     int
}

func (f *fakeRestartStatusClient) Status(context.Context) (DaemonStatus, error) {
	f.calls++
	if len(f.responses) == 0 {
		return DaemonStatus{}, errors.New("no fake daemon status response")
	}
	response := f.responses[0]
	if len(f.responses) > 1 {
		f.responses = f.responses[1:]
	}
	return response.status, response.err
}

type fakeRestartCore struct {
	actions    []string
	failAction string
	failCount  int
}

func (f *fakeRestartCore) CoreAction(_ context.Context, _ string, action string) error {
	f.actions = append(f.actions, action)
	if action == f.failAction && f.failCount > 0 {
		f.failCount--
		return errors.New("fake core action failure")
	}
	return nil
}

type fakeRestartController struct {
	result       DaemonControlResult
	statusErr    error
	restartErr   error
	startErr     error
	statusCalls  int
	restartCalls int
	startCalls   int
}

func (f *fakeRestartController) Status(context.Context) (DaemonControlResult, error) {
	f.statusCalls++
	return f.result, f.statusErr
}
func (f *fakeRestartController) Enable(context.Context) (DaemonControlResult, error) {
	return f.result, nil
}
func (f *fakeRestartController) Disable(context.Context) (DaemonControlResult, error) {
	return f.result, nil
}
func (f *fakeRestartController) Start(context.Context) (DaemonControlResult, error) {
	f.startCalls++
	return f.result, f.startErr
}
func (f *fakeRestartController) Stop(context.Context) (DaemonControlResult, error) {
	return f.result, nil
}
func (f *fakeRestartController) Restart(context.Context) (DaemonControlResult, error) {
	f.restartCalls++
	return f.result, f.restartErr
}

func managedActiveControl() DaemonControlResult {
	return DaemonControlResult{
		Backend: "systemd", Ready: true, Installed: true, Managed: true, Available: true,
		Active: true, ServiceActive: true, SocketActive: true,
	}
}

func daemonStatus(pid int, startedAt time.Time, state domain.CoreState) DaemonStatus {
	return DaemonStatus{
		ProtocolVersion: 1, State: "running", PID: pid, StartedAt: startedAt,
		Core: DaemonCoreStatus{State: state.String(), ProfileID: "legacy-mihomo"},
	}
}

func newRestartService(client *fakeRestartStatusClient, core *fakeRestartCore, controller *fakeRestartController) *DaemonService {
	return &DaemonService{
		Client: client, Core: core, Controller: controller,
		restartTimeout: time.Second, readyTimeout: time.Second, recoveryTimeout: time.Second,
		pollInterval: time.Millisecond,
	}
}

func TestDaemonRestart_CoreStateMatrix(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	tests := []struct {
		state       domain.CoreState
		final       domain.CoreState
		wantActions []string
		wantPhases  []DaemonRestartPhase
	}{
		{state: domain.CoreStateRunning, final: domain.CoreStateRunning, wantActions: []string{"stop", "start"}, wantPhases: []DaemonRestartPhase{DaemonRestartPhasePreflight, DaemonRestartPhaseStopCore, DaemonRestartPhaseRestartDaemon, DaemonRestartPhaseWaitDaemon, DaemonRestartPhaseRestoreCore, DaemonRestartPhaseVerify}},
		{state: domain.CoreStateStopped, final: domain.CoreStateStopped, wantPhases: []DaemonRestartPhase{DaemonRestartPhasePreflight, DaemonRestartPhaseRestartDaemon, DaemonRestartPhaseWaitDaemon, DaemonRestartPhaseVerify}},
		{state: domain.CoreStateDegraded, final: domain.CoreStateRunning, wantActions: []string{"stop", "start"}, wantPhases: []DaemonRestartPhase{DaemonRestartPhasePreflight, DaemonRestartPhaseStopCore, DaemonRestartPhaseRestartDaemon, DaemonRestartPhaseWaitDaemon, DaemonRestartPhaseRestoreCore, DaemonRestartPhaseVerify}},
		{state: domain.CoreStateFailed, final: domain.CoreStateRunning, wantActions: []string{"stop", "start"}, wantPhases: []DaemonRestartPhase{DaemonRestartPhasePreflight, DaemonRestartPhaseStopCore, DaemonRestartPhaseRestartDaemon, DaemonRestartPhaseWaitDaemon, DaemonRestartPhaseRestoreCore, DaemonRestartPhaseVerify}},
	}
	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			client := &fakeRestartStatusClient{responses: []restartStatusResponse{
				{status: daemonStatus(100, oldTime, tt.state)},
				{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
				{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
				{status: daemonStatus(200, newTime, tt.final)},
			}}
			core := &fakeRestartCore{}
			controller := &fakeRestartController{result: managedActiveControl()}
			service := newRestartService(client, core, controller)
			var phases []DaemonRestartPhase
			result, err := service.Restart(context.Background(), func(progress DaemonRestartProgress) {
				phases = append(phases, progress.Phase)
			})
			if err != nil {
				t.Fatal(err)
			}
			if !result.DaemonRestarted || result.PreviousCoreState != tt.state || result.CoreState != tt.final {
				t.Fatalf("unexpected result: %+v", result)
			}
			if (tt.final == domain.CoreStateRunning) != result.CoreRestored {
				t.Fatalf("unexpected core restored flag: %+v", result)
			}
			if !reflect.DeepEqual(core.actions, tt.wantActions) || !reflect.DeepEqual(phases, tt.wantPhases) {
				t.Fatalf("actions=%v phases=%v", core.actions, phases)
			}
			if controller.restartCalls != 1 || controller.startCalls != 0 {
				t.Fatalf("unexpected controller calls: %+v", controller)
			}
		})
	}
}

func TestDaemonRestart_TransitionalStatesHaveNoSideEffects(t *testing.T) {
	for _, state := range []domain.CoreState{domain.CoreStateStarting, domain.CoreStateStopping} {
		t.Run(state.String(), func(t *testing.T) {
			client := &fakeRestartStatusClient{responses: []restartStatusResponse{{status: daemonStatus(100, time.Now(), state)}}}
			core := &fakeRestartCore{}
			controller := &fakeRestartController{result: managedActiveControl()}
			_, err := newRestartService(client, core, controller).Restart(context.Background(), nil)
			var appErr *Error
			if !errors.As(err, &appErr) || appErr.Code != ErrorCodeDaemonRestartCoreBusy || appErr.Classification() != ErrorCategoryConflict {
				t.Fatalf("unexpected error: %#v", err)
			}
			if len(core.actions) != 0 || controller.restartCalls != 0 || controller.startCalls != 0 {
				t.Fatalf("preflight changed state: core=%v controller=%+v", core.actions, controller)
			}
		})
	}
}

func TestDaemonRestart_PreflightRejectsUnavailableOrUnmanagedBackend(t *testing.T) {
	tests := []struct {
		name   string
		result DaemonControlResult
	}{
		{name: "unavailable", result: DaemonControlResult{Backend: "systemd", Installed: true, Managed: true}},
		{name: "unmanaged", result: DaemonControlResult{Backend: "systemd", Ready: true, Installed: true, Available: true, ServiceActive: true, SocketActive: true}},
		{name: "backend missing", result: DaemonControlResult{Ready: true, Installed: true, Managed: true, Available: true, ServiceActive: true, SocketActive: true}},
		{name: "backend unsupported", result: DaemonControlResult{Backend: "other", Ready: true, Installed: true, Managed: true, Available: true}},
		{name: "systemd not ready", result: DaemonControlResult{Backend: "systemd", Installed: true, Managed: true, Available: true, SocketActive: true}},
		{name: "launchd not ready", result: DaemonControlResult{Backend: "launchd", Installed: true, Managed: true, Available: true, ServiceActive: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeRestartStatusClient{responses: []restartStatusResponse{{status: daemonStatus(100, time.Now(), domain.CoreStateRunning)}}}
			core := &fakeRestartCore{}
			controller := &fakeRestartController{result: tt.result}
			_, err := newRestartService(client, core, controller).Restart(context.Background(), nil)
			if err == nil {
				t.Fatal("expected preflight error")
			}
			if len(core.actions) != 0 || controller.restartCalls != 0 || controller.startCalls != 0 {
				t.Fatalf("preflight changed state: core=%v controller=%+v", core.actions, controller)
			}
		})
	}
}

func TestDaemonRestart_AcceptsReadyLaunchd(t *testing.T) {
	oldTime := time.Date(2026, 7, 29, 1, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	client := &fakeRestartStatusClient{responses: []restartStatusResponse{
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
	}}
	controller := &fakeRestartController{result: DaemonControlResult{
		Backend: "launchd", Ready: true, Installed: true, Managed: true, Available: true,
		Enabled: true, Active: true, ServiceActive: true,
	}}
	result, err := newRestartService(client, &fakeRestartCore{}, controller).Restart(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DaemonRestarted || result.CoreState != domain.CoreStateStopped || controller.restartCalls != 1 {
		t.Fatalf("unexpected launchd restart: result=%+v controller=%+v", result, controller)
	}
}

func TestDaemonRestart_RestartFailureRecoversOriginalRunningCore(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	client := &fakeRestartStatusClient{responses: []restartStatusResponse{
		{status: daemonStatus(100, oldTime, domain.CoreStateRunning)},
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime, domain.CoreStateRunning)},
	}}
	core := &fakeRestartCore{}
	controller := &fakeRestartController{result: managedActiveControl(), restartErr: errors.New("restart failed")}
	result, err := newRestartService(client, core, controller).Restart(context.Background(), nil)
	if err == nil || !result.RecoveryAttempted || !result.RecoverySucceeded || result.FailurePhase != DaemonRestartPhaseRestartDaemon || result.CoreState != domain.CoreStateRunning {
		t.Fatalf("unexpected recovery result: result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(core.actions, []string{"stop", "start"}) || controller.startCalls != 1 {
		t.Fatalf("unexpected recovery calls: core=%v controller=%+v", core.actions, controller)
	}
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Details["restart"] == nil {
		t.Fatalf("partial result missing from error: %#v", err)
	}
}

func TestDaemonRestart_StopFailureUsesBoundedRecovery(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	client := &fakeRestartStatusClient{responses: []restartStatusResponse{
		{status: daemonStatus(100, oldTime, domain.CoreStateRunning)},
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime, domain.CoreStateRunning)},
	}}
	core := &fakeRestartCore{failAction: "stop", failCount: 1}
	controller := &fakeRestartController{result: managedActiveControl()}
	result, err := newRestartService(client, core, controller).Restart(context.Background(), nil)
	if err == nil || result.FailurePhase != DaemonRestartPhaseStopCore || !result.RecoverySucceeded || controller.restartCalls != 0 {
		t.Fatalf("unexpected stop recovery: result=%+v controller=%+v err=%v", result, controller, err)
	}
	if !reflect.DeepEqual(core.actions, []string{"stop", "start"}) {
		t.Fatalf("unexpected core recovery actions: %v", core.actions)
	}
}

func TestDaemonRestart_ConcurrentDaemonChangeAfterStopDoesNotContinueForward(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	client := &fakeRestartStatusClient{responses: []restartStatusResponse{
		{status: daemonStatus(100, oldTime, domain.CoreStateRunning)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateRunning)},
	}}
	core := &fakeRestartCore{}
	controller := &fakeRestartController{result: managedActiveControl()}
	result, err := newRestartService(client, core, controller).Restart(context.Background(), nil)
	if err == nil || result.FailurePhase != DaemonRestartPhaseStopCore || !result.RecoverySucceeded || !result.DaemonRestarted || controller.restartCalls != 0 {
		t.Fatalf("unexpected concurrent-change result: result=%+v controller=%+v err=%v", result, controller, err)
	}
	if !reflect.DeepEqual(core.actions, []string{"stop", "start"}) {
		t.Fatalf("unexpected concurrent-change recovery: %v", core.actions)
	}
}

func TestDaemonRestart_CoreRestoreFailureIsRetriedByRecovery(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	client := &fakeRestartStatusClient{responses: []restartStatusResponse{
		{status: daemonStatus(100, oldTime, domain.CoreStateRunning)},
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateRunning)},
	}}
	core := &fakeRestartCore{failAction: "start", failCount: 1}
	controller := &fakeRestartController{result: managedActiveControl()}
	result, err := newRestartService(client, core, controller).Restart(context.Background(), nil)
	if err == nil || !result.RecoverySucceeded || result.FailurePhase != DaemonRestartPhaseRestoreCore || result.CoreState != domain.CoreStateRunning {
		t.Fatalf("unexpected retry result: result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(core.actions, []string{"stop", "start", "start"}) {
		t.Fatalf("restore was not retried exactly once: %v", core.actions)
	}
}

func TestDaemonRestart_VerifyFailureRecoversTargetState(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	client := &fakeRestartStatusClient{responses: []restartStatusResponse{
		{status: daemonStatus(100, oldTime, domain.CoreStateRunning)},
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopping)},
		{status: daemonStatus(200, newTime, domain.CoreStateStopped)},
		{status: daemonStatus(200, newTime, domain.CoreStateRunning)},
	}}
	core := &fakeRestartCore{}
	controller := &fakeRestartController{result: managedActiveControl()}
	result, err := newRestartService(client, core, controller).Restart(context.Background(), nil)
	if err == nil || result.FailurePhase != DaemonRestartPhaseVerify || !result.RecoverySucceeded || result.CoreState != domain.CoreStateRunning {
		t.Fatalf("unexpected verify recovery: result=%+v err=%v", result, err)
	}
	if !reflect.DeepEqual(core.actions, []string{"stop", "start", "start"}) {
		t.Fatalf("unexpected verify recovery actions: %v", core.actions)
	}
}

func TestDaemonRestart_RequiresBothPIDAndStartedAtToChange(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	client := &fakeRestartStatusClient{responses: []restartStatusResponse{
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime.Add(time.Minute), domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime.Add(time.Minute), domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime.Add(time.Minute), domain.CoreStateStopped)},
	}}
	core := &fakeRestartCore{}
	controller := &fakeRestartController{result: managedActiveControl()}
	service := newRestartService(client, core, controller)
	service.sleep = func(context.Context, time.Duration) error { return context.DeadlineExceeded }
	result, err := service.Restart(context.Background(), nil)
	if err == nil || result.FailurePhase != DaemonRestartPhaseWaitDaemon || result.DaemonRestarted || !result.RecoverySucceeded {
		t.Fatalf("same PID should not pass identity check: result=%+v err=%v", result, err)
	}
}

func TestDaemonRestart_ProtocolMismatchStopsRecoveryBeforeCoreAction(t *testing.T) {
	oldTime := time.Date(2026, 7, 22, 1, 0, 0, 0, time.UTC)
	protocolErr := &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "daemon 协议不兼容", Retryable: false}
	client := &fakeRestartStatusClient{responses: []restartStatusResponse{
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{status: daemonStatus(100, oldTime, domain.CoreStateStopped)},
		{err: protocolErr},
		{err: protocolErr},
	}}
	core := &fakeRestartCore{}
	controller := &fakeRestartController{result: managedActiveControl()}
	result, err := newRestartService(client, core, controller).Restart(context.Background(), nil)
	if err == nil || !result.RecoveryAttempted || result.RecoverySucceeded || result.FailurePhase != DaemonRestartPhaseWaitDaemon {
		t.Fatalf("unexpected protocol result: result=%+v err=%v", result, err)
	}
	if len(core.actions) != 0 || controller.startCalls != 1 {
		t.Fatalf("protocol mismatch should not control Core: core=%v controller=%+v", core.actions, controller)
	}
}
