//go:build linux

package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
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
