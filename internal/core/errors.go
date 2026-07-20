package core

import "errors"

var (
	ErrConfigChanged      = errors.New("config source changed")
	ErrInvalidConfig      = errors.New("invalid core config")
	ErrValidationFailed   = errors.New("core config validation failed")
	ErrValidationTimeout  = errors.New("core config validation timed out")
	ErrProcessExited      = errors.New("core process exited before ready")
	ErrReadinessTimeout   = errors.New("core readiness timed out")
	ErrUnsupportedProfile = errors.New("unsupported profile mode")
)
