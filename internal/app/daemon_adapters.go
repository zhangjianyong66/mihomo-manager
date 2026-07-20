package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/daemon"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform/systemd"
)

func NewDaemonService(paths config.ManagerPaths) *DaemonService {
	return &DaemonService{
		Client:     daemonIPCClient{client: ipc.NewClient(paths.Socket)},
		Runner:     localDaemonRunner{paths: paths},
		Controller: systemdController{controller: systemd.New(paths.UserUnitDir)},
	}
}

type daemonIPCClient struct{ client *ipc.Client }

func (c daemonIPCClient) Status(ctx context.Context) (DaemonStatus, error) {
	var status DaemonStatus
	if err := c.client.Do(ctx, "GET", "/v1/status", "", nil, &status); err != nil {
		return DaemonStatus{}, mapIPCError(err)
	}
	return status, nil
}

type localDaemonRunner struct{ paths config.ManagerPaths }

func (r localDaemonRunner) Run(ctx context.Context, diagnostics io.Writer) error {
	if diagnostics != nil {
		_, _ = fmt.Fprintf(diagnostics, "mihomo-manager daemon 前台运行，socket=%s\n", r.paths.Socket)
	}
	err := daemon.New(daemon.Options{Paths: r.paths}).Run(ctx)
	if errors.Is(err, daemon.ErrRootDaemon) {
		return &Error{Category: ErrorCategoryPermissionDenied, Code: ErrorCodePermissionDenied, Message: "daemon 必须以普通用户运行"}
	}
	if errors.Is(err, platform.ErrAlreadyLocked) {
		return &Error{Category: ErrorCategoryConflict, Code: ErrorCode("DAEMON_ALREADY_RUNNING"), Message: "daemon 已在运行"}
	}
	if errors.Is(err, platform.ErrUnsafePath) {
		return &Error{Category: ErrorCategoryPermissionDenied, Code: ErrorCodePermissionDenied, Message: "daemon 路径未通过安全校验"}
	}
	return err
}

type systemdController struct{ controller *systemd.Controller }

func (c systemdController) Status(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Status(ctx)
	return convertSystemdResult(result), mapSystemdError(err)
}
func (c systemdController) Enable(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Enable(ctx)
	return convertSystemdResult(result), mapSystemdError(err)
}
func (c systemdController) Disable(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Disable(ctx)
	return convertSystemdResult(result), mapSystemdError(err)
}
func (c systemdController) Start(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Start(ctx)
	return convertSystemdResult(result), mapSystemdError(err)
}
func (c systemdController) Stop(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Stop(ctx)
	return convertSystemdResult(result), mapSystemdError(err)
}

func convertSystemdResult(result systemd.Result) DaemonControlResult {
	return DaemonControlResult{Installed: result.Installed, Enabled: result.Enabled, Active: result.Active, Message: result.Message, Hint: result.Hint}
}

func mapIPCError(err error) error {
	if errors.Is(err, ipc.ErrPermissionDenied) {
		return &Error{Category: ErrorCategoryPermissionDenied, Code: ErrorCodePermissionDenied, Message: "daemon socket 权限不足"}
	}
	if errors.Is(err, ipc.ErrProtocolMismatch) {
		return &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "daemon 协议不兼容", Retryable: false}
	}
	if errors.Is(err, ipc.ErrRequestCancelled) {
		return &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "daemon 请求已取消", Retryable: true}
	}
	var protocolErr *ipc.Error
	if errors.As(err, &protocolErr) {
		return &Error{Category: ErrorCategory(protocolCategory(protocolErr.Body.Code)), Code: ErrorCode(protocolErr.Body.Code), Message: protocolErr.Body.Message, Retryable: protocolErr.Body.Retryable, Details: protocolErr.Body.Details}
	}
	return &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "daemon 不可用或协议不兼容", Retryable: true}
}

func protocolCategory(code string) ErrorCategory {
	switch code {
	case "REQUEST_ID_CONFLICT", "OPERATION_CONFLICT":
		return ErrorCategoryConflict
	case "INVALID_REQUEST":
		return ErrorCategoryInvalidArgument
	case "PERMISSION_DENIED":
		return ErrorCategoryPermissionDenied
	default:
		return ErrorCategoryDaemonUnavailable
	}
}

func mapSystemdError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, systemd.ErrUnavailable) {
		return &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "systemd user 会话不可用，请运行 mm daemon run", Retryable: true}
	}
	if errors.Is(err, os.ErrPermission) {
		return &Error{Category: ErrorCategoryPermissionDenied, Code: ErrorCodePermissionDenied, Message: "systemd unit 权限不足"}
	}
	return &Error{Category: ErrorCategoryInternal, Code: ErrorCodeInternal, Message: "systemd daemon 操作失败", Err: err}
}
