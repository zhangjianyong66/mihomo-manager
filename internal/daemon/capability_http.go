package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
	"github.com/zhangjianyong66/mihomo-manager/internal/ruleset"
	"github.com/zhangjianyong66/mihomo-manager/internal/store"
)

type capabilityHandler struct{ service *CapabilityService }

func registerCapabilityRoutes(mux *http.ServeMux, service *CapabilityService) {
	handler := &capabilityHandler{service: service}
	mux.HandleFunc("/v1/core/status", handler.coreStatus)
	mux.HandleFunc("/v1/mode", handler.mode)
	mux.HandleFunc("/v1/core/", handler.coreAction)
	mux.HandleFunc("/v1/config/", handler.configAction)
	mux.HandleFunc("/v1/proxy/system", handler.proxySystem)
	mux.HandleFunc("/v1/proxy/env", handler.proxyEnv)
	mux.HandleFunc("/v1/groups", handler.groups)
	mux.HandleFunc("/v1/groups/", handler.group)
	mux.HandleFunc("/v1/nodes", handler.nodes)
	mux.HandleFunc("/v1/nodes/test-single", handler.nodeTestSingle)
	mux.HandleFunc("/v1/nodes/test", handler.nodeTest)
	mux.HandleFunc("/v1/subscription", handler.subscription)
	mux.HandleFunc("/v1/routes/whitelist", handler.whitelist)
	mux.HandleFunc("/v1/routes/preset", handler.routePreset)
	mux.HandleFunc("/v1/routes/diagnose", handler.routeDiagnose)
	mux.HandleFunc("/v1/connections", handler.connections)
	mux.HandleFunc("/v1/connections/follow", handler.connectionFollow)
	mux.HandleFunc("/v1/logs", handler.logs)
	mux.HandleFunc("/v1/logs/follow", handler.logFollow)
	mux.HandleFunc("/v1/rulesets/status", handler.rulesetStatus)
	mux.HandleFunc("/v1/rulesets/install", handler.rulesetInstall)
}

func (h *capabilityHandler) rulesetStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	requested := ruleset.Target{Source: r.URL.Query().Get("source"), Ref: r.URL.Query().Get("ref"), DomainSHA256: r.URL.Query().Get("domainSha256"), IPSHA256: r.URL.Query().Get("ipSha256")}
	value, err := h.service.RuleSetStatusForTarget(r.Context(), requested)
	if err == nil {
		_ = ipc.WriteJSON(w, http.StatusOK, "RuleSetStatus", value)
		return
	}
	writeCapabilityError(w, err)
}

