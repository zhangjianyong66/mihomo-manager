package legacy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

type ApplyResult struct {
	Migration domain.LegacyMigration `json:"migration"`
	Profile   domain.Profile         `json:"profile"`
}

type RollbackResult struct {
	Migration domain.LegacyMigration `json:"migration"`
}

func (s *Service) Plan(ctx context.Context) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	plan, _, err := s.Discover()
	return plan, err
}

func (s *Service) Apply(ctx context.Context) (ApplyResult, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.repository == nil {
		return ApplyResult{}, errors.New("legacy repository is not configured")
	}
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	paths := s.pathsOrDefault()
	plan, files, err := s.Discover()
	if err != nil {
		return ApplyResult{}, err
	}
	for _, entry := range plan.Entries {
		if entry.Risk != "" {
			return ApplyResult{}, fmt.Errorf("%w: %s is %s", ErrUnsafePath, entry.RelativePath, entry.Risk)
		}
	}
	id := domain.RestorePointID(s.makeID("legacy"))
	backupDir := filepath.Join(s.manager.BackupsDir, id.String())
	if err := publishSnapshot(ctx, paths.ConfigDir, backupDir, files); err != nil {
		return ApplyResult{}, err
	}
	for i := range files {
		if files[i].BeforeExists {
			files[i].SnapshotPath = filepath.Join(backupDir, "files", files[i].RelativePath)
		}
	}
	now := s.now()
	migration := domain.LegacyMigration{ID: id, ProfileID: domain.ProfileID("legacy-mihomo"), SourceDir: paths.ConfigDir, State: domain.LegacyMigrationStatePending, Files: files, CreatedAt: now, UpdatedAt: now}
	profile := domain.Profile{ID: migration.ProfileID, Name: "Legacy mihomo", Mode: domain.ProfileModeLegacy, CoreType: domain.CoreTypeMihomo, ConfigPath: paths.ConfigFile, Active: false, Revision: 1, CreatedAt: now, UpdatedAt: now}
	operationID := domain.OperationID(s.makeID("migration"))
	operation := domain.Operation{ID: operationID, ProfileID: &migration.ProfileID, Kind: "migration.apply", State: domain.OperationStateRunning, Phase: "snapshot_published", Attempt: 1, Recovery: domainMigrationRecovery(id), CreatedAt: now, UpdatedAt: now}
	if err := s.repository.CreateLegacyMigration(ctx, migration, profile, operation); err != nil {
		_ = os.RemoveAll(filepath.Join(s.manager.BackupsDir, id.String()))
		return ApplyResult{}, err
	}
	validationErr := validateConfig(ctx, paths.ConfigDir, paths.ConfigFile, paths.MihomoBin)
	migration.UpdatedAt = s.now()
	operation.UpdatedAt = migration.UpdatedAt
	if validationErr != nil {
		migration.State = domain.LegacyMigrationStateFailed
		migration.ErrorCode = "VALIDATION_FAILED"
		migration.CompletedAt = &migration.UpdatedAt
		operation.State = domain.OperationStateFailed
		operation.Phase = "validation_failed"
		operation.ErrorCode = "VALIDATION_FAILED"
		if _, err := s.repository.UpdateLegacyMigration(ctx, migration, operation, false); err != nil {
			return ApplyResult{}, errors.Join(fmt.Errorf("%w: %v", ErrValidation, validationErr), err)
		}
		return ApplyResult{Migration: migration, Profile: profile}, fmt.Errorf("%w: %v", ErrValidation, validationErr)
	}
	migration.State = domain.LegacyMigrationStateSucceeded
	migration.CompletedAt = &migration.UpdatedAt
	operation.State = domain.OperationStateSucceeded
	operation.Phase = "ready"
	activated, err := s.repository.UpdateLegacyMigration(ctx, migration, operation, true)
	if err != nil {
		return ApplyResult{}, err
	}
	profile.Active = activated
	return ApplyResult{Migration: migration, Profile: profile}, nil
}

func (s *Service) Status(ctx context.Context) ([]domain.LegacyMigration, error) {
	if s.repository == nil {
		return nil, errors.New("legacy repository is not configured")
	}
	return s.repository.ListLegacyMigrations(ctx)
}

