package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

type ProxyLayer string

const (
	ProxyLayerSystem ProxyLayer = "system"
	ProxyLayerEnv    ProxyLayer = "env"
)

type ProxyEndpointStatus struct {
	Target  string `json:"target"`
	Scheme  string `json:"scheme"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	State   string `json:"state"`
	Warning string `json:"warning,omitempty"`
}

type ProxyConfigStatus struct {
	Layer             ProxyLayer            `json:"layer"`
	ProfileID         domain.ProfileID      `json:"profileId,omitempty"`
	CoreState         domain.CoreState      `json:"coreState"`
	Managed           bool                  `json:"managed"`
	SnapshotAvailable bool                  `json:"snapshotAvailable"`
	NextStart         bool                  `json:"nextStart"`
	Endpoints         []ProxyEndpointStatus `json:"endpoints"`
	Warnings          []string              `json:"warnings"`
	UpdatedAt         time.Time             `json:"updatedAt,omitempty"`
}

type ProxyRequest struct {
	ProfileID string `json:"profileId,omitempty"`
	Layer     string `json:"layer,omitempty"`
	Action    string `json:"action"`
	Target    string `json:"target,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
}

type gnomeProxyState struct {
	Original platform.GNOMEProxySnapshot `json:"original"`
	Expected platform.GNOMEProxySnapshot `json:"expected"`
}

type envProxyState struct {
	BlockHash string                                  `json:"blockHash"`
	Endpoints map[string]platform.ProxyConfigEndpoint `json:"endpoints"`
}