func (h *capabilityHandler) rulesetInstall(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if strings.TrimSpace(r.Header.Get(ipc.RequestIDHeader)) == "" {
		_ = ipc.WriteError(w, http.StatusBadRequest, "REQUEST_ID_REQUIRED", "规则集安装必须提供 MM-Request-ID", false, nil)
		return
	}
	var request struct {
		ProfileID    string `json:"profileId,omitempty"`
		Proxy        string `json:"proxy,omitempty"`
		Source       string `json:"source"`
		Ref          string `json:"ref"`
		DomainSHA256 string `json:"domainSha256"`
		IPSHA256     string `json:"ipSha256"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	if request.Proxy != "" {
		if _, err := ruleset.ValidateProxy(request.Proxy); err != nil {
			writeCapabilityError(w, err)
			return
		}
	}
	stream := h.service.InstallRuleSetsForProfile(r.Context(), request.ProfileID, ruleset.Target{Source: request.Source, Ref: request.Ref, DomainSHA256: request.DomainSHA256, IPSHA256: request.IPSHA256}, request.Proxy)
	w.Header().Set("Content-Type", "application/x-ndjson")
	writer := ipc.NewStreamWriter(w)
	for event := range stream {
		if event.Err != nil {
			_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "error", Error: capabilityErrorBody(event.Err)})
			flushResponse(w)
			return
		}
		data, _ := json.Marshal(map[string]any{"phase": event.Phase, "status": event.Status})
		if err := writer.Write(r.Context(), ipc.StreamEvent{Kind: "event", Data: data}); err != nil {
			return
		}
		flushResponse(w)
	}
	_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "done"})
	flushResponse(w)
}

func (h *capabilityHandler) proxySystem(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r, ProxyLayerSystem)
}

func (h *capabilityHandler) proxyEnv(w http.ResponseWriter, r *http.Request) {
	h.proxy(w, r, ProxyLayerEnv)
}

func (h *capabilityHandler) proxy(w http.ResponseWriter, r *http.Request, layer ProxyLayer) {
	if r.Method == http.MethodGet {
		value, err := h.service.ProxyStatus(r.Context(), string(layer), profileQuery(r))
		writeProxyResult(w, value, err)
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	if strings.TrimSpace(r.Header.Get(ipc.RequestIDHeader)) == "" {
		_ = ipc.WriteError(w, http.StatusBadRequest, "REQUEST_ID_REQUIRED", "代理修改必须提供 MM-Request-ID", false, nil)
		return
	}
	var request ProxyRequest
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	request.Layer = string(layer)
	var value ProxyConfigStatus
	var err error
	switch strings.ToLower(strings.TrimSpace(request.Action)) {
	case "set":
		value, err = h.service.SetProxy(r.Context(), request)
	case "restore":
		if layer != ProxyLayerSystem {
			writeCapabilityError(w, fmt.Errorf("%w: 环境代理请使用 disable 操作", ErrProxyRequestInvalid))
			return
		}
		value, err = h.service.RestoreProxy(r.Context(), request)
	case "disable":
		if layer != ProxyLayerEnv {
			writeCapabilityError(w, fmt.Errorf("%w: GNOME 代理请使用 restore 操作", ErrProxyRequestInvalid))
			return
		}
		value, err = h.service.DisableProxy(r.Context(), request)
	default:
		writeCapabilityError(w, fmt.Errorf("%w: 代理操作必须为 set、restore 或 disable", ErrProxyRequestInvalid))
		return
	}
	writeProxyResult(w, value, err)
}

func writeProxyResult(w http.ResponseWriter, value ProxyConfigStatus, err error) {
	if err == nil {
		_ = ipc.WriteJSONWarnings(w, http.StatusOK, "ProxyConfigStatus", value, value.Warnings)
		return
	}
	status, code, message, retryable := classifyCapabilityError(err)
	details := capabilityErrorDetails(err)
	if value.Layer != "" {
		if details == nil {
			details = map[string]any{}
		}
		details["status"] = value
	}
	_ = ipc.WriteError(w, status, code, message, retryable, details)
}

func (h *capabilityHandler) mode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		value, err := h.service.ModeStatus(r.Context(), profileQuery(r))
		writeModeResult(w, value, err)
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	if strings.TrimSpace(r.Header.Get(ipc.RequestIDHeader)) == "" {
		_ = ipc.WriteError(w, http.StatusBadRequest, "REQUEST_ID_REQUIRED", "修改模式必须提供 MM-Request-ID", false, nil)
		return
	}
	var request struct {
		ProfileID        string `json:"profileId,omitempty"`
		Mode             string `json:"mode"`
		CloseConnections bool   `json:"closeConnections"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	mode := domain.RoutingMode(strings.ToLower(strings.TrimSpace(request.Mode)))
	value, err := h.service.SetMode(r.Context(), request.ProfileID, mode, request.CloseConnections)
	writeModeResult(w, value, err)
}

func writeModeResult(w http.ResponseWriter, value ModeStatus, err error) {
	if err == nil {
		_ = ipc.WriteJSONWarnings(w, http.StatusOK, "RoutingModeStatus", value, value.Warnings)
		return
	}
	status, code, message, retryable := classifyCapabilityError(err)
	details := map[string]any(nil)
	if value.ProfileID != "" || value.ConfigMode != "" || value.OperationID != "" {
		details = map[string]any{"status": value}
	}
	_ = ipc.WriteError(w, status, code, message, retryable, details)
}

func profileQuery(r *http.Request) string { return strings.TrimSpace(r.URL.Query().Get("profileId")) }

func (h *capabilityHandler) coreStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	value, err := h.service.CoreStatus(r.Context(), profileQuery(r))
	writeCapabilityResult(w, "CoreStatus", value, err)
}

