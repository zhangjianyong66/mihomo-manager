package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"modernc.org/sqlite"
)

type onlineBackuper interface {
	NewBackup(string) (*sqlite.Backup, error)
}

func createBackup(ctx context.Context, db *sql.DB, databasePath string, version int) (_ string, finalErr error) {
	directory := filepath.Dir(databasePath)
	temp, err := os.CreateTemp(directory, ".state-backup-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create migration backup: %w", err)
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close migration backup placeholder: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return "", fmt.Errorf("prepare migration backup: %w", err)
	}
	defer func() {
		if finalErr != nil {
			_ = os.Remove(tempPath)
		}
	}()

	conn, err := db.Conn(ctx)
	if err != nil {
		return "", storeError("acquire migration backup connection", err)
	}
	defer conn.Close()
	err = conn.Raw(func(driverConn any) error {
		backuper, ok := driverConn.(onlineBackuper)
		if !ok {
			return fmt.Errorf("SQLite driver does not support online backup")
		}
		backup, err := backuper.NewBackup(tempPath)
		if err != nil {
			return err
		}
		for more := true; more; {
			more, err = backup.Step(-1)
			if err != nil {
				_ = backup.Finish()
				return err
			}
		}
		return backup.Finish()
	})
	if err != nil {
		return "", storeError("create migration backup", err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return "", fmt.Errorf("secure migration backup: %w: %v", ErrPermission, err)
	}
	if err := verifyBackup(ctx, tempPath); err != nil {
		return "", err
	}
	file, err := os.OpenFile(tempPath, os.O_RDONLY, 0)
	if err != nil {
		return "", fmt.Errorf("open migration backup for sync: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", fmt.Errorf("sync migration backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close migration backup: %w", err)
	}

	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	finalPath := fmt.Sprintf("%s.pre-v%d-%s.bak", databasePath, version, stamp)
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", fmt.Errorf("publish migration backup: %w", err)
	}
	if err := syncDirectory(directory); err != nil {
		return "", err
	}
	return finalPath, nil
}

func verifyBackup(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", sqliteDSN(path, defaultBusyTimeout))
	if err != nil {
		return fmt.Errorf("open migration backup: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		return storeError("connect migration backup", err)
	}
	if err := quickCheck(ctx, db); err != nil {
		return fmt.Errorf("verify migration backup: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open database directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync database directory: %w", err)
	}
	return nil
}
