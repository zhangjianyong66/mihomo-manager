package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type DaemonRestartPhase string

const (
	DaemonRestartPhasePreflight     DaemonRestartPhase = "preflight"
	DaemonRestartPhaseStopCore      DaemonRestartPhase = "stop_core"
	DaemonRestartPhaseRestartDaemon DaemonRestartPhase = "restart_daemon"
	DaemonRestartPhaseWaitDaemon    DaemonRestartPhase = "wait_daemon"
	DaemonRestartPhaseRestoreCore   DaemonRestartPhase = "restore_core"
	DaemonRestartPhaseVerify        DaemonRestartPhase = "verify"
	DaemonRestartPhaseRecover       DaemonRestartPhase = "recover"
)

const (
	ErrorCodeDaemonRestartCoreBusy  ErrorCode = "DAEMON_RESTART_CORE_BUSY"
	ErrorCodeDaemonRestartPreflight ErrorCode = "DAEMON_RESTART_PREFLIGHT_FAILED"
	ErrorCodeDaemonRestartTimeout   ErrorCode = "DAEMON_RESTART_TIMEOUT"
)

type DaemonRestartProgress struct {
	Phase   DaemonRestartPhase `json:"phase"`
	Step    int                `json:"step"`
	Total   int                `json:"total"`
	Message string             `json:"message"`
}

type DaemonRestartResult struct {
	PreviousDaemonPID int                `json:"previousDaemonPid"`
	DaemonPID         int                `json:"daemonPid"`
	PreviousStartedAt time.Time          `json:"previousStartedAt"`
	StartedAt         time.Time          `json:"startedAt"`
	PreviousCoreState domain.CoreState   `json:"previousCoreState"`
	CoreState         domain.CoreState   `json:"coreState"`
	DaemonRestarted   bool               `json:"daemonRestarted"`
	CoreRestored      bool               `json:"coreRestored"`
	RecoveryAttempted bool               `json:"recoveryAttempted"`
	RecoverySucceeded bool               `json:"recoverySucceeded"`
	FailurePhase      DaemonRestartPhase `json:"failurePhase,omitempty"`
}

type DaemonCoreController interface {
	CoreAction(context.Context, string, string) error
}

type DaemonRestartProgressFunc func(DaemonRestartProgress)

const (
	defaultDaemonRestartTimeout  = 40 * time.Second
	defaultDaemonReadyTimeout    = 10 * time.Second
	defaultDaemonRecoveryTimeout = 15 * time.Second
	defaultDaemonPollInterval    = 200 * time.Millisecond
)

