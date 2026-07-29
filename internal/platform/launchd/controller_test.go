package launchd

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	domain  bool
	loaded  bool
	running bool
	fail    map[string]bool
	calls   [][]string
}

func (f *fakeRunner) Output(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	joined := strings.Join(args, " ")
	if f.fail[joined] {
		return nil, errors.New("fixture failure")
	}
	if len(args) == 2 && args[0] == "print" && !strings.Contains(args[1], "/"+Label) {
		if !f.domain {
			return nil, errors.New("domain unavailable")
		}
		return []byte("domain = gui\n"), nil
	}
	if len(args) == 2 && args[0] == "print" && strings.HasSuffix(args[1], "/"+Label) {
		if !f.loaded {
			return nil, errors.New("service not loaded")
		}
		state := "waiting"
		if f.running {
			state = "running"
		}
		return []byte("state = " + state + "\n"), nil
	}
	switch args[0] {
	case "bootstrap", "kickstart":
		f.loaded = true
		f.running = true
	case "bootout":
		f.loaded = false
		f.running = false
	case "disable":
		f.loaded = false
		f.running = false
	}
	return nil, nil
}

func newTestController(t *testing.T, runner *fakeRunner) *Controller {
	t.Helper()
	home := t.TempDir()
	return &Controller{
		PlistPath:   filepath.Join(home, "Library", "LaunchAgents", Label+".plist"),
		MMBinary:    filepath.Join(home, ".local", "bin", "mm"),
		StdoutPath:  filepath.Join(home, ".local", "state", "mihomo-manager", "daemon.stdout.log"),
		StderrPath:  filepath.Join(home, ".local", "state", "mihomo-manager", "daemon.stderr.log"),
		Environment: map[string]string{"CONFIG_DIR": filepath.Join(home, ".config", "mihomo"), "MIHOMO_API_PORT": "9090"},
		UID:         os.Geteuid(), Runner: runner, Clock: time.Now,
	}
}

func TestEnable_FreshInstallsAndBootstrapsFixedAgent(t *testing.T) {
	runner := &fakeRunner{domain: true}
	controller := newTestController(t, runner)
	result, err := controller.Enable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || !result.Managed || !result.Available || !result.Ready {
		t.Fatalf("unexpected result: %+v", result)
	}
	content, err := os.ReadFile(controller.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	if !plistManagedContent(content) {
		t.Fatalf("plist should be managed:\n%s", content)
	}
	parsed, err := parsePlist(content)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Label != Label || !reflect.DeepEqual(parsed.Arguments, []string{controller.MMBinary, "daemon", "run"}) {
		t.Fatalf("unexpected plist contract: %+v", parsed)
	}
	if parsed.Environment["CONFIG_DIR"] != controller.Environment["CONFIG_DIR"] {
		t.Fatalf("environment missing: %+v", parsed.Environment)
	}
	info, err := os.Stat(controller.PlistPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected plist mode: %v %v", info.Mode(), err)
	}
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "system/") || strings.Contains(joined, "sudo") || strings.Contains(joined, "com.mihomo.monitor") {
			t.Fatalf("unsafe launchctl target: %q", joined)
		}
	}
}

