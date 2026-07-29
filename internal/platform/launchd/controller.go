package launchd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

const Label = "com.zhangjianyong.mihomo-manager.daemon"

var ErrUnavailable = errors.New("launchd GUI domain unavailable")

type Result struct {
	Installed     bool   `json:"installed"`
	Managed       bool   `json:"managed"`
	Available     bool   `json:"available"`
	Enabled       bool   `json:"enabled"`
	Active        bool   `json:"active"`
	Ready         bool   `json:"ready"`
	ServiceActive bool   `json:"serviceActive"`
	SocketActive  bool   `json:"socketActive"`
	Message       string `json:"message"`
	Hint          string `json:"hint,omitempty"`
}

type Runner interface {
	Output(context.Context, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
	if err == nil {
		return output, nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = "launchctl command failed"
	}
	return nil, errors.New(message)
}

type Controller struct {
	PlistPath   string
	MMBinary    string
	StdoutPath  string
	StderrPath  string
	Environment map[string]string
	UID         int
	Runner      Runner
	Clock       func() time.Time
}

func New(plistPath, mmBinary, stdoutPath, stderrPath string) *Controller {
	return &Controller{
		PlistPath: plistPath, MMBinary: mmBinary, StdoutPath: stdoutPath, StderrPath: stderrPath,
		UID: os.Geteuid(), Runner: ExecRunner{}, Clock: time.Now,
	}
}

func (c *Controller) Status(ctx context.Context) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	installed, managed := plistState(c.PlistPath, c.UID)
	if !c.domainAvailable(ctx) {
		return Result{Installed: installed, Managed: managed, Message: "launchd 用户登录域不可用", Hint: "请登录图形会话后运行 mm daemon start，或运行 mm daemon run 前台启动"}, nil
	}
	output, err := c.Runner.Output(ctx, "print", c.serviceTarget())
	loaded := err == nil
	running := loaded && launchdState(output) == "running"
	return Result{
		Installed: installed, Managed: managed, Available: true, Enabled: loaded,
		Active: running, Ready: managed && running, ServiceActive: running,
		Message: "launchd daemon 状态已探测",
	}, nil
}

func (c *Controller) Enable(ctx context.Context) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	if err := platform.EnsurePrivateDir(filepath.Dir(c.PlistPath)); err != nil {
		return Result{}, err
	}
	if err := platform.EnsurePrivateDir(filepath.Dir(c.StdoutPath)); err != nil {
		return Result{}, err
	}
	previous, err := readBackup(c.PlistPath, c.UID)
	if err != nil {
		return Result{}, err
	}
	rendered, err := c.render(previous.content)
	if err != nil {
		return Result{}, err
	}
	if previous.existed && !bytes.Equal(previous.content, rendered) {
		clock := c.Clock
		if clock == nil {
			clock = time.Now
		}
		backupPath := c.PlistPath + "." + clock().UTC().Format("20060102T150405Z") + ".mihomo-manager.bak"
		if err := writeAtomic(backupPath, previous.content); err != nil {
			return Result{}, err
		}
	}
	if err := writeAtomic(c.PlistPath, rendered); err != nil {
		return Result{}, err
	}
	rollback := func() { restore(previous) }
	if !c.domainAvailable(ctx) {
		return Result{Installed: true, Managed: true, Message: "launchd LaunchAgent 已安装但未启用", Hint: "请登录图形会话后运行 mm daemon start，或运行 mm daemon run 前台启动"}, nil
	}
	if _, err := c.Runner.Output(ctx, "enable", c.serviceTarget()); err != nil {
		rollback()
		return Result{}, fmt.Errorf("enable launchd agent: %w", err)
	}
	if _, err := c.Runner.Output(ctx, "print", c.serviceTarget()); err != nil {
		if _, err := c.Runner.Output(ctx, "bootstrap", c.domainTarget(), c.PlistPath); err != nil {
			_, _ = c.Runner.Output(ctx, "disable", c.serviceTarget())
			rollback()
			return Result{}, fmt.Errorf("bootstrap launchd agent: %w", err)
		}
	}
	return c.Status(ctx)
}

func (c *Controller) Disable(ctx context.Context) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	installed, managed := plistState(c.PlistPath, c.UID)
	if !c.domainAvailable(ctx) {
		return Result{Installed: installed, Managed: managed, Message: "launchd 用户登录域不可用", Hint: "如有前台 daemon，请在其终端中停止"}, nil
	}
	if !installed || !managed {
		return Result{}, fmt.Errorf("disable launchd agent: %w", platform.ErrUnsafePath)
	}
	if _, err := c.Runner.Output(ctx, "print", c.serviceTarget()); err == nil {
		if _, err := c.Runner.Output(ctx, "bootout", c.serviceTarget()); err != nil {
			return Result{}, fmt.Errorf("bootout launchd agent: %w", err)
		}
	}
	if _, err := c.Runner.Output(ctx, "disable", c.serviceTarget()); err != nil {
		return Result{}, fmt.Errorf("disable launchd agent: %w", err)
	}
	return Result{Installed: installed, Managed: managed, Available: true, Message: "launchd daemon 已停用"}, nil
}