func (s *Service) Rollback(ctx context.Context, id domain.RestorePointID) (RollbackResult, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.repository == nil {
		return RollbackResult{}, errors.New("legacy repository is not configured")
	}
	if err := id.Validate(); err != nil {
		return RollbackResult{}, err
	}
	migration, err := s.repository.GetLegacyMigration(ctx, id)
	if err != nil {
		return RollbackResult{}, err
	}
	if migration.State == domain.LegacyMigrationStateRolledBack {
		return RollbackResult{}, ErrAlreadyDone
	}
	if migration.State != domain.LegacyMigrationStateSucceeded && migration.State != domain.LegacyMigrationStateFailed {
		return RollbackResult{}, fmt.Errorf("%w: migration state is %s", ErrConflict, migration.State)
	}
	paths := s.pathsOrDefault()
	for _, file := range migration.Files {
		path, err := safeJoin(paths.ConfigDir, file.RelativePath)
		if err != nil {
			return RollbackResult{}, err
		}
		currentExists, currentDigest, err := currentDigest(path)
		if err != nil {
			return RollbackResult{}, err
		}
		if currentExists != file.ExpectedExists || (currentExists && currentDigest != file.ExpectedSHA256) {
			return RollbackResult{}, fmt.Errorf("%w: %s was modified outside manager", ErrConflict, file.RelativePath)
		}
	}
	backupDir := filepath.Join(s.manager.BackupsDir, "rollback-"+id.String())
	if err := os.MkdirAll(filepath.Join(backupDir, "files"), 0o700); err != nil {
		return RollbackResult{}, fmt.Errorf("prepare rollback backup: %w", err)
	}
	if err := secureDirectory(filepath.Dir(backupDir)); err != nil {
		return RollbackResult{}, err
	}
	if err := os.Chmod(backupDir, 0o700); err != nil {
		return RollbackResult{}, err
	}
	for _, file := range migration.Files {
		path, _ := safeJoin(paths.ConfigDir, file.RelativePath)
		if exists, _, _ := currentDigest(path); exists {
			if err := copyFile(path, filepath.Join(backupDir, "files", file.RelativePath), 0o600); err != nil {
				return RollbackResult{}, err
			}
		}
	}
	for _, file := range migration.Files {
		path, _ := safeJoin(paths.ConfigDir, file.RelativePath)
		if file.BeforeExists {
			if err := copyFile(file.SnapshotPath, path, os.FileMode(file.BeforeMode)); err != nil {
				return RollbackResult{}, fmt.Errorf("restore %s: %w", file.RelativePath, err)
			}
		} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return RollbackResult{}, fmt.Errorf("remove migrated file %s: %w", file.RelativePath, err)
		}
	}
	now := s.now()
	migration.State = domain.LegacyMigrationStateRolledBack
	migration.UpdatedAt = now
	migration.RolledBackAt = &now
	operationID := domain.OperationID(s.makeID("rollback"))
	operation := domain.Operation{ID: operationID, ProfileID: &migration.ProfileID, Kind: "migration.rollback", State: domain.OperationStateSucceeded, Phase: "restored", Attempt: 1, Recovery: domainMigrationRecovery(id), CreatedAt: now, UpdatedAt: now}
	if err := s.repository.RollbackLegacyMigration(ctx, migration, operation); err != nil {
		return RollbackResult{}, err
	}
	return RollbackResult{Migration: migration}, nil
}

func validateConfig(ctx context.Context, configDir, configFile, binary string) error {
	if _, err := os.Stat(configFile); err != nil {
		return fmt.Errorf("config.yaml: %w", err)
	}
	if binary == "" {
		return errors.New("MIHOMO_BIN is empty")
	}
	command := exec.CommandContext(ctx, binary, "-t", "-d", configDir, "-f", configFile)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return err
	}
	return nil
}

func publishSnapshot(ctx context.Context, sourceDir, destination string, files []domain.LegacyFileSnapshot) error {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create restore point: %w", err)
	}
	if err := secureDirectory(parent); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("%w: restore point %s already exists", ErrConflict, filepath.Base(destination))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temp, err := os.MkdirTemp(parent, ".restore-"+filepath.Base(destination)+"-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err := os.Chmod(temp, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(temp, "files"), 0o700); err != nil {
		return err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !file.BeforeExists {
			continue
		}
		source, err := safeJoin(sourceDir, file.RelativePath)
		if err != nil {
			return err
		}
		target := filepath.Join(temp, "files", file.RelativePath)
		if err := copyFile(source, target, 0o600); err != nil {
			return err
		}
		copiedDigest, err := fileSHA256(target)
		if err != nil {
			return err
		}
		if copiedDigest != file.BeforeSHA256 {
			return fmt.Errorf("%w: %s changed while creating restore point", ErrConflict, file.RelativePath)
		}
	}
	if err := syncTree(temp); err != nil {
		return err
	}
	if err := os.Rename(temp, destination); err != nil {
		return fmt.Errorf("publish restore point: %w", err)
	}
	return nil
}

func secureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("%w: unsafe manager directory %s", ErrUnsafePath, path)
	}
	return nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	if info, err := os.Lstat(source); err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s", ErrUnsafePath, source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".legacy-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode.Perm()); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := io.Copy(temp, in); err != nil {
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
	if err := os.Rename(tempName, destination); err != nil {
		return err
	}
	return nil
}

func syncTree(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func currentDigest(path string) (bool, string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, "", fmt.Errorf("%w: %s", ErrUnsafePath, path)
	}
	digest, err := fileSHA256(path)
	return true, digest, err
}

func (s *Service) now() time.Time {
	if s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock().UTC()
}

func (s *Service) makeID(prefix string) string {
	if s.newID != nil {
		return s.newID(prefix)
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func domainMigrationRecovery(id domain.RestorePointID) []byte {
	return []byte(fmt.Sprintf(`{"restorePoint":%q}`, id.String()))
}

func _legacyString(value string) string { return strings.TrimSpace(value) }