type proxyPersistentState struct {
	Version   int              `json:"version"`
	GNOME     *gnomeProxyState `json:"gnome,omitempty"`
	Env       *envProxyState   `json:"env,omitempty"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

var (
	ErrProxyStateCorrupt    = errors.New("代理状态文件损坏")
	ErrProxySnapshotMissing = errors.New("代理快照不存在")
	ErrProxyEndpointMissing = errors.New("没有可用的 mihomo 代理 listener")
	ErrProxyRequestInvalid  = errors.New("代理请求参数无效")
)

type gnomeProxyConfigurator interface {
	Read(context.Context) (platform.GNOMEProxySnapshot, error)
	Apply(context.Context, platform.GNOMEProxySnapshot, map[platform.ProxyTarget]platform.ProxyConfigEndpoint) (platform.GNOMEProxySnapshot, error)
	Restore(context.Context, platform.GNOMEProxySnapshot, platform.GNOMEProxySnapshot) error
}

type bashProxyConfigurator interface {
	Read(string) ([]byte, string, string, error)
	Set(string, string, map[platform.ProxyTarget]platform.ProxyConfigEndpoint) (string, error)
	Disable(string, string) error
	Restore(string, []byte) error
}

func (s *CapabilityService) initProxyConfig(managerPaths ...config.ManagerPaths) {
	if len(managerPaths) > 0 {
		s.managerPaths = managerPaths[0]
	}
	if s.managerPaths.ProxyState == "" {
		if resolved, err := config.LoadManagerPaths(); err == nil {
			s.managerPaths = resolved
		}
	}
	if s.gnomeConfigurator == nil {
		s.gnomeConfigurator = platform.NewGNOMEProxyConfigurator()
	}
	if s.bashConfigurator == nil {
		s.bashConfigurator = platform.NewBashProxyConfigurator()
	}
}

func (s *CapabilityService) loadProxyState() (proxyPersistentState, error) {
	s.initProxyConfig()
	if s.managerPaths.ProxyState == "" {
		return proxyPersistentState{Version: 1}, nil
	}
	if info, err := os.Lstat(s.managerPaths.ProxyState); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return proxyPersistentState{}, fmt.Errorf("%w: 代理状态文件不允许为符号链接", os.ErrPermission)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return proxyPersistentState{}, err
	}
	content, err := os.ReadFile(s.managerPaths.ProxyState)
	if errors.Is(err, os.ErrNotExist) {
		return proxyPersistentState{Version: 1}, nil
	}
	if err != nil {
		return proxyPersistentState{}, err
	}
	var state proxyPersistentState
	if err := json.Unmarshal(content, &state); err != nil || state.Version != 1 {
		return proxyPersistentState{}, ErrProxyStateCorrupt
	}
	return state, nil
}

func (s *CapabilityService) saveProxyState(state proxyPersistentState) error {
	s.initProxyConfig()
	state.Version = 1
	state.UpdatedAt = time.Now().UTC()
	dir := filepath.Dir(s.managerPaths.ProxyState)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(dir); err != nil {
		return err
	} else if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: 代理状态目录不允许为符号链接", os.ErrPermission)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".proxy-state-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(content, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, s.managerPaths.ProxyState); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (s *CapabilityService) ProxyStatus(ctx context.Context, layer, profileID string) (ProxyConfigStatus, error) {
	s.initProxyConfig()
	state, err := s.loadProxyState()
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	profile, restorePoint, profileErr := s.profile(ctx, profileID)
	result := ProxyConfigStatus{Layer: ProxyLayer(layer), Endpoints: []ProxyEndpointStatus{}, Warnings: []string{}, CoreState: domain.CoreStateStopped}
	if layer != string(ProxyLayerSystem) && layer != string(ProxyLayerEnv) {
		return result, fmt.Errorf("%w: 不支持的代理层 %q", ErrProxyRequestInvalid, layer)
	}
	if profileErr != nil {
		return result, profileErr
	}
	result.ProfileID = profile.ID
	if s.core != nil {
		result.CoreState = s.core.Status().State
	}
	listenerPorts, listenerErr := s.legacy.ListenerPorts(ctx, restorePoint)
	if listenerErr != nil {
		return result, listenerErr
	}
	listeners := diagnosticProxyListeners(listenerPorts)
	switch ProxyLayer(layer) {
	case ProxyLayerSystem:
		current, readErr := s.gnomeConfigurator.Read(ctx)
		if readErr != nil {
			result.Endpoints = []ProxyEndpointStatus{
				{Target: "http", State: string(platform.ProxyStateUnknown)},
				{Target: "https", State: string(platform.ProxyStateUnknown)},
				{Target: "socks", State: string(platform.ProxyStateUnknown)},
			}
			result.Warnings = append(result.Warnings, "无法读取 GNOME 系统代理，入口状态未知")
			if state.GNOME != nil {
				result.Managed = true
				result.SnapshotAvailable = true
			}
			break
		}
		result.Endpoints = proxySourcesToStatus(platform.DiagnoseProxySources(listeners, platform.GNOMEProxySourcesFromSnapshot(current)))
		if state.GNOME != nil {
			result.Managed = true
			result.SnapshotAvailable = true
			if !gnomeSnapshotsEqual(current, state.GNOME.Expected) {
				result.Warnings = append(result.Warnings, "GNOME 代理已被外部修改，恢复前需要先处理冲突")
			}
		}
	case ProxyLayerEnv:
		content, block, hash, readErr := s.bashConfigurator.Read(s.managerPaths.Bashrc)
		_ = content
		if readErr != nil {
			return result, readErr
		}
		result.Endpoints = proxySourcesToStatus(platform.DiagnoseProxySources(listeners, platform.BashProxySources(block)))
		if state.Env != nil {
			result.Managed = true
			result.SnapshotAvailable = true
			if hash != state.Env.BlockHash {
				result.Warnings = append(result.Warnings, "~/.bashrc 管理区块已被外部修改")
			}
		} else if block != "" {
			result.Warnings = append(result.Warnings, "~/.bashrc 存在未被状态文件跟踪的 manager 区块")
		}
	}
	for _, endpoint := range result.Endpoints {
		if endpoint.Warning != "" {
			result.Warnings = appendUniqueString(result.Warnings, endpoint.Warning)
		}
	}
	result.NextStart = result.CoreState != domain.CoreStateRunning
	result.UpdatedAt = state.UpdatedAt
	return result, nil
}

func (s *CapabilityService) SetProxy(ctx context.Context, request ProxyRequest) (ProxyConfigStatus, error) {
	return s.mutateProxy(ctx, request, "set")
}

func (s *CapabilityService) RestoreProxy(ctx context.Context, request ProxyRequest) (ProxyConfigStatus, error) {
	return s.mutateProxy(ctx, request, "restore")
}

func (s *CapabilityService) DisableProxy(ctx context.Context, request ProxyRequest) (ProxyConfigStatus, error) {
	return s.mutateProxy(ctx, request, "disable")
}

func (s *CapabilityService) mutateProxy(ctx context.Context, request ProxyRequest, action string) (ProxyConfigStatus, error) {
	s.initProxyConfig()
	layer := ProxyLayer(strings.ToLower(strings.TrimSpace(request.Layer)))
	if layer != ProxyLayerSystem && layer != ProxyLayerEnv {
		return ProxyConfigStatus{}, fmt.Errorf("%w: 不支持的代理层 %q", ErrProxyRequestInvalid, request.Layer)
	}
	coordinator, newID := s.mutationDependencies()
	release, err := coordinator.TryAcquire(ctx, newID("proxy-"+string(layer)), "proxy."+string(layer))
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	defer release()
	if action == "set" {
		endpoints, err := s.proxyEndpoints(ctx, request)
		if err != nil {
			return ProxyConfigStatus{}, err
		}
		if layer == ProxyLayerSystem {
			return s.setGNOMEProxy(ctx, request.ProfileID, endpoints)
		}
		return s.setEnvProxy(ctx, request.ProfileID, endpoints)
	}
	if layer == ProxyLayerSystem {
		return s.restoreGNOMEProxy(ctx, request.ProfileID)
	}
	return s.disableEnvProxy(ctx, request.ProfileID)
}

func (s *CapabilityService) proxyEndpoints(ctx context.Context, request ProxyRequest) (map[platform.ProxyTarget]platform.ProxyConfigEndpoint, error) {
	profile, restorePoint, err := s.profile(ctx, request.ProfileID)
	if err != nil {
		return nil, err
	}
	_ = profile
	target := strings.ToLower(strings.TrimSpace(request.Target))
	targets := []platform.ProxyTarget{}
	if target == "all" {
		targets = []platform.ProxyTarget{platform.ProxyTargetHTTP, platform.ProxyTargetHTTPS, platform.ProxyTargetSocks}
	} else {
		targets = []platform.ProxyTarget{platform.ProxyTarget(target)}
		if !targets[0].Valid() {
			return nil, fmt.Errorf("%w: 代理类型必须为 http、https、socks 或 all", ErrProxyRequestInvalid)
		}
	}
	result := make(map[platform.ProxyTarget]platform.ProxyConfigEndpoint, len(targets))
	if strings.TrimSpace(request.Host) != "" || request.Port != 0 {
		if strings.TrimSpace(request.Host) == "" || request.Port == 0 {
			return nil, fmt.Errorf("%w: IP 和端口必须同时提供", platform.ErrProxyEndpointInvalid)
		}
		endpoint, err := platform.ValidateProxyEndpoint(request.Host, request.Port)
		if err != nil {
			return nil, err
		}
		for _, item := range targets {
			result[item] = endpoint
		}
		return result, nil
	}
	listeners, err := s.legacy.ListenerPorts(ctx, restorePoint)
	if err != nil {
		return nil, err
	}
	for _, item := range targets {
		listener, ok := selectProxyListener(listeners, item)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrProxyEndpointMissing, item)
		}
		endpoint, err := platform.ValidateProxyEndpoint(listener.Host, listener.Port)
		if err != nil {
			return nil, err
		}
		result[item] = endpoint
	}
	return result, nil
}

func selectProxyListener(listeners []mihomo.ListenerPort, target platform.ProxyTarget) (mihomo.ListenerPort, bool) {
	fields := []string{mihomo.PortFieldMixedPort, mihomo.PortFieldHTTPPort}
	if target == platform.ProxyTargetSocks {
		fields = []string{mihomo.PortFieldSocksPort, mihomo.PortFieldMixedPort}
	}
	for _, field := range fields {
		for _, listener := range listeners {
			if listener.Field == field && listener.Port > 0 {
				return listener, true
			}
		}
	}
	return mihomo.ListenerPort{}, false
}

func diagnosticProxyListeners(values []mihomo.ListenerPort) []platform.ProxyListener {
	result := make([]platform.ProxyListener, 0, 3)
	for _, value := range values {
		protocol := ""
		switch value.Field {
		case mihomo.PortFieldMixedPort:
			protocol = "mixed"
		case mihomo.PortFieldHTTPPort:
			protocol = "http"
		case mihomo.PortFieldSocksPort:
			protocol = "socks"
		}
		if protocol != "" && value.Port > 0 {
			result = append(result, platform.ProxyListener{Protocol: protocol, Host: value.Host, Port: value.Port})
		}
	}
	return result
}

func (s *CapabilityService) setGNOMEProxy(ctx context.Context, profileID string, endpoints map[platform.ProxyTarget]platform.ProxyConfigEndpoint) (ProxyConfigStatus, error) {
	state, err := s.loadProxyState()
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	current, err := s.gnomeConfigurator.Read(ctx)
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	if state.GNOME != nil && !gnomeSnapshotsEqual(current, state.GNOME.Expected) {
		return ProxyConfigStatus{}, fmt.Errorf("%w: GNOME", platform.ErrProxyConflict)
	}
	original := current
	if state.GNOME != nil {
		original = state.GNOME.Original
	}
	expected, err := s.gnomeConfigurator.Apply(ctx, current, endpoints)
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	previous := state
	state.GNOME = &gnomeProxyState{Original: original, Expected: expected}
	if err := s.saveProxyState(state); err != nil {
		if restoreErr := s.gnomeConfigurator.Restore(ctx, expected, current); restoreErr != nil {
			return ProxyConfigStatus{}, fmt.Errorf("%w: %v", legacy.ErrRestoreFailed, restoreErr)
		}
		return ProxyConfigStatus{}, err
	}
	status, statusErr := s.ProxyStatus(ctx, string(ProxyLayerSystem), profileID)
	if statusErr != nil {
		state = previous
		stateErr := s.saveProxyState(state)
		restoreErr := s.gnomeConfigurator.Restore(ctx, expected, current)
		if stateErr != nil || restoreErr != nil {
			return ProxyConfigStatus{}, fmt.Errorf("%w: state=%v rollback=%v", legacy.ErrRestoreFailed, stateErr, restoreErr)
		}
		return ProxyConfigStatus{}, statusErr
	}
	return status, nil
}

func (s *CapabilityService) restoreGNOMEProxy(ctx context.Context, profileID string) (ProxyConfigStatus, error) {
	state, err := s.loadProxyState()
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	if state.GNOME == nil {
		return ProxyConfigStatus{}, ErrProxySnapshotMissing
	}
	stateBeforeRestore := *state.GNOME
	if err := s.gnomeConfigurator.Restore(ctx, state.GNOME.Expected, state.GNOME.Original); err != nil {
		return ProxyConfigStatus{}, err
	}
	state.GNOME = nil
	if err := s.saveProxyState(state); err != nil {
		if restoreErr := s.gnomeConfigurator.Restore(ctx, stateBeforeRestore.Expected, stateBeforeRestore.Original); restoreErr != nil {
			return ProxyConfigStatus{}, fmt.Errorf("%w: %v", legacy.ErrRestoreFailed, restoreErr)
		}
		return ProxyConfigStatus{}, err
	}
	return s.ProxyStatus(ctx, string(ProxyLayerSystem), profileID)
}

func (s *CapabilityService) setEnvProxy(ctx context.Context, profileID string, endpoints map[platform.ProxyTarget]platform.ProxyConfigEndpoint) (ProxyConfigStatus, error) {
	state, err := s.loadProxyState()
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	expectedHash := ""
	if state.Env != nil {
		expectedHash = state.Env.BlockHash
	}
	content, block, _, err := s.bashConfigurator.Read(s.managerPaths.Bashrc)
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	for _, source := range platform.BashProxySources(block) {
		if source.Endpoint == nil {
			continue
		}
		var target platform.ProxyTarget
		switch strings.TrimPrefix(source.Source, "bash.") {
		case "HTTP_PROXY":
			target = platform.ProxyTargetHTTP
		case "HTTPS_PROXY":
			target = platform.ProxyTargetHTTPS
		case "ALL_PROXY":
			target = platform.ProxyTargetSocks
		}
		if target.Valid() {
			if _, exists := endpoints[target]; !exists {
				endpoints[target] = platform.ProxyConfigEndpoint{Host: source.Endpoint.Host, Port: source.Endpoint.Port}
			}
		}
	}
	hash, err := s.bashConfigurator.Set(s.managerPaths.Bashrc, expectedHash, endpoints)
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	previous := state
	state.Env = &envProxyState{BlockHash: hash, Endpoints: make(map[string]platform.ProxyConfigEndpoint, len(endpoints))}
	for target, endpoint := range endpoints {
		state.Env.Endpoints[string(target)] = endpoint
	}
	if err := s.saveProxyState(state); err != nil {
		if restoreErr := s.bashConfigurator.Restore(s.managerPaths.Bashrc, content); restoreErr != nil {
			return ProxyConfigStatus{}, fmt.Errorf("%w: %v", legacy.ErrRestoreFailed, restoreErr)
		}
		return ProxyConfigStatus{}, err
	}
	status, statusErr := s.ProxyStatus(ctx, string(ProxyLayerEnv), profileID)
	if statusErr != nil {
		state = previous
		stateErr := s.saveProxyState(state)
		restoreErr := s.bashConfigurator.Restore(s.managerPaths.Bashrc, content)
		if stateErr != nil || restoreErr != nil {
			return ProxyConfigStatus{}, fmt.Errorf("%w: state=%v rollback=%v", legacy.ErrRestoreFailed, stateErr, restoreErr)
		}
		return ProxyConfigStatus{}, statusErr
	}
	return status, nil
}

func (s *CapabilityService) disableEnvProxy(ctx context.Context, profileID string) (ProxyConfigStatus, error) {
	state, err := s.loadProxyState()
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	expectedHash := ""
	if state.Env != nil {
		expectedHash = state.Env.BlockHash
	}
	content, _, _, err := s.bashConfigurator.Read(s.managerPaths.Bashrc)
	if err != nil {
		return ProxyConfigStatus{}, err
	}
	if err := s.bashConfigurator.Disable(s.managerPaths.Bashrc, expectedHash); err != nil {
		return ProxyConfigStatus{}, err
	}
	if state.Env == nil {
		return s.ProxyStatus(ctx, string(ProxyLayerEnv), profileID)
	}
	state.Env = nil
	if err := s.saveProxyState(state); err != nil {
		if restoreErr := s.bashConfigurator.Restore(s.managerPaths.Bashrc, content); restoreErr != nil {
			return ProxyConfigStatus{}, fmt.Errorf("%w: %v", legacy.ErrRestoreFailed, restoreErr)
		}
		return ProxyConfigStatus{}, err
	}
	return s.ProxyStatus(ctx, string(ProxyLayerEnv), profileID)
}

func proxySourcesToStatus(values []platform.ProxySource) []ProxyEndpointStatus {
	result := make([]ProxyEndpointStatus, 0, len(values))
	for _, value := range values {
		target := strings.TrimPrefix(strings.TrimPrefix(value.Source, "gnome."), "bash.")
		switch target {
		case "HTTP_PROXY":
			target = "http"
		case "HTTPS_PROXY":
			target = "https"
		case "ALL_PROXY":
			target = "socks"
		}
		item := ProxyEndpointStatus{Target: target, State: string(value.State), Warning: value.Warning}
		if value.Endpoint != nil {
			item.Scheme, item.Host, item.Port = value.Endpoint.Scheme, value.Endpoint.Host, value.Endpoint.Port
		}
		result = append(result, item)
	}
	return result
}

func gnomeSnapshotsEqual(left, right platform.GNOMEProxySnapshot) bool {
	if len(left.Values) != len(right.Values) {
		return false
	}
	for key, value := range left.Values {
		if strings.TrimSpace(right.Values[key]) != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}
