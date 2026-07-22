package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
)

type Compatibility struct{ service *Service }

func (s *Service) Compatibility() *Compatibility { return &Compatibility{service: s} }

func (c *Compatibility) Validate(ctx context.Context, id domain.RestorePointID) error {
	migration, err := c.load(ctx, id)
	if err != nil {
		return err
	}
	if err := checkExpected(migration); err != nil {
		return err
	}
	paths := c.service.pathsOrDefault()
	return validateConfig(ctx, paths.ConfigDir, paths.ConfigFile, paths.MihomoBin)
}

func (c *Compatibility) ListenerPorts(ctx context.Context, id domain.RestorePointID) ([]mihomo.ListenerPort, error) {
	var result []mihomo.ListenerPort
	paths := c.service.pathsOrDefault()
	err := c.inspect(ctx, id, func(*mihomo.Client) error {
		var err error
		result, err = mihomo.ReadListenerPorts(paths.ConfigFile)
		return err
	})
	return result, err
}

func (c *Compatibility) ReadSubscriptionURL(ctx context.Context, id domain.RestorePointID) (string, error) {
	migration, err := c.load(ctx, id)
	if err != nil {
		return "", err
	}
	if err := checkExpected(migration); err != nil {
		return "", err
	}
	return mihomo.New(c.service.pathsOrDefault()).ReadSubscriptionURL()
}

func (c *Compatibility) SaveSubscriptionURL(ctx context.Context, id domain.RestorePointID, value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("subscription URL must not be empty")
	}
	return c.mutate(ctx, id, false, func(client *mihomo.Client) error { return client.SaveSubscriptionURL(value) })
}

func (c *Compatibility) UpdateSubscription(ctx context.Context, id domain.RestorePointID) error {
	_, err := c.UpdateSubscriptionWithResult(ctx, id)
	return err
}

func (c *Compatibility) UpdateSubscriptionWithResult(ctx context.Context, id domain.RestorePointID) (mihomo.SubscriptionUpdateResult, error) {
	var result mihomo.SubscriptionUpdateResult
	err := c.mutate(ctx, id, true, func(client *mihomo.Client) error {
		var err error
		result, err = client.UpdateSubscriptionWithResult()
		return err
	})
	return result, err
}

func (c *Compatibility) ListWhitelist(ctx context.Context, id domain.RestorePointID) ([]string, error) {
	var result []string
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		var err error
		result, err = client.ListWhitelist()
		return err
	})
	return result, err
}

func (c *Compatibility) AddWhitelist(ctx context.Context, id domain.RestorePointID, value string) error {
	return c.mutate(ctx, id, true, func(client *mihomo.Client) error { return client.AddWhitelist(value) })
}

func (c *Compatibility) RemoveWhitelist(ctx context.Context, id domain.RestorePointID, value string) error {
	return c.mutate(ctx, id, true, func(client *mihomo.Client) error { return client.RemoveWhitelist(value) })
}

func (c *Compatibility) EditWhitelist(ctx context.Context, id domain.RestorePointID, oldValue, newValue string) error {
	if strings.TrimSpace(oldValue) == "" || strings.TrimSpace(newValue) == "" {
		return errors.New("whitelist domains must not be empty")
	}
	return c.mutate(ctx, id, true, func(client *mihomo.Client) error {
		if err := client.RemoveWhitelist(oldValue); err != nil {
			return err
		}
		return client.AddWhitelist(newValue)
	})
}

func (c *Compatibility) BackupConfig(ctx context.Context, id domain.RestorePointID) error {
	return c.mutate(ctx, id, false, func(client *mihomo.Client) error { return client.BackupConfig() })
}

func (c *Compatibility) RestoreConfig(ctx context.Context, id domain.RestorePointID) error {
	return c.mutate(ctx, id, true, func(client *mihomo.Client) error { return client.RestoreConfig() })
}

func (c *Compatibility) ReadConfig(ctx context.Context, id domain.RestorePointID) ([]byte, string, error) {
	var content []byte
	err := c.inspect(ctx, id, func(*mihomo.Client) error {
		var err error
		content, err = os.ReadFile(c.service.pathsOrDefault().ConfigFile)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(content)
	return content, hex.EncodeToString(sum[:]), nil
}

func (c *Compatibility) ReplaceConfig(ctx context.Context, id domain.RestorePointID, expectedSHA256 string, content []byte) error {
	if len(expectedSHA256) != sha256.Size*2 || len(content) == 0 {
		return errors.New("config content or expected digest is invalid")
	}
	return c.mutate(ctx, id, true, func(*mihomo.Client) error {
		paths := c.service.pathsOrDefault()
		current, err := fileSHA256(paths.ConfigFile)
		if err != nil {
			return err
		}
		if !strings.EqualFold(current, expectedSHA256) {
			return fmt.Errorf("%w: config changed while editing", ErrConflict)
		}
		return writeAtomic(paths.ConfigFile, content, 0o600)
	})
}

func (c *Compatibility) Reload(ctx context.Context, id domain.RestorePointID) error {
	return c.inspect(ctx, id, func(client *mihomo.Client) error { return client.Reload() })
}

func (c *Compatibility) ListGroups(ctx context.Context, id domain.RestorePointID) ([]mihomo.ProxyGroup, error) {
	var result []mihomo.ProxyGroup
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		var err error
		result, err = client.ListSelectableGroups()
		return err
	})
	return result, err
}

