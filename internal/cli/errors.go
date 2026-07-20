package cli

import (
	"errors"
	"fmt"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

const (
	ExitInternal          = 1
	ExitInvalidArgument   = 2
	ExitNotFound          = 3
	ExitConflict          = 4
	ExitDaemonUnavailable = 5
	ExitValidationFailed  = 6
	ExitPermissionDenied  = 7
	ExitUpstreamFailure   = 8
)

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	appErr := normalizeError(err)
	switch appErr.Classification() {
	case app.ErrorCategoryInvalidArgument:
		return ExitInvalidArgument
	case app.ErrorCategoryNotFound:
		return ExitNotFound
	case app.ErrorCategoryConflict:
		return ExitConflict
	case app.ErrorCategoryDaemonUnavailable:
		return ExitDaemonUnavailable
	case app.ErrorCategoryValidationFailed:
		return ExitValidationFailed
	case app.ErrorCategoryPermissionDenied:
		return ExitPermissionDenied
	case app.ErrorCategoryUpstreamFailure:
		return ExitUpstreamFailure
	default:
		return ExitInternal
	}
}

func (p Presenter) WriteError(err error) error {
	appErr := normalizeError(err)
	switch p.options.Format {
	case OutputTable:
		_, writeErr := fmt.Fprintln(p.stderr, appErr.Message)
		return writeErr
	case OutputJSON:
		details := appErr.Details
		if details == nil {
			details = map[string]any{}
		}
		return writeJSON(p.stderr, errorEnvelope{
			APIVersion: APIVersion,
			Kind:       "Error",
			Error: errorBody{
				Code:      appErr.Code,
				Message:   appErr.Message,
				Retryable: appErr.Retryable,
				Details:   revealSecrets(details, p.options.ShowSecrets),
			},
		})
	default:
		_, parseErr := ParseOutputFormat(p.options.Format.String())
		return parseErr
	}
}

type errorEnvelope struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Error      errorBody `json:"error"`
}

type errorBody struct {
	Code      app.ErrorCode `json:"code"`
	Message   string        `json:"message"`
	Retryable bool          `json:"retryable"`
	Details   any           `json:"details"`
}

func normalizeError(err error) *app.Error {
	var appErr *app.Error
	if errors.As(err, &appErr) {
		copy := *appErr
		if copy.Code == "" {
			copy.Code = app.ErrorCodeInternal
		}
		if copy.Message == "" {
			copy.Message = defaultErrorMessage(copy.Classification())
		}
		return &copy
	}
	return &app.Error{
		Code:    app.ErrorCodeInternal,
		Message: defaultErrorMessage(app.ErrorCategoryInternal),
		Err:     err,
	}
}

func defaultErrorMessage(category app.ErrorCategory) string {
	switch category {
	case app.ErrorCategoryInvalidArgument:
		return "输入参数无效"
	case app.ErrorCategoryNotFound:
		return "资源不存在"
	case app.ErrorCategoryConflict:
		return "当前状态不允许执行该操作"
	case app.ErrorCategoryDaemonUnavailable:
		return "daemon 不可用或协议不兼容"
	case app.ErrorCategoryValidationFailed:
		return "配置、迁移或校验失败"
	case app.ErrorCategoryPermissionDenied:
		return "权限不足或安全策略拒绝"
	case app.ErrorCategoryUpstreamFailure:
		return "上游服务失败"
	default:
		return "内部错误"
	}
}

func revealSecrets(value any, showSecrets bool) any {
	switch value := value.(type) {
	case Secret:
		return value.Display(showSecrets)
	case *Secret:
		if value == nil {
			return nil
		}
		return value.Display(showSecrets)
	case map[string]any:
		resolved := make(map[string]any, len(value))
		for key, item := range value {
			resolved[key] = revealSecrets(item, showSecrets)
		}
		return resolved
	case []any:
		resolved := make([]any, len(value))
		for i, item := range value {
			resolved[i] = revealSecrets(item, showSecrets)
		}
		return resolved
	default:
		return value
	}
}