func (s *DaemonService) Restart(ctx context.Context, progress DaemonRestartProgressFunc) (DaemonRestartResult, error) {
	var result DaemonRestartResult
	if s == nil || s.Client == nil || s.Controller == nil || s.Core == nil {
		return result, daemonRestartFailure(DaemonRestartPhasePreflight, result, &Error{
			Code: ErrorCodeInternal, Message: "daemon 组合重启依赖未配置",
		}, false, nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	operationCtx, cancel := context.WithTimeout(ctx, s.duration(s.restartTimeout, defaultDaemonRestartTimeout))
	defer cancel()

	emitDaemonRestartProgress(progress, DaemonRestartPhasePreflight, "检查 daemon、后台服务管理器与 Core 状态")
	previous, err := s.Client.Status(operationCtx)
	if err != nil {
		return result, daemonRestartFailure(DaemonRestartPhasePreflight, result, err, false, nil)
	}
	result = restartResultFromStatus(previous)

	control, err := s.Controller.Status(operationCtx)
	if err != nil {
		return result, daemonRestartFailure(DaemonRestartPhasePreflight, result, err, false, nil)
	}
	if !control.Available {
		err = &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonRestartPreflight, Message: "后台服务管理器不可用，不能组合重启前台 daemon"}
		return result, daemonRestartFailure(DaemonRestartPhasePreflight, result, err, false, nil)
	}
	if !control.Installed || !control.Managed {
		err = &Error{Category: ErrorCategoryConflict, Code: ErrorCodeDaemonRestartPreflight, Message: "daemon 后台服务不是完整的受管资产，已拒绝组合重启"}
		return result, daemonRestartFailure(DaemonRestartPhasePreflight, result, err, false, nil)
	}
	if !daemonControlReady(control) {
		err = &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonRestartPreflight, Message: "受管 daemon 后台服务必须处于就绪状态"}
		return result, daemonRestartFailure(DaemonRestartPhasePreflight, result, err, false, nil)
	}

	previousState := domain.CoreState(previous.Core.State)
	if err := previousState.Validate(); err != nil {
		appErr := &Error{Category: ErrorCategoryConflict, Code: ErrorCodeDaemonRestartPreflight, Message: "Core 状态不可识别，已拒绝组合重启", Err: err}
		return result, daemonRestartFailure(DaemonRestartPhasePreflight, result, appErr, false, nil)
	}
	if previousState == domain.CoreStateStarting || previousState == domain.CoreStateStopping {
		appErr := &Error{Category: ErrorCategoryConflict, Code: ErrorCodeDaemonRestartCoreBusy, Message: "Core 正在切换状态，请稍后重试"}
		return result, daemonRestartFailure(DaemonRestartPhasePreflight, result, appErr, false, nil)
	}
	targetRunning := previousState != domain.CoreStateStopped
	profileID := previous.Core.ProfileID
	sideEffects := false

	if targetRunning {
		emitDaemonRestartProgress(progress, DaemonRestartPhaseStopCore, "停止 Core 并复核状态")
		sideEffects = true
		if err := s.Core.CoreAction(operationCtx, profileID, "stop"); err != nil {
			return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseStopCore, result, profileID, targetRunning, sideEffects, err)
		}
	}

	stopped, err := s.Client.Status(operationCtx)
	if err != nil {
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseStopCore, result, profileID, targetRunning, sideEffects, err)
	}
	result.CoreState = domain.CoreState(stopped.Core.State)
	if !sameDaemon(previous, stopped) {
		err = &Error{Category: ErrorCategoryConflict, Code: ErrorCodeConflict, Message: "预检期间 daemon 已发生变化，已拒绝继续重启"}
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseStopCore, result, profileID, targetRunning, sideEffects, err)
	}
	if result.CoreState != domain.CoreStateStopped {
		err = &Error{Category: ErrorCategoryConflict, Code: ErrorCodeDaemonRestartCoreBusy, Message: "Core 未稳定停止，已拒绝重启 daemon"}
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseStopCore, result, profileID, targetRunning, sideEffects, err)
	}

	emitDaemonRestartProgress(progress, DaemonRestartPhaseRestartDaemon, "重启受管 daemon 后台服务")
	sideEffects = true
	if _, err := s.Controller.Restart(operationCtx); err != nil {
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseRestartDaemon, result, profileID, targetRunning, sideEffects, err)
	}

	emitDaemonRestartProgress(progress, DaemonRestartPhaseWaitDaemon, "等待新 daemon 完成协议握手")
	ready, err := s.waitForDaemon(operationCtx, &previous, true, s.duration(s.readyTimeout, defaultDaemonReadyTimeout))
	updateRestartResult(&result, ready)
	if err != nil {
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseWaitDaemon, result, profileID, targetRunning, sideEffects, err)
	}

	if targetRunning {
		emitDaemonRestartProgress(progress, DaemonRestartPhaseRestoreCore, "在新 daemon 中恢复 Core")
		if err := s.Core.CoreAction(operationCtx, profileID, "start"); err != nil {
			return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseRestoreCore, result, profileID, targetRunning, sideEffects, err)
		}
	}

	emitDaemonRestartProgress(progress, DaemonRestartPhaseVerify, "验证 daemon 与 Core 最终状态")
	finalStatus, err := s.Client.Status(operationCtx)
	updateRestartResult(&result, finalStatus)
	if err != nil {
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseVerify, result, profileID, targetRunning, sideEffects, err)
	}
	if !sameDaemon(ready, finalStatus) {
		err = &Error{Category: ErrorCategoryConflict, Code: ErrorCodeConflict, Message: "最终验证期间 daemon 身份再次变化"}
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseVerify, result, profileID, targetRunning, sideEffects, err)
	}
	if !restartTargetReached(result.CoreState, targetRunning) {
		err = &Error{Category: ErrorCategoryUpstreamFailure, Code: ErrorCodeUpstreamFailure, Message: "Core 未达到组合重启目标状态"}
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseVerify, result, profileID, targetRunning, sideEffects, err)
	}
	finalControl, err := s.Controller.Status(operationCtx)
	if err != nil || !finalControl.Available || !finalControl.Managed || !daemonControlReady(finalControl) || !sameDaemonBackend(control, finalControl) {
		if err == nil {
			err = &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "后台服务管理器最终状态验证失败"}
		}
		return s.failAndRecover(operationCtx, progress, DaemonRestartPhaseVerify, result, profileID, targetRunning, sideEffects, err)
	}
	result.CoreRestored = targetRunning && result.CoreState == domain.CoreStateRunning
	return result, nil
}

