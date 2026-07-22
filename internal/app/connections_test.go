package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
)

func TestDaemonCapabilitiesConnectionsDecodesTypedSnapshot(t *testing.T) {
	httpClient := &http.Client{Transport: modeRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/connections" || request.URL.Query().Get("profileId") != "legacy-mihomo" {
			t.Fatalf("request=%s %s", request.Method, request.URL.String())
		}
		return connectionIPCResponse(http.StatusOK, `{"apiVersion":"mm/v1","kind":"RouteConnections","data":[{"id":"a","host":"example.com","destinationIp":"203.0.113.1","destinationPort":443,"network":"tcp","rule":"RuleSet","rulePayload":"mm-cn-domain","chains":["node-a","GLOBAL"],"finalNode":"node-a","upload":12,"download":34,"start":"2026-07-22T10:20:30Z","warnings":[]}],"warnings":[]}`), nil
	})}
	service := &DaemonCapabilities{client: &ipc.Client{HTTPClient: httpClient, MinVersion: 1, MaxVersion: 1}}
	values, err := service.Connections(context.Background(), ConnectionRequest{ProfileID: "legacy-mihomo"})
	if err != nil || len(values) != 1 {
		t.Fatalf("connections=%+v err=%v", values, err)
	}
	if values[0].FinalNode != "node-a" || len(values[0].Chains) != 2 || values[0].DestinationPort != 443 {
		t.Fatalf("connection=%+v", values[0])
	}
}

func TestDaemonCapabilitiesFollowConnectionsPreservesSequenceAndTerminal(t *testing.T) {
	var buffer bytes.Buffer
	writer := ipc.NewStreamWriter(&buffer)
	data := []byte(`{"action":"open","time":"2026-07-22T10:20:30Z","connection":{"id":"a","destinationIp":"2001:db8::1","destinationPort":443,"chains":["node-a","GLOBAL"],"finalNode":"node-a","warnings":[]}}`)
	if err := writer.Write(context.Background(), ipc.StreamEvent{Kind: "event", Data: data}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(context.Background(), ipc.StreamEvent{Kind: "done"}); err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Transport: modeRoundTripper(func(*http.Request) (*http.Response, error) {
		response := connectionIPCResponse(http.StatusOK, buffer.String())
		response.Header.Set("Content-Type", "application/x-ndjson")
		return response, nil
	})}
	service := &DaemonCapabilities{client: &ipc.Client{HTTPClient: httpClient, MinVersion: 1, MaxVersion: 1}}
	events := service.FollowConnections(context.Background(), ConnectionRequest{})
	first := <-events
	if first.Seq != 1 || first.Action != ConnectionActionOpen || first.Connection == nil || first.Connection.FinalNode != "node-a" || !first.Time.Equal(time.Date(2026, 7, 22, 10, 20, 30, 0, time.UTC)) {
		t.Fatalf("first=%+v", first)
	}
	terminal := <-events
	if terminal.Seq != 2 || !terminal.Finished || terminal.Err != nil {
		t.Fatalf("terminal=%+v", terminal)
	}
	if _, ok := <-events; ok {
		t.Fatal("stream did not close after terminal")
	}
}

func TestDaemonCapabilitiesConnectionsMapsUnavailableRuntime(t *testing.T) {
	httpClient := &http.Client{Transport: modeRoundTripper(func(*http.Request) (*http.Response, error) {
		return connectionIPCResponse(http.StatusConflict, `{"apiVersion":"mm/v1","kind":"Error","warnings":[],"error":{"code":"CORE_NOT_RUNNING","message":"mihomo core 未运行，无法读取活动连接","retryable":true,"details":{}}}`), nil
	})}
	service := &DaemonCapabilities{client: &ipc.Client{HTTPClient: httpClient, MinVersion: 1, MaxVersion: 1}}
	_, err := service.Connections(context.Background(), ConnectionRequest{})
	if err == nil || !strings.Contains(err.Error(), "core 未运行") {
		t.Fatalf("err=%v", err)
	}
}

func connectionIPCResponse(status int, body string) *http.Response {
	header := make(http.Header)
	header.Set(ipc.ProtocolHeader, "1")
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}
