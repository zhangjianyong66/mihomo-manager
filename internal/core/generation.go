package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

const maxConfigBytes = 16 << 20

type GenerationStoreOptions struct {
	GenerationsDir string
	RuntimeState   string
	LogPath        string
	Clock          func() time.Time
}

type GenerationStore struct {
	options GenerationStoreOptions
}

type generationMetadata struct {
	Version     int                `json:"version"`
	ProfileID   domain.ProfileID   `json:"profileId"`
	Revision    int64              `json:"revision"`
	CoreType    domain.CoreType    `json:"coreType"`
	Mode        domain.ProfileMode `json:"mode"`
	SHA256      string             `json:"sha256"`
	GeneratedAt time.Time          `json:"generatedAt"`
}

func NewGenerationStore(options GenerationStoreOptions) *GenerationStore {
	if options.Clock == nil {
		options.Clock = time.Now
	}
	return &GenerationStore{options: options}
}

func (s *GenerationStore) Prepare(ctx context.Context, snapshot ProfileSnapshot, adapter Adapter) (RuntimeSpec, error) {
	if err := snapshot.Validate(); err != nil {
		return RuntimeSpec{}, fmt.Errorf("prepare core config: %w", err)
	}
	if adapter == nil {
		return RuntimeSpec{}, errors.New("prepare core config: adapter is nil")
	}
	switch snapshot.Mode {
	case domain.ProfileModeManaged:
		return s.prepareManaged(ctx, snapshot, adapter)
	case domain.ProfileModeExternal, domain.ProfileModeLegacy:
		return s.prepareExternal(ctx, snapshot, adapter)
	default:
		return RuntimeSpec{}, ErrUnsupportedProfile
	}
}

func (s *GenerationStore) prepareManaged(ctx context.Context, snapshot ProfileSnapshot, adapter Adapter) (RuntimeSpec, error) {
	rendered, err := adapter.Render(ctx, snapshot)
	if err != nil {
		return RuntimeSpec{}, fmt.Errorf("render managed config: %w", err)
	}
	if len(rendered.Content) == 0 || rendered.SHA256 == "" || rendered.SHA256 != digestBytes(rendered.Content) {
		return RuntimeSpec{}, fmt.Errorf("render managed config: %w", ErrInvalidConfig)
	}
	profileDir := filepath.Join(s.options.GenerationsDir, profileDirectory(snapshot.ProfileID))
	if err := platform.EnsurePrivateDir(profileDir); err != nil {
		return RuntimeSpec{}, err
	}
	generationID := "r" + strconv.FormatInt(snapshot.Revision, 10) + "-" + rendered.SHA256[:16]
	finalDir := filepath.Join(profileDir, generationID)
	finalSpec := runtimeSpec(snapshot, generationID, finalDir, filepath.Join(finalDir, "config.yaml"), s.options.LogPath, rendered.ControllerEndpoint, rendered.SHA256)
	if info, statErr := os.Stat(finalDir); statErr == nil && info.IsDir() {
		if err := checkFileDigest(finalSpec.ConfigPath, finalSpec.SourceSHA256); err != nil {
			return RuntimeSpec{}, fmt.Errorf("reuse managed generation: %w", err)
		}
		if err := adapter.Validate(ctx, finalSpec); err != nil {
			return RuntimeSpec{}, err
		}
		return finalSpec, nil
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return RuntimeSpec{}, fmt.Errorf("inspect managed generation: %w", statErr)
	}

	tempDir, err := os.MkdirTemp(profileDir, ".pending-")
	if err != nil {
		return RuntimeSpec{}, fmt.Errorf("create pending generation: %w", err)
	}
	defer os.RemoveAll(tempDir)
	if err := os.Chmod(tempDir, 0o700); err != nil {
		return RuntimeSpec{}, fmt.Errorf("secure pending generation: %w", err)
	}
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := writeSyncedFile(configPath, rendered.Content); err != nil {
		return RuntimeSpec{}, err
	}
	metadata := generationMetadata{
		Version: 1, ProfileID: snapshot.ProfileID, Revision: snapshot.Revision,
		CoreType: adapter.Type(), Mode: snapshot.Mode, SHA256: rendered.SHA256,
		GeneratedAt: s.options.Clock().UTC(),
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return RuntimeSpec{}, fmt.Errorf("encode generation metadata: %w", err)
	}
	metadataBytes = append(metadataBytes, '\n')
	if err := writeSyncedFile(filepath.Join(tempDir, "metadata.json"), metadataBytes); err != nil {
		return RuntimeSpec{}, err
	}
	if err := syncDirectory(tempDir); err != nil {
		return RuntimeSpec{}, err
	}
	tempSpec := runtimeSpec(snapshot, generationID, tempDir, configPath, s.options.LogPath, rendered.ControllerEndpoint, rendered.SHA256)
	if err := adapter.Validate(ctx, tempSpec); err != nil {
		return RuntimeSpec{}, err
	}
	if err := os.Rename(tempDir, finalDir); err != nil {
		return RuntimeSpec{}, fmt.Errorf("publish managed generation: %w", err)
	}
	if err := syncDirectory(profileDir); err != nil {
		return RuntimeSpec{}, err
	}
	return finalSpec, nil
}