func (h *capabilityHandler) coreAction(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/v1/core/")
	if action != "start" && action != "stop" && action != "restart" && action != "reload" {
		writeCapabilityError(w, fmt.Errorf("unknown core action: %w", ErrCapabilityNotFound))
		return
	}
	var request struct {
		ProfileID string `json:"profileId,omitempty"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	err := h.service.CoreAction(r.Context(), request.ProfileID, action)
	writeCapabilityResult(w, "CoreAction", map[string]string{"action": action, "profileId": request.ProfileID}, err)
}

func (h *capabilityHandler) configAction(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/v1/config/")
	if action == "edit" {
		h.configEdit(w, r)
		return
	}
	if action == "ports" {
		h.configPorts(w, r)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request struct {
		ProfileID string `json:"profileId,omitempty"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	var err error
	switch action {
	case "validate":
		err = h.service.ValidateConfig(r.Context(), request.ProfileID)
	case "backup":
		err = h.service.ConfigBackup(r.Context(), request.ProfileID)
	case "restore":
		err = h.service.ConfigRestore(r.Context(), request.ProfileID)
	default:
		writeCapabilityError(w, fmt.Errorf("unknown config action: %w", ErrCapabilityNotFound))
		return
	}
	writeCapabilityResult(w, "ConfigAction", map[string]string{"action": action, "profileId": request.ProfileID}, err)
}

func (h *capabilityHandler) configPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		value, err := h.service.ListenerPorts(r.Context(), profileQuery(r))
		writeListenerPortResult(w, value, err)
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	if strings.TrimSpace(r.Header.Get(ipc.RequestIDHeader)) == "" {
		_ = ipc.WriteError(w, http.StatusBadRequest, "REQUEST_ID_REQUIRED", "修改监听端口必须提供 MM-Request-ID", false, nil)
		return
	}
	var request struct {
		ProfileID string `json:"profileId,omitempty"`
		Field     string `json:"field"`
		Port      int    `json:"port"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	value, err := h.service.SetListenerPort(r.Context(), request.ProfileID, request.Field, request.Port)
	writeListenerPortResult(w, value, err)
}

func writeListenerPortResult(w http.ResponseWriter, value ListenerPortStatus, err error) {
	if err == nil {
		_ = ipc.WriteJSON(w, http.StatusOK, "ListenerPortStatus", value)
		return
	}
	status, code, message, retryable := classifyCapabilityError(err)
	details := capabilityErrorDetails(err)
	if value.ProfileID != "" {
		if details == nil {
			details = map[string]any{}
		}
		details["status"] = value
	}
	_ = ipc.WriteError(w, status, code, message, retryable, details)
}

func (h *capabilityHandler) configEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		value, err := h.service.ReadConfig(r.Context(), profileQuery(r))
		writeCapabilityResult(w, "ConfigDocument", value, err)
		return
	}
	if !requireMethod(w, r, http.MethodPut) {
		return
	}
	var request struct {
		ProfileID      string `json:"profileId,omitempty"`
		ExpectedSHA256 string `json:"expectedSha256"`
		Content        []byte `json:"content"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	err := h.service.ReplaceConfig(r.Context(), request.ProfileID, request.ExpectedSHA256, request.Content)
	writeCapabilityResult(w, "ConfigAction", map[string]string{"action": "edit", "profileId": request.ProfileID}, err)
}

func (h *capabilityHandler) groups(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	value, err := h.service.Groups(r.Context(), profileQuery(r))
	writeCapabilityResult(w, "GroupList", value, err)
}

func (h *capabilityHandler) group(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/groups/"))
	if groupID == "" {
		writeCapabilityError(w, errors.New("group id is required"))
		return
	}
	if r.Method == http.MethodGet {
		value, err := h.service.Group(r.Context(), profileQuery(r), groupID)
		writeCapabilityResult(w, "Group", value, err)
		return
	}
	if r.Method != http.MethodPost {
		requireMethod(w, r, http.MethodPost)
		return
	}
	var request struct {
		ProfileID string `json:"profileId,omitempty"`
		NodeID    string `json:"nodeId"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	if strings.TrimSpace(request.NodeID) == "" {
		writeCapabilityError(w, errors.New("node id is required"))
		return
	}
	err := h.service.SelectGroupNode(r.Context(), request.ProfileID, groupID, request.NodeID)
	writeCapabilityResult(w, "GroupSelection", map[string]string{"groupId": groupID, "nodeId": request.NodeID}, err)
}

func (h *capabilityHandler) nodes(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	value, err := h.service.Nodes(r.Context(), profileQuery(r), r.URL.Query().Get("groupId"))
	writeCapabilityResult(w, "NodeList", value, err)
}

func (h *capabilityHandler) nodeTest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request struct {
		ProfileID   string `json:"profileId,omitempty"`
		GroupID     string `json:"groupId,omitempty"`
		Concurrency int    `json:"concurrency"`
		Limit       int    `json:"limit"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	if request.Concurrency < 1 || request.Concurrency > 64 || request.Limit < 0 || request.Limit > 1000 {
		writeCapabilityError(w, errors.New("invalid node test limits"))
		return
	}
	stream, err := h.service.TestNodes(r.Context(), request.ProfileID, request.GroupID, request.Concurrency, request.Limit)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	writeNodeTestStream(w, r, stream)
}

func (h *capabilityHandler) nodeTestSingle(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request struct {
		ProfileID string `json:"profileId,omitempty"`
		GroupID   string `json:"groupId,omitempty"`
		NodeID    string `json:"nodeId"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	if strings.TrimSpace(request.NodeID) == "" {
		writeCapabilityError(w, errors.New("node id is required"))
		return
	}
	stream, err := h.service.TestNode(r.Context(), request.ProfileID, request.GroupID, request.NodeID)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	writeNodeTestStream(w, r, stream)
}

func writeNodeTestStream(w http.ResponseWriter, r *http.Request, stream <-chan mihomo.NodeTestEvent) {
	writer := ipc.NewStreamWriter(w)
	for event := range stream {
		if event.Err != nil {
			_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "error", Error: capabilityErrorBody(event.Err)})
			flushResponse(w)
			return
		}
		if event.Finished {
			data, _ := json.Marshal(map[string]int{"done": event.Done, "total": event.Total})
			_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "done", Data: data})
			flushResponse(w)
			return
		}
		if event.Result == nil {
			_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "error", Error: capabilityErrorBody(errors.New("node test returned an empty result"))})
			flushResponse(w)
			return
		}
		status := event.Result.Status
		if status == "" {
			status = mihomo.NodeTestStatusFailed
			if event.Result.Delay > 0 {
				status = mihomo.NodeTestStatusSuccess
			}
		}
		value := map[string]any{"done": event.Done, "total": event.Total, "name": event.Result.Name, "delayMs": event.Result.Delay, "status": status}
		if !event.Result.TestedAt.IsZero() {
			value["testedAt"] = event.Result.TestedAt
		}
		data, _ := json.Marshal(value)
		if err := writer.Write(r.Context(), ipc.StreamEvent{Kind: "event", Data: data}); err != nil {
			return
		}
		flushResponse(w)
	}
}

func (h *capabilityHandler) subscription(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		value, err := h.service.Subscription(r.Context(), profileQuery(r))
		writeCapabilityResult(w, "Subscription", value, err)
	case http.MethodPut:
		var request struct {
			ProfileID string `json:"profileId,omitempty"`
			URL       string `json:"url"`
		}
		if err := ipc.DecodeJSON(w, r, &request); err != nil {
			return
		}
		if strings.TrimSpace(request.URL) == "" {
			writeCapabilityError(w, errors.New("subscription URL is required"))
			return
		}
		err := h.service.SetSubscription(r.Context(), request.ProfileID, request.URL)
		writeCapabilityResult(w, "SubscriptionSet", map[string]string{"profileId": request.ProfileID}, err)
	case http.MethodPost:
		var request struct {
			ProfileID string `json:"profileId,omitempty"`
		}
		if err := ipc.DecodeJSON(w, r, &request); err != nil {
			return
		}
		err := h.service.UpdateSubscription(r.Context(), request.ProfileID)
		writeCapabilityResult(w, "SubscriptionUpdate", map[string]string{"profileId": request.ProfileID}, err)
	default:
		requireMethod(w, r, http.MethodGet, http.MethodPut, http.MethodPost)
	}
}

func (h *capabilityHandler) whitelist(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		value, err := h.service.Whitelist(r.Context(), profileQuery(r))
		writeCapabilityResult(w, "Whitelist", value, err)
		return
	}
	var request struct {
		ProfileID string `json:"profileId,omitempty"`
		Domain    string `json:"domain,omitempty"`
		OldDomain string `json:"oldDomain,omitempty"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	var err error
	var action string
	switch r.Method {
	case http.MethodPost:
		action, err = "add", h.service.AddWhitelist(r.Context(), request.ProfileID, request.Domain)
	case http.MethodPut:
		action, err = "edit", h.service.EditWhitelist(r.Context(), request.ProfileID, request.OldDomain, request.Domain)
	case http.MethodDelete:
		action, err = "remove", h.service.RemoveWhitelist(r.Context(), request.ProfileID, request.Domain)
	default:
		requireMethod(w, r, http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete)
		return
	}
	writeCapabilityResult(w, "WhitelistMutation", map[string]string{"action": action, "domain": request.Domain}, err)
}

func (h *capabilityHandler) routePreset(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var request struct {
		ProfileID string `json:"profileId,omitempty"`
		Preset    string `json:"preset"`
	}
	if err := ipc.DecodeJSON(w, r, &request); err != nil {
		return
	}
	err := h.service.ApplyRoutePreset(r.Context(), request.ProfileID, request.Preset)
	writeCapabilityResult(w, "RoutePreset", map[string]string{"preset": request.Preset}, err)
}

func (h *capabilityHandler) routeDiagnose(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	input := strings.TrimSpace(r.URL.Query().Get("input"))
	if input == "" {
		writeCapabilityError(w, errors.New("route diagnosis input is required"))
		return
	}
	value, err := h.service.DiagnoseRoute(r.Context(), profileQuery(r), input)
	writeCapabilityResult(w, "RouteDiagnosis", value, err)
}

func (h *capabilityHandler) connections(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	value, err := h.service.Connections(r.Context(), profileQuery(r))
	writeCapabilityResult(w, "RouteConnections", value, err)
}

func (h *capabilityHandler) connectionFollow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	stream, err := h.service.FollowConnections(r.Context(), profileQuery(r))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	writeConnectionStream(w, r, stream)
}

func writeConnectionStream(w http.ResponseWriter, r *http.Request, stream <-chan ConnectionEvent) {
	writer := ipc.NewStreamWriter(w)
	for event := range stream {
		if event.Err != nil {
			_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "error", Error: capabilityErrorBody(event.Err)})
			flushResponse(w)
			return
		}
		if event.Finished {
			break
		}
		data, err := json.Marshal(event)
		if err != nil {
			_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "error", Error: capabilityErrorBody(err)})
			flushResponse(w)
			return
		}
		if err := writer.Write(r.Context(), ipc.StreamEvent{Kind: "event", Data: data}); err != nil {
			return
		}
		flushResponse(w)
	}
	if r.Context().Err() != nil {
		return
	}
	_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "done"})
	flushResponse(w)
}

