//go:build darwin

package app

import (
	"context"
	"errors"
	"os"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform/launchd"
)

type launchdController struct{ controller *launchd.Controller }

func newDaemonController(paths config.ManagerPaths) DaemonController {
	controller := launchd.New(paths.LaunchAgent, paths.MMBinary, paths.DaemonStdout, paths.DaemonStderr)
	controller.Environment = daemonUnitEnvironment()
	return launchdController{controller: controller}
}

func (c launchdController) Status(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Status(ctx)
	return convertLaunchdResult(result), mapLaunchdError(err)
}
func (c launchdController) Enable(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Enable(ctx)
	return convertLaunchdResult(result), mapLaunchdError(err)
}
func (c launchdController) Disable(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Disable(ctx)
	return convertLaunchdResult(result), mapLaunchdError(err)
}
func (c launchdController) Start(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Start(ctx)
	return convertLaunchdResult(result), mapLaunchdError(err)
}
func (c launchdController) Stop(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Stop(ctx)
	return convertLaunchdResult(result), mapLaunchdError(err)
}
func (c launchdController) Restart(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Restart(ctx)
	return convertLaunchdResult(result), mapLaunchdError(err)
}

func convertLaunchdResult(result launchd.Result) DaemonControlResult {
	return DaemonControlResult{
		Backend: "launchd", Ready: result.Ready,
		Installed: result.Installed, Managed: result.Managed, Available: result.Available,
		Enabled: result.Enabled, Active: result.Active,
		ServiceActive: result.ServiceActive, SocketActive: result.SocketActive,
		Message: result.Message, Hint: result.Hint,
	}
}

func mapLaunchdError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, launchd.ErrUnavailable) {
		return &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "launchd 用户登录域不可用，请运行 mm daemon run", Retryable: true}
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, platform.ErrUnsafePath) {
		return &Error{Category: ErrorCategoryPermissionDenied, Code: ErrorCodePermissionDenied, Message: "launchd plist 权限不足"}
	}
	return &Error{Category: ErrorCategoryInternal, Code: ErrorCodeInternal, Message: "launchd daemon 操作失败", Err: err}
}