func (s *GenerationStore) prepareExternal(ctx context.Context, snapshot ProfileSnapshot, adapter Adapter) (RuntimeSpec, error) {
	configPath := filepath.Clean(snapshot.ExternalConfigPath)
	if !filepath.IsAbs(configPath) {
		return RuntimeSpec{}, fmt.Errorf("external config path: %w", platform.ErrUnsafePath)
	}
	digest, err := digestFile(configPath)
	if err != nil {
		return RuntimeSpec{}, fmt.Errorf("read external config: %w", err)
	}
	spec := runtimeSpec(snapshot, "external-"+digest[:16], filepath.Dir(configPath), configPath, s.options.LogPath, snapshot.ControllerEndpoint, digest)
	if err := adapter.Validate(ctx, spec); err != nil {
		return RuntimeSpec{}, err
	}
	return spec, nil
}

func (s *GenerationStore) CheckSource(spec RuntimeSpec) error {
	if err := checkFileDigest(spec.ConfigPath, spec.SourceSHA256); err != nil {
		return fmt.Errorf("check config source: %w", err)
	}
	return nil
}

func (s *GenerationStore) MarkActive(spec RuntimeSpec) error {
	profileDir := filepath.Join(s.options.GenerationsDir, profileDirectory(spec.ProfileID))
	if err := platform.EnsurePrivateDir(profileDir); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode active runtime: %w", err)
	}
	payload = append(payload, '\n')
	if err := atomicWrite(filepath.Join(profileDir, "current.json"), payload); err != nil {
		return err
	}
	if err := platform.EnsurePrivateDir(filepath.Dir(s.options.RuntimeState)); err != nil {
		return err
	}
	return atomicWrite(s.options.RuntimeState, payload)
}

func runtimeSpec(snapshot ProfileSnapshot, generationID, configDir, configPath, logPath, endpoint, digest string) RuntimeSpec {
	return RuntimeSpec{
		ProfileID: snapshot.ProfileID, Revision: snapshot.Revision, Mode: snapshot.Mode,
		GenerationID: generationID, ConfigDir: configDir, ConfigPath: configPath,
		LogPath: logPath, ControllerEndpoint: endpoint, SourceSHA256: digest,
	}
}

func profileDirectory(id domain.ProfileID) string {
	sum := sha256.Sum256([]byte(id.String()))
	return hex.EncodeToString(sum[:8])
}

func digestBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func digestFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", platform.ErrUnsafePath
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return "", err
	}
	if n > maxConfigBytes {
		return "", fmt.Errorf("config exceeds %d bytes", maxConfigBytes)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkFileDigest(path, expected string) error {
	actual, err := digestFile(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return ErrConfigChanged
	}
	return nil
}

func writeSyncedFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create private file: %w", err)
	}
	_, writeErr := io.Copy(file, bytes.NewReader(content))
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write private file: %w", err)
	}
	return nil
}

func atomicWrite(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := platform.EnsurePrivateDir(dir); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".atomic-")
	if err != nil {
		return fmt.Errorf("create atomic file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure atomic file: %w", err)
	}
	_, writeErr := temp.Write(content)
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write atomic file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace atomic file: %w", err)
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