func (h *capabilityHandler) logs(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	lines, err := positiveIntQuery(r, "lines", 50, 10000)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	value, err := h.service.TailLogs(r.Context(), profileQuery(r), lines)
	writeCapabilityResult(w, "CoreLogs", map[string]string{"content": value}, err)
}

func (h *capabilityHandler) logFollow(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	lines, err := positiveIntQuery(r, "lines", 50, 10000)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	stream, err := h.service.FollowLogs(r.Context(), profileQuery(r), lines)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	writer := ipc.NewStreamWriter(w)
	for event := range stream {
		if event.Err != nil {
			_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "error", Error: capabilityErrorBody(event.Err)})
			flushResponse(w)
			return
		}
		if event.Finished {
			_ = writer.Write(r.Context(), ipc.StreamEvent{Kind: "done"})
			flushResponse(w)
			return
		}
		data, _ := json.Marshal(map[string]string{"line": event.Line})
		if err := writer.Write(r.Context(), ipc.StreamEvent{Kind: "event", Data: data}); err != nil {
			return
		}
		flushResponse(w)
	}
}

func requireMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, method := range allowed {
		if r.Method == method {
			return true
		}
	}
	_ = ipc.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求方法无效", false, map[string]any{"allowed": allowed})
	return false
}

func positiveIntQuery(r *http.Request, key string, fallback, maximum int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 || parsed > maximum {
		return 0, fmt.Errorf("%s must be between 1 and %d", key, maximum)
	}
	return parsed, nil
}

