package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/daemon"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

type CapabilityAPI interface {
	ModeStatus(context.Context, string) (RoutingModeStatus, error)
	SetMode(context.Context, SetRoutingModeRequest) (RoutingModeStatus, error)
	CoreStatus(context.Context, string) (CoreStatus, error)
	CoreAction(context.Context, string, string) error
	ValidateConfig(context.Context, string) error
	Groups(context.Context, string) ([]Group, error)
	Group(context.Context, string, string) (Group, error)
	SelectGroupNode(context.Context, string, string, string) error
	Nodes(context.Context, string, string) ([]Node, error)
	TestNodes(context.Context, NodeTestRequest) <-chan NodeTestEvent
	Subscription(context.Context, string) (Subscription, error)
	SetSubscription(context.Context, string, string) error
	UpdateSubscription(context.Context, string) error
	Whitelist(context.Context, string) ([]string, error)
	AddWhitelist(context.Context, string, string) error
	RemoveWhitelist(context.Context, string, string) error
	EditWhitelist(context.Context, string, string, string) error
	ApplyRoutePreset(context.Context, string, string) error
	DiagnoseRoute(context.Context, string, string) (RouteDiagnosis, error)
	Connections(context.Context, ConnectionRequest) ([]Connection, error)
	FollowConnections(context.Context, ConnectionRequest) <-chan ConnectionEvent
	ConfigBackup(context.Context, string) error
	ConfigRestore(context.Context, string) error
	ReadConfig(context.Context, string) (ConfigDocument, error)
	ReplaceConfig(context.Context, string, string, []byte) error
	TailLogs(context.Context, LogRequest) ([]LogLine, error)
	FollowLogs(context.Context, LogRequest) <-chan LogEvent
}

type ConfigDocument struct {
	Content []byte
	SHA256  string
}

type DaemonCapabilities struct {
	client    *ipc.Client
	lookupEnv func(string) (string, bool)
}

func NewCapabilityService(paths config.ManagerPaths) *DaemonCapabilities {
	return &DaemonCapabilities{client: ipc.NewClient(paths.Socket), lookupEnv: os.LookupEnv}
}

func (c *DaemonCapabilities) ModeStatus(ctx context.Context, profileID string) (RoutingModeStatus, error) {
	if c == nil || c.client == nil {
		return RoutingModeStatus{}, &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "daemon 客户端未配置"}
	}
	var value daemon.ModeStatus
	if err := c.client.Do(ctx, http.MethodGet, capabilityPath("/v1/mode", profileID), "", nil, &value); err != nil {
		decodeModeStatusError(err, &value)
		return c.convertModeStatus(value), c.mapError(err)
	}
	return c.convertModeStatus(value), nil
}

func (c *DaemonCapabilities) SetMode(ctx context.Context, request SetRoutingModeRequest) (RoutingModeStatus, error) {
	if err := request.Mode.Validate(); err != nil {
		return RoutingModeStatus{}, &Error{Category: ErrorCategoryInvalidArgument, Code: ErrorCode("INVALID_ROUTING_MODE"), Message: "路由模式必须为 global、rule 或 direct", Err: err}
	}
	if c == nil || c.client == nil {
		return RoutingModeStatus{}, &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "daemon 客户端未配置"}
	}
	requestID := strings.TrimSpace(request.RequestID)
	if requestID == "" {
		requestID = newRequestID("routing-mode")
	}
	var value daemon.ModeStatus
	err := c.client.Do(ctx, http.MethodPut, "/v1/mode", requestID, map[string]any{
		"profileId": request.ProfileID, "mode": request.Mode, "closeConnections": request.CloseConnections,
	}, &value)
	if err == nil {
		return c.convertModeStatus(value), nil
	}
	decodeModeStatusError(err, &value)
	return c.convertModeStatus(value), c.mapError(err)
}

func decodeModeStatusError(err error, value *daemon.ModeStatus) {
	var ipcErr *ipc.Error
	if value == nil || !errors.As(err, &ipcErr) {
		return
	}
	raw, ok := ipcErr.Body.Details["status"]
	if !ok {
		return
	}
	if encoded, marshalErr := json.Marshal(raw); marshalErr == nil {
		_ = json.Unmarshal(encoded, value)
	}
}

