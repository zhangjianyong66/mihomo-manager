//go:build !linux && !darwin

package app

import (
	"context"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
)

type unsupportedDaemonController struct{}

func newDaemonController(config.ManagerPaths) DaemonController { return unsupportedDaemonController{} }

func (unsupportedDaemonController) result() (DaemonControlResult, error) {
	return DaemonControlResult{}, &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "当前平台不支持后台 daemon 管理"}
}
func (c unsupportedDaemonController) Status(context.Context) (DaemonControlResult, error) {
	return c.result()
}
func (c unsupportedDaemonController) Enable(context.Context) (DaemonControlResult, error) {
	return c.result()
}
func (c unsupportedDaemonController) Disable(context.Context) (DaemonControlResult, error) {
	return c.result()
}
func (c unsupportedDaemonController) Start(context.Context) (DaemonControlResult, error) {
	return c.result()
}
func (c unsupportedDaemonController) Stop(context.Context) (DaemonControlResult, error) {
	return c.result()
}
func (c unsupportedDaemonController) Restart(context.Context) (DaemonControlResult, error) {
	return c.result()
}
