package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

const profileColumns = `id, name, mode, core_type, config_path, active, revision, created_at, updated_at`

func (s *Store) CreateProfile(ctx context.Context, profile domain.Profile) error {
	if err := profile.Validate(); err != nil {
		return invalid("create profile", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	_, err = db.ExecContext(ctx, `
		INSERT INTO profiles(id, name, mode, core_type, config_path, active, revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profile.ID.String(), profile.Name, profile.Mode.String(), profile.CoreType.String(), nullableText(profile.ConfigPath),
		profile.Active, profile.Revision, formatTime(profile.CreatedAt), formatTime(profile.UpdatedAt),
	)
	return storeError("create profile "+profile.ID.String(), err)
}

func (s *Store) GetProfile(ctx context.Context, id domain.ProfileID) (domain.Profile, error) {
	if err := id.Validate(); err != nil {
		return domain.Profile{}, invalid("get profile", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return domain.Profile{}, err
	}
	defer release()
	profile, err := scanProfile(db.QueryRowContext(ctx, "SELECT "+profileColumns+" FROM profiles WHERE id = ?", id.String()))
	if err != nil {
		return domain.Profile{}, storeError("get profile "+id.String(), err)
	}
	return profile, nil
}

func (s *Store) ListProfiles(ctx context.Context) ([]domain.Profile, error) {
	db, release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	rows, err := db.QueryContext(ctx, "SELECT "+profileColumns+" FROM profiles ORDER BY name, id")
	if err != nil {
		return nil, storeError("list profiles", err)
	}
	defer rows.Close()
	var profiles []domain.Profile
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			return nil, storeError("list profiles", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, storeError("list profiles", err)
	}
	return profiles, nil
}

func (s *Store) UpdateProfile(ctx context.Context, profile domain.Profile) (domain.Profile, error) {
	if err := profile.Validate(); err != nil {
		return domain.Profile{}, invalid("update profile", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return domain.Profile{}, err
	}
	defer release()
	result, err := db.ExecContext(ctx, `
		UPDATE profiles
		SET name = ?, mode = ?, core_type = ?, config_path = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ?`,
		profile.Name, profile.Mode.String(), profile.CoreType.String(), nullableText(profile.ConfigPath),
		formatTime(profile.UpdatedAt), profile.ID.String(), profile.Revision,
	)
	if err != nil {
		return domain.Profile{}, storeError("update profile "+profile.ID.String(), err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return domain.Profile{}, storeError("update profile "+profile.ID.String(), err)
	}
	if affected == 0 {
		var exists int
		err := db.QueryRowContext(ctx, "SELECT 1 FROM profiles WHERE id = ?", profile.ID.String()).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Profile{}, fmt.Errorf("update profile %s: %w", profile.ID, ErrNotFound)
		}
		if err != nil {
			return domain.Profile{}, storeError("update profile "+profile.ID.String(), err)
		}
		return domain.Profile{}, fmt.Errorf("update profile %s: %w", profile.ID, ErrConflict)
	}
	updated, err := scanProfile(db.QueryRowContext(ctx, "SELECT "+profileColumns+" FROM profiles WHERE id = ?", profile.ID.String()))
	if err != nil {
		return domain.Profile{}, storeError("read updated profile "+profile.ID.String(), err)
	}
	return updated, nil
}

func (s *Store) SetActiveProfile(ctx context.Context, id domain.ProfileID, updatedAt time.Time) error {
	if err := id.Validate(); err != nil {
		return invalid("set active profile", err)
	}
	return s.RestoreActiveProfile(ctx, &id, updatedAt)
}

func (s *Store) ActiveProfileID(ctx context.Context) (domain.ProfileID, bool, error) {
	db, release, err := s.acquire()
	if err != nil {
		return "", false, err
	}
	defer release()
	var id string
	err = db.QueryRowContext(ctx, "SELECT id FROM profiles WHERE active = 1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, storeError("get active profile", err)
	}
	return domain.ProfileID(id), true, nil
}

func (s *Store) RestoreActiveProfile(ctx context.Context, id *domain.ProfileID, updatedAt time.Time) error {
	if id != nil {
		if err := id.Validate(); err != nil {
			return invalid("restore active profile", err)
		}
	}
	if err := validateStoreTime(updatedAt); err != nil {
		return invalid("restore active profile", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return storeError("begin active profile restore", err)
	}
	defer tx.Rollback()
	if id != nil {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM profiles WHERE id = ?", id.String()).Scan(&exists); err != nil {
			return storeError("find active profile target "+id.String(), err)
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE profiles SET active = 0, revision = revision + 1, updated_at = ? WHERE active = 1", formatTime(updatedAt)); err != nil {
		return storeError("clear active profile", err)
	}
	if id != nil {
		if _, err := tx.ExecContext(ctx, "UPDATE profiles SET active = 1, revision = revision + 1, updated_at = ? WHERE id = ?", formatTime(updatedAt), id.String()); err != nil {
			return storeError("restore active profile "+id.String(), err)
		}
	}
	return storeError("commit active profile restore", tx.Commit())
}

func (s *Store) DeleteProfile(ctx context.Context, id domain.ProfileID) error {
	if err := id.Validate(); err != nil {
		return invalid("delete profile", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	result, err := db.ExecContext(ctx, "DELETE FROM profiles WHERE id = ?", id.String())
	if err != nil {
		return storeError("delete profile "+id.String(), err)
	}
	return requireAffected("delete profile "+id.String(), result)
}

func validateStoreTime(value time.Time) error {
	if value.IsZero() || value.Location() != time.UTC {
		return errors.New("time must be non-zero UTC")
	}
	return nil
}

func requireAffected(action string, result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return storeError(action, err)
	}
	if affected == 0 {
		return fmt.Errorf("%s: %w", action, ErrNotFound)
	}
	return nil
}