func (c *DaemonCapabilities) CoreStatus(ctx context.Context, profileID string) (CoreStatus, error) {
	var value daemon.CoreStatus
	if err := c.do(ctx, http.MethodGet, capabilityPath("/v1/core/status", profileID), "", nil, &value); err != nil {
		return CoreStatus{}, err
	}
	return CoreStatus{Type: domain.CoreTypeMihomo, State: value.State, ProfileID: value.ProfileID, PID: value.PID}, nil
}

func (c *DaemonCapabilities) CoreAction(ctx context.Context, profileID, action string) error {
	var result map[string]any
	request := map[string]string{"profileId": profileID}
	return c.do(ctx, http.MethodPost, "/v1/core/"+url.PathEscape(action), newRequestID("core-"+action), request, &result)
}

func (c *DaemonCapabilities) ValidateConfig(ctx context.Context, profileID string) error {
	var result map[string]any
	return c.do(ctx, http.MethodPost, "/v1/config/validate", newRequestID("config-validate"), map[string]string{"profileId": profileID}, &result)
}

func (c *DaemonCapabilities) Groups(ctx context.Context, profileID string) ([]Group, error) {
	var values []daemon.GroupInfo
	if err := c.do(ctx, http.MethodGet, capabilityPath("/v1/groups", profileID), "", nil, &values); err != nil {
		return nil, err
	}
	return convertGroups(values), nil
}

func (c *DaemonCapabilities) Group(ctx context.Context, profileID, groupID string) (Group, error) {
	var value daemon.GroupInfo
	if err := c.do(ctx, http.MethodGet, capabilityPath("/v1/groups/"+url.PathEscape(groupID), profileID), "", nil, &value); err != nil {
		return Group{}, err
	}
	return convertGroup(value), nil
}

func (c *DaemonCapabilities) SelectGroupNode(ctx context.Context, profileID, groupID, nodeID string) error {
	var result map[string]any
	request := map[string]string{"profileId": profileID, "nodeId": nodeID}
	return c.do(ctx, http.MethodPost, "/v1/groups/"+url.PathEscape(groupID), newRequestID("group-select"), request, &result)
}

func (c *DaemonCapabilities) Nodes(ctx context.Context, profileID, groupID string) ([]Node, error) {
	path := capabilityPath("/v1/nodes", profileID)
	if groupID != "" {
		path = addQuery(path, "groupId", groupID)
	}
	var values []daemon.NodeInfo
	if err := c.do(ctx, http.MethodGet, path, "", nil, &values); err != nil {
		return nil, err
	}
	result := make([]Node, 0, len(values))
	for _, value := range values {
		result = append(result, Node{ID: value.ID, Name: value.Name, Protocol: value.Protocol})
	}
	return result, nil
}

func (c *DaemonCapabilities) TestNodes(ctx context.Context, request NodeTestRequest) <-chan NodeTestEvent {
	result := make(chan NodeTestEvent, 32)
	go func() {
		defer close(result)
		body, err := c.client.OpenStream(ctx, http.MethodPost, "/v1/nodes/test", newRequestID("node-test"), map[string]any{
			"profileId": request.ProfileID, "groupId": request.GroupID, "concurrency": request.Concurrency, "limit": request.Limit,
		})
		if err != nil {
			sendNodeTestEvent(ctx, result, NodeTestEvent{Err: c.mapError(err), Finished: true})
			return
		}
		defer body.Close()
		decoder := ipc.NewStreamDecoder(body)
		for {
			event, decodeErr := decoder.Next(ctx)
			if decodeErr != nil {
				if errors.Is(decodeErr, ipc.ErrRequestCancelled) || ctx.Err() != nil {
					sendNodeTestEvent(ctx, result, NodeTestEvent{Err: ctx.Err(), Finished: true})
					return
				}
				sendNodeTestEvent(ctx, result, NodeTestEvent{Err: c.mapError(decodeErr), Finished: true})
				return
			}
			switch event.Kind {
			case "event":
				var value struct {
					Done  int    `json:"done"`
					Total int    `json:"total"`
					Name  string `json:"name"`
					Delay int    `json:"delayMs"`
				}
				if err := json.Unmarshal(event.Data, &value); err != nil {
					sendNodeTestEvent(ctx, result, NodeTestEvent{Err: err, Finished: true})
					return
				}
				sendNodeTestEvent(ctx, result, NodeTestEvent{Done: value.Done, Total: value.Total, Result: &NodeDelay{NodeID: domain.NodeID(value.Name), Delay: time.Duration(value.Delay) * time.Millisecond}})
			case "done":
				var value struct{ Done, Total int }
				_ = json.Unmarshal(event.Data, &value)
				sendNodeTestEvent(ctx, result, NodeTestEvent{Done: value.Done, Total: value.Total, Finished: true})
				return
			case "error":
				if event.Error == nil {
					sendNodeTestEvent(ctx, result, NodeTestEvent{Err: errors.New("stream error event is missing details"), Finished: true})
				} else {
					sendNodeTestEvent(ctx, result, NodeTestEvent{Err: c.mapError(&ipc.Error{Body: *event.Error}), Finished: true})
				}
				return
			}
		}
	}()
	return result
}