func (c *Controller) Start(ctx context.Context) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	if !c.domainAvailable(ctx) {
		return Result{}, fmt.Errorf("start launchd agent: %w", ErrUnavailable)
	}
	if installed, managed := plistState(c.PlistPath, c.UID); !installed || !managed {
		return Result{}, fmt.Errorf("start launchd agent: %w", platform.ErrUnsafePath)
	}
	output, err := c.Runner.Output(ctx, "print", c.serviceTarget())
	if err != nil {
		if _, err := c.Runner.Output(ctx, "enable", c.serviceTarget()); err != nil {
			return Result{}, fmt.Errorf("enable launchd agent: %w", err)
		}
		if _, err := c.Runner.Output(ctx, "bootstrap", c.domainTarget(), c.PlistPath); err != nil {
			return Result{}, fmt.Errorf("bootstrap launchd agent: %w", err)
		}
	} else if launchdState(output) != "running" {
		if _, err := c.Runner.Output(ctx, "kickstart", c.serviceTarget()); err != nil {
			return Result{}, fmt.Errorf("start launchd agent: %w", err)
		}
	}
	return c.Status(ctx)
}

func (c *Controller) Stop(ctx context.Context) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	if !c.domainAvailable(ctx) {
		return Result{}, fmt.Errorf("stop launchd agent: %w", ErrUnavailable)
	}
	installed, managed := plistState(c.PlistPath, c.UID)
	if !installed || !managed {
		return Result{}, fmt.Errorf("stop launchd agent: %w", platform.ErrUnsafePath)
	}
	if _, err := c.Runner.Output(ctx, "print", c.serviceTarget()); err == nil {
		if _, err := c.Runner.Output(ctx, "bootout", c.serviceTarget()); err != nil {
			return Result{}, fmt.Errorf("stop launchd agent: %w", err)
		}
	}
	return Result{Installed: installed, Managed: managed, Available: true, Enabled: true, Message: "launchd daemon 已停止"}, nil
}

func (c *Controller) Restart(ctx context.Context) (Result, error) {
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	status, err := c.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !status.Available {
		return Result{}, fmt.Errorf("restart launchd agent: %w", ErrUnavailable)
	}
	if !status.Installed || !status.Managed || !status.Enabled {
		return Result{}, fmt.Errorf("restart launchd agent: %w", platform.ErrUnsafePath)
	}
	if _, err := c.Runner.Output(ctx, "kickstart", "-k", c.serviceTarget()); err != nil {
		return Result{}, fmt.Errorf("restart launchd agent: %w", err)
	}
	return Result{Installed: true, Managed: true, Available: true, Enabled: true, Active: true, Ready: true, ServiceActive: true, Message: "launchd daemon 已重启"}, nil
}

func (c *Controller) validate() error {
	for name, value := range map[string]string{"plist": c.PlistPath, "mm": c.MMBinary, "stdout": c.StdoutPath, "stderr": c.StderrPath} {
		if value == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("launchd %s path: %w", name, platform.ErrUnsafePath)
		}
	}
	if filepath.Base(c.PlistPath) != Label+".plist" || c.UID < 0 || c.UID != os.Geteuid() {
		return fmt.Errorf("launchd target: %w", platform.ErrUnsafePath)
	}
	if err := platform.RejectExistingSymlinkComponents(c.PlistPath); err != nil {
		return fmt.Errorf("launchd plist path: %w", err)
	}
	parent, err := os.Lstat(filepath.Dir(c.PlistPath))
	if err == nil && (!parent.IsDir() || !ownedByUID(parent, c.UID)) {
		return fmt.Errorf("launchd plist directory: %w", platform.ErrUnsafePath)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect launchd plist directory: %w", err)
	}
	return nil
}

func (c *Controller) domainTarget() string  { return "gui/" + strconv.Itoa(c.UID) }
func (c *Controller) serviceTarget() string { return c.domainTarget() + "/" + Label }

func (c *Controller) domainAvailable(ctx context.Context) bool {
	if c.Runner == nil {
		return false
	}
	_, err := c.Runner.Output(ctx, "print", c.domainTarget())
	return err == nil
}

