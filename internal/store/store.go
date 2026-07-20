package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const defaultBusyTimeout = 5 * time.Second

type OpenOptions struct {
	BusyTimeout time.Duration
}

type SchemaInfo struct {
	Version    int
	Migrated   bool
	BackupPath string
}

type Store struct {
	mu     sync.RWMutex
	db     *sql.DB
	path   string
	info   SchemaInfo
	closed bool
}

func Open(ctx context.Context, path string, opts OpenOptions) (_ *Store, finalErr error) {
	if path == "" {
		return nil, invalid("open database", errors.New("path must not be empty"))
	}
	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, invalid("open database", errors.New("invalid path"))
	}
	parent := filepath.Dir(cleanPath)
	if err := secureDirectory(parent); err != nil {
		return nil, err
	}

	newFile, err := secureDatabaseFile(cleanPath)
	if err != nil {
		return nil, err
	}
	if newFile {
		defer func() {
			if finalErr != nil {
				_ = os.Remove(cleanPath)
			}
		}()
	}

	timeout := opts.BusyTimeout
	if timeout == 0 {
		timeout = defaultBusyTimeout
	}
	if timeout < 0 {
		return nil, invalid("open database", errors.New("busy timeout must not be negative"))
	}

	db, err := sql.Open("sqlite", sqliteDSN(cleanPath, timeout))
	if err != nil {
		return nil, fmt.Errorf("open database connection: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	defer func() {
		if finalErr != nil {
			_ = db.Close()
		}
	}()

	if err := db.PingContext(ctx); err != nil {
		return nil, storeError("connect database", err)
	}
	if err := configureAndVerify(ctx, db, timeout); err != nil {
		return nil, err
	}
	if err := quickCheck(ctx, db); err != nil {
		return nil, err
	}

	info, err := migrate(ctx, db, cleanPath)
	if err != nil {
		return nil, err
	}
	if err := foreignKeyCheck(ctx, db); err != nil {
		return nil, err
	}
	if err := quickCheck(ctx, db); err != nil {
		return nil, err
	}
	if err := secureDatabaseArtifacts(cleanPath); err != nil {
		return nil, err
	}

	return &Store{db: db, path: cleanPath, info: info}, nil
}

func (s *Store) SchemaInfo() SchemaInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

func (s *Store) acquire() (*sql.DB, func(), error) {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return nil, nil, ErrClosed
	}
	return s.db, s.mu.RUnlock, nil
}

func sqliteDSN(path string, timeout time.Duration) string {
	u := &url.URL{Scheme: "file", Path: path}
	query := u.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout("+strconv.FormatInt(timeout.Milliseconds(), 10)+")")
	query.Add("_pragma", "journal_mode(DELETE)")
	query.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = query.Encode()
	return u.String()
}

func secureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure database directory: %w: %v", ErrPermission, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect database directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("inspect database directory: %w", ErrPermission)
	}
	return nil
}

func secureDatabaseFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect database file: %w", err)
		}
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if createErr != nil {
			return false, fmt.Errorf("create database file: %w", createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return false, fmt.Errorf("close database file: %w", closeErr)
		}
		return true, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("inspect database file: %w: symbolic links are not allowed", ErrPermission)
	}
	if !info.Mode().IsRegular() {
		return false, invalid("open database", errors.New("path is not a regular file"))
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return false, fmt.Errorf("secure database file: %w: %v", ErrPermission, err)
	}
	return false, nil
}

func secureDatabaseArtifacts(path string) error {
	for _, candidate := range []string{path, path + "-journal", path + "-wal", path + "-shm"} {
		info, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect database artifact: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("inspect database artifact: %w", ErrPermission)
		}
		if err := os.Chmod(candidate, 0o600); err != nil {
			return fmt.Errorf("secure database artifact: %w: %v", ErrPermission, err)
		}
		updated, err := os.Stat(candidate)
		if err != nil || updated.Mode().Perm() != 0o600 {
			return fmt.Errorf("verify database artifact: %w", ErrPermission)
		}
	}
	return nil
}

func configureAndVerify(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	statements := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = " + strconv.FormatInt(timeout.Milliseconds(), 10),
		"PRAGMA journal_mode = DELETE",
		"PRAGMA synchronous = FULL",
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return storeError("configure database", err)
		}
	}

	var foreignKeys, busyTimeout, synchronous int
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return storeError("verify foreign keys", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return storeError("verify busy timeout", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return storeError("verify journal mode", err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		return storeError("verify synchronous mode", err)
	}
	if foreignKeys != 1 || busyTimeout != int(timeout.Milliseconds()) || journalMode != "delete" || synchronous != 2 {
		return fmt.Errorf("verify database settings: %w", ErrInvalid)
	}
	return nil
}

func quickCheck(ctx context.Context, db *sql.DB) error {
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return storeError("check database integrity", err)
	}
	if result != "ok" {
		return fmt.Errorf("check database integrity: %w", ErrCorrupt)
	}
	return nil
}

func foreignKeyCheck(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return storeError("check database foreign keys", err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("check database foreign keys: %w", ErrCorrupt)
	}
	if err := rows.Err(); err != nil {
		return storeError("check database foreign keys", err)
	}
	return nil
}
