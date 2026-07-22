package systemd

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
	available bool
	failAt    int
	failArgs  map[string]bool
	calls     [][]string
}

func (f *fakeRunner) Available(context.Context) bool { return f.available }
func (f *fakeRunner) Run(_ context.Context, args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.failArgs[strings.Join(args, " ")] {
		return errors.New("fixture failure")
	}
	if f.failAt > 0 && len(f.calls) == f.failAt {
		return errors.New("fixture failure")
	}
	return nil
}

func TestEnable_InstallsWithoutSystemdAndUsesSafeUnits(t *testing.T) {
	runner := &fakeRunner{}
	controller := &Controller{UnitDir: filepath.Join(t.TempDir(), "systemd", "user"), Runner: runner}
	result, err := controller.Enable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || result.Enabled || result.Hint == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if err := validateUnits(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mm.socket", "mm.service"} {
		content, readErr := os.ReadFile(filepath.Join(controller.UnitDir, name))
		if readErr != nil || !strings.Contains(string(content), "content-sha256=") {
			t.Fatalf("unit checksum marker missing for %s: %v", name, readErr)
		}
		info, err := os.Stat(filepath.Join(controller.UnitDir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("unexpected unit mode for %s: %v %v", name, info.Mode(), err)
		}
	}
}

func TestEnable_RollsBackOnSystemctlFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("# user unit\n")
	if err := os.WriteFile(filepath.Join(dir, "mm.socket"), original, 0o640); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{available: true, failAt: 2}
	controller := &Controller{UnitDir: dir, Runner: runner}
	if _, err := controller.Enable(context.Background()); err == nil {
		t.Fatal("expected enable failure")
	}
	got, err := os.ReadFile(filepath.Join(dir, "mm.socket"))
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("original unit not restored: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mm.service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new unit not removed: %v", err)
	}
}

func TestEnable_PersistsAndPreservesManagedEnvironment(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	want := map[string]string{
		"CONFIG_DIR":      "/tmp/配置 path/100%",
		"MIHOMO_BIN":      "/tmp/bin/mihomo",
		"MIHOMO_API_PORT": "19090",
	}
	controller := &Controller{UnitDir: dir, Runner: &fakeRunner{}, Environment: want}
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	servicePath := filepath.Join(dir, "mm.service")
	content, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseManagedEnvironment(content)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected managed environment: %#v err=%v\n%s", got, err, content)
	}

	controller = &Controller{UnitDir: dir, Runner: &fakeRunner{}}
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	got, err = parseManagedEnvironment(content)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("managed environment was not preserved: %#v err=%v", got, err)
	}

	controller = &Controller{UnitDir: dir, Runner: &fakeRunner{}, Environment: map[string]string{"MIHOMO_API_PORT": "29090"}}
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	got, err = parseManagedEnvironment(content)
	want["MIHOMO_API_PORT"] = "29090"
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("managed environment override did not preserve other keys: %#v err=%v", got, err)
	}
}

func TestEnable_RejectsUnsafeEnvironmentAndRollsBackUnits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	controller := &Controller{
		UnitDir:     dir,
		Runner:      &fakeRunner{},
		Environment: map[string]string{"CONFIG_DIR": "/tmp/unsafe\npath"},
	}
	if _, err := controller.Enable(context.Background()); err == nil {
		t.Fatal("expected unsafe environment failure")
	}
	for _, name := range []string{"mm.socket", "mm.service"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unit %s should have been rolled back: %v", name, err)
		}
	}
}

func TestEnable_DoesNotPreserveEnvironmentFromUnknownUnit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	unknown := []byte("[Service]\n" + environmentBegin + "\nEnvironment=\"CONFIG_DIR=/tmp/untrusted\"\n" + environmentEnd + "\n")
	if err := os.WriteFile(filepath.Join(dir, "mm.service"), unknown, 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &Controller{UnitDir: dir, Runner: &fakeRunner{}}
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "mm.service"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "/tmp/untrusted") {
		t.Fatalf("unknown unit environment was preserved: %s", content)
	}
}

func TestEnable_BacksUpModifiedManagedUnit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	runner := &fakeRunner{}
	controller := &Controller{UnitDir: dir, Runner: runner}
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mm.socket")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, []byte("# local change\n")...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	controller.Clock = func() time.Time { return time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC) }
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	backupPath := path + ".20260720T010203Z.mihomo-manager.bak"
	backup, err := os.ReadFile(backupPath)
	if err != nil || !strings.Contains(string(backup), "local change") {
		t.Fatalf("modified unit was not backed up: %q %v", backup, err)
	}
}

func TestControl_UsesOnlyManagerUnits(t *testing.T) {
	runner := &fakeRunner{available: true}
	controller := &Controller{UnitDir: t.TempDir(), Runner: runner}
	if _, err := controller.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"start", "mm.socket"}, {"stop", "mm.service", "mm.socket"}, {"restart", "mm.service"}, {"disable", "--now", "mm.socket", "mm.service"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("unexpected systemctl calls: %#v", runner.calls)
	}
}

func TestStatus_ReportsManagedServiceAndSocketSeparately(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	controller := &Controller{UnitDir: dir, Runner: &fakeRunner{}}
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{available: true, failArgs: map[string]bool{"is-active --quiet mm.service": true}}
	controller.Runner = runner
	result, err := controller.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available || !result.Installed || !result.Managed || result.ServiceActive || !result.SocketActive {
		t.Fatalf("unexpected status: %+v", result)
	}
	want := [][]string{{"is-active", "--quiet", "mm.service"}, {"is-active", "--quiet", "mm.socket"}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("unexpected status calls: %#v", runner.calls)
	}
}

func TestStatus_RejectsModifiedManagedUnit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	controller := &Controller{UnitDir: dir, Runner: &fakeRunner{}}
	if _, err := controller.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	servicePath := filepath.Join(dir, "mm.service")
	file, err := os.OpenFile(servicePath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("# local unmanaged edit\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	controller.Runner = &fakeRunner{available: true}
	result, err := controller.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || result.Managed {
		t.Fatalf("modified units should not be managed: %+v", result)
	}
}

func TestStatus_RejectsForgedManagedHeaderWithoutUnitContract(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "systemd", "user")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	forgedBody := []byte("# mihomo-manager template-version=2\n[Service]\nExecStart=/tmp/not-mm\n")
	digest := sha256.Sum256(forgedBody)
	forged := append([]byte(fmt.Sprintf("# Managed by mihomo-manager; content-sha256=%x\n", digest)), forgedBody...)
	for _, name := range []string{"mm.socket", "mm.service"} {
		if err := os.WriteFile(filepath.Join(dir, name), forged, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	controller := &Controller{UnitDir: dir, Runner: &fakeRunner{available: true}}
	result, err := controller.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || result.Managed {
		t.Fatalf("forged unit contract should be rejected: %+v", result)
	}
}

func TestRestart_ReportsUnavailableAndCommandFailure(t *testing.T) {
	controller := &Controller{UnitDir: t.TempDir(), Runner: &fakeRunner{}}
	if _, err := controller.Restart(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unexpected unavailable error: %v", err)
	}
	controller.Runner = &fakeRunner{available: true, failAt: 1}
	if _, err := controller.Restart(context.Background()); err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("unexpected restart command error: %v", err)
	}
}
