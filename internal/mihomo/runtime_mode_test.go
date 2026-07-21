package mihomo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func TestRuntimeClientRoutingOperations(t *testing.T) {
	t.Helper()
	requests := make([]string, 0, 7)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/configs":
			_, _ = w.Write([]byte(`{"mode":"rule"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/configs":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["mode"] != "direct" {
				t.Errorf("unexpected mode payload: %#v err=%v", body, err)
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && r.URL.Path == "/configs":
			if r.URL.Query().Get("force") != "true" {
				t.Errorf("force query = %q", r.URL.RawQuery)
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["path"] != "/tmp/config.yaml" {
				t.Errorf("reload path = %q", body["path"])
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rules":
			_, _ = w.Write([]byte(`{"rules":[{"type":"RuleSet","payload":"mm-cn-domain","proxy":"DIRECT"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/connections":
			_, _ = w.Write([]byte(`{"connections":[{},{}]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/connections":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/GLOBAL":
			_, _ = w.Write([]byte(`{"now":"node-a"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := newRuntimeClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mode, err := client.Mode(ctx)
	if err != nil || mode != domain.RoutingModeRule {
		t.Fatalf("mode=%q err=%v", mode, err)
	}
	if err := client.SetMode(ctx, domain.RoutingModeDirect); err != nil {
		t.Fatal(err)
	}
	if err := client.Reload(ctx, "/tmp/config.yaml"); err != nil {
		t.Fatal(err)
	}
	rules, err := client.LoadedRules(ctx)
	if err != nil || len(rules) != 1 || rules[0].Payload != CNDomainProviderName {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	count, err := client.ConnectionCount(ctx)
	if err != nil || count != 2 {
		t.Fatalf("connections=%d err=%v", count, err)
	}
	if err := client.CloseConnections(ctx); err != nil {
		t.Fatal(err)
	}
	selected, err := client.SelectedProxy(ctx, "GLOBAL")
	if err != nil || selected != "node-a" {
		t.Fatalf("selected=%q err=%v", selected, err)
	}
	want := []string{
		"GET /configs", "PATCH /configs", "PUT /configs?force=true", "GET /rules",
		"GET /connections", "DELETE /connections", "GET /proxies/GLOBAL",
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests=%v want=%v", requests, want)
	}
}

func TestRuntimeClientRejectsInvalidModeAndUpstreamStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/configs" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"mode":"unknown"}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := newRuntimeClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Mode(context.Background()); err == nil {
		t.Fatal("expected invalid runtime mode to fail")
	}
	if err := client.CloseConnections(context.Background()); err == nil {
		t.Fatal("expected non-2xx close to fail")
	}
	if err := client.SetMode(context.Background(), domain.RoutingMode("bad")); err == nil {
		t.Fatal("expected invalid requested mode to fail before HTTP")
	}
}
