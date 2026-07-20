package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"
)

var (
	ErrInvalid        = errors.New("invalid store input")
	ErrNotFound       = errors.New("store record not found")
	ErrConflict       = errors.New("store conflict")
	ErrClosed         = errors.New("store is closed")
	ErrSchemaTooNew   = errors.New("database schema is newer than this program")
	ErrMigrationDrift = errors.New("database migration history differs from this program")
	ErrCorrupt        = errors.New("database is corrupt")
	ErrPermission     = errors.New("database permissions are unsafe")
)

func invalid(action string, err error) error {
	return fmt.Errorf("%s: %w: %v", action, ErrInvalid, err)
}

func storeError(action string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", action, ErrNotFound)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", action, err)
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() & 0xff {
		case 5, 6, 19:
			return fmt.Errorf("%s: %w", action, ErrConflict)
		case 11, 26:
			return fmt.Errorf("%s: %w", action, ErrCorrupt)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
