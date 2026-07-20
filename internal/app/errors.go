package app

type ErrorCode string

type ErrorCategory string

const (
	ErrorCategoryInternal          ErrorCategory = "internal"
	ErrorCategoryInvalidArgument   ErrorCategory = "invalid_argument"
	ErrorCategoryNotFound          ErrorCategory = "not_found"
	ErrorCategoryConflict          ErrorCategory = "conflict"
	ErrorCategoryDaemonUnavailable ErrorCategory = "daemon_unavailable"
	ErrorCategoryValidationFailed  ErrorCategory = "validation_failed"
	ErrorCategoryPermissionDenied  ErrorCategory = "permission_denied"
	ErrorCategoryUpstreamFailure   ErrorCategory = "upstream_failure"
)

const (
	ErrorCodeInternal          ErrorCode = "INTERNAL"
	ErrorCodeInvalidArgument   ErrorCode = "INVALID_ARGUMENT"
	ErrorCodeNotFound          ErrorCode = "NOT_FOUND"
	ErrorCodeConflict          ErrorCode = "CONFLICT"
	ErrorCodeDaemonUnavailable ErrorCode = "DAEMON_UNAVAILABLE"
	ErrorCodeValidationFailed  ErrorCode = "VALIDATION_FAILED"
	ErrorCodePermissionDenied  ErrorCode = "PERMISSION_DENIED"
	ErrorCodeUpstreamFailure   ErrorCode = "UPSTREAM_FAILURE"
)

type Error struct {
	Category  ErrorCategory
	Code      ErrorCode
	Message   string
	Retryable bool
	Details   map[string]any
	Err       error
}

func (e *Error) Classification() ErrorCategory {
	if e == nil {
		return ErrorCategoryInternal
	}
	if e.Category != "" {
		return e.Category
	}
	switch e.Code {
	case ErrorCodeInvalidArgument:
		return ErrorCategoryInvalidArgument
	case ErrorCodeNotFound:
		return ErrorCategoryNotFound
	case ErrorCodeConflict:
		return ErrorCategoryConflict
	case ErrorCodeDaemonUnavailable:
		return ErrorCategoryDaemonUnavailable
	case ErrorCodeValidationFailed:
		return ErrorCategoryValidationFailed
	case ErrorCodePermissionDenied:
		return ErrorCategoryPermissionDenied
	case ErrorCodeUpstreamFailure:
		return ErrorCategoryUpstreamFailure
	default:
		return ErrorCategoryInternal
	}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
