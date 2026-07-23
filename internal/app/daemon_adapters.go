package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/daemon"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform/systemd"
)

func NewDaemonService(paths config.ManagerPaths) *DaemonService {
	controller := systemd.New(paths.UserUnitDir)
	controller.Environment = daemonUnitEnvironment()
	return &DaemonService{
		Client:     daemonIPCClient{client: ipc.NewClient(paths.Socket)},
		Runner:     localDaemonRunner{paths: paths},
		Controller: systemdController{controller: controller},
		Core:       NewCapabilityService(paths),
	}
}

func daemonUnitEnvironment() map[string]string {
	paths := config.Load()
	environment := make(map[string]string, 3)
	if _, ok := os.LookupEnv("CONFIG_DIR"); ok {
		environment["CONFIG_DIR"] = paths.ConfigDir
	}
	if _, ok := os.LookupEnv("MIHOMO_BIN"); ok {
		environment["MIHOMO_BIN"] = paths.MihomoBin
	}
	if _, ok := os.LookupEnv("MIHOMO_API_PORT"); ok {
		environment["MIHOMO_API_PORT"] = strings.TrimPrefix(paths.APIAddr, "http://127.0.0.1:")
	}
	return environment
}

func NewMigrationService(paths config.ManagerPaths) *MigrationService {
	return &MigrationService{Client: daemonMigrationIPCClient{client: ipc.NewClient(paths.Socket)}}
}

type daemonIPCClient struct{ client *ipc.Client }

type daemonMigrationIPCClient struct{ client *ipc.Client }

func (c daemonMigrationIPCClient) Plan(ctx context.Context) (legacy.Plan, error) {
	var result legacy.Plan
	if err := c.client.Do(ctx, "GET", "/v1/migrations/plan", "", nil, &result); err != nil {
		return legacy.Plan{}, mapIPCError(err)
	}
	return result, nil
}

func (c daemonMigrationIPCClient) Apply(ctx context.Context) (legacy.ApplyResult, error) {
	var result legacy.ApplyResult
	if err := c.client.Do(ctx, "POST", "/v1/migrations/apply", newRequestID("migration-apply"), nil, &result); err != nil {
		return legacy.ApplyResult{}, mapIPCError(err)
	}
	return result, nil
}

func (c daemonMigrationIPCClient) Status(ctx context.Context) ([]domain.LegacyMigration, error) {
	var result []domain.LegacyMigration
	if err := c.client.Do(ctx, "GET", "/v1/migrations/status", "", nil, &result); err != nil {
		return nil, mapIPCError(err)
	}
	return result, nil
}

func (c daemonMigrationIPCClient) Rollback(ctx context.Context, id domain.RestorePointID) (legacy.RollbackResult, error) {
	var result legacy.RollbackResult
	request := struct {
		RestorePoint string `json:"restorePoint"`
	}{RestorePoint: id.String()}
	if err := c.client.Do(ctx, "POST", "/v1/migrations/rollback", newRequestID("migration-rollback"), request, &result); err != nil {
		return legacy.RollbackResult{}, mapIPCError(err)
	}
	return result, nil
}

func newRequestID(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }

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
	corePaths := config.Load()
	adapter := mihomo.NewAdapter(mihomo.AdapterOptions{Binary: corePaths.MihomoBin})
	err := daemon.New(daemon.Options{Paths: r.paths, CoreAdapter: adapter}).Run(ctx)
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
func (c systemdController) Restart(ctx context.Context) (DaemonControlResult, error) {
	result, err := c.controller.Restart(ctx)
	return convertSystemdResult(result), mapSystemdError(err)
}

func convertSystemdResult(result systemd.Result) DaemonControlResult {
	return DaemonControlResult{
		Installed: result.Installed, Managed: result.Managed, Available: result.Available,
		Enabled: result.Enabled, Active: result.Active,
		ServiceActive: result.ServiceActive, SocketActive: result.SocketActive,
		Message: result.Message, Hint: result.Hint,
	}
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
	case "REQUEST_ID_CONFLICT", "OPERATION_CONFLICT", "CONFLICT", "CONFIG_CHANGED", "PORT_CONFLICT", "PROFILE_MODE_UNSUPPORTED", "UNSUPPORTED_PROFILE", "PROXY_AUTH_UNSUPPORTED":
		return ErrorCategoryConflict
	case "INVALID_REQUEST", "INVALID_ROUTING_MODE", "REQUEST_ID_REQUIRED":
		return ErrorCategoryInvalidArgument
	case "NOT_FOUND", "PROXY_SNAPSHOT_NOT_FOUND":
		return ErrorCategoryNotFound
	case "VALIDATION_FAILED":
		return ErrorCategoryValidationFailed
	case "PERMISSION_DENIED":
		return ErrorCategoryPermissionDenied
	case "UPSTREAM_FAILURE", "MODE_RUNTIME_MISMATCH", "CONNECTION_CLOSE_FAILED":
		return ErrorCategoryUpstreamFailure
	case "RESTORE_FAILED":
		return ErrorCategoryInternal
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
