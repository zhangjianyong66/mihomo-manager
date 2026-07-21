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
	"sort"
	"strconv"
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
	UnitDir     string
	Runner      Runner
	Clock       func() time.Time
	Environment map[string]string
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
		state, err := readBackup(path)
		if err != nil {
			c.restore(backups)
			return nil, err
		}
		backups = append(backups, state)
		rendered, err := c.renderUnit(unit.name, unit.content, state.content)
		if err != nil {
			c.restore(backups)
			return nil, err
		}
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

const (
	environmentBegin = "# >>> mihomo-manager environment >>>"
	environmentEnd   = "# <<< mihomo-manager environment <<<"
)

func (c *Controller) renderUnit(name string, content, previous []byte) ([]byte, error) {
	body := append([]byte(nil), content...)
	if name == "mm.service" {
		environment, err := parseManagedEnvironment(previous)
		if err != nil {
			return nil, err
		}
		if len(c.Environment) > 0 {
			if environment == nil {
				environment = make(map[string]string, len(c.Environment))
			}
			for key, value := range c.Environment {
				environment[key] = value
			}
		}
		block, err := renderManagedEnvironment(environment)
		if err != nil {
			return nil, err
		}
		if len(block) > 0 {
			marker := []byte("[Service]\n")
			if !bytes.Contains(body, marker) {
				return nil, errors.New("service unit has no Service section")
			}
			body = bytes.Replace(body, marker, append(marker, block...), 1)
		}
	}
	digest := sha256.Sum256(body)
	header := fmt.Sprintf("# Managed by mihomo-manager; content-sha256=%x\n", digest)
	return append([]byte(header), body...), nil
}

func renderManagedEnvironment(environment map[string]string) ([]byte, error) {
	if len(environment) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var result strings.Builder
	result.WriteString(environmentBegin + "\n")
	for _, key := range keys {
		value := environment[key]
		if err := validateEnvironment(key, value); err != nil {
			return nil, err
		}
		value = strings.ReplaceAll(value, "%", "%%")
		result.WriteString("Environment=")
		result.WriteString(strconv.Quote(key + "=" + value))
		result.WriteByte('\n')
	}
	result.WriteString(environmentEnd + "\n")
	return []byte(result.String()), nil
}

func parseManagedEnvironment(content []byte) (map[string]string, error) {
	if len(content) == 0 {
		return nil, nil
	}
	text := string(content)
	if !strings.HasPrefix(text, "# Managed by mihomo-manager; content-sha256=") {
		return nil, nil
	}
	start := strings.Index(text, environmentBegin)
	end := strings.Index(text, environmentEnd)
	if start < 0 && end < 0 {
		return nil, nil
	}
	if start < 0 || end < start {
		return nil, errors.New("managed service environment block is incomplete")
	}
	block := text[start+len(environmentBegin) : end]
	environment := make(map[string]string)
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		quoted, ok := strings.CutPrefix(line, "Environment=")
		if !ok {
			return nil, errors.New("managed service environment block contains an invalid line")
		}
		assignment, err := strconv.Unquote(quoted)
		if err != nil {
			return nil, fmt.Errorf("decode managed service environment: %w", err)
		}
		key, value, ok := strings.Cut(assignment, "=")
		if !ok || key == "" {
			return nil, errors.New("managed service environment assignment is invalid")
		}
		value = strings.ReplaceAll(value, "%%", "%")
		if err := validateEnvironment(key, value); err != nil {
			return nil, err
		}
		if _, exists := environment[key]; exists {
			return nil, fmt.Errorf("managed service environment contains duplicate key %q", key)
		}
		environment[key] = value
	}
	return environment, nil
}

func validateEnvironment(key, value string) error {
	switch key {
	case "CONFIG_DIR", "MIHOMO_BIN":
		if !filepath.IsAbs(value) {
			return fmt.Errorf("systemd environment %s must be an absolute path", key)
		}
	case "MIHOMO_API_PORT":
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return errors.New("systemd environment MIHOMO_API_PORT must be a valid port")
		}
	default:
		return fmt.Errorf("unsupported systemd environment key %q", key)
	}
	for _, character := range value {
		if character == 0 || character == '\n' || character == '\r' || character < 0x20 || character == 0x7f {
			return fmt.Errorf("systemd environment %s contains control characters", key)
		}
	}
	return nil
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
	if !strings.Contains(socket, "template-version=2") || !strings.Contains(service, "template-version=2") {
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