func writeCapabilityResult(w http.ResponseWriter, kind string, value any, err error) {
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	_ = ipc.WriteJSON(w, http.StatusOK, kind, value)
}

func capabilityErrorBody(err error) *ipc.ErrorBody {
	status, code, message, retryable := classifyCapabilityError(err)
	_ = status
	return &ipc.ErrorBody{Code: code, Message: message, Retryable: retryable, Details: capabilityErrorDetails(err)}
}

func writeCapabilityError(w http.ResponseWriter, err error) {
	status, code, message, retryable := classifyCapabilityError(err)
	_ = ipc.WriteError(w, status, code, message, retryable, capabilityErrorDetails(err))
}

func capabilityErrorDetails(err error) map[string]any {
	var conflictErr *core.PortConflictError
	if errors.As(err, &conflictErr) {
		return map[string]any{"conflicts": conflictErr.Conflicts}
	}
	if errors.Is(err, ruleset.ErrNotReady) {
		return map[string]any{"hint": "mm ruleset install"}
	}
	return nil
}

func classifyCapabilityError(err error) (int, string, string, bool) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout, "REQUEST_CANCELLED", "请求已取消", true
	case errors.Is(err, legacy.ErrRestoreFailed):
		return http.StatusInternalServerError, "RESTORE_FAILED", "模式切换恢复失败", false
	case errors.Is(err, ErrReconfigureRestoreFailed):
		return http.StatusInternalServerError, "RESTORE_FAILED", "端口配置或旧 core 恢复失败", false
	case errors.Is(err, legacy.ErrConnectionCloseFailed):
		return http.StatusFailedDependency, "CONNECTION_CLOSE_FAILED", "模式已生效，但关闭活动连接失败", true
	case errors.Is(err, core.ErrPortConflict):
		return http.StatusConflict, "PORT_CONFLICT", err.Error(), false
	case errors.Is(err, ruleset.ErrNotReady):
		return http.StatusConflict, "RULESET_NOT_READY", "CN 规则集尚未安装；请执行 mm ruleset install", false
	case errors.Is(err, ruleset.ErrRestoreFailed):
		return http.StatusInternalServerError, "RESTORE_FAILED", "规则集发布恢复失败，Core 状态可能不确定", false
	case errors.Is(err, ruleset.ErrProxyAuthUnsupported):
		return http.StatusBadRequest, "RULESET_PROXY_AUTH_UNSUPPORTED", "规则集下载不支持带认证信息的代理", false
	case errors.Is(err, ErrCapabilityNotFound), errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND", "资源不存在", false
	case errors.Is(err, mihomo.ErrProxyNotFound):
		return http.StatusNotFound, "NOT_FOUND", "节点或代理组不存在", false
	case errors.Is(err, mihomo.ErrNodeNotInGroup), errors.Is(err, mihomo.ErrNodeNotTestable):
		return http.StatusBadRequest, "INVALID_REQUEST", "节点不属于代理组或不可测速", false
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest, "INVALID_REQUEST", "请求参数无效", false
	case errors.Is(err, ErrOperationConflict), errors.Is(err, legacy.ErrConflict), errors.Is(err, store.ErrConflict):
		return http.StatusConflict, "CONFLICT", "操作状态冲突", false
	case errors.Is(err, ErrCapabilityUnsupported):
		return http.StatusConflict, "PROFILE_MODE_UNSUPPORTED", "当前档案不支持此操作", false
	case errors.Is(err, ErrConnectionRuntimeUnavailable):
		return http.StatusConflict, "CORE_NOT_RUNNING", "mihomo core 未运行，无法读取活动连接", true
	case errors.Is(err, legacy.ErrInvalidRoutingMode):
		return http.StatusBadRequest, "INVALID_ROUTING_MODE", "路由模式必须为 global、rule 或 direct", false
	case errors.Is(err, legacy.ErrConfigChanged):
		return http.StatusConflict, "CONFIG_CHANGED", "配置已被外部修改", false
	case errors.Is(err, legacy.ErrModeRuntimeMismatch):
		return http.StatusBadGateway, "MODE_RUNTIME_MISMATCH", "mihomo 运行模式核验失败", true
	case errors.Is(err, legacy.ErrValidation), errors.Is(err, core.ErrInvalidConfig), errors.Is(err, core.ErrConfigChanged):
		return http.StatusUnprocessableEntity, "VALIDATION_FAILED", "配置校验失败", false
	case errors.Is(err, platform.ErrUnsupported):
		return http.StatusNotImplemented, "PLATFORM_UNSUPPORTED", "macOS 系统代理暂不支持；请在系统设置中手动配置代理", false
	case errors.Is(err, platform.ErrProxyAuthUnsupported):
		return http.StatusConflict, "PROXY_AUTH_UNSUPPORTED", "GNOME 代理已启用认证，首版不接管认证代理", false
	case errors.Is(err, platform.ErrProxySnapshotNotFound), errors.Is(err, ErrProxySnapshotMissing):
		return http.StatusNotFound, "PROXY_SNAPSHOT_NOT_FOUND", "没有可恢复的代理快照", false
	case errors.Is(err, platform.ErrProxyConflict):
		return http.StatusConflict, "CONFLICT", "代理配置已被外部修改", false
	case errors.Is(err, platform.ErrProxyRestoreFailed):
		return http.StatusInternalServerError, "RESTORE_FAILED", "代理配置回滚失败", false
	case errors.Is(err, platform.ErrProxyEndpointInvalid), errors.Is(err, ErrProxyEndpointMissing), errors.Is(err, ErrProxyRequestInvalid):
		return http.StatusBadRequest, "INVALID_REQUEST", "代理 IP、端口或 listener 参数无效", false
	case errors.Is(err, legacy.ErrUnsafePath), errors.Is(err, store.ErrPermission):
		return http.StatusForbidden, "PERMISSION_DENIED", "路径或权限不安全", false
	case errors.Is(err, os.ErrPermission):
		return http.StatusForbidden, "PERMISSION_DENIED", "代理配置文件路径或权限不安全", false
	case errors.Is(err, ErrProxyStateCorrupt):
		return http.StatusInternalServerError, "INTERNAL", "代理状态文件损坏", false
	case errors.Is(err, store.ErrClosed), errors.Is(err, store.ErrCorrupt), errors.Is(err, store.ErrSchemaTooNew), errors.Is(err, store.ErrMigrationDrift):
		return http.StatusInternalServerError, "INTERNAL", "daemon 状态存储不可用", false
	}
	if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") || strings.Contains(err.Error(), "unknown") || strings.Contains(err.Error(), "empty") {
		return http.StatusBadRequest, "INVALID_REQUEST", "请求参数无效", false
	}
	return http.StatusBadGateway, "UPSTREAM_FAILURE", "mihomo 或 legacy 操作失败", true
}

func flushResponse(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
