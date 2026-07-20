package store

import (
	"context"
	"errors"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

const settingColumns = `profile_id, key, value, updated_at`

func (s *Store) SetSetting(ctx context.Context, setting domain.Setting) error {
	if err := setting.Validate(); err != nil {
		return invalid("set setting", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	var statement string
	if setting.ProfileID == nil {
		statement = `INSERT INTO settings(profile_id, key, value, updated_at) VALUES (NULL, ?, ?, ?)
			ON CONFLICT(key) WHERE profile_id IS NULL DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`
		_, err = db.ExecContext(ctx, statement, setting.Key, string(setting.Value), formatTime(setting.UpdatedAt))
	} else {
		statement = `INSERT INTO settings(profile_id, key, value, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(profile_id, key) WHERE profile_id IS NOT NULL DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`
		_, err = db.ExecContext(ctx, statement, setting.ProfileID.String(), setting.Key, string(setting.Value), formatTime(setting.UpdatedAt))
	}
	return storeError("set setting "+setting.Key, err)
}

func (s *Store) GetSetting(ctx context.Context, profileID *domain.ProfileID, key string) (domain.Setting, error) {
	if err := validateSettingKey(profileID, key); err != nil {
		return domain.Setting{}, invalid("get setting", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return domain.Setting{}, err
	}
	defer release()
	var row rowScanner
	if profileID == nil {
		row = db.QueryRowContext(ctx, "SELECT "+settingColumns+" FROM settings WHERE profile_id IS NULL AND key = ?", key)
	} else {
		row = db.QueryRowContext(ctx, "SELECT "+settingColumns+" FROM settings WHERE profile_id = ? AND key = ?", profileID.String(), key)
	}
	setting, err := scanSetting(row)
	if err != nil {
		return domain.Setting{}, storeError("get setting "+key, err)
	}
	return setting, nil
}

func (s *Store) DeleteSetting(ctx context.Context, profileID *domain.ProfileID, key string) error {
	if err := validateSettingKey(profileID, key); err != nil {
		return invalid("delete setting", err)
	}
	db, release, err := s.acquire()
	if err != nil {
		return err
	}
	defer release()
	if profileID == nil {
		result, err := db.ExecContext(ctx, "DELETE FROM settings WHERE profile_id IS NULL AND key = ?", key)
		if err != nil {
			return storeError("delete setting "+key, err)
		}
		return requireAffected("delete setting "+key, result)
	}
	result, err := db.ExecContext(ctx, "DELETE FROM settings WHERE profile_id = ? AND key = ?", profileID.String(), key)
	if err != nil {
		return storeError("delete setting "+key, err)
	}
	return requireAffected("delete setting "+key, result)
}

func (s *Store) ListSettings(ctx context.Context, profileID *domain.ProfileID) ([]domain.Setting, error) {
	if profileID != nil {
		if err := profileID.Validate(); err != nil {
			return nil, invalid("list settings", err)
		}
	}
	db, release, err := s.acquire()
	if err != nil {
		return nil, err
	}
	defer release()
	var rows interface {
		Next() bool
		Scan(...any) error
		Err() error
		Close() error
	}
	if profileID == nil {
		rows, err = db.QueryContext(ctx, "SELECT "+settingColumns+" FROM settings WHERE profile_id IS NULL ORDER BY key")
	} else {
		rows, err = db.QueryContext(ctx, "SELECT "+settingColumns+" FROM settings WHERE profile_id = ? ORDER BY key", profileID.String())
	}
	if err != nil {
		return nil, storeError("list settings", err)
	}
	defer rows.Close()
	var settings []domain.Setting
	for rows.Next() {
		setting, err := scanSetting(rows)
		if err != nil {
			return nil, storeError("list settings", err)
		}
		settings = append(settings, setting)
	}
	if err := rows.Err(); err != nil {
		return nil, storeError("list settings", err)
	}
	return settings, nil
}

func validateSettingKey(profileID *domain.ProfileID, key string) error {
	if profileID != nil {
		if err := profileID.Validate(); err != nil {
			return err
		}
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("setting key must not be empty")
	}
	return nil
}