func (c *Compatibility) GroupNodes(ctx context.Context, id domain.RestorePointID, group string) ([]string, string, error) {
	var nodes []string
	var selected string
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		var err error
		nodes, selected, err = client.GroupNodes(group)
		return err
	})
	return nodes, selected, err
}

func (c *Compatibility) GroupDetail(ctx context.Context, id domain.RestorePointID, group string) (mihomo.ProxyGroup, error) {
	var result mihomo.ProxyGroup
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		var err error
		result, err = client.GroupDetail(group)
		return err
	})
	return result, err
}

func (c *Compatibility) GlobalNodes(ctx context.Context, id domain.RestorePointID) ([]string, error) {
	var nodes []string
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		var err error
		nodes, err = client.GlobalNodes()
		return err
	})
	return nodes, err
}

func (c *Compatibility) SelectNode(ctx context.Context, id domain.RestorePointID, group, node string) error {
	if strings.TrimSpace(group) == "" || strings.TrimSpace(node) == "" {
		return errors.New("group and node must not be empty")
	}
	return c.inspect(ctx, id, func(client *mihomo.Client) error { return client.SwitchNodeInGroup(group, node) })
}

func (c *Compatibility) TestNodes(ctx context.Context, id domain.RestorePointID, group string, concurrency, limit int) (<-chan mihomo.NodeTestEvent, error) {
	var stream <-chan mihomo.NodeTestEvent
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		if strings.TrimSpace(group) == "" {
			stream = client.TestNodesStreamWithStop(concurrency, limit, ctx.Done())
		} else {
			stream = client.TestGroupNodesStreamWithStop(group, concurrency, limit, ctx.Done())
		}
		return nil
	})
	return stream, err
}

func (c *Compatibility) TestNode(ctx context.Context, id domain.RestorePointID, group, node string) (<-chan mihomo.NodeTestEvent, error) {
	var stream <-chan mihomo.NodeTestEvent
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		stream = client.TestNodeStreamWithStop(group, node, ctx.Done())
		return nil
	})
	return stream, err
}

func (c *Compatibility) DiagnoseRoute(ctx context.Context, id domain.RestorePointID, input string) (mihomo.RouteDiagnosisResult, error) {
	var result mihomo.RouteDiagnosisResult
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		var err error
		result, err = client.DiagnoseRoute(input)
		return err
	})
	return result, err
}

func (c *Compatibility) ApplyRouteCN(ctx context.Context, id domain.RestorePointID) error {
	return c.mutate(ctx, id, true, func(client *mihomo.Client) error { return client.ApplyRouteCN() })
}

func (c *Compatibility) TailLogs(ctx context.Context, id domain.RestorePointID, lines int) (string, error) {
	var result string
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		var err error
		result, err = client.TailLogs(lines)
		return err
	})
	return result, err
}

func (c *Compatibility) FollowLogs(ctx context.Context, id domain.RestorePointID, lines int) (<-chan mihomo.LogEvent, error) {
	var stream <-chan mihomo.LogEvent
	err := c.inspect(ctx, id, func(client *mihomo.Client) error {
		stream = client.TailLogsStreamWithStop(lines, ctx.Done())
		return nil
	})
	return stream, err
}

func (c *Compatibility) inspect(ctx context.Context, id domain.RestorePointID, action func(*mihomo.Client) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	migration, err := c.load(ctx, id)
	if err != nil {
		return err
	}
	if err := checkExpected(migration); err != nil {
		return err
	}
	return action(compatibilityClient(c.service.pathsOrDefault()))
}

func (c *Compatibility) load(ctx context.Context, id domain.RestorePointID) (domain.LegacyMigration, error) {
	if c == nil || c.service == nil || c.service.repository == nil {
		return domain.LegacyMigration{}, errors.New("legacy compatibility service is not configured")
	}
	migration, err := c.service.repository.GetLegacyMigration(ctx, id)
	if err != nil {
		return domain.LegacyMigration{}, err
	}
	if migration.State != domain.LegacyMigrationStateSucceeded {
		return domain.LegacyMigration{}, fmt.Errorf("%w: migration state is %s", ErrConflict, migration.State)
	}
	return migration, nil
}