func launchdState(output []byte) string {
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(key) == "state" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

const (
	xmlDeclaration = `<?xml version="1.0" encoding="UTF-8"?>`
	markerPrefix   = "<!-- Managed by mihomo-manager; content-sha256="
)

func (c *Controller) render(previous []byte) ([]byte, error) {
	environment := map[string]string{}
	if plistManagedContent(previous) {
		parsed, err := parsePlist(previous)
		if err != nil {
			return nil, err
		}
		for key, value := range parsed.Environment {
			environment[key] = value
		}
	}
	for key, value := range c.Environment {
		environment[key] = value
	}
	for key, value := range environment {
		if err := validateEnvironment(key, value); err != nil {
			return nil, err
		}
	}
	body := c.renderBody(environment)
	digest := sha256.Sum256([]byte(body))
	marker := fmt.Sprintf("%s%x -->", markerPrefix, digest)
	return []byte(xmlDeclaration + "\n" + marker + "\n" + body), nil
}

func (c *Controller) renderBody(environment map[string]string) string {
	var result strings.Builder
	result.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	result.WriteString("<plist version=\"1.0\">\n<dict>\n")
	writeKeyString(&result, "Label", Label)
	result.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	writeString(&result, c.MMBinary, 4)
	writeString(&result, "daemon", 4)
	writeString(&result, "run", 4)
	result.WriteString("  </array>\n")
	result.WriteString("  <key>RunAtLoad</key>\n  <true/>\n  <key>KeepAlive</key>\n  <true/>\n")
	result.WriteString("  <key>ProcessType</key>\n  <string>Background</string>\n")
	result.WriteString("  <key>Umask</key>\n  <integer>63</integer>\n  <key>ThrottleInterval</key>\n  <integer>10</integer>\n")
	if len(environment) > 0 {
		result.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
		keys := make([]string, 0, len(environment))
		for key := range environment {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			writeKeyStringAt(&result, key, environment[key], 4)
		}
		result.WriteString("  </dict>\n")
	}
	writeKeyString(&result, "StandardOutPath", c.StdoutPath)
	writeKeyString(&result, "StandardErrorPath", c.StderrPath)
	result.WriteString("</dict>\n</plist>\n")
	return result.String()
}

func writeKeyString(result *strings.Builder, key, value string) {
	writeKeyStringAt(result, key, value, 2)
}

func writeKeyStringAt(result *strings.Builder, key, value string, indent int) {
	padding := strings.Repeat(" ", indent)
	result.WriteString(padding + "<key>" + escapeXML(key) + "</key>\n")
	result.WriteString(padding + "<string>" + escapeXML(value) + "</string>\n")
}

func writeString(result *strings.Builder, value string, indent int) {
	result.WriteString(strings.Repeat(" ", indent) + "<string>" + escapeXML(value) + "</string>\n")
}

func escapeXML(value string) string {
	var result bytes.Buffer
	_ = xml.EscapeText(&result, []byte(value))
	return result.String()
}

func validateEnvironment(key, value string) error {
	switch key {
	case "CONFIG_DIR", "MIHOMO_BIN":
		if !filepath.IsAbs(value) {
			return fmt.Errorf("launchd environment %s must be an absolute path", key)
		}
	case "MIHOMO_API_PORT":
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 {
			return errors.New("launchd environment MIHOMO_API_PORT must be a valid port")
		}
	default:
		return fmt.Errorf("unsupported launchd environment key %q", key)
	}
	for _, character := range value {
		if character == 0 || character == '\n' || character == '\r' || character < 0x20 || character == 0x7f {
			return fmt.Errorf("launchd environment %s contains control characters", key)
		}
	}
	return nil
}

type plistConfig struct {
	Label       string
	Arguments   []string
	Environment map[string]string
	Stdout      string
	Stderr      string
	ProcessType string
	Umask       string
	Throttle    string
	RunAtLoad   bool
	KeepAlive   bool
}

type xmlNode struct {
	XMLName  xml.Name
	Text     string    `xml:",chardata"`
	Children []xmlNode `xml:",any"`
}

func parsePlist(content []byte) (plistConfig, error) {
	var root xmlNode
	if err := xml.Unmarshal(content, &root); err != nil {
		return plistConfig{}, fmt.Errorf("parse launchd plist: %w", err)
	}
	if root.XMLName.Local != "plist" {
		return plistConfig{}, errors.New("launchd plist root is invalid")
	}
	dict := firstChild(root, "dict")
	if dict == nil {
		return plistConfig{}, errors.New("launchd plist dictionary is missing")
	}
	result := plistConfig{Environment: map[string]string{}}
	values := dictValues(*dict)
	result.Label = nodeText(values["Label"], "string")
	result.Stdout = nodeText(values["StandardOutPath"], "string")
	result.Stderr = nodeText(values["StandardErrorPath"], "string")
	result.ProcessType = nodeText(values["ProcessType"], "string")
	result.Umask = nodeText(values["Umask"], "integer")
	result.Throttle = nodeText(values["ThrottleInterval"], "integer")
	result.RunAtLoad = nodeBool(values["RunAtLoad"])
	result.KeepAlive = nodeBool(values["KeepAlive"])
	if arguments := values["ProgramArguments"]; arguments != nil && arguments.XMLName.Local == "array" {
		for _, child := range arguments.Children {
			if child.XMLName.Local == "string" {
				result.Arguments = append(result.Arguments, child.Text)
			}
		}
	}
	if environment := values["EnvironmentVariables"]; environment != nil && environment.XMLName.Local == "dict" {
		for key, value := range dictValues(*environment) {
			if value == nil || value.XMLName.Local != "string" {
				return plistConfig{}, fmt.Errorf("launchd environment %s is not a string", key)
			}
			result.Environment[key] = value.Text
		}
	}
	return result, nil
}

func firstChild(node xmlNode, name string) *xmlNode {
	for index := range node.Children {
		if node.Children[index].XMLName.Local == name {
			return &node.Children[index]
		}
	}
	return nil
}

func dictValues(dict xmlNode) map[string]*xmlNode {
	result := map[string]*xmlNode{}
	for index := 0; index+1 < len(dict.Children); index++ {
		if dict.Children[index].XMLName.Local != "key" {
			continue
		}
		key := strings.TrimSpace(dict.Children[index].Text)
		index++
		result[key] = &dict.Children[index]
	}
	return result
}

func nodeText(node *xmlNode, kind string) string {
	if node == nil || node.XMLName.Local != kind {
		return ""
	}
	return node.Text
}

func nodeBool(node *xmlNode) bool { return node != nil && node.XMLName.Local == "true" }

func plistState(path string, uid int) (bool, bool) {
	return plistInstalled(path, uid), plistManaged(path, uid)
}

func plistInstalled(path string, uid int) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && ownedByUID(info, uid)
}

