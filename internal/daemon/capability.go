package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
)

const legacySubscriptionID domain.SubscriptionID = "legacy-subscription"

type CapabilityStore interface {
	CoreRepository
	GetProfile(context.Context, domain.ProfileID) (domain.Profile, error)
	ListProfiles(context.Context) ([]domain.Profile, error)
	ListLegacyMigrations(context.Context) ([]domain.LegacyMigration, error)
}

type GroupInfo struct {
	ID           domain.GroupID `json:"id"`
	Name         string         `json:"name"`
	Type         string         `json:"type"`
	SelectedNode string         `json:"selectedNode"`
	Nodes        []string       `json:"nodes"`
}

type NodeInfo struct {
	ID       domain.NodeID  `json:"id"`
	Name     string         `json:"name"`
	Protocol string         `json:"protocol,omitempty"`
	GroupID  domain.GroupID `json:"groupId,omitempty"`
}

type SubscriptionInfo struct {
	ID      domain.SubscriptionID `json:"id"`
	Name    string                `json:"name"`
	URL     string                `json:"url"`
	Enabled bool                  `json:"enabled"`
}

type RouteInfo struct {
	Input       string `json:"input"`
	Host        string `json:"host"`
	MatchedRule string `json:"matchedRule"`
	Target      string `json:"target"`
	CurrentNode string `json:"currentNode"`
	Confidence  string `json:"confidence"`
	Note        string `json:"note,omitempty"`
}

type ConfigDocument struct {
	Content []byte `json:"content"`
	SHA256  string `json:"sha256"`
}

type CapabilityService struct {
	store  CapabilityStore
	core   *CoreManager
	legacy *legacy.Compatibility
	paths  config.Paths
}

func NewCapabilityService(store CapabilityStore, manager *CoreManager, compatibility *legacy.Compatibility, paths config.Paths) *CapabilityService {
	return &CapabilityService{store: store, core: manager, legacy: compatibility, paths: paths}
}

func (s *CapabilityService) profile(ctx context.Context, id string) (domain.Profile, domain.RestorePointID, error) {
	if s == nil || s.store == nil || s.legacy == nil {
		return domain.Profile{}, "", errors.New("capability service is not configured")
	}
	profileID := domain.ProfileID(strings.TrimSpace(id))
	if profileID == "" {
		active, ok, err := s.store.ActiveProfileID(ctx)
		if err != nil {
			return domain.Profile{}, "", err
		}
		if !ok {
			return domain.Profile{}, "", fmt.Errorf("active profile: %w", ErrCapabilityNotFound)
		}
		profileID = active
	}
	if err := profileID.Validate(); err != nil {
		return domain.Profile{}, "", err
	}
	profile, err := s.store.GetProfile(ctx, profileID)
	if err != nil {
		return domain.Profile{}, "", err
	}
	if profile.Mode != domain.ProfileModeLegacy {
		return domain.Profile{}, "", fmt.Errorf("profile %s: %w", profile.ID, ErrCapabilityUnsupported)
	}
	migrations, err := s.store.ListLegacyMigrations(ctx)
	if err != nil {
		return domain.Profile{}, "", err
	}
	for _, migration := range migrations {
		if migration.ProfileID == profile.ID && migration.State == domain.LegacyMigrationStateSucceeded {
			return profile, migration.ID, nil
		}
	}
	return domain.Profile{}, "", fmt.Errorf("profile %s migration: %w", profile.ID, ErrCapabilityNotFound)
}

var (
	ErrCapabilityNotFound    = errors.New("capability resource not found")
	ErrCapabilityUnsupported = errors.New("capability is unsupported for this profile")
)

func (s *CapabilityService) CoreStatus(context.Context, string) (CoreStatus, error) {
	if s == nil || s.core == nil {
		return CoreStatus{}, errors.New("core service is not configured")
	}
	return s.core.Status(), nil
}

func (s *CapabilityService) CoreAction(ctx context.Context, profileID, action string) error {
	profile, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	if s.core == nil {
		return errors.New("core service is not configured")
	}
	snapshot := core.ProfileSnapshot{ProfileID: profile.ID, Revision: profile.Revision, Mode: profile.Mode, ExternalConfigPath: profile.ConfigPath, ControllerEndpoint: s.paths.APIAddr}
	switch action {
	case "start":
		status := s.core.Status()
		if status.State == domain.CoreStateRunning && status.ProfileID == profile.ID {
			return nil
		}
		return s.core.Activate(ctx, snapshot)
	case "stop":
		return s.core.Stop(ctx)
	case "restart":
		return s.core.Activate(ctx, snapshot)
	case "reload":
		return s.legacy.Reload(ctx, restorePoint)
	default:
		return fmt.Errorf("unknown core action %q", action)
	}
}

func (s *CapabilityService) ValidateConfig(ctx context.Context, profileID string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.legacy.Validate(ctx, restorePoint)
}

func (s *CapabilityService) Groups(ctx context.Context, profileID string) ([]GroupInfo, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	groups, err := s.legacy.ListGroups(ctx, restorePoint)
	if err != nil {
		return nil, err
	}
	result := make([]GroupInfo, 0, len(groups))
	for _, group := range groups {
		result = append(result, GroupInfo{ID: domain.GroupID(group.Name), Name: group.Name, Type: group.Type, SelectedNode: group.Now, Nodes: group.All})
	}
	return result, nil
}

