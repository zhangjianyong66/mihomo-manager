//go:build linux

package app

import (
	"context"
	"errors"
	"os"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform/systemd"
)

type systemdController struct{ controller *systemd.Controller }

func newDaemonController(paths config.ManagerPaths) DaemonController {
	controller := systemd.New(paths.UserUnitDir)
	controller.Environment = daemonUnitEnvironment()
	return systemdController{controller: controller}
}

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
func (c systemdController) Restart(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Restart(ctx)
	return convertSystemdResult(result), mapSystemdError(err)
}

func convertSystemdResult(result systemd.Result) DaemonControlResult {
	return DaemonControlResult{
		Backend: "systemd", Ready: result.Managed && result.Available && result.ServiceActive && result.SocketActive,
		Installed: result.Installed, Managed: result.Managed, Available: result.Available,
		Enabled: result.Enabled, Active: result.Active,
		ServiceActive: result.ServiceActive, SocketActive: result.SocketActive,
		Message: result.Message, Hint: result.Hint,
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