func plistManaged(path string, uid int) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByUID(info, uid) {
		return false
	}
	content, err := os.ReadFile(path)
	return err == nil && plistManagedContent(content)
}

func ownedByUID(info os.FileInfo, uid int) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && uint64(stat.Uid) == uint64(uid)
}

func plistManagedContent(content []byte) bool {
	if !managedChecksumValid(content) {
		return false
	}
	parsed, err := parsePlist(content)
	if err != nil {
		return false
	}
	if parsed.Label != Label || len(parsed.Arguments) != 3 || !filepath.IsAbs(parsed.Arguments[0]) ||
		parsed.Arguments[1] != "daemon" || parsed.Arguments[2] != "run" || !parsed.RunAtLoad || !parsed.KeepAlive ||
		parsed.ProcessType != "Background" || parsed.Umask != "63" || parsed.Throttle != "10" ||
		!filepath.IsAbs(parsed.Stdout) || !filepath.IsAbs(parsed.Stderr) {
		return false
	}
	for key, value := range parsed.Environment {
		if validateEnvironment(key, value) != nil {
			return false
		}
	}
	return true
}

func managedChecksumValid(content []byte) bool {
	lines := bytes.SplitN(content, []byte("\n"), 3)
	if len(lines) != 3 || string(lines[0]) != xmlDeclaration {
		return false
	}
	marker := string(lines[1])
	if !strings.HasPrefix(marker, markerPrefix) || !strings.HasSuffix(marker, " -->") {
		return false
	}
	want := strings.TrimSuffix(strings.TrimPrefix(marker, markerPrefix), " -->")
	digest := sha256.Sum256(lines[2])
	return want == fmt.Sprintf("%x", digest)
}

type backup struct {
	path    string
	existed bool
	content []byte
	mode    os.FileMode
}

func readBackup(path string, uid int) (backup, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return backup{path: path}, nil
	}
	if err != nil {
		return backup{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !ownedByUID(info, uid) {
		return backup{}, fmt.Errorf("inspect launchd plist: %w", platform.ErrUnsafePath)
	}
	content, err := os.ReadFile(path)
	return backup{path: path, existed: true, content: content, mode: info.Mode().Perm()}, err
}

func restore(state backup) {
	if state.existed {
		_ = writeAtomic(state.path, state.content)
		_ = os.Chmod(state.path, state.mode)
		return
	}
	_ = os.Remove(state.path)
}

func writeAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".mm-launchd-*")
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
