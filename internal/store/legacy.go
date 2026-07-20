package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

const legacyMigrationColumns = `id, profile_id, source_dir, state, error_code, created_at, updated_at, completed_at, rolled_back_at`

type LegacyMigrationRepository interface {
	CreateLegacyMigration(context.Context, domain.LegacyMigration, domain.Profile, domain.Operation) error
	GetLegacyMigration(context.Context, domain.RestorePointID) (domain.LegacyMigration, error)
	ListLegacyMigrations(context.Context) ([]domain.LegacyMigration, error)
	UpdateLegacyMigration(context.Context, domain.LegacyMigration, domain.Operation, bool) (bool, error)
	RollbackLegacyMigration(context.Context, domain.LegacyMigration, domain.Operation) error
	UpdateLegacyFileExpectations(context.Context, domain.LegacyMigration) error
}

func (s *Store) UpdateLegacyFileExpectations(ctx context.Context, migration domain.LegacyMigration) error {
	if err := migration.Validate(); err != nil {
		return invalid("update legacy file expectations", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("begin legacy file expectation update", err)
	}
	defer tx.Rollback()
	for _, file := range migration.Files {
		if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_files(migration_id, relative_path, before_exists, before_mode, before_size, before_sha256, expected_exists, expected_sha256, snapshot_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(migration_id, relative_path) DO UPDATE SET expected_exists = excluded.expected_exists, expected_sha256 = excluded.expected_sha256`,
			migration.ID.String(), file.RelativePath, boolInt(file.BeforeExists), file.BeforeMode, file.BeforeSize, nullableText(file.BeforeSHA256), boolInt(file.ExpectedExists), nullableText(file.ExpectedSHA256), nullableText(file.SnapshotPath)); err != nil {
			return storeError("update legacy file expectation", err)
		}
	}
	return storeError("commit legacy file expectation update", tx.Commit())
}

func (s *Store) CreateLegacyMigration(ctx context.Context, migration domain.LegacyMigration, profile domain.Profile, operation domain.Operation) error {
	if err := migration.Validate(); err != nil {
		return invalid("create legacy migration", err)
	}
	if err := profile.Validate(); err != nil {
		return invalid("create legacy migration profile", err)
	}
	if profile.Mode != domain.ProfileModeLegacy || profile.ID != migration.ProfileID || profile.ConfigPath == "" {
		return invalid("create legacy migration profile", errors.New("profile must be a legacy profile for the migration"))
	}
	if err := operation.Validate(); err != nil {
		return invalid("create legacy migration operation", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("begin legacy migration", err)
	}
	defer tx.Rollback()
	var existing domain.Profile
	row := tx.QueryRowContext(ctx, "SELECT "+profileColumns+" FROM profiles WHERE id = ?", profile.ID.String())
	existing, err = scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO profiles(id, name, mode, core_type, config_path, active, revision, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?)`, profile.ID.String(), profile.Name, profile.Mode.String(), profile.CoreType.String(), profile.ConfigPath,
			profile.Revision, formatTime(profile.CreatedAt), formatTime(profile.UpdatedAt))
		if err != nil {
			return storeError("create legacy profile", err)
		}
	} else if err != nil {
		return storeError("inspect legacy profile", err)
	} else if existing.Mode != domain.ProfileModeLegacy || existing.ConfigPath != profile.ConfigPath {
		return fmt.Errorf("legacy profile %s: %w", profile.ID, ErrConflict)
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(1) FROM legacy_migrations WHERE source_dir = ? AND state != 'rolled_back'", migration.SourceDir).Scan(&count); err != nil {
		return storeError("inspect existing legacy migration", err)
	}
	if count > 0 {
		return fmt.Errorf("legacy source %s: %w", migration.SourceDir, ErrConflict)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO legacy_migrations(id, profile_id, source_dir, state, error_code, created_at, updated_at, completed_at, rolled_back_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, migration.ID.String(), migration.ProfileID.String(), migration.SourceDir, migration.State.String(), nullableText(migration.ErrorCode),
		formatTime(migration.CreatedAt), formatTime(migration.UpdatedAt), nullableTime(migration.CompletedAt), nullableTime(migration.RolledBackAt))
	if err != nil {
		return storeError("create legacy migration", err)
	}
	if err := insertLegacyFiles(ctx, tx, migration); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id, profile_id, kind, state, phase, attempt, recovery, error_code, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, operation.ID.String(), nullableProfileID(operation.ProfileID), operation.Kind, operation.State.String(), operation.Phase,
		operation.Attempt, string(operation.Recovery), nullableText(operation.ErrorCode), formatTime(operation.CreatedAt), formatTime(operation.UpdatedAt)); err != nil {
		return storeError("create legacy migration operation", err)
	}
	return storeError("commit legacy migration", tx.Commit())
}

func insertLegacyFiles(ctx context.Context, tx *sql.Tx, migration domain.LegacyMigration) error {
	for _, file := range migration.Files {
		_, err := tx.ExecContext(ctx, `INSERT INTO legacy_files(migration_id, relative_path, before_exists, before_mode, before_size, before_sha256, expected_exists, expected_sha256, snapshot_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, migration.ID.String(), file.RelativePath, boolInt(file.BeforeExists), file.BeforeMode, file.BeforeSize,
			nullableText(file.BeforeSHA256), boolInt(file.ExpectedExists), nullableText(file.ExpectedSHA256), nullableText(file.SnapshotPath))
		if err != nil {
			return storeError("create legacy file manifest", err)
		}
	}
	return nil
}

func (s *Store) GetLegacyMigration(ctx context.Context, id domain.RestorePointID) (domain.LegacyMigration, error) {
	if err := id.Validate(); err != nil {
		return domain.LegacyMigration{}, invalid("get legacy migration", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return domain.LegacyMigration{}, err
	}
	defer release()
	migration, err := scanLegacyMigration(db.QueryRowContext(ctx, "SELECT "+legacyMigrationColumns+" FROM legacy_migrations WHERE id = ?", id.String()))
	if err != nil {
		return domain.LegacyMigration{}, storeError("get legacy migration "+id.String(), err)
	}
	if err := loadLegacyFiles(ctx, db, &migration); err != nil {
		return domain.LegacyMigration{}, err
	}
	return migration, nil
}

func (s *Store) ListLegacyMigrations(ctx context.Context) ([]domain.LegacyMigration, error) {
	db, release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	rows, err := db.QueryContext(ctx, "SELECT "+legacyMigrationColumns+" FROM legacy_migrations ORDER BY created_at, id")
	if err != nil {
		return nil, storeError("list legacy migrations", err)
	}
	var result []domain.LegacyMigration
	for rows.Next() {
		migration, err := scanLegacyMigration(rows)
		if err != nil {
			_ = rows.Close()
			return nil, storeError("list legacy migrations", err)
		}
		result = append(result, migration)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, storeError("list legacy migrations", err)
	}
	if err := rows.Close(); err != nil {
		return nil, storeError("close legacy migrations", err)
	}
	for index := range result {
		if err := loadLegacyFiles(ctx, db, &result[index]); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func loadLegacyFiles(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, migration *domain.LegacyMigration) error {
	rows, err := db.QueryContext(ctx, `SELECT relative_path, before_exists, before_mode, before_size, before_sha256, expected_exists, expected_sha256, snapshot_path
		FROM legacy_files WHERE migration_id = ? ORDER BY relative_path`, migration.ID.String())
	if err != nil {
		return storeError("load legacy file manifest", err)
	}
	defer rows.Close()
	for rows.Next() {
		var file domain.LegacyFileSnapshot
		var beforeExists, expectedExists int
		var beforeSHA, expectedSHA, snapshot sql.NullString
		if err := rows.Scan(&file.RelativePath, &beforeExists, &file.BeforeMode, &file.BeforeSize, &beforeSHA, &expectedExists, &expectedSHA, &snapshot); err != nil {
			return storeError("scan legacy file manifest", err)
		}
		file.BeforeExists = beforeExists == 1
		file.ExpectedExists = expectedExists == 1
		file.BeforeSHA256, file.ExpectedSHA256, file.SnapshotPath = beforeSHA.String, expectedSHA.String, snapshot.String
		migration.Files = append(migration.Files, file)
	}
	return storeError("load legacy file manifest", rows.Err())
}

func scanLegacyMigration(row rowScanner) (domain.LegacyMigration, error) {
	var migration domain.LegacyMigration
	var errorCode, completedAt, rolledBackAt sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&migration.ID, &migration.ProfileID, &migration.SourceDir, &migration.State, &errorCode, &createdAt, &updatedAt, &completedAt, &rolledBackAt); err != nil {
		return domain.LegacyMigration{}, err
	}
	migration.ErrorCode = errorCode.String
	var err error
	migration.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return domain.LegacyMigration{}, err
	}
	migration.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return domain.LegacyMigration{}, err
	}
	migration.CompletedAt, err = parseNullableTime(completedAt)
	if err != nil {
		return domain.LegacyMigration{}, err
	}
	migration.RolledBackAt, err = parseNullableTime(rolledBackAt)
	return migration, err
}

func (s *Store) UpdateLegacyMigration(ctx context.Context, migration domain.LegacyMigration, operation domain.Operation, activate bool) (bool, error) {
	if err := migration.Validate(); err != nil {
		return false, invalid("update legacy migration", err)
	}
	if err := operation.Validate(); err != nil {
		return false, invalid("update legacy migration operation", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return false, err
	}
	defer release()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, storeError("begin legacy migration update", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE legacy_migrations SET state = ?, error_code = ?, updated_at = ?, completed_at = ?, rolled_back_at = ? WHERE id = ?`,
		migration.State.String(), nullableText(migration.ErrorCode), formatTime(migration.UpdatedAt), nullableTime(migration.CompletedAt), nullableTime(migration.RolledBackAt), migration.ID.String()); err != nil {
		return false, storeError("update legacy migration", err)
	}
	if err := updateLegacyFiles(ctx, tx, migration); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE operations SET state = ?, phase = ?, error_code = ?, updated_at = ? WHERE id = ?`, operation.State.String(), operation.Phase, nullableText(operation.ErrorCode), formatTime(operation.UpdatedAt), operation.ID.String()); err != nil {
		return false, storeError("update legacy migration operation", err)
	}
	if activate {
		if _, err := tx.ExecContext(ctx, `UPDATE profiles SET active = 1, revision = revision + 1, updated_at = ? WHERE id = ? AND NOT EXISTS (SELECT 1 FROM profiles WHERE active = 1)`, formatTime(migration.UpdatedAt), migration.ProfileID.String()); err != nil {
			return false, storeError("activate legacy profile", err)
		}
	}
	var active int
	if err := tx.QueryRowContext(ctx, "SELECT active FROM profiles WHERE id = ?", migration.ProfileID.String()).Scan(&active); err != nil {
		return false, storeError("read legacy profile activation", err)
	}
	if err := tx.Commit(); err != nil {
		return false, storeError("commit legacy migration update", err)
	}
	return active == 1, nil
}

func updateLegacyFiles(ctx context.Context, tx *sql.Tx, migration domain.LegacyMigration) error {
	for _, file := range migration.Files {
		result, err := tx.ExecContext(ctx, `UPDATE legacy_files SET expected_exists = ?, expected_sha256 = ? WHERE migration_id = ? AND relative_path = ?`, boolInt(file.ExpectedExists), nullableText(file.ExpectedSHA256), migration.ID.String(), file.RelativePath)
		if err != nil {
			return storeError("update legacy file manifest", err)
		}
		if err := requireAffected("update legacy file manifest", result); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RollbackLegacyMigration(ctx context.Context, migration domain.LegacyMigration, operation domain.Operation) error {
	if err := migration.Validate(); err != nil {
		return invalid("rollback legacy migration", err)
	}
	if err := operation.Validate(); err != nil {
		return invalid("rollback legacy migration operation", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("begin legacy rollback", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE legacy_migrations SET state = ?, updated_at = ?, rolled_back_at = ? WHERE id = ? AND state != 'rolled_back'`, migration.State.String(), formatTime(migration.UpdatedAt), nullableTime(migration.RolledBackAt), migration.ID.String()); err != nil {
		return storeError("mark legacy migration rolled back", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE profiles SET active = 0, revision = revision + 1, updated_at = ? WHERE id = ?`, formatTime(migration.UpdatedAt), migration.ProfileID.String()); err != nil {
		return storeError("deactivate legacy profile", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id, profile_id, kind, state, phase, attempt, recovery, error_code, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, operation.ID.String(), nullableProfileID(operation.ProfileID), operation.Kind, operation.State.String(), operation.Phase, operation.Attempt, string(operation.Recovery), nullableText(operation.ErrorCode), formatTime(operation.CreatedAt), formatTime(operation.UpdatedAt)); err != nil {
		return storeError("create legacy rollback operation", err)
	}
	return storeError("commit legacy rollback", tx.Commit())
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func migrationRecovery(id domain.RestorePointID) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"restorePoint": id.String()})
	return b
}

func validLegacySource(source string) bool { return strings.TrimSpace(source) != "" }
