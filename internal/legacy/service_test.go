package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/store"
)

func TestServiceApplyStatusConflictAndRollback(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "config.yaml")
	subscriptionFile := filepath.Join(configDir, "subscription.url")
	if err := os.WriteFile(configFile, []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subscriptionFile, []byte("https://example.invalid/sub?token=secret-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "mihomo.log"), []byte("secret log"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeConfig, _ := os.ReadFile(configFile)
	beforeSubscription, _ := os.ReadFile(subscriptionFile)
	service, stateStore := testService(t, root, configDir, fakeValidator(t, root, true))

	plan, err := service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustJSON(t, plan)), "secret-value") {
		t.Fatal("migration plan leaked subscription URL")
	}
	result, err := service.Apply(context.Background())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Migration.State != domain.LegacyMigrationStateSucceeded || !result.Profile.Active {
		t.Fatalf("unexpected apply result: %+v", result)
	}
	profile, err := stateStore.GetProfile(context.Background(), result.Migration.ProfileID)
	if err != nil || !profile.Active || profile.Mode != domain.ProfileModeLegacy || profile.ConfigPath != configFile {
		t.Fatalf("unexpected profile: %+v %v", profile, err)
	}
	if got, _ := os.ReadFile(configFile); string(got) != string(beforeConfig) {
		t.Fatal("apply rewrote legacy config")
	}
	if got, _ := os.ReadFile(subscriptionFile); string(got) != string(beforeSubscription) {
		t.Fatal("apply rewrote subscription URL")
	}
	for _, file := range result.Migration.Files {
		if file.BeforeExists {
			assertFileMode(t, file.SnapshotPath, 0o600)
		}
		if file.RelativePath == "mihomo.log" {
			t.Fatal("log file was included in recovery snapshot")
		}
	}
	if _, err := service.Apply(context.Background()); !errors.Is(err, ErrConflict) && !errors.Is(err, store.ErrConflict) {
		t.Fatalf("repeat apply error = %v, want conflict", err)
	}
	if err := os.WriteFile(configFile, []byte("externally changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Rollback(context.Background(), result.Migration.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("external modification error = %v, want conflict", err)
	}
	if err := os.WriteFile(configFile, beforeConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := service.Rollback(context.Background(), result.Migration.ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolledBack.Migration.State != domain.LegacyMigrationStateRolledBack {
		t.Fatalf("rollback state = %s", rolledBack.Migration.State)
	}
	if _, err := service.Rollback(context.Background(), result.Migration.ID); !errors.Is(err, ErrAlreadyDone) {
		t.Fatalf("second rollback error = %v", err)
	}
}

func TestServiceApplyInvalidAndEmptyConfiguration(t *testing.T) {
	for _, test := range []struct {
		name      string
		writeFile bool
		validator bool
	}{
		{name: "invalid", writeFile: true, validator: false},
		{name: "empty", writeFile: false, validator: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			configDir := filepath.Join(root, "legacy")
			if err := os.MkdirAll(configDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if test.writeFile {
				if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("invalid"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			service, stateStore := testService(t, root, configDir, fakeValidator(t, root, test.validator))
			result, err := service.Apply(context.Background())
			if !errors.Is(err, ErrValidation) || result.Migration.State != domain.LegacyMigrationStateFailed {
				t.Fatalf("apply result=%+v err=%v", result, err)
			}
			profile, profileErr := stateStore.GetProfile(context.Background(), result.Migration.ProfileID)
			if profileErr != nil || profile.Active {
				t.Fatalf("invalid profile unexpectedly active: %+v %v", profile, profileErr)
			}
			status, statusErr := service.Status(context.Background())
			if statusErr != nil || len(status) != 1 || status[0].ErrorCode != "VALIDATION_FAILED" {
				t.Fatalf("unexpected status: %+v %v", status, statusErr)
			}
		})
	}
}

func TestServiceRejectsUnsafeLegacyPaths(t *testing.T) {
	t.Run("symbolic CONFIG_DIR", func(t *testing.T) {
		root := t.TempDir()
		realDir := filepath.Join(root, "real")
		if err := os.Mkdir(realDir, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(realDir, link); err != nil {
			t.Fatal(err)
		}
		service, _ := testService(t, root, link, fakeValidator(t, root, true))
		if _, err := service.Plan(context.Background()); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("symlink plan error = %v", err)
		}
	})
	t.Run("world writable config", func(t *testing.T) {
		root := t.TempDir()
		configDir := filepath.Join(root, "legacy")
		if err := os.Mkdir(configDir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(configDir, "config.yaml")
		if err := os.WriteFile(path, []byte("config"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o606); err != nil {
			t.Fatal(err)
		}
		service, _ := testService(t, root, configDir, fakeValidator(t, root, true))
		if _, err := service.Apply(context.Background()); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("unsafe permission error = %v", err)
		}
	})
}

func TestCompatibilityWritesTrackExpectedFilesAndRollback(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "config.yaml")
	original := []byte("mixed-port: 7890\nexternal-controller: 127.0.0.1:9090\nrules:\n  - MATCH,DIRECT\n")
	if err := os.WriteFile(configFile, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service, _ := testService(t, root, configDir, fakeValidator(t, root, true))
	result, err := service.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	compat := service.Compatibility()
	if err := compat.SaveSubscriptionURL(context.Background(), result.Migration.ID, "https://example.invalid/new"); err != nil {
		t.Fatalf("save subscription: %v", err)
	}
	if value, err := compat.ReadSubscriptionURL(context.Background(), result.Migration.ID); err != nil || value != "https://example.invalid/new" {
		t.Fatalf("read subscription = %q, %v", value, err)
	}
	if err := compat.AddWhitelist(context.Background(), result.Migration.ID, "example.com"); err != nil {
		t.Fatalf("add whitelist: %v", err)
	}
	domains, err := compat.ListWhitelist(context.Background(), result.Migration.ID)
	if err != nil || len(domains) != 1 || domains[0] != "example.com" {
		t.Fatalf("whitelist = %v, %v", domains, err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "config.yaml.bak")); err != nil {
		t.Fatalf("compatibility backup missing: %v", err)
	}
	if _, err := service.Rollback(context.Background(), result.Migration.ID); err != nil {
		t.Fatalf("rollback compatibility writes: %v", err)
	}
	if got, err := os.ReadFile(configFile); err != nil || string(got) != string(original) {
		t.Fatalf("config after compatibility rollback = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "subscription.url")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("subscription after rollback error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "whitelist.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("whitelist after rollback error = %v", err)
	}
}

func testService(t *testing.T, root, configDir, validator string) (*Service, *store.Store) {
	t.Helper()
	manager, err := config.ResolveManagerPaths(config.ManagerEnvironment{HomeDir: root, DataHome: filepath.Join(root, "data"), StateHome: filepath.Join(root, "state"), RuntimeDir: filepath.Join(root, "run"), ConfigHome: filepath.Join(root, "config")})
	if err != nil {
		t.Fatal(err)
	}
	stateStore, err := store.Open(context.Background(), manager.Database, store.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stateStore.Close() })
	paths := config.Paths{MihomoBin: validator, ConfigDir: configDir, ConfigFile: filepath.Join(configDir, "config.yaml"), BackupFile: filepath.Join(configDir, "config.yaml.bak"), SubscriptionURL: filepath.Join(configDir, "subscription.url"), WhitelistFile: filepath.Join(configDir, "whitelist.yaml"), APIAddr: "http://127.0.0.1:9090"}
	service := NewService(paths, manager, stateStore)
	fixed := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return fixed }
	service.newID = func(prefix string) string { return prefix + "-test" }
	return service, stateStore
}

func fakeValidator(t *testing.T, root string, valid bool) string {
	t.Helper()
	path := filepath.Join(root, "fake-mihomo-"+strings.ReplaceAll(t.Name(), "/", "-"))
	exit := "0"
	if !valid {
		exit = "1"
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit "+exit+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("mode %s = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