func (s *DaemonService) failAndRecover(ctx context.Context, progress DaemonRestartProgressFunc, phase DaemonRestartPhase, result DaemonRestartResult, profileID string, targetRunning, sideEffects bool, cause error) (DaemonRestartResult, error) {
	result.FailurePhase = phase
	if !sideEffects {
		return result, daemonRestartFailure(phase, result, cause, false, nil)
	}
	emitDaemonRestartProgress(progress, DaemonRestartPhaseRecover, "尝试恢复 daemon 与 Core 目标状态")
	result.RecoveryAttempted = true
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.duration(s.recoveryTimeout, defaultDaemonRecoveryTimeout))
	defer cancel()
	recovered, recoveryErr := s.recover(recoveryCtx, result, profileID, targetRunning)
	recovered.FailurePhase = phase
	recovered.RecoveryAttempted = true
	recovered.RecoverySucceeded = recoveryErr == nil
	return recovered, daemonRestartFailure(phase, recovered, cause, recoveryErr == nil, recoveryErr)
}

func (s *DaemonService) recover(ctx context.Context, result DaemonRestartResult, profileID string, targetRunning bool) (DaemonRestartResult, error) {
	if _, err := s.Controller.Start(ctx); err != nil {
		return result, err
	}
	status, err := s.waitForDaemon(ctx, nil, false, s.duration(s.readyTimeout, defaultDaemonReadyTimeout))
	updateRestartResult(&result, status)
	if err != nil {
		return result, err
	}
	state := domain.CoreState(status.Core.State)
	if targetRunning && state != domain.CoreStateRunning {
		if err := s.Core.CoreAction(ctx, profileID, "start"); err != nil {
			return result, err
		}
	}
	if !targetRunning && state != domain.CoreStateStopped {
		if err := s.Core.CoreAction(ctx, profileID, "stop"); err != nil {
			return result, err
		}
	}
	status, err = s.Client.Status(ctx)
	updateRestartResult(&result, status)
	if err != nil {
		return result, err
	}
	if !restartTargetReached(result.CoreState, targetRunning) {
		return result, &Error{Category: ErrorCategoryUpstreamFailure, Code: ErrorCodeUpstreamFailure, Message: "自动恢复后 Core 状态仍不符合预期"}
	}
	control, err := s.Controller.Status(ctx)
	if err != nil {
		return result, err
	}
	if !control.Available || !control.Managed || !daemonControlReady(control) {
		return result, &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "自动恢复后后台服务状态仍不完整"}
	}
	result.CoreRestored = targetRunning && result.CoreState == domain.CoreStateRunning
	return result, nil
}

func daemonControlReady(control DaemonControlResult) bool {
	switch control.Backend {
	case "systemd", "launchd":
		return control.Ready
	default:
		return false
	}
}

func sameDaemonBackend(left, right DaemonControlResult) bool {
	return left.Backend != "" && left.Backend == right.Backend
}

func (s *DaemonService) waitForDaemon(ctx context.Context, previous *DaemonStatus, requireChanged bool, timeout time.Duration) (DaemonStatus, error) {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last DaemonStatus
	for {
		status, err := s.Client.Status(waitCtx)
		if err == nil {
			last = status
			if !requireChanged || (previous != nil && daemonIdentityChanged(*previous, status)) {
				return status, nil
			}
		} else if !retryableDaemonError(err) {
			return last, err
		}
		if err := s.sleepFor(waitCtx, s.duration(s.pollInterval, defaultDaemonPollInterval)); err != nil {
			return last, &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonRestartTimeout, Message: "等待 daemon 协议握手超时", Retryable: true, Err: err}
		}
	}
}

func restartResultFromStatus(status DaemonStatus) DaemonRestartResult {
	state := domain.CoreState(status.Core.State)
	return DaemonRestartResult{
		PreviousDaemonPID: status.PID, DaemonPID: status.PID,
		PreviousStartedAt: status.StartedAt, StartedAt: status.StartedAt,
		PreviousCoreState: state, CoreState: state,
	}
}

