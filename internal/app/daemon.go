package app

import (
	"context"
	"fmt"
	"io"
	"time"
)

type DaemonStatus struct {
	ProtocolVersion int              `json:"protocolVersion"`
	State           string           `json:"state"`
	PID             int              `json:"pid"`
	StartedAt       time.Time        `json:"startedAt"`
	SchemaVersion   int              `json:"schemaVersion"`
	Core            DaemonCoreStatus `json:"core"`
}

type DaemonCoreStatus struct {
	State        string `json:"state"`
	ProfileID    string `json:"profileId,omitempty"`
	GenerationID string `json:"generationId,omitempty"`
	PID          int    `json:"pid,omitempty"`
	ErrorCode    string `json:"errorCode,omitempty"`
}

type DaemonStatusClient interface {
	Status(context.Context) (DaemonStatus, error)
}

type DaemonRunner interface {
	Run(context.Context, io.Writer) error
}

type DaemonController interface {
	Status(context.Context) (DaemonControlResult, error)
	Enable(context.Context) (DaemonControlResult, error)
	Disable(context.Context) (DaemonControlResult, error)
	Start(context.Context) (DaemonControlResult, error)
	Stop(context.Context) (DaemonControlResult, error)
	Restart(context.Context) (DaemonControlResult, error)
}

type DaemonControlResult struct {
	Backend       string `json:"backend,omitempty"`
	Ready         bool   `json:"ready"`
	Installed     bool   `json:"installed"`
	Managed       bool   `json:"managed"`
	Available     bool   `json:"available"`
	Enabled       bool   `json:"enabled"`
	Active        bool   `json:"active"`
	ServiceActive bool   `json:"serviceActive"`
	SocketActive  bool   `json:"socketActive"`
	Message       string `json:"message"`
	Hint          string `json:"hint,omitempty"`
}

type DaemonService struct {
	Client     DaemonStatusClient
	Runner     DaemonRunner
	Controller DaemonController
	Core       DaemonCoreController

	restartTimeout  time.Duration
	readyTimeout    time.Duration
	recoveryTimeout time.Duration
	pollInterval    time.Duration
	sleep           func(context.Context, time.Duration) error
}

func (s *DaemonService) Status(ctx context.Context) (DaemonStatus, error) {
	if s == nil || s.Client == nil {
		return DaemonStatus{}, &Error{Code: ErrorCodeDaemonUnavailable, Message: "daemon 客户端未配置"}
	}
	status, err := s.Client.Status(ctx)
	if err == nil {
		return status, nil
	}
	if s.Controller != nil {
		if diagnostic, diagnosticErr := s.Controller.Status(ctx); diagnosticErr == nil && (diagnostic.Message != "" || diagnostic.Hint != "") {
			if appErr, ok := err.(*Error); ok {
				copy := *appErr
				if diagnostic.Message != "" {
					copy.Message = copy.Message + "；" + diagnostic.Message
				}
				if diagnostic.Hint != "" {
					copy.Message = copy.Message + "；" + diagnostic.Hint
				}
				copy.Details = map[string]any{"backend": diagnostic.Backend, "serviceManager": diagnostic.Message, "hint": diagnostic.Hint}
				return DaemonStatus{}, &copy
			}
		}
	}
	return DaemonStatus{}, err
}

func (s *DaemonService) Run(ctx context.Context, diagnostics io.Writer) error {
	if s == nil || s.Runner == nil {
		return &Error{Code: ErrorCodeInternal, Message: "daemon 运行器未配置"}
	}
	return s.Runner.Run(ctx, diagnostics)
}

func (s *DaemonService) Control(ctx context.Context, action string) (DaemonControlResult, error) {
	if s == nil || s.Controller == nil {
		return DaemonControlResult{}, &Error{Code: ErrorCodeInternal, Message: "daemon 控制器未配置"}
	}
	var result DaemonControlResult
	var err error
	switch action {
	case "enable":
		result, err = s.Controller.Enable(ctx)
	case "disable":
		result, err = s.Controller.Disable(ctx)
	case "start":
		result, err = s.Controller.Start(ctx)
	case "stop":
		result, err = s.Controller.Stop(ctx)
	default:
		return DaemonControlResult{}, &Error{Code: ErrorCodeInvalidArgument, Message: fmt.Sprintf("未知 daemon 操作 %q", action)}
	}
	return result, err
}