func (c *DaemonCapabilities) Subscription(ctx context.Context, profileID string) (Subscription, error) {
	var value daemon.SubscriptionInfo
	if err := c.do(ctx, http.MethodGet, capabilityPath("/v1/subscription", profileID), "", nil, &value); err != nil {
		return Subscription{}, err
	}
	return Subscription{ID: value.ID, Name: value.Name, URL: value.URL, Enabled: value.Enabled}, nil
}

func (c *DaemonCapabilities) SetSubscription(ctx context.Context, profileID, value string) error {
	var result map[string]any
	return c.do(ctx, http.MethodPut, "/v1/subscription", newRequestID("subscription-set"), map[string]string{"profileId": profileID, "url": value}, &result)
}

func (c *DaemonCapabilities) UpdateSubscription(ctx context.Context, profileID string) error {
	var result map[string]any
	return c.do(ctx, http.MethodPost, "/v1/subscription", newRequestID("subscription-update"), map[string]string{"profileId": profileID}, &result)
}

func (c *DaemonCapabilities) Whitelist(ctx context.Context, profileID string) ([]string, error) {
	var value []string
	if err := c.do(ctx, http.MethodGet, capabilityPath("/v1/routes/whitelist", profileID), "", nil, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func (c *DaemonCapabilities) AddWhitelist(ctx context.Context, profileID, value string) error {
	return c.whitelistMutation(ctx, http.MethodPost, "add", profileID, value, "")
}
func (c *DaemonCapabilities) RemoveWhitelist(ctx context.Context, profileID, value string) error {
	return c.whitelistMutation(ctx, http.MethodDelete, "remove", profileID, value, "")
}
func (c *DaemonCapabilities) EditWhitelist(ctx context.Context, profileID, oldValue, newValue string) error {
	return c.whitelistMutation(ctx, http.MethodPut, "edit", profileID, newValue, oldValue)
}
func (c *DaemonCapabilities) whitelistMutation(ctx context.Context, method, action, profileID, value, oldValue string) error {
	var result map[string]any
	request := map[string]string{"profileId": profileID, "domain": value}
	if oldValue != "" {
		request["oldDomain"] = oldValue
	}
	return c.do(ctx, method, "/v1/routes/whitelist", newRequestID("whitelist-"+action), request, &result)
}

func (c *DaemonCapabilities) ApplyRoutePreset(ctx context.Context, profileID, preset string) error {
	var result map[string]any
	return c.do(ctx, http.MethodPost, "/v1/routes/preset", newRequestID("route-preset"), map[string]string{"profileId": profileID, "preset": preset}, &result)
}

func (c *DaemonCapabilities) DiagnoseRoute(ctx context.Context, profileID, input string) (RouteDiagnosis, error) {
	var value daemon.RouteInfo
	path := addQuery(capabilityPath("/v1/routes/diagnose", profileID), "input", input)
	if err := c.do(ctx, http.MethodGet, path, "", nil, &value); err != nil {
		return RouteDiagnosis{}, err
	}
	return RouteDiagnosis{Input: value.Input, Host: value.Host, MatchedRule: value.MatchedRule, Target: value.Target, CurrentNode: value.CurrentNode, Confidence: value.Confidence, Note: value.Note}, nil
}

func (c *DaemonCapabilities) Connections(ctx context.Context, request ConnectionRequest) ([]Connection, error) {
	var values []daemon.ConnectionInfo
	if err := c.do(ctx, http.MethodGet, capabilityPath("/v1/connections", request.ProfileID.String()), "", nil, &values); err != nil {
		return nil, err
	}
	result := make([]Connection, 0, len(values))
	for _, value := range values {
		result = append(result, convertConnectionInfo(value))
	}
	return result, nil
}

func (c *DaemonCapabilities) FollowConnections(ctx context.Context, request ConnectionRequest) <-chan ConnectionEvent {
	result := make(chan ConnectionEvent, 64)
	go func() {
		defer close(result)
		if c == nil || c.client == nil {
			sendConnectionEvent(ctx, result, ConnectionEvent{Err: &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "daemon 客户端未配置"}, Finished: true})
			return
		}
		path := capabilityPath("/v1/connections/follow", request.ProfileID.String())
		body, err := c.client.OpenStream(ctx, http.MethodGet, path, newRequestID("connections-follow"), nil)
		if err != nil {
			sendConnectionEvent(ctx, result, ConnectionEvent{Err: c.mapError(err), Finished: true})
			return
		}
		defer body.Close()
		decoder := ipc.NewStreamDecoder(body)
		for {
			event, decodeErr := decoder.Next(ctx)
			if decodeErr != nil {
				if ctx.Err() != nil || errors.Is(decodeErr, ipc.ErrRequestCancelled) {
					sendConnectionEvent(ctx, result, ConnectionEvent{Err: ctx.Err(), Finished: true})
				} else {
					sendConnectionEvent(ctx, result, ConnectionEvent{Err: c.mapError(decodeErr), Finished: true})
				}
				return
			}
			switch event.Kind {
			case "event":
				var value daemon.ConnectionEvent
				if err := json.Unmarshal(event.Data, &value); err != nil {
					sendConnectionEvent(ctx, result, ConnectionEvent{Seq: event.Seq, Err: err, Finished: true})
					return
				}
				action := ConnectionAction(value.Action)
				if action != ConnectionActionOpen && action != ConnectionActionUpdate && action != ConnectionActionClosed {
					sendConnectionEvent(ctx, result, ConnectionEvent{Seq: event.Seq, Err: errors.New("connection stream action is invalid"), Finished: true})
					return
				}
				connection := convertConnectionInfo(value.Connection)
				sendConnectionEvent(ctx, result, ConnectionEvent{Seq: event.Seq, Action: action, Connection: &connection, Time: value.Time})
			case "done":
				sendConnectionEvent(ctx, result, ConnectionEvent{Seq: event.Seq, Finished: true})
				return
			case "error":
				if event.Error == nil {
					sendConnectionEvent(ctx, result, ConnectionEvent{Seq: event.Seq, Err: errors.New("stream error event is missing details"), Finished: true})
				} else {
					sendConnectionEvent(ctx, result, ConnectionEvent{Seq: event.Seq, Err: c.mapError(&ipc.Error{Body: *event.Error}), Finished: true})
				}
				return
			}
		}
	}()
	return result
}

func convertConnectionInfo(value daemon.ConnectionInfo) Connection {
	return Connection{
		ID: value.ID, Host: value.Host, DestinationIP: value.DestinationIP, DestinationPort: value.DestinationPort,
		Network: value.Network, Rule: value.Rule, RulePayload: value.RulePayload,
		Chains: append([]string(nil), value.Chains...), FinalNode: value.FinalNode,
		Upload: value.Upload, Download: value.Download, Start: value.Start,
		Warnings: append([]string(nil), value.Warnings...),
	}
}

func (c *DaemonCapabilities) ConfigBackup(ctx context.Context, profileID string) error {
	return c.configMutation(ctx, "backup", profileID)
}
func (c *DaemonCapabilities) ConfigRestore(ctx context.Context, profileID string) error {
	return c.configMutation(ctx, "restore", profileID)
}
func (c *DaemonCapabilities) configMutation(ctx context.Context, action, profileID string) error {
	var result map[string]any
	return c.do(ctx, http.MethodPost, "/v1/config/"+action, newRequestID("config-"+action), map[string]string{"profileId": profileID}, &result)
}

func (c *DaemonCapabilities) ReadConfig(ctx context.Context, profileID string) (ConfigDocument, error) {
	var value daemon.ConfigDocument
	if err := c.do(ctx, http.MethodGet, capabilityPath("/v1/config/edit", profileID), "", nil, &value); err != nil {
		return ConfigDocument{}, err
	}
	return ConfigDocument{Content: value.Content, SHA256: value.SHA256}, nil
}

func (c *DaemonCapabilities) ReplaceConfig(ctx context.Context, profileID, expectedSHA256 string, content []byte) error {
	var result map[string]any
	request := map[string]any{"profileId": profileID, "expectedSha256": expectedSHA256, "content": content}
	return c.do(ctx, http.MethodPut, "/v1/config/edit", newRequestID("config-edit"), request, &result)
}

func (c *DaemonCapabilities) TailLogs(ctx context.Context, request LogRequest) ([]LogLine, error) {
	path := addQuery(capabilityPath("/v1/logs", request.ProfileID.String()), "lines", strconv.Itoa(request.Lines))
	var value struct {
		Content string `json:"content"`
	}
	if err := c.do(ctx, http.MethodGet, path, "", nil, &value); err != nil {
		return nil, err
	}
	return splitLogLines(value.Content), nil
}

func (c *DaemonCapabilities) FollowLogs(ctx context.Context, request LogRequest) <-chan LogEvent {
	result := make(chan LogEvent, 32)
	go func() {
		defer close(result)
		path := addQuery(capabilityPath("/v1/logs/follow", request.ProfileID.String()), "lines", strconv.Itoa(request.Lines))
		body, err := c.client.OpenStream(ctx, http.MethodGet, path, newRequestID("logs-follow"), nil)
		if err != nil {
			sendLogEvent(ctx, result, LogEvent{Err: c.mapError(err), Finished: true})
			return
		}
		defer body.Close()
		decoder := ipc.NewStreamDecoder(body)
		for {
			event, decodeErr := decoder.Next(ctx)
			if decodeErr != nil {
				if ctx.Err() != nil {
					sendLogEvent(ctx, result, LogEvent{Err: ctx.Err(), Finished: true})
				} else {
					sendLogEvent(ctx, result, LogEvent{Err: c.mapError(decodeErr), Finished: true})
				}
				return
			}
			switch event.Kind {
			case "event":
				var value struct {
					Line string `json:"line"`
				}
				if err := json.Unmarshal(event.Data, &value); err != nil {
					sendLogEvent(ctx, result, LogEvent{Err: err, Finished: true})
					return
				}
				sendLogEvent(ctx, result, LogEvent{Line: &LogLine{Message: value.Line}})
			case "done":
				sendLogEvent(ctx, result, LogEvent{Finished: true})
				return
			case "error":
				if event.Error == nil {
					sendLogEvent(ctx, result, LogEvent{Err: errors.New("stream error event is missing details"), Finished: true})
				} else {
					sendLogEvent(ctx, result, LogEvent{Err: c.mapError(&ipc.Error{Body: *event.Error}), Finished: true})
				}
				return
			}
		}
	}()
	return result
}

func (c *DaemonCapabilities) do(ctx context.Context, method, path, requestID string, request, result any) error {
	if c == nil || c.client == nil {
		return &Error{Category: ErrorCategoryDaemonUnavailable, Code: ErrorCodeDaemonUnavailable, Message: "daemon 客户端未配置"}
	}
	return c.mapError(c.client.Do(ctx, method, path, requestID, request, result))
}

func (c *DaemonCapabilities) mapError(err error) error {
	if err == nil {
		return nil
	}
	return mapIPCError(err)
}

func capabilityPath(path, profileID string) string {
	if strings.TrimSpace(profileID) == "" {
		return path
	}
	return addQuery(path, "profileId", profileID)
}

func addQuery(path, key, value string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

func convertGroups(values []daemon.GroupInfo) []Group {
	result := make([]Group, 0, len(values))
	for _, value := range values {
		result = append(result, convertGroup(value))
	}
	return result
}

func convertGroup(value daemon.GroupInfo) Group {
	nodes := make([]domain.NodeID, 0, len(value.Nodes))
	for _, node := range value.Nodes {
		nodes = append(nodes, domain.NodeID(node))
	}
	return Group{ID: value.ID, Name: value.Name, Type: value.Type, SelectedNodeID: domain.NodeID(value.SelectedNode), NodeIDs: nodes}
}

func (c *DaemonCapabilities) convertModeStatus(value daemon.ModeStatus) RoutingModeStatus {
	rules := make([]RuleSetHealth, 0, len(value.RuleSets))
	for _, item := range value.RuleSets {
		rules = append(rules, RuleSetHealth{Name: item.Name, Available: item.Available, Loaded: item.Loaded})
	}
	result := RoutingModeStatus{
		ProfileID: value.ProfileID, ConfigMode: value.ConfigMode, RuntimeMode: value.RuntimeMode,
		RuntimeAvailable: value.RuntimeAvailable, CoreState: value.CoreState,
		EffectiveGroup: value.EffectiveGroup, EffectiveNode: value.EffectiveNode,
		RuleSets: rules, ActiveConnections: value.ActiveConnections,
		ConnectionsAvailable: value.ConnectionsAvailable, ConnectionsClosed: value.ConnectionsClosed,
		NextStart: value.NextStart, OperationID: value.OperationID, OperationPhase: value.OperationPhase,
		Warnings: append([]string(nil), value.Warnings...), Listeners: convertListeners(value.Listeners),
		SystemProxy: convertProxySources(value.SystemProxy),
	}
	if c != nil && c.lookupEnv != nil {
		environment := platform.DiagnoseProxySources(value.Listeners, platform.InspectProxyEnvironment(c.lookupEnv))
		result.EnvironmentProxy = convertProxySources(environment)
		for _, source := range environment {
			if source.Warning != "" {
				result.Warnings = appendUniqueWarning(result.Warnings, source.Warning)
			}
		}
	}
	if result.EnvironmentProxy == nil {
		result.EnvironmentProxy = []ProxySourceStatus{}
	}
	return result
}

func appendUniqueWarning(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func convertListeners(values []platform.ProxyListener) []ProxyListener {
	result := make([]ProxyListener, 0, len(values))
	for _, value := range values {
		result = append(result, ProxyListener{Protocol: value.Protocol, Host: value.Host, Port: value.Port})
	}
	return result
}

func convertProxySources(values []platform.ProxySource) []ProxySourceStatus {
	result := make([]ProxySourceStatus, 0, len(values))
	for _, value := range values {
		var endpoint *ProxyEndpoint
		if value.Endpoint != nil {
			endpoint = &ProxyEndpoint{Scheme: value.Endpoint.Scheme, Host: value.Endpoint.Host, Port: value.Endpoint.Port}
		}
		result = append(result, ProxySourceStatus{Source: value.Source, ExpectedProtocol: value.ExpectedProtocol, State: string(value.State), Endpoint: endpoint, Warning: value.Warning})
	}
	return result
}

func splitLogLines(content string) []LogLine {
	lines := strings.Split(content, "\n")
	result := make([]LogLine, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, LogLine{Message: line})
		}
	}
	return result
}

func sendNodeTestEvent(ctx context.Context, output chan<- NodeTestEvent, event NodeTestEvent) bool {
	select {
	case output <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func sendLogEvent(ctx context.Context, output chan<- LogEvent, event LogEvent) bool {
	select {
	case output <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func sendConnectionEvent(ctx context.Context, output chan<- ConnectionEvent, event ConnectionEvent) bool {
	select {
	case output <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

var _ CapabilityAPI = (*DaemonCapabilities)(nil)
