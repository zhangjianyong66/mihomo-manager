package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/daemon"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

type modeRoundTripper func(*http.Request) (*http.Response, error)

func (f modeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestDaemonCapabilitiesSetModeMapsStatusAndPartialFailure(t *testing.T) {
	responses := []struct {
		status int
		body   string
	}{
		{status: http.StatusOK, body: `{"apiVersion":"mm/v1","kind":"RoutingModeStatus","data":{"profileId":"legacy-mihomo","configMode":"rule","runtimeMode":"rule","runtimeAvailable":true,"coreState":"running","ruleSets":[],"activeConnections":2,"connectionsAvailable":true,"connectionsClosed":false,"nextStart":false,"warnings":[]},"warnings":[]}`},
		{status: http.StatusFailedDependency, body: `{"apiVersion":"mm/v1","kind":"Error","warnings":[],"error":{"code":"CONNECTION_CLOSE_FAILED","message":"模式已生效，但关闭活动连接失败","retryable":true,"details":{"status":{"profileId":"legacy-mihomo","configMode":"direct","runtimeMode":"direct","runtimeAvailable":true,"coreState":"running","ruleSets":[],"activeConnections":2,"connectionsAvailable":true,"connectionsClosed":false,"nextStart":false,"operationPhase":"connections_close_failed","warnings":["关闭失败"]}}}}`},
	}
	requests := 0
	httpClient := &http.Client{Transport: modeRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPut || request.URL.Path != "/v1/mode" || request.Header.Get(ipc.RequestIDHeader) != "mode-request" {
			t.Fatalf("unexpected request: %s %s id=%q", request.Method, request.URL.Path, request.Header.Get(ipc.RequestIDHeader))
		}
		response := responses[requests]
		requests++
		header := make(http.Header)
		header.Set(ipc.ProtocolHeader, "1")
		return &http.Response{
			StatusCode: response.status,
			Header:     header,
			Body:       io.NopCloser(strings.NewReader(response.body)),
		}, nil
	})}
	capabilities := &DaemonCapabilities{client: &ipc.Client{HTTPClient: httpClient, MinVersion: 1, MaxVersion: 1}}

	status, err := capabilities.SetMode(context.Background(), SetRoutingModeRequest{
		ProfileID: "legacy-mihomo", Mode: domain.RoutingModeRule, RequestID: "mode-request",
	})
	if err != nil || status.ConfigMode != domain.RoutingModeRule || status.RuntimeMode == nil || *status.RuntimeMode != domain.RoutingModeRule {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	status, err = capabilities.SetMode(context.Background(), SetRoutingModeRequest{
		ProfileID: "legacy-mihomo", Mode: domain.RoutingModeDirect, RequestID: "mode-request", CloseConnections: true,
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "CONNECTION_CLOSE_FAILED" || appErr.Category != ErrorCategoryUpstreamFailure {
		t.Fatalf("status=%+v err=%#v", status, err)
	}
	if status.ConfigMode != domain.RoutingModeDirect || status.OperationPhase != "connections_close_failed" || len(status.Warnings) != 1 {
		t.Fatalf("partial status=%+v", status)
	}
}

func TestDaemonCapabilitiesSetModeRejectsInvalidModeLocally(t *testing.T) {
	capabilities := &DaemonCapabilities{}
	_, err := capabilities.SetMode(context.Background(), SetRoutingModeRequest{Mode: "invalid"})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "INVALID_ROUTING_MODE" || appErr.Category != ErrorCategoryInvalidArgument {
		t.Fatalf("error=%#v", err)
	}
}

func TestConvertModeStatusAddsSanitizedEnvironmentDiagnosis(t *testing.T) {
	environment := map[string]string{
		"HTTP_PROXY":  "http://user:secret@127.0.0.1:7890/private?token=secret",
		"HTTPS_PROXY": "http://127.0.0.1:10808",
		"ALL_PROXY":   "socks5://[::1]:7891/query",
	}
	capabilities := &DaemonCapabilities{lookupEnv: func(key string) (string, bool) { value, ok := environment[key]; return value, ok }}
	status := capabilities.convertModeStatus(daemon.ModeStatus{
		Listeners: []platform.ProxyListener{{Protocol: "mixed", Host: "127.0.0.1", Port: 7890}, {Protocol: "socks", Host: "127.0.0.1", Port: 7891}},
		Warnings:  []string{}, SystemProxy: []platform.ProxySource{},
	})
	if len(status.EnvironmentProxy) != 3 || status.EnvironmentProxy[0].State != "matched" || status.EnvironmentProxy[1].State != "mismatched" || status.EnvironmentProxy[2].State != "matched" {
		t.Fatalf("environment=%+v", status.EnvironmentProxy)
	}
	encoded, err := json.Marshal(status.EnvironmentProxy)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"user", "secret", "private", "token", "query"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("environment diagnosis leaked %q: %s", secret, encoded)
		}
	}
	if len(status.Warnings) != 1 || !strings.Contains(status.Warnings[0], "普通应用流量不会进入 mihomo") {
		t.Fatalf("warnings=%v", status.Warnings)
	}
}
