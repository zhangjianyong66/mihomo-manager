package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

func TestRouteConnectionsSnapshotTableUsesRuntimeChains(t *testing.T) {
	fake := &fakeCapabilityAPI{connections: []app.Connection{{
		ID: "conn-a", Host: "example.com", DestinationIP: "203.0.113.1", DestinationPort: 443,
		Network: "tcp", Rule: "RuleSet", RulePayload: "mm-cn-domain",
		Chains: []string{"node-a", "GLOBAL"}, FinalNode: "node-a", Upload: 1024, Download: 2048,
	}}}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"route", "connections"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, expected := range []string{"example.com:443", "RuleSet:mm-cn-domain", "node-a -> GLOBAL", "node-a", "1.0 KiB", "2.0 KiB"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("output missing %q: %q", expected, stdout.String())
		}
	}
}

func TestRouteConnectionsFollowNDJSONHasMonotonicTerminalSequence(t *testing.T) {
	connection := app.Connection{ID: "a", Host: "example.com", DestinationPort: 443, Rule: "Match", Chains: []string{"node-a", "GLOBAL"}, FinalNode: "node-a"}
	fake := &fakeCapabilityAPI{connectionEvents: []app.ConnectionEvent{
		{Seq: 1, Action: app.ConnectionActionOpen, Connection: &connection, Time: time.Date(2026, 7, 22, 10, 20, 30, 0, time.UTC)},
		{Seq: 2, Action: app.ConnectionActionUpdate, Connection: &connection, Time: time.Date(2026, 7, 22, 10, 20, 31, 0, time.UTC)},
		{Seq: 3, Action: app.ConnectionActionClosed, Connection: &connection, Time: time.Date(2026, 7, 22, 10, 20, 32, 0, time.UTC)},
		{Seq: 4, Finished: true},
	}}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"route", "connections", "--follow", "--output", "ndjson"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines=%q", lines)
	}
	for index, line := range lines {
		var value map[string]any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("line %d is invalid JSON: %v", index, err)
		}
		if value["seq"] != float64(index+1) {
			t.Fatalf("line %d seq=%v", index, value["seq"])
		}
	}
	if !strings.Contains(lines[0], `"action":"open"`) || !strings.Contains(lines[2], `"action":"closed"`) || !strings.Contains(lines[3], `"kind":"ConnectionStreamDone"`) {
		t.Fatalf("lines=%q", lines)
	}
}

func TestRouteConnectionsFollowNDJSONWritesOneTerminalError(t *testing.T) {
	fake := &fakeCapabilityAPI{connectionEvents: []app.ConnectionEvent{{Seq: 1, Err: &app.Error{Category: app.ErrorCategoryUpstreamFailure, Code: app.ErrorCodeUpstreamFailure, Message: "上游不可用"}, Finished: true}}}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"route", "connections", "--follow", "--output=ndjson"}, nil, &stdout, &stderr)
	if code != ExitUpstreamFailure || stderr.Len() != 0 || strings.Count(strings.TrimSpace(stdout.String()), "\n") != 0 || !strings.Contains(stdout.String(), `"kind":"ConnectionStreamError"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRouteConnectionsSnapshotJSONIsStable(t *testing.T) {
	fake := &fakeCapabilityAPI{connections: []app.Connection{{ID: "conn-v6", DestinationIP: "2001:db8::1", DestinationPort: 53, Chains: []string{"DIRECT"}, FinalNode: "DIRECT"}}}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), Dependencies{Capabilities: fake}, []string{"route", "connections", "--output", "json"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	var envelope struct {
		APIVersion string           `json:"apiVersion"`
		Kind       string           `json:"kind"`
		Data       []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.APIVersion != "mm/v1" || envelope.Kind != "RouteConnections" || len(envelope.Data) != 1 || envelope.Data[0]["target"] != "[2001:db8::1]:53" || envelope.Data[0]["finalNode"] != "DIRECT" {
		t.Fatalf("envelope=%+v", envelope)
	}
}
