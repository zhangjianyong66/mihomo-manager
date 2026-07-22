package mihomo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
)

func TestGroupDetailParsesLatestHistoryAndTestableNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"proxies": {
				"GLOBAL": {"type":"Selector","now":"node-a","all":["node-a","node-b","nested","DIRECT"]},
				"node-a": {"type":"VLESS","history":[{"time":"2026-07-22T11:00:00Z","delay":80},{"time":"2026-07-22T12:00:00Z","delay":120}]},
				"node-b": {"type":"Trojan","history":[{"time":"2026-07-22T12:01:00Z","delay":0}]},
				"nested": {"type":"Selector","all":["node-a"]},
				"DIRECT": {"type":"Direct","history":null}
			}
		}`))
	}))
	defer server.Close()

	client := New(config.Paths{APIAddr: server.URL})
	group, err := client.GroupDetail("GLOBAL")
	if err != nil {
		t.Fatal(err)
	}
	if group.Now != "node-a" || len(group.NodeStates) != 4 {
		t.Fatalf("unexpected group: %+v", group)
	}
	states := map[string]ProxyNodeState{}
	for _, state := range group.NodeStates {
		states[state.Name] = state
	}
	if !states["node-a"].Testable || states["node-a"].Latest == nil || states["node-a"].Latest.Delay != 120 || states["node-a"].Latest.Status != NodeTestStatusSuccess {
		t.Fatalf("unexpected successful history: %+v", states["node-a"])
	}
	if states["node-b"].Latest == nil || states["node-b"].Latest.Status != NodeTestStatusFailed {
		t.Fatalf("unexpected failed history: %+v", states["node-b"])
	}
	if states["nested"].Testable || states["DIRECT"].Testable || states["DIRECT"].Latest != nil {
		t.Fatalf("non-leaf nodes became testable: %+v", states)
	}
}

func TestSingleNodeTestValidatesMembershipAndMakesOneDelayRequest(t *testing.T) {
	var delayCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxies":
			_, _ = w.Write([]byte(`{"proxies":{"GLOBAL":{"type":"Selector","all":["node-a"]},"node-a":{"type":"VLESS"},"node-b":{"type":"VLESS"}}}`))
		case "/proxies/node-a/delay":
			delayCalls.Add(1)
			_, _ = w.Write([]byte(`{"delay":25}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New(config.Paths{APIAddr: server.URL})

	events := collectNodeTestEvents(client.TestNodeStreamWithStop("GLOBAL", "node-a", nil))
	if len(events) != 2 || events[0].Result == nil || events[0].Result.Status != NodeTestStatusSuccess || events[0].Total != 1 || !events[1].Finished || delayCalls.Load() != 1 {
		t.Fatalf("unexpected single-node stream: events=%+v calls=%d", events, delayCalls.Load())
	}

	events = collectNodeTestEvents(client.TestNodeStreamWithStop("GLOBAL", "node-b", nil))
	if len(events) != 1 || !events[0].Finished || !strings.Contains(events[0].Err.Error(), ErrNodeNotInGroup.Error()) || delayCalls.Load() != 1 {
		t.Fatalf("membership validation was not enforced: events=%+v calls=%d", events, delayCalls.Load())
	}
}

func TestGroupNodeTestLimitZeroDoesNotTruncateAt120(t *testing.T) {
	const nodeCount = 125
	proxies := map[string]any{}
	members := make([]string, 0, nodeCount)
	for index := 0; index < nodeCount; index++ {
		name := fmt.Sprintf("node-%03d", index)
		members = append(members, name)
		proxies[name] = map[string]any{"type": "VLESS"}
	}
	proxies["GLOBAL"] = map[string]any{"type": "Selector", "all": members}
	payload, err := json.Marshal(map[string]any{"proxies": proxies})
	if err != nil {
		t.Fatal(err)
	}
	var delayCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			_, _ = w.Write(payload)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/delay") {
			delayCalls.Add(1)
			_, _ = w.Write([]byte(`{"delay":10}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := New(config.Paths{APIAddr: server.URL})

	events := collectNodeTestEvents(client.TestGroupNodesStreamWithStop("GLOBAL", 5, 0, nil))
	if len(events) != nodeCount+1 || !events[len(events)-1].Finished || events[len(events)-1].Done != nodeCount || events[len(events)-1].Total != nodeCount || delayCalls.Load() != nodeCount {
		t.Fatalf("batch was truncated: events=%d terminal=%+v calls=%d", len(events), events[len(events)-1], delayCalls.Load())
	}
}

func TestNodeTestCancellationCancelsDelayRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxies":
			_, _ = w.Write([]byte(`{"proxies":{"GLOBAL":{"type":"Selector","all":["node-a"]},"node-a":{"type":"VLESS"}}}`))
		case "/proxies/node-a/delay":
			close(requestStarted)
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New(config.Paths{APIAddr: server.URL})
	stop := make(chan struct{})
	stream := client.TestNodeStreamWithStop("GLOBAL", "node-a", stop)
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("delay request did not start")
	}
	close(stop)
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("cancelled stream emitted a result")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled stream did not close")
	}
}

func collectNodeTestEvents(stream <-chan NodeTestEvent) []NodeTestEvent {
	result := make([]NodeTestEvent, 0)
	for event := range stream {
		result = append(result, event)
	}
	return result
}