func (s *CapabilityService) Group(ctx context.Context, profileID, groupID string) (GroupInfo, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return GroupInfo{}, err
	}
	nodes, selected, err := s.legacy.GroupNodes(ctx, restorePoint, groupID)
	if err != nil {
		return GroupInfo{}, err
	}
	return GroupInfo{ID: domain.GroupID(groupID), Name: groupID, SelectedNode: selected, Nodes: nodes}, nil
}

func (s *CapabilityService) SelectGroupNode(ctx context.Context, profileID, groupID, nodeID string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.legacy.SelectNode(ctx, restorePoint, groupID, nodeID)
}

func (s *CapabilityService) Nodes(ctx context.Context, profileID, groupID string) ([]NodeInfo, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	var names []string
	if strings.TrimSpace(groupID) == "" {
		names, err = s.legacy.GlobalNodes(ctx, restorePoint)
	} else {
		names, _, err = s.legacy.GroupNodes(ctx, restorePoint, groupID)
	}
	if err != nil {
		return nil, err
	}
	result := make([]NodeInfo, 0, len(names))
	for _, name := range names {
		result = append(result, NodeInfo{ID: domain.NodeID(name), Name: name, GroupID: domain.GroupID(groupID)})
	}
	return result, nil
}

func (s *CapabilityService) TestNodes(ctx context.Context, profileID, groupID string, concurrency, limit int) (<-chan mihomo.NodeTestEvent, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return s.legacy.TestNodes(ctx, restorePoint, groupID, concurrency, limit)
}

func (s *CapabilityService) Subscription(ctx context.Context, profileID string) (SubscriptionInfo, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return SubscriptionInfo{}, err
	}
	urlValue, err := s.legacy.ReadSubscriptionURL(ctx, restorePoint)
	if err != nil {
		return SubscriptionInfo{}, err
	}
	return SubscriptionInfo{ID: legacySubscriptionID, Name: "legacy subscription", URL: urlValue, Enabled: true}, nil
}

func (s *CapabilityService) SetSubscription(ctx context.Context, profileID, value string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.legacy.SaveSubscriptionURL(ctx, restorePoint, value)
}

func (s *CapabilityService) UpdateSubscription(ctx context.Context, profileID string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	if err := s.legacy.UpdateSubscription(ctx, restorePoint); err != nil {
		return err
	}
	if s.core != nil && s.core.Status().State == domain.CoreStateRunning {
		return s.legacy.Reload(ctx, restorePoint)
	}
	return nil
}

func (s *CapabilityService) Whitelist(ctx context.Context, profileID string) ([]string, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return s.legacy.ListWhitelist(ctx, restorePoint)
}

func (s *CapabilityService) AddWhitelist(ctx context.Context, profileID, value string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.legacy.AddWhitelist(ctx, restorePoint, value)
}

func (s *CapabilityService) RemoveWhitelist(ctx context.Context, profileID, value string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.legacy.RemoveWhitelist(ctx, restorePoint, value)
}

func (s *CapabilityService) EditWhitelist(ctx context.Context, profileID, oldValue, newValue string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.legacy.EditWhitelist(ctx, restorePoint, oldValue, newValue)
}

func (s *CapabilityService) ApplyRoutePreset(ctx context.Context, profileID, preset string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	if preset != "cn" && preset != "CN" {
		return fmt.Errorf("unknown route preset %q", preset)
	}
	return s.legacy.ApplyRouteCN(ctx, restorePoint)
}

func (s *CapabilityService) DiagnoseRoute(ctx context.Context, profileID, input string) (RouteInfo, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return RouteInfo{}, err
	}
	value, err := s.legacy.DiagnoseRoute(ctx, restorePoint, input)
	if err != nil {
		return RouteInfo{}, err
	}
	return RouteInfo{Input: value.Input, Host: value.Host, MatchedRule: value.MatchedRule, Target: value.Target, CurrentNode: value.CurrentNode, Confidence: value.Confidence, Note: value.Note}, nil
}

func (s *CapabilityService) ConfigBackup(ctx context.Context, profileID string) error {
	return s.configMutation(ctx, profileID, s.legacy.BackupConfig)
}
func (s *CapabilityService) ConfigRestore(ctx context.Context, profileID string) error {
	return s.configMutation(ctx, profileID, s.legacy.RestoreConfig)
}

func (s *CapabilityService) ReadConfig(ctx context.Context, profileID string) (ConfigDocument, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return ConfigDocument{}, err
	}
	content, digest, err := s.legacy.ReadConfig(ctx, restorePoint)
	if err != nil {
		return ConfigDocument{}, err
	}
	return ConfigDocument{Content: content, SHA256: digest}, nil
}

func (s *CapabilityService) ReplaceConfig(ctx context.Context, profileID, expectedSHA256 string, content []byte) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.legacy.ReplaceConfig(ctx, restorePoint, expectedSHA256, content)
}

func (s *CapabilityService) configMutation(ctx context.Context, profileID string, action func(context.Context, domain.RestorePointID) error) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return action(ctx, restorePoint)
}

func (s *CapabilityService) TailLogs(ctx context.Context, profileID string, lines int) (string, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return "", err
	}
	return s.legacy.TailLogs(ctx, restorePoint, lines)
}

func (s *CapabilityService) FollowLogs(ctx context.Context, profileID string, lines int) (<-chan mihomo.LogEvent, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return s.legacy.FollowLogs(ctx, restorePoint, lines)
}