func updateRestartResult(result *DaemonRestartResult, status DaemonStatus) {
	if result == nil || status.PID == 0 {
		return
	}
	result.DaemonPID = status.PID
	result.StartedAt = status.StartedAt
	result.CoreState = domain.CoreState(status.Core.State)
	result.DaemonRestarted = result.PreviousDaemonPID != 0 && result.PreviousDaemonPID != status.PID && !result.PreviousStartedAt.Equal(status.StartedAt)
}

func daemonIdentityChanged(previous, current DaemonStatus) bool {
	return previous.PID != 0 && current.PID != 0 && previous.PID != current.PID && !previous.StartedAt.Equal(current.StartedAt)
}

func sameDaemon(left, right DaemonStatus) bool {
	return left.PID != 0 && left.PID == right.PID && left.StartedAt.Equal(right.StartedAt)
}

func restartTargetReached(state domain.CoreState, targetRunning bool) bool {
	if targetRunning {
		return state == domain.CoreStateRunning
	}
	return state == domain.CoreStateStopped
}

func retryableDaemonError(err error) bool {
	var appErr *Error
	if !errors.As(err, &appErr) {
		return true
	}
	return appErr.Classification() == ErrorCategoryDaemonUnavailable && appErr.Retryable
}

func (s *DaemonService) duration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func (s *DaemonService) sleepFor(ctx context.Context, duration time.Duration) error {
	if s.sleep != nil {
		return s.sleep(ctx, duration)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func emitDaemonRestartProgress(progress DaemonRestartProgressFunc, phase DaemonRestartPhase, message string) {
	if progress == nil {
		return
	}
	step := map[DaemonRestartPhase]int{
		DaemonRestartPhasePreflight: 1, DaemonRestartPhaseStopCore: 2,
		DaemonRestartPhaseRestartDaemon: 3, DaemonRestartPhaseWaitDaemon: 4,
		DaemonRestartPhaseRestoreCore: 5, DaemonRestartPhaseVerify: 6,
		DaemonRestartPhaseRecover: 7,
	}[phase]
	total := 6
	if phase == DaemonRestartPhaseRecover {
		total = 7
	}
	progress(DaemonRestartProgress{Phase: phase, Step: step, Total: total, Message: message})
}

func daemonRestartFailure(phase DaemonRestartPhase, result DaemonRestartResult, cause error, recovered bool, recoveryErr error) error {
	result.FailurePhase = phase
	appErr := normalizeRestartError(cause)
	details := make(map[string]any, len(appErr.Details)+2)
	for key, value := range appErr.Details {
		details[key] = value
	}
	details["restart"] = result
	if recoveryErr != nil {
		details["recovery"] = "failed"
	}
	appErr.Details = details
	phaseName := daemonRestartPhaseName(phase)
	if recovered {
		appErr.Message = fmt.Sprintf("组合重启在%s阶段失败，服务已恢复；请执行 mm daemon status 和 mm core status 复核", phaseName)
	} else if result.RecoveryAttempted {
		appErr.Message = fmt.Sprintf("组合重启在%s阶段失败，自动恢复未完成；请执行 mm daemon start，然后执行 mm core start", phaseName)
	} else {
		appErr.Message = fmt.Sprintf("组合重启在%s阶段失败：%s", phaseName, appErr.Message)
	}
	return appErr
}

func normalizeRestartError(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		copy := *appErr
		return &copy
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonRestartTimeout, Message: "组合重启等待超时", Retryable: true, Err: err}
	}
	return &Error{Category: ErrorCategoryInternal, Code: ErrorCodeInternal, Message: "组合重启内部失败", Err: err}
}

func daemonRestartPhaseName(phase DaemonRestartPhase) string {
	switch phase {
	case DaemonRestartPhasePreflight:
		return "预检"
	case DaemonRestartPhaseStopCore:
		return "停止 Core"
	case DaemonRestartPhaseRestartDaemon:
		return "重启 daemon"
	case DaemonRestartPhaseWaitDaemon:
		return "等待 daemon"
	case DaemonRestartPhaseRestoreCore:
		return "恢复 Core"
	case DaemonRestartPhaseVerify:
		return "最终验证"
	case DaemonRestartPhaseRecover:
		return "自动恢复"
	default:
		return string(phase)
	}
}
