package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/daemon"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
)

func TestConvertGroupPreservesAdditiveNodeStatesByID(t *testing.T) {
	testedAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	group := convertGroup(daemon.GroupInfo{
		ID: "GLOBAL", Nodes: []string{"node-a", "node-b"},
		NodeStates: []daemon.NodeStateInfo{
			{NodeID: "node-b", Testable: true, Latest: &daemon.NodeTestResultInfo{Status: "failed", TestedAt: testedAt}},
			{NodeID: "node-a", Testable: true, Latest: &daemon.NodeTestResultInfo{Status: "success", DelayMS: 20, TestedAt: testedAt}},
		},
	})
	if len(group.NodeStates) != 2 || group.NodeStates[0].NodeID != "node-b" || group.NodeStates[1].Latest.Delay != 20*time.Millisecond {
		t.Fatalf("node states were not preserved by id: %+v", group)
	}
}

func TestDaemonCapabilitiesNodeTestSelectsSingleRouteAndDecodesStatus(t *testing.T) {
	testedAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	var requestedPath string
	var requestedBody map[string]any
	streamBody := nodeTestIPCStream(t,
		map[string]any{"done": 1, "total": 1, "name": "node-a", "status": "failed", "delayMs": -1, "testedAt": testedAt},
		map[string]any{"done": 1, "total": 1},
	)
	httpClient := &http.Client{Transport: modeRoundTripper(func(request *http.Request) (*http.Response, error) {
		requestedPath = request.URL.Path
		if err := json.NewDecoder(request.Body).Decode(&requestedBody); err != nil {
			t.Fatal(err)
		}
		response := connectionIPCResponse(http.StatusOK, streamBody)
		response.Header.Set("Content-Type", "application/x-ndjson")
		return response, nil
	})}
	service := &DaemonCapabilities{client: &ipc.Client{HTTPClient: httpClient, MinVersion: 1, MaxVersion: 1}}
	events := service.TestNodes(context.Background(), NodeTestRequest{ProfileID: "legacy", GroupID: "GLOBAL", NodeID: "node-a"})
	first := <-events
	terminal := <-events
	if requestedPath != "/v1/nodes/test-single" || requestedBody["nodeId"] != "node-a" || first.Result == nil || first.Result.Status != NodeTestStatusFailed || !first.Result.TestedAt.Equal(testedAt) || !terminal.Finished {
		t.Fatalf("unexpected single route/decode: path=%q body=%v first=%+v terminal=%+v", requestedPath, requestedBody, first, terminal)
	}
}

func TestDaemonCapabilitiesNodeTestKeepsBatchRouteAndOldEventCompatibility(t *testing.T) {
	var requestedPath string
	streamBody := nodeTestIPCStream(t,
		map[string]any{"done": 1, "total": 1, "name": "node-a", "delayMs": 30},
		map[string]any{"done": 1, "total": 1},
	)
	httpClient := &http.Client{Transport: modeRoundTripper(func(request *http.Request) (*http.Response, error) {
		requestedPath = request.URL.Path
		response := connectionIPCResponse(http.StatusOK, streamBody)
		response.Header.Set("Content-Type", "application/x-ndjson")
		return response, nil
	})}
	service := &DaemonCapabilities{client: &ipc.Client{HTTPClient: httpClient, MinVersion: 1, MaxVersion: 1}}
	first := <-service.TestNodes(context.Background(), NodeTestRequest{GroupID: domain.GroupID("GLOBAL"), Concurrency: 5})
	if requestedPath != "/v1/nodes/test" || first.Result == nil || first.Result.Status != NodeTestStatusSuccess || first.Result.Delay != 30*time.Millisecond {
		t.Fatalf("old batch event was not decoded: path=%q event=%+v", requestedPath, first)
	}
}

func nodeTestIPCStream(t *testing.T, event, terminal map[string]any) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := ipc.NewStreamWriter(&buffer)
	eventData, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	terminalData, err := json.Marshal(terminal)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(context.Background(), ipc.StreamEvent{Kind: "event", Data: eventData}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(context.Background(), ipc.StreamEvent{Kind: "done", Data: terminalData}); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}
