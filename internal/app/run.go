package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// RunInteractiveContext owns terminal setup while the caller injects the
// already-constructed model and its daemon-backed capabilities.
func RunInteractiveContext(ctx context.Context, input io.Reader, output io.Writer, model tea.Model) error {
	if model == nil {
		return &Error{Code: ErrorCodeInternal, Message: "TUI 模型未配置"}
	}
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output))
	_, err := p.Run()
	return err
}

// InteractiveCapabilities adds the one client-local side effect needed by
// the TUI: opening an editor. All business reads and writes remain delegated
// to the embedded daemon capability.
type InteractiveCapabilities struct {
	CapabilityAPI
	restarter DaemonRestarter
	editor    string
	run       func(context.Context, string, ...string) error
}

type DaemonRestarter interface {
	Restart(context.Context, DaemonRestartProgressFunc) (DaemonRestartResult, error)
}

func NewInteractiveCapabilities(capabilities CapabilityAPI, editor string, restarters ...DaemonRestarter) *InteractiveCapabilities {
	interactive := &InteractiveCapabilities{
		CapabilityAPI: capabilities,
		editor:        strings.TrimSpace(editor),
		run: func(ctx context.Context, name string, args ...string) error {
			command := exec.CommandContext(ctx, name, args...)
			command.Stdin = os.Stdin
			command.Stdout = os.Stdout
			command.Stderr = os.Stderr
			return command.Run()
		},
	}
	if len(restarters) > 0 {
		interactive.restarter = restarters[0]
	}
	return interactive
}

func (c *InteractiveCapabilities) RestartDaemon(ctx context.Context, progress DaemonRestartProgressFunc) (DaemonRestartResult, error) {
	if c == nil || c.restarter == nil {
		return DaemonRestartResult{}, &Error{Code: ErrorCodeInternal, Message: "daemon 组合重启服务未配置"}
	}
	return c.restarter.Restart(ctx, progress)
}

func (c *InteractiveCapabilities) EditConfig(ctx context.Context, profileID string) error {
	if c == nil || c.CapabilityAPI == nil {
		return &Error{Code: ErrorCodeInternal, Message: "TUI daemon capability 未配置"}
	}
	document, err := c.ReadConfig(ctx, profileID)
	if err != nil {
		return err
	}
	editor := c.editor
	if editor == "" {
		editor = "vi"
	}
	directory, err := os.MkdirTemp("", "mihomo-manager-edit-*")
	if err != nil {
		return fmt.Errorf("创建配置编辑目录: %w", err)
	}
	defer os.RemoveAll(directory)
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("收紧配置编辑目录权限: %w", err)
	}
	path := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(path, document.Content, 0o600); err != nil {
		return fmt.Errorf("写入配置编辑临时文件: %w", err)
	}
	if err := c.run(ctx, editor, path); err != nil {
		return &Error{Category: ErrorCategoryUpstreamFailure, Code: ErrorCodeUpstreamFailure, Message: "配置编辑器执行失败", Err: err}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取编辑后的配置: %w", err)
	}
	if bytes.Equal(content, document.Content) {
		return nil
	}
	return c.ReplaceConfig(ctx, profileID, document.SHA256, content)
}
