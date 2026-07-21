package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

var (
	ErrConflict    = errors.New("legacy migration conflict")
	ErrValidation  = errors.New("legacy configuration validation failed")
	ErrUnsafePath  = errors.New("legacy path is unsafe")
	ErrNotFound    = errors.New("legacy restore point not found")
	ErrAlreadyDone = errors.New("legacy restore point already rolled back")
)

type FileAction string

const (
	FileActionRead     FileAction = "read"
	FileActionPreserve FileAction = "preserve"
	FileActionCreate   FileAction = "create"
	FileActionAbsent   FileAction = "absent"
)

type FileObservation struct {
	RelativePath string     `json:"relativePath"`
	Action       FileAction `json:"action"`
	Exists       bool       `json:"exists"`
	Mode         uint32     `json:"mode,omitempty"`
	Size         int64      `json:"size,omitempty"`
	SHA256       string     `json:"sha256,omitempty"`
	Sensitive    bool       `json:"sensitive,omitempty"`
	Risk         string     `json:"risk,omitempty"`
}

type Plan struct {
	ConfigDir string            `json:"configDir"`
	CorePath  string            `json:"corePath"`
	APIAddr   string            `json:"apiAddr"`
	Entries   []FileObservation `json:"entries"`
	Warnings  []string          `json:"warnings,omitempty"`
}

type Service struct {
	paths      config.Paths
	manager    config.ManagerPaths
	repository Repository
	clock      func() time.Time
	newID      func(string) string
	opMu       sync.Mutex
	modeWriter func(string, []byte, os.FileMode) error
}

type Repository interface {
	CreateLegacyMigration(ctx context.Context, migration domain.LegacyMigration, profile domain.Profile, operation domain.Operation) error
	GetLegacyMigration(ctx context.Context, id domain.RestorePointID) (domain.LegacyMigration, error)
	ListLegacyMigrations(ctx context.Context) ([]domain.LegacyMigration, error)
	UpdateLegacyMigration(ctx context.Context, migration domain.LegacyMigration, operation domain.Operation, activate bool) (bool, error)
	RollbackLegacyMigration(ctx context.Context, migration domain.LegacyMigration, operation domain.Operation) error
	UpdateLegacyFileExpectations(ctx context.Context, migration domain.LegacyMigration) error
}

func (s *Service) pathsOrDefault() config.Paths {
	if s.paths.ConfigDir == "" {
		return config.Load()
	}
	return s.paths
}

func NewService(paths config.Paths, manager config.ManagerPaths, repository Repository) *Service {
	return &Service{
		paths: paths, manager: manager, repository: repository,
		clock:      func() time.Time { return time.Now().UTC() },
		newID:      func(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) },
		modeWriter: writeAtomic,
	}
}

func (s *Service) Discover() (Plan, []domain.LegacyFileSnapshot, error) {
	paths := s.pathsOrDefault()
	if !filepath.IsAbs(paths.ConfigDir) || !filepath.IsAbs(paths.ConfigFile) {
		return Plan{}, nil, fmt.Errorf("%w: CONFIG_DIR must be absolute", ErrUnsafePath)
	}
	entries, err := discoverFiles(paths.ConfigDir)
	if err != nil {
		return Plan{}, nil, err
	}
	return Plan{ConfigDir: paths.ConfigDir, CorePath: paths.MihomoBin, APIAddr: paths.APIAddr, Entries: entries, Warnings: []string{"legacy 源文件只会在兼容写操作中按原语义备份后写回"}}, snapshotsFromObservations(paths.ConfigDir, s.manager, entries), nil
}

func discoverFiles(configDir string) ([]FileObservation, error) {
	allowed := []string{"config.yaml", "config.yaml.bak", "subscription.url", "whitelist.yaml", "mihomo.log", "node_speed.txt", "fastest_node.txt"}
	if info, err := os.Lstat(configDir); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: CONFIG_DIR must not be a symbolic link", ErrUnsafePath)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: CONFIG_DIR is not a directory", ErrUnsafePath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect CONFIG_DIR: %w", err)
	}
	if entries, err := os.ReadDir(configDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, "config.yaml.") && strings.HasSuffix(name, ".bak") {
				allowed = append(allowed, name)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect legacy directory: %w", err)
	}
	sort.Strings(allowed)
	observations := make([]FileObservation, 0, len(allowed))
	for _, relative := range allowed {
		path, err := safeJoin(configDir, relative)
		if err != nil {
			return nil, err
		}
		observation := FileObservation{RelativePath: relative, Action: FileActionAbsent, Sensitive: relative == "subscription.url" || relative == "config.yaml"}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			observations = append(observations, observation)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect legacy file %s: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s is not a regular file", ErrUnsafePath, relative)
		}
		if info.Mode().Perm()&0o002 != 0 {
			observation.Risk = "world-writable"
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return nil, fmt.Errorf("digest legacy file %s: %w", relative, err)
		}
		observation.Action = FileActionPreserve
		observation.Exists = true
		observation.Mode = uint32(info.Mode().Perm())
		observation.Size = info.Size()
		observation.SHA256 = sum
		observations = append(observations, observation)
	}
	return observations, nil
}

func snapshotsFromObservations(configDir string, manager config.ManagerPaths, observations []FileObservation) []domain.LegacyFileSnapshot {
	files := make([]domain.LegacyFileSnapshot, 0, len(observations))
	for _, item := range observations {
		if !isRestorable(item.RelativePath) {
			continue
		}
		file := domain.LegacyFileSnapshot{RelativePath: item.RelativePath, BeforeExists: item.Exists, BeforeMode: item.Mode, BeforeSize: item.Size, BeforeSHA256: item.SHA256, ExpectedExists: item.Exists, ExpectedSHA256: item.SHA256}
		if item.Exists {
			file.SnapshotPath = filepath.Join(manager.BackupsDir, "pending", "files", item.RelativePath)
		}
		files = append(files, file)
	}
	return files
}

func isRestorable(relative string) bool {
	switch relative {
	case "config.yaml", "config.yaml.bak", "subscription.url", "whitelist.yaml":
		return true
	default:
		return strings.HasPrefix(relative, "config.yaml.") && strings.HasSuffix(relative, ".bak")
	}
}

func safeJoin(base, relative string) (string, error) {
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: relative path %q", ErrUnsafePath, relative)
	}
	cleanBase := filepath.Clean(base)
	joined := filepath.Join(cleanBase, relative)
	if filepath.Dir(joined) != cleanBase && !strings.HasPrefix(joined, cleanBase+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes CONFIG_DIR", ErrUnsafePath)
	}
	return joined, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
