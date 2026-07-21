package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/legacy"
	"github.com/zhangjianyong66/mihomo-manager/internal/store"
)

type capabilityHandler struct{ service *CapabilityService }

func registerCapabilityRoutes(mux *http.ServeMux, service *CapabilityService) {
	handler := &capabilityHandler{service: service}
	mux.HandleFunc("/v1/core/status", handler.coreStatus)
	mux.HandleFunc("/v1/core/", handler.coreAction)
	mux.HandleFunc("/v1/config/", handler.configAction)
	mux.HandleFunc("/v1/groups", handler.groups)
	mux.HandleFunc("/v1/groups/", handler.group)
	mux.HandleFunc("/v1/nodes", handler.nodes)
	mux.HandleFunc("/v1/nodes/test", handler.nodeTest)
	mux.HandleFunc("/v1/subscription", handler.subscription)
	mux.HandleFunc("/v1/routes/whitelist", handler.whitelist)
	mux.HandleFunc("/v1/routes/preset", handler.routePreset)
	mux.HandleFunc("/v1/routes/diagnose", handler.routeDiagnose)
	mux.HandleFunc("/v1/logs", handler.logs)
	mux.HandleFunc("/v1/logs/follow", handler.logFollow)
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
		data, _ := json.Marshal(map[string]any{"done": event.Done, "total": event.Total, "name": event.Result.Name, "delayMs": event.Result.Delay})
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
	return &ipc.ErrorBody{Code: code, Message: message, Retryable: retryable}
}

func writeCapabilityError(w http.ResponseWriter, err error) {
	status, code, message, retryable := classifyCapabilityError(err)
	_ = ipc.WriteError(w, status, code, message, retryable, nil)
}

func classifyCapabilityError(err error) (int, string, string, bool) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout, "REQUEST_CANCELLED", "请求已取消", true
	case errors.Is(err, ErrCapabilityNotFound), errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "NOT_FOUND", "资源不存在", false
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest, "INVALID_REQUEST", "请求参数无效", false
	case errors.Is(err, ErrOperationConflict), errors.Is(err, legacy.ErrConflict), errors.Is(err, store.ErrConflict):
		return http.StatusConflict, "CONFLICT", "操作状态冲突", false
	case errors.Is(err, ErrCapabilityUnsupported):
		return http.StatusConflict, "UNSUPPORTED_PROFILE", "当前档案不支持此操作", false
	case errors.Is(err, legacy.ErrValidation), errors.Is(err, core.ErrInvalidConfig), errors.Is(err, core.ErrConfigChanged):
		return http.StatusUnprocessableEntity, "VALIDATION_FAILED", "配置校验失败", false
	case errors.Is(err, legacy.ErrUnsafePath), errors.Is(err, store.ErrPermission):
		return http.StatusForbidden, "PERMISSION_DENIED", "路径或权限不安全", false
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
