package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerClient_ProtocolAndJSONRoundTrip(t *testing.T) {
	server := httptest.NewServer(NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" || r.Header.Get(RequestIDHeader) != "req-1" {
			t.Fatal("request contract not preserved")
		}
		_ = WriteJSON(w, http.StatusOK, "Status", map[string]any{"state": "running"})
	})))
	defer server.Close()
	client := testHTTPClient(server)
	var result map[string]string
	if err := client.Do(context.Background(), http.MethodGet, "/v1/status", "req-1", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result["state"] != "running" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestServerClient_ProtocolMismatch(t *testing.T) {
	server := httptest.NewServer(NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	defer server.Close()
	client := testHTTPClient(server)
	client.MinVersion, client.MaxVersion = 2, 2
	err := client.Do(context.Background(), http.MethodGet, "/v1/status", "", nil, nil)
	var protocolErr *Error
	if !errors.As(err, &protocolErr) || !errors.Is(err, ErrProtocolMismatch) {
		t.Fatalf("expected protocol mismatch, got %v", err)
	}
}

func testHTTPClient(server *httptest.Server) *Client {
	base := server.Client()
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = server.Listener.Addr().String()
		return base.Transport.RoundTrip(clone)
	})
	return &Client{HTTPClient: &http.Client{Transport: transport}, MinVersion: 1, MaxVersion: 1}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestStreamDecoder_RequiresMonotonicTerminal(t *testing.T) {
	var buffer bytes.Buffer
	writer := NewStreamWriter(&buffer)
	if err := writer.Write(context.Background(), StreamEvent{Kind: "event", Data: json.RawMessage(`{"x":1}`)}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write(context.Background(), StreamEvent{Kind: "done"}); err != nil {
		t.Fatal(err)
	}
	decoder := NewStreamDecoder(&buffer)
	first, err := decoder.Next(context.Background())
	if err != nil || first.Seq != 1 || first.Kind != "event" {
		t.Fatalf("unexpected first event: %+v %v", first, err)
	}
	done, err := decoder.Next(context.Background())
	if err != nil || done.Seq != 2 || done.Kind != "done" {
		t.Fatalf("unexpected done event: %+v %v", done, err)
	}
	if _, err := decoder.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("expected eof, got %v", err)
	}
	invalid := NewStreamDecoder(bytes.NewBufferString(`{"kind":"done","seq":2}` + "\n"))
	if _, err := invalid.Next(context.Background()); !errors.Is(err, ErrStreamInvalid) {
		t.Fatalf("expected invalid sequence, got %v", err)
	}
}

func TestServer_RejectsOversizedBodyBeforeHandler(t *testing.T) {
	called := false
	server := httptest.NewServer(NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })))
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/mutate", strings.NewReader(strings.Repeat("x", MaxBodySize+1)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(ProtocolMin, "1")
	req.Header.Set(ProtocolMax, "1")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge || called {
		t.Fatalf("unexpected oversized request result: status=%d called=%v", resp.StatusCode, called)
	}
}

func TestDecodeJSONRejectsTrailingValue(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/v1/mode", strings.NewReader(`{"mode":"rule"} {"mode":"direct"}`))
	recorder := httptest.NewRecorder()
	var destination map[string]any
	if err := DecodeJSON(recorder, request, &destination); err == nil {
		t.Fatal("expected trailing JSON value to fail")
	}
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "INVALID_REQUEST") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStreamDecoder_RejectsOversizedLine(t *testing.T) {
	decoder := NewStreamDecoder(strings.NewReader(strings.Repeat("x", MaxBodySize+1) + "\n"))
	if _, err := decoder.Next(context.Background()); !errors.Is(err, ErrStreamInvalid) {
		t.Fatalf("expected oversized line rejection, got %v", err)
	}
}
