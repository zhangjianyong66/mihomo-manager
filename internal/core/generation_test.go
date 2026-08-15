package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func TestGenerationStore_ManagedIsDeterministicPrivateAndValidatedBeforePublish(t *testing.T) {
	root := t.TempDir()
	adapter := &fakeAdapter{rendered: NewRenderedConfig([]byte("mixed-port: 7890\n"), "http://127.0.0.1:19090")}
	store := testGenerationStore(root)
	snapshot := managedSnapshot()

	first, err := store.Prepare(context.Background(), snapshot, adapter)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Prepare(context.Background(), snapshot, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if first.GenerationID != second.GenerationID || first.ConfigPath != second.ConfigPath {
		t.Fatalf("generation was not deterministic: %+v %+v", first, second)
	}
	if adapter.validateCalls != 2 {
		t.Fatalf("expected both new and reused generation validation, got %d", adapter.validateCalls)
	}
	assertMode(t, filepath.Dir(first.ConfigPath), 0o700)
	assertMode(t, first.ConfigPath, 0o600)
	assertMode(t, filepath.Join(filepath.Dir(first.ConfigPath), "metadata.json"), 0o600)
	if err := store.MarkActive(first); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(root, "state", "runtime.json"), 0o600)

	var active RuntimeSpec
	raw, err := os.ReadFile(filepath.Join(root, "state", "runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &active); err != nil || active.GenerationID != first.GenerationID {
		t.Fatalf("unexpected runtime metadata: %+v, %v", active, err)
	}
}

func TestGenerationStore_ValidationFailureDoesNotPublish(t *testing.T) {
	root := t.TempDir()
	adapter := &fakeAdapter{
		rendered:    NewRenderedConfig([]byte("mixed-port: 7890\n"), "http://127.0.0.1:19090"),
		validateErr: errors.New("invalid"),
	}
	store := testGenerationStore(root)
	_, err := store.Prepare(context.Background(), managedSnapshot(), adapter)
	if err == nil {
		t.Fatal("expected validation failure")
	}
	entries, readErr := os.ReadDir(filepath.Join(root, "generations", profileDirectory("profile-one")))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("pending generation leaked: %v", entries)
	}
}

func TestGenerationStore_ExternalIsReadOnlyAndDetectsChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "external.yaml")
	if err := os.WriteFile(path, []byte("mode: rule\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &fakeAdapter{}
	store := testGenerationStore(root)
	snapshot := ProfileSnapshot{
		ProfileID: "external", Revision: 1, Mode: domain.ProfileModeExternal,
		ExternalConfigPath: path, ControllerEndpoint: "http://127.0.0.1:19090",
	}
	spec, err := store.Prepare(context.Background(), snapshot, adapter)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode() != after.Mode() || before.Size() != after.Size() {
		t.Fatalf("external config was modified: before=%v after=%v", before.Mode(), after.Mode())
	}
	if err := os.WriteFile(path, []byte("mode: global\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckSource(spec); !errors.Is(err, ErrConfigChanged) {
		t.Fatalf("expected config changed, got %v", err)
	}
}

func TestGenerationStore_RejectsExternalSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.yaml")
	link := filepath.Join(root, "link.yaml")
	if err := os.WriteFile(target, []byte("mode: rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := testGenerationStore(root).Prepare(context.Background(), ProfileSnapshot{
		ProfileID: "external", Revision: 1, Mode: domain.ProfileModeExternal,
		ExternalConfigPath: link, ControllerEndpoint: "http://127.0.0.1:19090",
	}, &fakeAdapter{})
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestGenerationStore_BootstrapIsPrivateValidatedAndCleaned(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "external.yaml")
	source := []byte("mode: rule\n")
	if err := os.WriteFile(path, source, 0o640); err != nil {
		t.Fatal(err)
	}
	adapter := &fakeAdapter{}
	store := testGenerationStore(root)
	spec, cleanup, err := store.PrepareBootstrap(context.Background(), ProfileSnapshot{
		ProfileID: "external", Revision: 1, Mode: domain.ProfileModeLegacy,
		ExternalConfigPath: path, ControllerEndpoint: "http://127.0.0.1:19090",
	}, adapter, func(content []byte) ([]byte, error) {
		return append(content, []byte("mode: global\n")...), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if adapter.validateCalls != 1 || spec.GenerationID[:10] != "bootstrap-" {
		t.Fatalf("unexpected bootstrap spec: %+v", spec)
	}
	bootstrapDir := filepath.Dir(spec.ConfigPath)
	assertMode(t, bootstrapDir, 0o700)
	assertMode(t, spec.ConfigPath, 0o600)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(source) {
		t.Fatalf("source changed: %q, %v", got, err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bootstrapDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bootstrap generation still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "state", "runtime.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bootstrap unexpectedly published runtime metadata: %v", err)
	}
}

func TestGenerationStore_BootstrapValidationFailureCleansPendingDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "external.yaml")
	if err := os.WriteFile(path, []byte("mode: rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := testGenerationStore(root)
	_, _, err := store.PrepareBootstrap(context.Background(), ProfileSnapshot{
		ProfileID: "external", Revision: 1, Mode: domain.ProfileModeLegacy,
		ExternalConfigPath: path, ControllerEndpoint: "http://127.0.0.1:19090",
	}, &fakeAdapter{validateErr: errors.New("invalid")}, func(content []byte) ([]byte, error) { return content, nil })
	if err == nil {
		t.Fatal("expected validation failure")
	}
	entries, readErr := os.ReadDir(filepath.Join(root, "generations", profileDirectory("external")))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("bootstrap directory leaked: %v", entries)
	}
}

type fakeAdapter struct {
	rendered      RenderedConfig
	validateErr   error
	validateCalls int
}

func (a *fakeAdapter) Type() domain.CoreType { return domain.CoreTypeMihomo }
func (a *fakeAdapter) Render(context.Context, ProfileSnapshot) (RenderedConfig, error) {
	return a.rendered, nil
}
func (a *fakeAdapter) Validate(context.Context, RuntimeSpec) error {
	a.validateCalls++
	return a.validateErr
}
func (a *fakeAdapter) Start(context.Context, RuntimeSpec) (Process, error) { return nil, nil }
func (a *fakeAdapter) Runtime(string) (RuntimeClient, error)               { return nil, nil }

func managedSnapshot() ProfileSnapshot {
	return ProfileSnapshot{
		ProfileID: "profile-one", Revision: 3, Mode: domain.ProfileModeManaged,
		ControllerEndpoint: "http://127.0.0.1:19090", MixedPort: 17890, SocksPort: 17891,
		Proxies: []Proxy{{ID: "node-one", Name: "node one", Protocol: "socks5", Spec: json.RawMessage(`{"server":"127.0.0.1","port":1080}`)}},
	}
}

func testGenerationStore(root string) *GenerationStore {
	return NewGenerationStore(GenerationStoreOptions{
		GenerationsDir: filepath.Join(root, "generations"),
		RuntimeState:   filepath.Join(root, "state", "runtime.json"),
		LogPath:        filepath.Join(root, "state", "mihomo.log"),
		Clock:          func() time.Time { return time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC) },
	})
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
