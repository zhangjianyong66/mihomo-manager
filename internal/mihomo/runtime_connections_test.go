package mihomo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeConnectionsNormalizesControllerPayload(t *testing.T) {
	payload := `{"connections":[
{"id":"a","metadata":{"network":"TCP","host":"example.com","destinationIP":"203.0.113.1","destinationPort":"443"},"upload":12,"download":34,"start":"2026-07-22T10:20:30.123Z","chains":["node-a","GLOBAL"],"rule":"RuleSet","rulePayload":"mm-cn-domain"},
{"id":"b","metadata":{"network":"udp","destinationIP":"2001:db8::1","destinationPort":53},"upload":-1,"download":-2,"start":"bad","chains":["", "DIRECT"],"rule":"Match"}
]}`
	client := runtimeClientForConnections(t, payload)
	values, err := client.Connections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].DestinationPort != 443 || values[0].Network != "tcp" || values[0].Chains[0] != "node-a" {
		t.Fatalf("connections=%+v", values)
	}
	if values[1].Host != "" || values[1].DestinationIP != "2001:db8::1" || values[1].DestinationPort != 53 || values[1].Upload != 0 || values[1].Download != 0 || len(values[1].Warnings) != 3 {
		t.Fatalf("normalized fallback=%+v", values[1])
	}
}

func TestRuntimeConnectionsTreatsNullAsEmpty(t *testing.T) {
	client := runtimeClientForConnections(t, `{"connections":null}`)
	values, err := client.Connections(context.Background())
	if err != nil || len(values) != 0 || values == nil {
		t.Fatalf("connections=%#v err=%v", values, err)
	}
}

func TestRuntimeConnectionsRejectsInvalidPortAndLimits(t *testing.T) {
	client := runtimeClientForConnections(t, `{"connections":[{"id":"a","metadata":{"destinationPort":"secret"}}]}`)
	values, err := client.Connections(context.Background())
	if err != nil || len(values) != 1 || values[0].DestinationPort != 0 || len(values[0].Warnings) != 1 {
		t.Fatalf("connections=%+v err=%v", values, err)
	}

	items := strings.Repeat(`{"id":"x"},`, MaxRuntimeConnections) + `{"id":"overflow"}`
	client = runtimeClientForConnections(t, `{"connections":[`+items+`]}`)
	if _, err := client.Connections(context.Background()); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected connection limit error, got %v", err)
	}
}

func TestRuntimeConnectionsUsesExistingResponseSizeLimit(t *testing.T) {
	client := runtimeClientForConnections(t, `{"connections":[],"padding":"`+strings.Repeat("x", maxRuntimeResponseBytes)+`"}`)
	if _, err := client.Connections(context.Background()); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected response limit error, got %v", err)
	}
}

func runtimeClientForConnections(t *testing.T, payload string) *runtimeClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/connections" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, payload)
	}))
	t.Cleanup(server.Close)
	client, err := newRuntimeClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}
