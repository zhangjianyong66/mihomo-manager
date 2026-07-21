package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
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
