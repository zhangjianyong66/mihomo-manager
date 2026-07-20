package systemd

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

//go:embed units/mm.socket
var socketUnit []byte

//go:embed units/mm.service
var serviceUnit []byte

var ErrUnavailable = errors.New("systemd user session unavailable")

type Result struct {
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Active    bool   `json:"active"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
}

type Runner interface {
	Available(context.Context) bool
	Run(context.Context, ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Available(ctx context.Context) bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return exec.CommandContext(ctx, "systemctl", "--user", "show-environment").Run() == nil
}

func (ExecRunner) Run(ctx context.Context, args ...string) error {
	commandArgs := append([]string{"--user"}, args...)
	if output, err := exec.CommandContext(ctx, "systemctl", commandArgs...).CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = "systemctl command failed"
		}
		return errors.New(message)
	}
	return nil
}

type Controller struct {
	UnitDir string
	Runner  Runner
	Clock   func() time.Time
}

func (c *Controller) Status(ctx context.Context) (Result, error) {
	if c.Runner == nil || !c.Runner.Available(ctx) {
		return Result{Installed: unitsInstalled(c.UnitDir), Message: "systemd user 会话不可用", Hint: "请运行 mm daemon run 前台启动"}, nil
	}
	active := c.Runner.Run(ctx, "is-active", "--quiet", "mm.socket") == nil
	return Result{Installed: unitsInstalled(c.UnitDir), Enabled: active, Active: active, Message: "systemd daemon 状态已探测"}, nil
}

func New(unitDir string) *Controller {
	return &Controller{UnitDir: unitDir, Runner: ExecRunner{}, Clock: time.Now}
}

func (c *Controller) Enable(ctx context.Context) (Result, error) {
	if err := validateUnits(); err != nil {
		return Result{}, err
	}
	if err := platform.EnsurePrivateDir(c.UnitDir); err != nil {
		return Result{}, err
	}
	backups, err := c.installUnits()
	if err != nil {
		return Result{}, err
	}
	if c.Runner == nil || !c.Runner.Available(ctx) {
		return Result{Installed: true, Message: "systemd user unit 已安装但未启用", Hint: "请运行 mm daemon run 前台启动"}, nil
	}
	if err := c.Runner.Run(ctx, "daemon-reload"); err != nil {
		c.restore(backups)
		return Result{}, fmt.Errorf("reload systemd user units: %w", err)
	}
	if err := c.Runner.Run(ctx, "enable", "--now", "mm.socket"); err != nil {
		c.restore(backups)
		_ = c.Runner.Run(ctx, "daemon-reload")
		return Result{}, fmt.Errorf("enable systemd user socket: %w", err)
	}
	return Result{Installed: true, Enabled: true, Active: true, Message: "daemon socket 已启用"}, nil
}

func (c *Controller) Disable(ctx context.Context) (Result, error) {
	if c.Runner == nil || !c.Runner.Available(ctx) {
		return Result{Installed: unitsInstalled(c.UnitDir), Message: "systemd user 会话不可用", Hint: "如有前台 daemon，请在其终端中停止"}, nil
	}
	if err := c.Runner.Run(ctx, "disable", "--now", "mm.socket", "mm.service"); err != nil {
		return Result{}, fmt.Errorf("disable systemd user units: %w", err)
	}
	return Result{Installed: unitsInstalled(c.UnitDir), Message: "daemon user units 已停用"}, nil
}

func (c *Controller) Start(ctx context.Context) (Result, error) {
	return c.run(ctx, "start", []string{"start", "mm.socket"}, Result{Installed: unitsInstalled(c.UnitDir), Enabled: true, Active: true, Message: "daemon socket 已启动"})
}

func (c *Controller) Stop(ctx context.Context) (Result, error) {
	return c.run(ctx, "stop", []string{"stop", "mm.service", "mm.socket"}, Result{Installed: unitsInstalled(c.UnitDir), Message: "daemon 已停止"})
}

func (c *Controller) run(ctx context.Context, action string, args []string, success Result) (Result, error) {
	if c.Runner == nil || !c.Runner.Available(ctx) {
		return Result{}, fmt.Errorf("%s daemon: %w", action, ErrUnavailable)
	}
	if err := c.Runner.Run(ctx, args...); err != nil {
		return Result{}, fmt.Errorf("%s daemon: %w", action, err)
	}
	return success, nil
}

type backup struct {
	path    string
	existed bool
	content []byte
	mode    os.FileMode
}

func (c *Controller) installUnits() ([]backup, error) {
	units := []struct {
		name    string
		content []byte
	}{{"mm.socket", socketUnit}, {"mm.service", serviceUnit}}
	backups := make([]backup, 0, len(units))
	for _, unit := range units {
		path := filepath.Join(c.UnitDir, unit.name)
		rendered := renderUnit(unit.content)
		state, err := readBackup(path)
		if err != nil {
			c.restore(backups)
			return nil, err
		}
		backups = append(backups, state)
		if state.existed && !bytes.Equal(state.content, rendered) {
			clock := c.Clock
			if clock == nil {
				clock = time.Now
			}
			backupPath := path + "." + clock().UTC().Format("20060102T150405Z") + ".mihomo-manager.bak"
			if err := writeAtomic(backupPath, state.content); err != nil {
				c.restore(backups)
				return nil, err
			}
		}
		if err := writeAtomic(path, rendered); err != nil {
			c.restore(backups)
			return nil, err
		}
	}
	return backups, nil
}

func renderUnit(content []byte) []byte {
	digest := sha256.Sum256(content)
	header := fmt.Sprintf("# Managed by mihomo-manager; content-sha256=%x\n", digest)
	return append([]byte(header), content...)
}

func readBackup(path string) (backup, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return backup{path: path}, nil
	}
	if err != nil {
		return backup{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return backup{}, fmt.Errorf("inspect unit file: %w", platform.ErrUnsafePath)
	}
	content, err := os.ReadFile(path)
	return backup{path: path, existed: true, content: content, mode: info.Mode().Perm()}, err
}

func (c *Controller) restore(backups []backup) {
	for _, state := range backups {
		if state.existed {
			_ = writeAtomic(state.path, state.content)
			_ = os.Chmod(state.path, state.mode)
		} else {
			_ = os.Remove(state.path)
		}
	}
}

func writeAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".mm-unit-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func validateUnits() error {
	socket := string(socketUnit)
	service := string(serviceUnit)
	for _, required := range []string{"ListenStream=%t/mihomo-manager/mm.sock", "SocketMode=0600", "DirectoryMode=0700", "Accept=no", "RemoveOnStop=yes"} {
		if !strings.Contains(socket, required) {
			return fmt.Errorf("invalid socket unit: missing %s", required)
		}
	}
	if !strings.Contains(socket, "template-version=1") || !strings.Contains(service, "template-version=1") {
		return errors.New("unit template version missing")
	}
	for _, required := range []string{"mm daemon run", "Restart=on-failure", "RestartSec=2s", "NoNewPrivileges=yes", "UMask=0077"} {
		if !strings.Contains(service, required) {
			return fmt.Errorf("invalid service unit: missing %s", required)
		}
	}
	if strings.Contains(service, "mihomo ") || strings.Contains(service, "sudo") {
		return errors.New("service unit must not start core or sudo")
	}
	return nil
}

func unitsInstalled(dir string) bool {
	for _, name := range []string{"mm.socket", "mm.service"} {
		if info, err := os.Lstat(filepath.Join(dir, name)); err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}