func TestEnable_LoadedUpgradeDoesNotRestartAndPreservesEnvironment(t *testing.T) {
	runner := &fakeRunner{domain: true, loaded: true, running: true}
	controller := newTestController(t, runner)
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	controller.Environment = map[string]string{"MIHOMO_API_PORT": "19090"}
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls {
		if call[0] == "bootstrap" || call[0] == "kickstart" || call[0] == "bootout" {
			t.Fatalf("loaded upgrade changed running job: %#v", runner.calls)
		}
	}
	content, err := os.ReadFile(controller.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parsePlist(content)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Environment["CONFIG_DIR"] == "" || parsed.Environment["MIHOMO_API_PORT"] != "19090" {
		t.Fatalf("environment not preserved: %+v", parsed.Environment)
	}
}

func TestEnable_GUIUnavailableInstallsWithoutLoading(t *testing.T) {
	controller := newTestController(t, &fakeRunner{})
	result, err := controller.Enable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || !result.Managed || result.Available || result.Ready || result.Hint == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestEnable_RestoresPlistWhenBootstrapFails(t *testing.T) {
	runner := &fakeRunner{domain: true}
	controller := newTestController(t, runner)
	original := []byte("user plist\n")
	if err := os.MkdirAll(filepath.Dir(controller.PlistPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controller.PlistPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	runner.fail = map[string]bool{"bootstrap " + controller.domainTarget() + " " + controller.PlistPath: true}
	if _, err := controller.Enable(context.Background()); err == nil {
		t.Fatal("expected bootstrap failure")
	}
	content, err := os.ReadFile(controller.PlistPath)
	if err != nil || string(content) != string(original) {
		t.Fatalf("original plist not restored: %q %v", content, err)
	}
	info, err := os.Stat(controller.PlistPath)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("original mode not restored: %v %v", info.Mode(), err)
	}
}

func TestEnable_BacksUpModifiedManagedAndUnknownPlist(t *testing.T) {
	runner := &fakeRunner{}
	controller := newTestController(t, runner)
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(controller.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, []byte("<!-- external edit -->\n")...)
	if err := os.WriteFile(controller.PlistPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	controller.Clock = func() time.Time { return time.Date(2026, 7, 29, 1, 2, 3, 0, time.UTC) }
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	backupPath := controller.PlistPath + ".20260729T010203Z.mihomo-manager.bak"
	backup, err := os.ReadFile(backupPath)
	if err != nil || !strings.Contains(string(backup), "external edit") {
		t.Fatalf("modified plist backup missing: %q %v", backup, err)
	}
}

func TestControl_OnlyUsesFixedServiceTarget(t *testing.T) {
	runner := &fakeRunner{domain: true}
	controller := newTestController(t, runner)
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	if _, err := controller.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls {
		for _, argument := range call {
			if strings.HasPrefix(argument, "gui/") && argument != controller.domainTarget() && argument != controller.serviceTarget() {
				t.Fatalf("unexpected launchctl target: %#v", call)
			}
		}
	}
}

func TestEnable_RejectsUnsafeEnvironmentBeforeWriting(t *testing.T) {
	controller := newTestController(t, &fakeRunner{})
	controller.Environment = map[string]string{"CONFIG_DIR": "relative"}
	if _, err := controller.Enable(context.Background()); err == nil {
		t.Fatal("expected environment rejection")
	}
	if _, err := os.Stat(controller.PlistPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plist should not be written: %v", err)
	}
}

func TestControl_RejectsDifferentUIDBeforeLaunchctl(t *testing.T) {
	runner := &fakeRunner{domain: true, loaded: true, running: true}
	controller := newTestController(t, runner)
	controller.UID++
	if _, err := controller.Status(context.Background()); err == nil {
		t.Fatal("expected non-current UID rejection")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unsafe target reached launchctl: %#v", runner.calls)
	}
}

func TestControl_RejectsSymlinkedPlistDirectoryBeforeLaunchctl(t *testing.T) {
	runner := &fakeRunner{domain: true}
	controller := newTestController(t, runner)
	realDir := filepath.Join(t.TempDir(), "LaunchAgents")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(t.TempDir(), "LaunchAgents")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	controller.PlistPath = filepath.Join(linkDir, Label+".plist")
	if _, err := controller.Status(context.Background()); err == nil {
		t.Fatal("expected symlinked plist directory rejection")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unsafe plist path reached launchctl: %#v", runner.calls)
	}
}

func TestControl_RejectsUnmanagedPlistBeforeMutation(t *testing.T) {
	runner := &fakeRunner{domain: true, loaded: true, running: true}
	controller := newTestController(t, runner)
	if err := os.MkdirAll(filepath.Dir(controller.PlistPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controller.PlistPath, []byte("user plist\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Stop(context.Background()); err == nil {
		t.Fatal("expected unmanaged stop rejection")
	}
	if _, err := controller.Disable(context.Background()); err == nil {
		t.Fatal("expected unmanaged disable rejection")
	}
	for _, call := range runner.calls {
		if call[0] == "bootout" || call[0] == "disable" {
			t.Fatalf("unmanaged plist changed launchd state: %#v", runner.calls)
		}
	}
}

func TestStatus_RequiresPrivateManagedPlist(t *testing.T) {
	runner := &fakeRunner{domain: true, loaded: true, running: true}
	controller := newTestController(t, runner)
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(controller.PlistPath, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := controller.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || result.Managed || result.Ready {
		t.Fatalf("non-private plist should not be managed: %+v", result)
	}
}

func TestManagedPlist_RejectsForgedContractWithValidChecksum(t *testing.T) {
	controller := newTestController(t, &fakeRunner{})
	content, err := controller.render(nil)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "<integer>63</integer>", "<integer>18</integer>", 1))
	lines := strings.SplitN(string(content), "\n", 3)
	digest := sha256.Sum256([]byte(lines[2]))
	content = []byte(lines[0] + "\n" + markerPrefix + fmt.Sprintf("%x", digest) + " -->\n" + lines[2])
	if plistManagedContent(content) {
		t.Fatal("forged plist contract should not be managed")
	}
}