func (c *Compatibility) mutate(ctx context.Context, id domain.RestorePointID, validate bool, action func(*mihomo.Client) error) error {
	c.service.opMu.Lock()
	defer c.service.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	migration, err := c.load(ctx, id)
	if err != nil {
		return err
	}
	if err := checkExpected(migration); err != nil {
		return err
	}
	paths := c.service.pathsOrDefault()
	backupRelative := ""
	if _, err := os.Stat(paths.ConfigFile); err == nil {
		backupRelative = "config.yaml." + c.service.makeID("backup") + ".bak"
	}
	relatives := mutationRelatives(migration, backupRelative)
	before, err := captureSource(paths.ConfigDir, relatives)
	if err != nil {
		return err
	}
	if backupRelative != "" {
		backupPath, _ := safeJoin(paths.ConfigDir, backupRelative)
		if err := copyFile(paths.ConfigFile, backupPath, 0o600); err != nil {
			return err
		}
	}
	client := compatibilityClient(paths)
	if err := action(client); err != nil {
		_ = restoreSource(paths.ConfigDir, before)
		return err
	}
	if err := secureMutationFiles(paths.ConfigDir, relatives); err != nil {
		_ = restoreSource(paths.ConfigDir, before)
		return err
	}
	if validate {
		if err := validateConfig(ctx, paths.ConfigDir, paths.ConfigFile, paths.MihomoBin); err != nil {
			_ = restoreSource(paths.ConfigDir, before)
			return fmt.Errorf("%w: %v", ErrValidation, err)
		}
	}
	if err := refreshExpected(&migration); err != nil {
		_ = restoreSource(paths.ConfigDir, before)
		return err
	}
	if err := c.service.repository.UpdateLegacyFileExpectations(ctx, migration); err != nil {
		_ = restoreSource(paths.ConfigDir, before)
		return err
	}
	return nil
}

func compatibilityClient(paths config.Paths) *mihomo.Client {
	if endpoint, err := mihomo.ReadControllerEndpoint(paths.ConfigFile); err == nil {
		paths.APIAddr = endpoint
	}
	return mihomo.New(paths)
}

func checkExpected(migration domain.LegacyMigration) error {
	tracked := make(map[string]domain.LegacyFileSnapshot, len(migration.Files))
	for _, file := range migration.Files {
		tracked[file.RelativePath] = file
		path, err := safeJoin(migration.SourceDir, file.RelativePath)
		if err != nil {
			return err
		}
		exists, digest, err := currentDigest(path)
		if err != nil {
			return err
		}
		if exists != file.ExpectedExists || (exists && digest != file.ExpectedSHA256) {
			return fmt.Errorf("%w: %s was modified outside manager", ErrConflict, file.RelativePath)
		}
	}
	observations, err := discoverFiles(migration.SourceDir)
	if err != nil {
		return err
	}
	for _, item := range observations {
		if item.Exists && isRestorable(item.RelativePath) {
			if _, ok := tracked[item.RelativePath]; !ok {
				return fmt.Errorf("%w: untracked legacy file %s", ErrConflict, item.RelativePath)
			}
		}
	}
	return nil
}

type sourceState struct {
	exists bool
	mode   os.FileMode
	data   []byte
}

func mutationRelatives(migration domain.LegacyMigration, extra string) []string {
	set := map[string]struct{}{"config.yaml": {}, "config.yaml.bak": {}, "subscription.url": {}, "whitelist.yaml": {}}
	for _, file := range migration.Files {
		set[file.RelativePath] = struct{}{}
	}
	if extra != "" {
		set[extra] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for relative := range set {
		result = append(result, relative)
	}
	sort.Strings(result)
	return result
}

func captureSource(configDir string, relatives []string) (map[string]sourceState, error) {
	result := make(map[string]sourceState, len(relatives))
	for _, relative := range relatives {
		path, err := safeJoin(configDir, relative)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			result[relative] = sourceState{}
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s", ErrUnsafePath, relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		result[relative] = sourceState{exists: true, mode: info.Mode().Perm(), data: data}
	}
	return result, nil
}

func restoreSource(configDir string, before map[string]sourceState) error {
	var result error
	for relative, state := range before {
		path, err := safeJoin(configDir, relative)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if state.exists {
			result = errors.Join(result, writeAtomic(path, state.data, state.mode))
		} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func secureMutationFiles(configDir string, relatives []string) error {
	for _, relative := range relatives {
		path, _ := safeJoin(configDir, relative)
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return fmt.Errorf("%w: %s", ErrUnsafePath, relative)
			}
			if err := os.Chmod(path, 0o600); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func refreshExpected(migration *domain.LegacyMigration) error {
	observations, err := discoverFiles(migration.SourceDir)
	if err != nil {
		return err
	}
	prior := make(map[string]domain.LegacyFileSnapshot, len(migration.Files))
	for _, file := range migration.Files {
		prior[file.RelativePath] = file
	}
	for _, item := range observations {
		if !isRestorable(item.RelativePath) {
			continue
		}
		file, ok := prior[item.RelativePath]
		if !ok {
			file = domain.LegacyFileSnapshot{RelativePath: item.RelativePath}
		}
		file.ExpectedExists = item.Exists
		file.ExpectedSHA256 = item.SHA256
		prior[item.RelativePath] = file
	}
	migration.Files = migration.Files[:0]
	for _, file := range prior {
		migration.Files = append(migration.Files, file)
	}
	sort.Slice(migration.Files, func(i, j int) bool { return migration.Files[i].RelativePath < migration.Files[j].RelativePath })
	return migration.Validate()
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".legacy-write-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode.Perm()); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
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
	return os.Rename(tempName, path)
}
