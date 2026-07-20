package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version  int
	name     string
	checksum string
	sql      string
}

var migrations = mustLoadMigrations()

func mustLoadMigrations() []migration {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		panic(fmt.Sprintf("read embedded migrations: %v", err))
	}
	result := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		parts := strings.SplitN(strings.TrimSuffix(entry.Name(), ".sql"), "_", 2)
		if len(parts) != 2 {
			panic("invalid migration filename: " + entry.Name())
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil || version < 1 {
			panic("invalid migration version: " + entry.Name())
		}
		body, err := migrationFiles.ReadFile(filepath.ToSlash(filepath.Join("migrations", entry.Name())))
		if err != nil {
			panic(fmt.Sprintf("read migration %s: %v", entry.Name(), err))
		}
		sum := sha256.Sum256(body)
		result = append(result, migration{
			version: version, name: parts[1], checksum: hex.EncodeToString(sum[:]), sql: string(body),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for i, item := range result {
		if item.version != i+1 {
			panic("migration versions must be continuous")
		}
	}
	return result
}

func migrate(ctx context.Context, db *sql.DB, path string) (SchemaInfo, error) {
	current, hasSchema, err := currentSchema(ctx, db)
	if err != nil {
		return SchemaInfo{}, err
	}
	latest := len(migrations)
	if current > latest {
		return SchemaInfo{}, fmt.Errorf("read database schema version %d: %w", current, ErrSchemaTooNew)
	}
	if hasSchema {
		if err := verifyMigrationHistory(ctx, db, current); err != nil {
			return SchemaInfo{}, err
		}
	}
	info := SchemaInfo{Version: current}
	if current == latest {
		return info, nil
	}
	if current > 0 {
		backupPath, err := createBackup(ctx, db, path, current)
		if err != nil {
			return SchemaInfo{}, err
		}
		info.BackupPath = backupPath
	}

	if err := applyMigrationBatch(ctx, db, current, migrations); err != nil {
		return SchemaInfo{}, err
	}
	info.Version = latest
	info.Migrated = true
	return info, nil
}

func applyMigrationBatch(ctx context.Context, db *sql.DB, current int, items []migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("begin database migration", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range items[current:] {
		if _, err := tx.ExecContext(ctx, item.sql); err != nil {
			return storeError(fmt.Sprintf("apply database migration %d", item.version), err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, name, checksum, applied_at) VALUES (?, ?, ?, ?)",
			item.version, item.name, item.checksum, now,
		); err != nil {
			return storeError(fmt.Sprintf("record database migration %d", item.version), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return storeError("commit database migrations", err)
	}
	return nil
}

func currentSchema(ctx context.Context, db *sql.DB) (version int, hasSchema bool, err error) {
	rows, err := db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name",
	)
	if err != nil {
		return 0, false, storeError("inspect database schema", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, false, storeError("inspect database schema", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return 0, false, storeError("inspect database schema", err)
	}
	if len(tables) == 0 {
		return 0, false, nil
	}
	for _, table := range tables {
		if table == "schema_migrations" {
			hasSchema = true
			break
		}
	}
	if !hasSchema {
		return 0, false, fmt.Errorf("inspect database schema: %w: migration metadata is missing", ErrMigrationDrift)
	}
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, true, storeError("read database schema version", err)
	}
	if version == 0 {
		return 0, true, fmt.Errorf("inspect database schema: %w: migration history is empty", ErrMigrationDrift)
	}
	return version, true, nil
}

func verifyMigrationHistory(ctx context.Context, db *sql.DB, current int) error {
	rows, err := db.QueryContext(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return storeError("read database migration history", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var version int
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			return storeError("read database migration history", err)
		}
		seen++
		if version != seen || version > len(migrations) {
			if version > len(migrations) {
				return fmt.Errorf("read database migration %d: %w", version, ErrSchemaTooNew)
			}
			return fmt.Errorf("read database migration %d: %w", version, ErrMigrationDrift)
		}
		expected := migrations[version-1]
		if name != expected.name || checksum != expected.checksum {
			return fmt.Errorf("verify database migration %d: %w", version, ErrMigrationDrift)
		}
	}
	if err := rows.Err(); err != nil {
		return storeError("read database migration history", err)
	}
	if seen != current {
		return fmt.Errorf("verify database migration history: %w", ErrMigrationDrift)
	}
	return nil
}
