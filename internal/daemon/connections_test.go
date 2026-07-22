package daemon

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

func TestProduceConnectionEventsEmitsOpenUpdateClosedAndError(t *testing.T) {
	snapshots := [][]mihomo.RuntimeConnection{
		{{ID: "a", Host: "one.example", Upload: 1, Chains: []string{"node-a", "GLOBAL"}}},
		{{ID: "a", Host: "one.example", Upload: 2, Chains: []string{"node-a", "GLOBAL"}}, {ID: "b", DestinationIP: "2001:db8::1", Chains: []string{"DIRECT"}}},
		{{ID: "b", DestinationIP: "2001:db8::1", Chains: []string{"DIRECT"}}},
	}
	var call atomic.Int32
	upstreamErr := errors.New("upstream unavailable")
	output := make(chan ConnectionEvent, 16)
	go produceConnectionEvents(context.Background(), output, time.Millisecond, func() time.Time {
		return time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	}, func(context.Context) ([]mihomo.RuntimeConnection, error) {
		index := int(call.Add(1)) - 1
		if index >= len(snapshots) {
			return nil, upstreamErr
		}
		return snapshots[index], nil
	})

	var events []ConnectionEvent
	for event := range output {
		events = append(events, event)
	}
	if len(events) != 5 {
		t.Fatalf("events=%+v", events)
	}
	wantActions := []string{"open", "update", "open", "closed"}
	wantIDs := []string{"a", "a", "b", "a"}
	for index := range wantActions {
		if events[index].Action != wantActions[index] || events[index].Connection.ID != wantIDs[index] {
			t.Fatalf("event[%d]=%+v", index, events[index])
		}
	}
	if !errors.Is(events[4].Err, upstreamErr) || !events[4].Finished {
		t.Fatalf("terminal=%+v", events[4])
	}
}

func TestWriteConnectionStreamProducesMonotonicTerminalNDJSON(t *testing.T) {
	stream := make(chan ConnectionEvent, 3)
	stream <- ConnectionEvent{Action: "open", Connection: ConnectionInfo{ID: "a", Chains: []string{}, Warnings: []string{}}, Time: time.Now()}
	stream <- ConnectionEvent{Action: "closed", Connection: ConnectionInfo{ID: "a", Chains: []string{}, Warnings: []string{}}, Time: time.Now()}
	close(stream)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/v1/connections/follow", nil)
	writeConnectionStream(recorder, request, stream)
	decoder := ipc.NewStreamDecoder(recorder.Body)
	for seq, kind := range []string{"event", "event", "done"} {
		event, err := decoder.Next(context.Background())
		if err != nil || event.Seq != uint64(seq+1) || event.Kind != kind {
			t.Fatalf("event[%d]=%+v err=%v", seq, event, err)
		}
	}
}

type fakeProxyInspector struct{ sources []platform.ProxySource }

func (f fakeProxyInspector) Inspect(context.Context) []platform.ProxySource {
	return append([]platform.ProxySource(nil), f.sources...)
}

func TestCapabilityServiceEnrichProxyStatusAddsMismatchWarning(t *testing.T) {
	service := &CapabilityService{proxyInspector: fakeProxyInspector{sources: []platform.ProxySource{{
		Source: "gnome.http", ExpectedProtocol: "http", Endpoint: &platform.ProxyEndpoint{Scheme: "http", Host: "127.0.0.1", Port: 10808},
	}}}}
	status := ModeStatus{Listeners: []platform.ProxyListener{{Protocol: "mixed", Host: "127.0.0.1", Port: 7890}}, Warnings: []string{}}
	service.enrichProxyStatus(context.Background(), &status)
	if len(status.SystemProxy) != 1 || status.SystemProxy[0].State != platform.ProxyStateMismatched || len(status.Warnings) != 1 {
		t.Fatalf("status=%+v", status)
	}
}

func TestProduceConnectionEventsCancellationStopsProducer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	output := make(chan ConnectionEvent, 1)
	started := make(chan struct{})
	var once atomic.Bool
	go produceConnectionEvents(ctx, output, time.Millisecond, time.Now, func(context.Context) ([]mihomo.RuntimeConnection, error) {
		if once.CompareAndSwap(false, true) {
			close(started)
		}
		return nil, nil
	})
	<-started
	cancel()
	select {
	case _, ok := <-output:
		if ok {
			t.Fatal("unexpected event after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("connection producer did not stop")
	}
}

func TestConvertConnectionUsesRuntimeChainWithoutGuessing(t *testing.T) {
	value := convertConnection(mihomo.RuntimeConnection{ID: "a", Chains: []string{"unknown-node", "custom-group"}})
	if value.FinalNode != "unknown-node" || len(value.Chains) != 2 {
		t.Fatalf("connection=%+v", value)
	}
	empty := convertConnection(mihomo.RuntimeConnection{ID: "b"})
	if empty.FinalNode != "" || empty.Chains == nil || empty.Warnings == nil {
		t.Fatalf("empty connection=%+v", empty)
	}
}
