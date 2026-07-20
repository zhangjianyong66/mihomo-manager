package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestOpen_InitializesSchemaAndSecuresFiles(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(directory, "state.db")
	store, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	info := store.SchemaInfo()
	if info.Version != len(migrations) || !info.Migrated || info.BackupPath != "" {
		t.Fatalf("unexpected schema info: %+v", info)
	}
	if got := store.db.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1", got)
	}
	assertMode(t, directory, 0o700)
	assertMode(t, path, 0o600)
	assertPragma(t, store.db, "foreign_keys", "1")
	assertPragma(t, store.db, "journal_mode", "delete")
	assertPragma(t, store.db, "synchronous", "2")

	wantTables := []string{"legacy_files", "legacy_migrations", "nodes", "operations", "profiles", "schema_migrations", "settings", "subscriptions"}
	if got := userTables(t, store.db); !equalStrings(got, wantTables) {
		t.Fatalf("tables = %v, want %v", got, wantTables)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store again: %v", err)
	}
	if _, err := store.ListProfiles(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed store error = %v, want ErrClosed", err)
	}

	reopened, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	if info := reopened.SchemaInfo(); info.Version != len(migrations) || info.Migrated || info.BackupPath != "" {
		t.Fatalf("unexpected reopened schema info: %+v", info)
	}
}

func TestOpen_RejectsSymbolicLinkAndUnknownDatabase(t *testing.T) {
	t.Run("symbolic link", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "target.db")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(directory, "state.db")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), link, OpenOptions{}); !errors.Is(err, ErrPermission) {
			t.Fatalf("open symbolic link error = %v, want ErrPermission", err)
		}
	})

	t.Run("missing migration metadata", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.db")
		db := openRawDB(t, path)
		if _, err := db.Exec("CREATE TABLE foreign_data(id INTEGER PRIMARY KEY)"); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), path, OpenOptions{}); !errors.Is(err, ErrMigrationDrift) {
			t.Fatalf("open unknown database error = %v, want ErrMigrationDrift", err)
		}
	})

	t.Run("empty migration metadata", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.db")
		db := openRawDB(t, path)
		if _, err := db.Exec(`CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL
		)`); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), path, OpenOptions{}); !errors.Is(err, ErrMigrationDrift) {
			t.Fatalf("open empty migration history error = %v, want ErrMigrationDrift", err)
		}
	})
}

func TestOpen_TightensExistingPermissions(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	assertMode(t, directory, 0o700)
	assertMode(t, path, 0o600)

	journal := path + "-journal"
	if err := os.WriteFile(journal, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := secureDatabaseArtifacts(path); err != nil {
		t.Fatalf("secure database artifacts: %v", err)
	}
	assertMode(t, journal, 0o600)
}

func TestOpen_RejectsCorruptAndIncompatibleSchema(t *testing.T) {
	t.Run("corrupt", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.db")
		if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), path, OpenOptions{}); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("open corrupt database error = %v, want ErrCorrupt", err)
		}
	})

	t.Run("schema too new", func(t *testing.T) {
		path := initializedPath(t)
		db := openRawDB(t, path)
		if _, err := db.Exec(`INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (4, 'future', 'future', ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), path, OpenOptions{}); !errors.Is(err, ErrSchemaTooNew) {
			t.Fatalf("open future schema error = %v, want ErrSchemaTooNew", err)
		}
	})

	t.Run("checksum drift", func(t *testing.T) {
		path := initializedPath(t)
		db := openRawDB(t, path)
		if _, err := db.Exec("UPDATE schema_migrations SET checksum = 'changed' WHERE version = 1"); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), path, OpenOptions{}); !errors.Is(err, ErrMigrationDrift) {
			t.Fatalf("open drifted schema error = %v, want ErrMigrationDrift", err)
		}
	})
}

func TestMigration_UpgradeCreatesConsistentBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db := openRawDB(t, path)
	if err := applyMigrationBatch(context.Background(), db, 0, migrations[:1]); err != nil {
		t.Fatalf("apply version 1: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatalf("upgrade store: %v", err)
	}
	defer store.Close()
	info := store.SchemaInfo()
	if info.Version != len(migrations) || !info.Migrated || info.BackupPath == "" {
		t.Fatalf("unexpected upgrade info: %+v", info)
	}
	assertMode(t, info.BackupPath, 0o600)
	backup := openRawDB(t, info.BackupPath)
	defer backup.Close()
	var version int
	if err := backup.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatalf("read backup version: %v", err)
	}
	if version != 1 {
		t.Fatalf("backup version = %d, want 1", version)
	}
	var operations int
	if err := backup.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'operations'").Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if operations != 0 {
		t.Fatal("version 1 backup unexpectedly contains operations table")
	}
}

func TestMigration_FailedBatchRollsBackAllDDL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db := openRawDB(t, path)
	defer db.Close()
	items := append([]migration(nil), migrations[0])
	items = append(items, migration{version: 2, name: "broken", checksum: "broken", sql: "CREATE TABLE broken(id); invalid SQL"})
	if err := applyMigrationBatch(context.Background(), db, 0, items); err == nil {
		t.Fatal("expected migration batch to fail")
	}
	if got := userTables(t, db); len(got) != 0 {
		t.Fatalf("failed migration left tables behind: %v", got)
	}
}

func TestSchema_EnforcesActiveAndExpectedBoundaries(t *testing.T) {
	store := openTestStore(t)
	now := time.Now().UTC()
	if _, err := store.db.Exec(`INSERT INTO profiles(id, name, mode, core_type, config_path, active, revision, created_at, updated_at)
		VALUES ('p1', 'one', 'managed', 'mihomo', NULL, 1, 1, ?, ?)`, formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO profiles(id, name, mode, core_type, config_path, active, revision, created_at, updated_at)
		VALUES ('p2', 'two', 'managed', 'mihomo', NULL, 1, 1, ?, ?)`, formatTime(now), formatTime(now)); err == nil {
		t.Fatal("expected second active profile to violate unique index")
	}
	for _, absent := range []string{"routes", "dns", "traffic", "logs"} {
		var count int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", absent).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("out-of-scope table %q exists", absent)
		}
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "state", "state.db"), OpenOptions{BusyTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test store: %v", err)
		}
	})
	return store
}

func initializedPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := Open(context.Background(), path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func openRawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteDSN(path, defaultBusyTimeout))
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping raw database: %v", err)
	}
	return db
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode for %s = %#o, want %#o", path, got, want)
	}
}

func assertPragma(t *testing.T, db *sql.DB, name, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
		t.Fatalf("read pragma %s: %v", name, err)
	}
	if got != want {
		t.Fatalf("pragma %s = %q, want %q", name, got, want)
	}
}

func userTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		result = append(result, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(result)
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
