//go:build linux

package daemon

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

func TestServer_RunStatusMultipleClientsAndCleanup(t *testing.T) {
	paths := testPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	server := New(Options{Paths: paths})
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	waitForSocket(t, paths.Socket)
	for i := 0; i < 2; i++ {
		var status Status
		if err := ipc.NewClient(paths.Socket).Do(context.Background(), http.MethodGet, "/v1/status", "", nil, &status); err != nil {
			t.Fatal(err)
		}
		if status.State != StateRunning || status.PID != os.Getpid() || status.SchemaVersion < 1 {
			t.Fatalf("unexpected status: %+v", status)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(paths.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket was not removed: %v", err)
	}
	lock, err := platform.AcquireFileLock(paths.Lock)
	if err != nil {
		t.Fatalf("daemon lock was not released: %v", err)
	}
	_ = lock.Close()
}

func TestServer_WithCoreAdapterDoesNotAutoStartCore(t *testing.T) {
	paths := testPaths(t)
	ctx, cancel := context.WithCancel(context.Background())
	server := New(Options{Paths: paths, CoreAdapter: &managerFakeAdapter{}})
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	waitForSocket(t, paths.Socket)
	var status Status
	if err := ipc.NewClient(paths.Socket).Do(context.Background(), http.MethodGet, "/v1/status", "", nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.Core.State != domain.CoreStateStopped {
		t.Fatalf("daemon unexpectedly started core: %+v", status.Core)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCoordinator_RejectsConcurrentMutation(t *testing.T) {
	coordinator := NewCoordinator()
	release, err := coordinator.TryAcquire(context.Background(), "op-1", "switch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.TryAcquire(context.Background(), "op-2", "start"); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	release()
	if _, err := coordinator.TryAcquire(context.Background(), "op-3", "start"); err != nil {
		t.Fatal(err)
	}
}

func TestRequestCache_ReplayConflictAndExpiry(t *testing.T) {
	now := time.Now()
	cache := NewRequestCache(time.Minute, 2)
	cache.clock = func() time.Time { return now }
	var calls atomic.Int32
	handler := cache.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = ipc.WriteJSON(w, http.StatusOK, "Mutation", map[string]int{"call": int(calls.Load())})
	}))
	request := func(body string) *responseRecorder {
		recorder := &responseRecorder{header: make(http.Header), status: http.StatusOK}
		req, _ := http.NewRequest(http.MethodPost, "/v1/mutate", strings.NewReader(body))
		req.Header.Set(ipc.RequestIDHeader, "same")
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	first := request("one")
	second := request("one")
	if calls.Load() != 1 || first.body.String() != second.body.String() {
		t.Fatalf("response not replayed: calls=%d", calls.Load())
	}
	conflict := request("two")
	if conflict.status != http.StatusConflict {
		t.Fatalf("expected conflict, got %d", conflict.status)
	}
	now = now.Add(2 * time.Minute)
	request("two")
	if calls.Load() != 2 {
		t.Fatalf("expired request was not executed: %d", calls.Load())
	}
}

func TestRequestCache_FlushedResponseStreamsAndIsNotCached(t *testing.T) {
	cache := NewRequestCache(time.Minute, 2)
	var calls atomic.Int32
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	handler := cache.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"seq\":1,\"kind\":\"event\"}\n"))
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "response writer does not support flushing", http.StatusInternalServerError)
			return
		}
		flusher.Flush()

		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("{\"seq\":2,\"kind\":\"done\"}\n"))
		flusher.Flush()
	}))
	server := httptest.NewServer(handler)
	defer server.Close()

	doRequest := func() (*http.Response, error) {
		req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/stream", strings.NewReader("{}"))
		if err != nil {
			return nil, err
		}
		req.Header.Set(ipc.RequestIDHeader, "stream-request")
		return server.Client().Do(req)
	}

	type responseResult struct {
		response *http.Response
		err      error
	}
	responseCh := make(chan responseResult, 1)
	go func() {
		response, err := doRequest()
		responseCh <- responseResult{response: response, err: err}
	}()

	var first *http.Response
	select {
	case result := <-responseCh:
		if result.err != nil {
			t.Fatal(result.err)
		}
		first = result.response
	case <-time.After(time.Second):
		t.Fatal("client did not receive flushed response before handler completed")
	}
	if first.StatusCode != http.StatusOK {
		t.Fatalf("unexpected response status: %d", first.StatusCode)
	}
	if contentType := first.Header.Get("Content-Type"); contentType != "application/x-ndjson" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	reader := bufio.NewReader(first.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "{\"seq\":1,\"kind\":\"event\"}\n" {
		t.Fatalf("unexpected first event: %q", line)
	}

	close(release)
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "{\"seq\":2,\"kind\":\"done\"}\n" {
		t.Fatalf("unexpected terminal event: %q", line)
	}
	if extra, err := reader.ReadString('\n'); err != io.EOF || extra != "" {
		t.Fatalf("unexpected data after terminal event: data=%q err=%v", extra, err)
	}
	if err := first.Body.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := doRequest()
	if err != nil {
		t.Fatal(err)
	}
	secondReader := bufio.NewReader(second.Body)
	for _, expected := range []string{
		"{\"seq\":1,\"kind\":\"event\"}\n",
		"{\"seq\":2,\"kind\":\"done\"}\n",
	} {
		line, err := secondReader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line != expected {
			t.Fatalf("unexpected replay event: got %q, want %q", line, expected)
		}
	}
	if err := second.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("stream response was cached: handler calls=%d", calls.Load())
	}
}

func testPaths(t *testing.T) config.ManagerPaths {
	t.Helper()
	root := t.TempDir()
	paths, err := config.ResolveManagerPaths(config.ManagerEnvironment{HomeDir: root, DataHome: filepath.Join(root, "data"), StateHome: filepath.Join(root, "state"), RuntimeDir: filepath.Join(root, "run"), ConfigHome: filepath.Join(root, "config")})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("daemon socket did not appear")
}
