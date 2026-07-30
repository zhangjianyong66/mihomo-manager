package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
	"github.com/zhangjianyong66/mihomo-manager/internal/ruleset"
	"github.com/zhangjianyong66/mihomo-manager/internal/store"
	"gopkg.in/yaml.v3"
)

const legacySubscriptionID domain.SubscriptionID = "legacy-subscription"

type CapabilityStore interface {
	CoreRepository
	GetProfile(context.Context, domain.ProfileID) (domain.Profile, error)
	ListProfiles(context.Context) ([]domain.Profile, error)
	ListLegacyMigrations(context.Context) ([]domain.LegacyMigration, error)
}

type GroupInfo struct {
	ID           domain.GroupID  `json:"id"`
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	SelectedNode string          `json:"selectedNode"`
	Nodes        []string        `json:"nodes"`
	NodeStates   []NodeStateInfo `json:"nodeStates,omitempty"`
}

type NodeStateInfo struct {
	NodeID   domain.NodeID       `json:"nodeId"`
	Testable bool                `json:"testable"`
	Latest   *NodeTestResultInfo `json:"latest,omitempty"`
}

type NodeTestResultInfo struct {
	Status   mihomo.NodeTestStatus `json:"status"`
	DelayMS  int                   `json:"delayMs"`
	TestedAt time.Time             `json:"testedAt"`
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

type ListenerPortInfo struct {
	Field    string   `json:"field"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Required bool     `json:"required"`
	Enabled  bool     `json:"enabled"`
	Networks []string `json:"networks"`
}

type ListenerPortStatus struct {
	ProfileID     domain.ProfileID    `json:"profileId"`
	CoreState     domain.CoreState    `json:"coreState"`
	Restarted     bool                `json:"restarted"`
	NextStart     bool                `json:"nextStart"`
	Ports         []ListenerPortInfo  `json:"ports"`
	PortConflicts []core.PortConflict `json:"portConflicts"`
}

type RuleSetHealth struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Loaded    bool   `json:"loaded"`
}

type ModeStatus struct {
	ProfileID            domain.ProfileID         `json:"profileId"`
	ConfigMode           domain.RoutingMode       `json:"configMode"`
	RuntimeMode          *domain.RoutingMode      `json:"runtimeMode,omitempty"`
	RuntimeAvailable     bool                     `json:"runtimeAvailable"`
	CoreState            domain.CoreState         `json:"coreState"`
	EffectiveGroup       string                   `json:"effectiveGroup,omitempty"`
	EffectiveNode        string                   `json:"effectiveNode,omitempty"`
	RuleSets             []RuleSetHealth          `json:"ruleSets"`
	ActiveConnections    int                      `json:"activeConnections"`
	ConnectionsAvailable bool                     `json:"connectionsAvailable"`
	ConnectionsClosed    bool                     `json:"connectionsClosed"`
	NextStart            bool                     `json:"nextStart"`
	OperationID          domain.OperationID       `json:"operationId,omitempty"`
	OperationPhase       string                   `json:"operationPhase,omitempty"`
	Warnings             []string                 `json:"warnings"`
	Listeners            []platform.ProxyListener `json:"listeners"`
	SystemProxy          []platform.ProxySource   `json:"systemProxy"`
}

type CapabilityService struct {
	store                  CapabilityStore
	core                   *CoreManager
	legacy                 *legacy.Compatibility
	paths                  config.Paths
	coordinator            *Coordinator
	newID                  func(string) string
	clock                  func() time.Time
	connectionPollInterval time.Duration
	proxyInspector         platform.ProxyInspector
	managerPaths           config.ManagerPaths
	gnomeConfigurator      gnomeProxyConfigurator
	bashConfigurator       bashProxyConfigurator
}

func (s *CapabilityService) RuleSetStatus(ctx context.Context, _ string) (ruleset.Status, error) {
	if s == nil {
		return ruleset.Status{}, errors.New("capability service is not configured")
	}
	target := ruleset.DefaultCatalog().Target(s.paths.ConfigDir)
	return ruleset.Inspect(ctx, target, s.paths.MihomoBin)
}

func (s *CapabilityService) RuleSetStatusForTarget(ctx context.Context, requested ruleset.Target) (ruleset.Status, error) {
	target, err := s.resolveRuleSetTarget(requested)
	if err != nil {
		return ruleset.Status{}, err
	}
	return ruleset.Inspect(ctx, target, s.paths.MihomoBin)
}

func (s *CapabilityService) resolveRuleSetTarget(requested ruleset.Target) (ruleset.Target, error) {
	base := ruleset.DefaultCatalog().Target(s.paths.ConfigDir)
	if requested.Source != "" {
		base.Source = requested.Source
	}
	if requested.Ref != "" {
		base.Ref = requested.Ref
	}
	if requested.DomainSHA256 != "" {
		base.DomainSHA256 = strings.ToLower(requested.DomainSHA256)
	}
	if requested.IPSHA256 != "" {
		base.IPSHA256 = strings.ToLower(requested.IPSHA256)
	}
	if err := base.Validate(); err != nil {
		return ruleset.Target{}, err
	}
	return base, nil
}

func (s *CapabilityService) ensureRuleSetInstalled(ctx context.Context) error {
	status, err := ruleset.Inspect(ctx, ruleset.DefaultCatalog().Target(s.paths.ConfigDir), s.paths.MihomoBin)
	if err != nil {
		return err
	}
	if status.State != ruleset.StateInstalled {
		return fmt.Errorf("%w: run mm ruleset install", ruleset.ErrNotReady)
	}
	return nil
}

func (s *CapabilityService) readLegacyConfig(ctx context.Context, restorePoint domain.RestorePointID) (map[string]any, error) {
	content, _, err := s.legacy.ReadConfig(ctx, restorePoint)
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (s *CapabilityService) ensureCurrentRuleSetDependencies(ctx context.Context, restorePoint domain.RestorePointID) error {
	cfg, err := s.readLegacyConfig(ctx, restorePoint)
	if err != nil {
		return err
	}
	if !ruleset.ReferencesManagerProviders(cfg) {
		return nil
	}
	return s.ensureRuleSetInstalled(ctx)
}

func (s *CapabilityService) ensureRuleSetForRoutingMutation(ctx context.Context, restorePoint domain.RestorePointID) error {
	cfg, err := s.readLegacyConfig(ctx, restorePoint)
	if err != nil {
		return err
	}
	mode := domain.RoutingModeRule
	if value := strings.TrimSpace(fmt.Sprint(cfg["mode"])); value != "" {
		mode = domain.RoutingMode(strings.ToLower(value))
	}
	if mode != domain.RoutingModeRule && !ruleset.ReferencesManagerProviders(cfg) {
		return nil
	}
	return s.ensureRuleSetInstalled(ctx)
}

// InstallRuleSets emits bounded progress events. Downloads happen before the
// final publish, so cancellation cannot leave a partial pair on disk.
func (s *CapabilityService) InstallRuleSets(ctx context.Context, _ string) <-chan RuleSetInstallEvent {
	return s.InstallRuleSetsWithProxy(ctx, "", "")
}

func (s *CapabilityService) InstallRuleSetsWithProxy(ctx context.Context, _ string, selectedProxy string) <-chan RuleSetInstallEvent {
	return s.InstallRuleSetsForProfile(ctx, "", ruleset.Target{}, selectedProxy)
}

func (s *CapabilityService) InstallRuleSetsWithTarget(ctx context.Context, requested ruleset.Target, selectedProxy string) <-chan RuleSetInstallEvent {
	return s.InstallRuleSetsForProfile(ctx, "", requested, selectedProxy)
}

func (s *CapabilityService) InstallRuleSetsForProfile(ctx context.Context, profileID string, requested ruleset.Target, selectedProxy string) <-chan RuleSetInstallEvent {
	result := make(chan RuleSetInstallEvent, 16)
	go func() {
		defer close(result)
		target, targetErr := s.resolveRuleSetTarget(requested)
		send := func(phase string, status *ruleset.Status, err error) bool {
			select {
			case result <- RuleSetInstallEvent{Phase: phase, Status: status, Err: err}:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if targetErr != nil {
			send("failed", nil, targetErr)
			return
		}
		if strings.TrimSpace(selectedProxy) == "" {
			selectedProxy = s.runningRuleSetProxy(ctx, profileID)
		}
		if !send("checking", nil, nil) {
			return
		}
		status, err := ruleset.Inspect(ctx, target, s.paths.MihomoBin)
		if err != nil {
			send("failed", &status, err)
			return
		}
		if status.State == ruleset.StateInstalled {
			if !status.MetadataValid {
				if err := ruleset.WriteMetadata(target); err != nil {
					send("failed", &status, err)
					return
				}
				status, err = ruleset.Inspect(ctx, target, s.paths.MihomoBin)
				if err != nil {
					send("failed", &status, err)
					return
				}
			}
			send("succeeded", &status, nil)
			return
		}
		dir := filepath.Dir(target.DomainPath)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			send("failed", &status, err)
			return
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			send("failed", &status, err)
			return
		}
		downloader := ruleset.Downloader{Proxy: selectedProxy}
		if !send("downloading_domain", nil, nil) {
			return
		}
		domainTmp, ipTmp, err := downloader.Download(ctx, target, dir, nil)
		if err != nil {
			send("failed", &status, err)
			return
		}
		defer os.Remove(domainTmp)
		defer os.Remove(ipTmp)
		if !send("downloading_ip", nil, nil) {
			return
		}
		if !send("validating", nil, nil) {
			return
		}
		if !send("waiting_to_publish", nil, nil) {
			return
		}
		if err := ctx.Err(); err != nil {
			return
		}
		if !send("publishing", nil, nil) {
			return
		}
		transactionCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sendCommitted := func(phase string, value *ruleset.Status, committedErr error) {
			result <- RuleSetInstallEvent{Phase: phase, Status: value, Err: committedErr}
		}
		reload, shouldReload := s.rulesetReloadHook(ctx, profileID)
		if shouldReload {
			sendCommitted("reloading", nil, nil)
		}
		published, err := ruleset.PublishWithHook(transactionCtx, target, domainTmp, ipTmp, s.paths.MihomoBin, reload)
		if err != nil {
			if errors.Is(err, ruleset.ErrRestoreFailed) && s.core != nil && strings.TrimSpace(profileID) != "" {
				s.core.markFailed(domain.ProfileID(profileID), "RESTORE_FAILED")
			}
			sendCommitted("failed", &status, err)
			return
		}
		if shouldReload {
			sendCommitted("verifying", &published, nil)
		}
		sendCommitted("succeeded", &published, nil)
	}()
	return result
}

func (s *CapabilityService) runningRuleSetProxy(ctx context.Context, profileID string) string {
	if s == nil || s.core == nil || s.legacy == nil || s.core.Status().State != domain.CoreStateRunning {
		return ""
	}
	if strings.TrimSpace(profileID) == "" {
		profileID = s.core.Status().ProfileID.String()
	}
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return ""
	}
	ports, err := s.legacy.ListenerPorts(ctx, restorePoint)
	if err != nil {
		return ""
	}
	for _, field := range []string{"mixed-port", "port"} {
		for _, listener := range ports {
			if listener.Field != field || listener.Port <= 0 || !rulesetLoopbackHost(listener.Host) {
				continue
			}
			return "http://" + net.JoinHostPort(listener.Host, strconv.Itoa(listener.Port))
		}
	}
	return ""
}

func rulesetLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func (s *CapabilityService) rulesetReloadHook(ctx context.Context, profileID string) (func(context.Context) error, bool) {
	if s == nil || s.core == nil || s.legacy == nil || s.core.Status().State != domain.CoreStateRunning {
		return nil, false
	}
	profile, restorePoint, err := s.profile(ctx, profileID)
	if err != nil || profile.ID == "" {
		return nil, false
	}
	cfg, err := s.readLegacyConfig(ctx, restorePoint)
	if err != nil || !ruleset.ReferencesManagerProviders(cfg) {
		return nil, false
	}
	return func(reloadCtx context.Context) error { return s.legacy.Reload(reloadCtx, restorePoint) }, true
}

type RuleSetInstallEvent struct {
	Phase  string
	Status *ruleset.Status
	Err    error
}

func NewCapabilityService(store CapabilityStore, manager *CoreManager, compatibility *legacy.Compatibility, paths config.Paths, managerPaths ...config.ManagerPaths) *CapabilityService {
	coordinator := NewCoordinator()
	if manager != nil && manager.coordinator != nil {
		coordinator = manager.coordinator
	}
	service := &CapabilityService{
		store: store, core: manager, legacy: compatibility, paths: paths, coordinator: coordinator,
		newID: func(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }, clock: time.Now,
	}
	service.initProxyConfig(managerPaths...)
	return service
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

func (s *CapabilityService) ModeStatus(ctx context.Context, profileID string) (ModeStatus, error) {
	profile, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return ModeStatus{}, err
	}
	if !profile.Active {
		return ModeStatus{}, fmt.Errorf("profile %s is not active: %w", profile.ID, ErrCapabilityUnsupported)
	}
	coreState, runtime, err := s.routingRuntime(profile.ID)
	if err != nil {
		return ModeStatus{}, err
	}
	status, err := s.legacy.RoutingModeStatus(ctx, restorePoint, coreState, runtime)
	result := convertModeStatus(profile.ID, status)
	s.enrichProxyStatus(ctx, &result)
	return result, err
}

func (s *CapabilityService) SetMode(ctx context.Context, profileID string, mode domain.RoutingMode, closeConnections bool) (ModeStatus, error) {
	if err := mode.Validate(); err != nil {
		return ModeStatus{}, fmt.Errorf("%w: %v", legacy.ErrInvalidRoutingMode, err)
	}
	profile, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return ModeStatus{}, err
	}
	if !profile.Active {
		return ModeStatus{}, fmt.Errorf("profile %s is not active: %w", profile.ID, ErrCapabilityUnsupported)
	}
	if mode == domain.RoutingModeRule {
		if err := s.ensureRuleSetInstalled(ctx); err != nil {
			return ModeStatus{}, err
		}
	}
	coordinator, newID := s.mutationDependencies()
	release, err := coordinator.TryAcquire(ctx, newID("routing-mode"), "routing.mode.set")
	if err != nil {
		return ModeStatus{}, err
	}
	defer release()
	coreState, runtime, err := s.routingRuntime(profile.ID)
	if err != nil {
		return ModeStatus{}, err
	}
	status, err := s.legacy.SetRoutingMode(ctx, restorePoint, legacy.SetModeRequest{
		Mode: mode, CloseConnections: closeConnections, CoreState: coreState, Runtime: runtime,
	})
	if errors.Is(err, legacy.ErrRestoreFailed) && s.core != nil {
		s.core.markFailed(profile.ID, "RESTORE_FAILED")
		status.CoreState = domain.CoreStateFailed
	}
	result := convertModeStatus(profile.ID, status)
	s.enrichProxyStatus(ctx, &result)
	return result, err
}

func (s *CapabilityService) routingRuntime(profileID domain.ProfileID) (domain.CoreState, mihomo.RoutingRuntime, error) {
	if s.core == nil {
		return domain.CoreStateStopped, nil, nil
	}
	status := s.core.Status()
	if status.State != domain.CoreStateRunning {
		return status.State, nil, nil
	}
	if status.ProfileID != profileID {
		return status.State, nil, fmt.Errorf("running profile %s does not match %s: %w", status.ProfileID, profileID, ErrOperationConflict)
	}
	client, running, err := s.core.Runtime()
	if err != nil {
		return status.State, nil, err
	}
	if !running {
		return domain.CoreStateStopped, nil, nil
	}
	runtime, ok := client.(mihomo.RoutingRuntime)
	if !ok {
		return status.State, nil, errors.New("mihomo runtime does not support routing mode operations")
	}
	return status.State, runtime, nil
}

func convertModeStatus(profileID domain.ProfileID, status legacy.ModeStatus) ModeStatus {
	rules := make([]RuleSetHealth, 0, len(status.RuleSets))
	for _, item := range status.RuleSets {
		rules = append(rules, RuleSetHealth{Name: item.Name, Available: item.Available, Loaded: item.Loaded})
	}
	warnings := append([]string(nil), status.Warnings...)
	if warnings == nil {
		warnings = []string{}
	}
	return ModeStatus{
		ProfileID: profileID, ConfigMode: status.ConfigMode, RuntimeMode: status.RuntimeMode,
		RuntimeAvailable: status.RuntimeAvailable, CoreState: status.CoreState,
		EffectiveGroup: status.EffectiveGroup, EffectiveNode: status.EffectiveNode,
		RuleSets: rules, ActiveConnections: status.ActiveConnections,
		ConnectionsAvailable: status.ConnectionsAvailable, ConnectionsClosed: status.ConnectionsClosed,
		NextStart: status.NextStart, OperationID: status.OperationID, OperationPhase: status.OperationPhase,
		Warnings: warnings, Listeners: convertProxyListeners(status.Listeners), SystemProxy: []platform.ProxySource{},
	}
}

func convertProxyListeners(values []mihomo.ProxyListener) []platform.ProxyListener {
	result := make([]platform.ProxyListener, 0, len(values))
	for _, value := range values {
		result = append(result, platform.ProxyListener{Protocol: value.Protocol, Host: value.Host, Port: value.Port})
	}
	return result
}

func (s *CapabilityService) enrichProxyStatus(ctx context.Context, status *ModeStatus) {
	if status == nil || s == nil || s.proxyInspector == nil {
		return
	}
	status.SystemProxy = platform.DiagnoseProxySources(status.Listeners, s.proxyInspector.Inspect(ctx))
	for _, source := range status.SystemProxy {
		if source.Warning != "" {
			status.Warnings = appendUniqueString(status.Warnings, source.Warning)
		}
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

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
	if action != "stop" {
		if err := s.ensureCurrentRuleSetDependencies(ctx, restorePoint); err != nil {
			return err
		}
	}
	ports, err := s.legacy.ListenerPorts(ctx, restorePoint)
	if err != nil {
		return err
	}
	controllerEndpoint, err := listenerControllerEndpoint(ports, "", 0)
	if err != nil {
		return err
	}
	snapshot := core.ProfileSnapshot{
		ProfileID: profile.ID, Revision: profile.Revision, Mode: profile.Mode,
		ExternalConfigPath: profile.ConfigPath, ControllerEndpoint: controllerEndpoint,
	}
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
		return s.withMutation(ctx, "core.reload", func() error { return s.legacy.Reload(ctx, restorePoint) })
	default:
		return fmt.Errorf("unknown core action %q", action)
	}
}

func (s *CapabilityService) ValidateConfig(ctx context.Context, profileID string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	if err := s.ensureCurrentRuleSetDependencies(ctx, restorePoint); err != nil {
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
	group, err := s.legacy.GroupDetail(ctx, restorePoint, groupID)
	if err != nil {
		return GroupInfo{}, err
	}
	states := make([]NodeStateInfo, 0, len(group.NodeStates))
	for _, state := range group.NodeStates {
		item := NodeStateInfo{NodeID: domain.NodeID(state.Name), Testable: state.Testable}
		if state.Latest != nil {
			item.Latest = &NodeTestResultInfo{Status: state.Latest.Status, DelayMS: state.Latest.Delay, TestedAt: state.Latest.TestedAt}
		}
		states = append(states, item)
	}
	return GroupInfo{ID: domain.GroupID(group.Name), Name: group.Name, Type: group.Type, SelectedNode: group.Now, Nodes: group.All, NodeStates: states}, nil
}

func (s *CapabilityService) SelectGroupNode(ctx context.Context, profileID, groupID, nodeID string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.withMutation(ctx, "group.select", func() error { return s.legacy.SelectNode(ctx, restorePoint, groupID, nodeID) })
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

func (s *CapabilityService) TestNode(ctx context.Context, profileID, groupID, nodeID string) (<-chan mihomo.NodeTestEvent, error) {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return s.legacy.TestNode(ctx, restorePoint, groupID, nodeID)
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
	return s.withMutation(ctx, "subscription.set", func() error { return s.legacy.SaveSubscriptionURL(ctx, restorePoint, value) })
}

func (s *CapabilityService) UpdateSubscription(ctx context.Context, profileID string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	if err := s.ensureRuleSetForRoutingMutation(ctx, restorePoint); err != nil {
		return err
	}
	return s.withMutation(ctx, "subscription.update", func() error {
		if err := s.legacy.UpdateSubscription(ctx, restorePoint); err != nil {
			return err
		}
		if s.core != nil && s.core.Status().State == domain.CoreStateRunning {
			return s.legacy.Reload(ctx, restorePoint)
		}
		return nil
	})
}

func (s *CapabilityService) withMutation(ctx context.Context, kind string, action func() error) error {
	coordinator, newID := s.mutationDependencies()
	release, err := coordinator.TryAcquire(ctx, newID(kind), kind)
	if err != nil {
		return err
	}
	defer release()
	return action()
}

func (s *CapabilityService) mutationDependencies() (*Coordinator, func(string) string) {
	coordinator := s.coordinator
	if coordinator == nil {
		coordinator = NewCoordinator()
	}
	newID := s.newID
	if newID == nil {
		newID = func(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }
	}
	return coordinator, newID
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
	if err := s.ensureRuleSetForRoutingMutation(ctx, restorePoint); err != nil {
		return err
	}
	return s.withMutation(ctx, "routing.whitelist.add", func() error { return s.legacy.AddWhitelist(ctx, restorePoint, value) })
}

func (s *CapabilityService) RemoveWhitelist(ctx context.Context, profileID, value string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	if err := s.ensureRuleSetForRoutingMutation(ctx, restorePoint); err != nil {
		return err
	}
	return s.withMutation(ctx, "routing.whitelist.remove", func() error { return s.legacy.RemoveWhitelist(ctx, restorePoint, value) })
}

func (s *CapabilityService) EditWhitelist(ctx context.Context, profileID, oldValue, newValue string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	if err := s.ensureRuleSetForRoutingMutation(ctx, restorePoint); err != nil {
		return err
	}
	return s.withMutation(ctx, "routing.whitelist.edit", func() error { return s.legacy.EditWhitelist(ctx, restorePoint, oldValue, newValue) })
}

func (s *CapabilityService) ApplyRoutePreset(ctx context.Context, profileID, preset string) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	if preset != "cn" && preset != "CN" {
		return fmt.Errorf("unknown route preset %q", preset)
	}
	if err := s.ensureRuleSetInstalled(ctx); err != nil {
		return err
	}
	return s.withMutation(ctx, "routing.preset.apply", func() error { return s.legacy.ApplyRouteCN(ctx, restorePoint) })
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

func (s *CapabilityService) ListenerPorts(ctx context.Context, profileID string) (ListenerPortStatus, error) {
	profile, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return ListenerPortStatus{}, err
	}
	ports, err := s.legacy.ListenerPorts(ctx, restorePoint)
	if err != nil {
		return ListenerPortStatus{}, err
	}
	status := CoreStatus{State: domain.CoreStateStopped}
	if s.core != nil {
		status = s.core.Status()
	}
	result := ListenerPortStatus{
		ProfileID: profile.ID, CoreState: status.State, NextStart: status.State != domain.CoreStateRunning,
		Ports: convertListenerPorts(ports), PortConflicts: []core.PortConflict{},
	}
	if status.ProfileID == profile.ID {
		result.PortConflicts = append(result.PortConflicts, status.PortConflicts...)
	}
	return result, nil
}

func (s *CapabilityService) SetListenerPort(ctx context.Context, profileID, field string, port int) (ListenerPortStatus, error) {
	if err := mihomo.ValidateListenerPort(field, port); err != nil {
		return ListenerPortStatus{}, fmt.Errorf("%w: %v", store.ErrInvalid, err)
	}
	profile, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return ListenerPortStatus{}, err
	}
	if s.core == nil {
		return ListenerPortStatus{}, errors.New("core service is not configured")
	}
	currentPorts, err := s.legacy.ListenerPorts(ctx, restorePoint)
	if err != nil {
		return ListenerPortStatus{}, err
	}
	controllerEndpoint, err := listenerControllerEndpoint(currentPorts, field, port)
	if err != nil {
		return ListenerPortStatus{}, err
	}
	snapshot := core.ProfileSnapshot{
		ProfileID: profile.ID, Revision: profile.Revision, Mode: profile.Mode,
		ExternalConfigPath: profile.ConfigPath, ControllerEndpoint: controllerEndpoint,
	}
	restarted, err := s.core.Reconfigure(ctx, snapshot, func(ctx context.Context) (func(context.Context) error, error) {
		document, readErr := s.ReadConfig(ctx, profile.ID.String())
		if readErr != nil {
			return nil, readErr
		}
		candidate, updateErr := mihomo.UpdateListenerPort(document.Content, field, port)
		if updateErr != nil {
			return nil, updateErr
		}
		if replaceErr := s.legacy.ReplaceConfig(ctx, restorePoint, document.SHA256, candidate); replaceErr != nil {
			return nil, replaceErr
		}
		candidateDigest := sha256.Sum256(candidate)
		expected := hex.EncodeToString(candidateDigest[:])
		before := append([]byte(nil), document.Content...)
		return func(restoreCtx context.Context) error {
			return s.legacy.ReplaceConfig(restoreCtx, restorePoint, expected, before)
		}, nil
	})
	result, statusErr := s.ListenerPorts(ctx, profile.ID.String())
	if statusErr != nil {
		return result, errors.Join(err, statusErr)
	}
	result.Restarted = restarted
	return result, err
}

func convertListenerPorts(values []mihomo.ListenerPort) []ListenerPortInfo {
	result := make([]ListenerPortInfo, 0, len(values))
	for _, value := range values {
		result = append(result, ListenerPortInfo{
			Field: value.Field, Host: value.Host, Port: value.Port, Required: value.Required,
			Enabled: value.Port != 0, Networks: append([]string(nil), value.Networks...),
		})
	}
	return result
}

func listenerControllerEndpoint(values []mihomo.ListenerPort, field string, port int) (string, error) {
	for _, value := range values {
		if value.Field != mihomo.PortFieldExternalController {
			continue
		}
		if field == mihomo.PortFieldExternalController {
			value.Port = port
		}
		return "http://" + net.JoinHostPort(value.Host, strconv.Itoa(value.Port)), nil
	}
	return "", fmt.Errorf("external-controller is required: %w", core.ErrInvalidConfig)
}

func (s *CapabilityService) ReplaceConfig(ctx context.Context, profileID, expectedSHA256 string, content []byte) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	var candidate map[string]any
	if err := yaml.Unmarshal(content, &candidate); err != nil {
		return err
	}
	if ruleset.ReferencesManagerProviders(candidate) {
		if err := s.ensureRuleSetInstalled(ctx); err != nil {
			return err
		}
	}
	return s.withMutation(ctx, "config.replace", func() error { return s.legacy.ReplaceConfig(ctx, restorePoint, expectedSHA256, content) })
}

func (s *CapabilityService) configMutation(ctx context.Context, profileID string, action func(context.Context, domain.RestorePointID) error) error {
	_, restorePoint, err := s.profile(ctx, profileID)
	if err != nil {
		return err
	}
	return s.withMutation(ctx, "config.mutate", func() error { return action(ctx, restorePoint) })
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
