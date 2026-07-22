package core

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

var (
	ErrConfigChanged      = errors.New("config source changed")
	ErrInvalidConfig      = errors.New("invalid core config")
	ErrValidationFailed   = errors.New("core config validation failed")
	ErrValidationTimeout  = errors.New("core config validation timed out")
	ErrProcessExited      = errors.New("core process exited before ready")
	ErrReadinessTimeout   = errors.New("core readiness timed out")
	ErrPortConflict       = errors.New("core listener port conflict")
	ErrUnsupportedProfile = errors.New("unsupported profile mode")
)

type PortConflict struct {
	Field   string `json:"field"`
	Network string `json:"network"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

type PortConflictError struct {
	Conflicts []PortConflict
}

func (e *PortConflictError) Error() string {
	if e == nil || len(e.Conflicts) == 0 {
		return ErrPortConflict.Error()
	}
	items := make([]string, 0, len(e.Conflicts))
	for _, conflict := range e.Conflicts {
		address := net.JoinHostPort(conflict.Host, strconv.Itoa(conflict.Port))
		items = append(items, fmt.Sprintf("%s %s %s", conflict.Field, conflict.Network, address))
	}
	return "端口已被占用：" + strings.Join(items, "，") + "；请修改配置端口或停止占用端口的程序"
}

func (e *PortConflictError) Unwrap() error { return ErrPortConflict }
