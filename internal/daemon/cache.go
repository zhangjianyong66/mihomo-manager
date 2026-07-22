package daemon

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/ipc"
)

type cacheEntry struct {
	signature [32]byte
	status    int
	header    http.Header
	body      []byte
	expiresAt time.Time
	createdAt time.Time
}

type RequestCache struct {
	mu       sync.Mutex
	entries  map[string]cacheEntry
	ttl      time.Duration
	capacity int
	clock    func() time.Time
}

func NewRequestCache(ttl time.Duration, capacity int) *RequestCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if capacity <= 0 {
		capacity = 1024
	}
	return &RequestCache{entries: make(map[string]cacheEntry), ttl: ttl, capacity: capacity, clock: time.Now}
}

func (c *RequestCache) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(ipc.RequestIDHeader)
		if requestID == "" || r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, ipc.MaxBodySize+1))
		if err != nil || len(body) > ipc.MaxBodySize {
			_ = ipc.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求体无效", false, nil)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		signature := sha256.Sum256(append([]byte(r.Method+"\x00"+r.URL.Path+"\x00"), body...))
		if entry, found, conflict := c.lookup(requestID, signature); found {
			copyHeader(w.Header(), entry.header)
			w.WriteHeader(entry.status)
			_, _ = w.Write(entry.body)
			return
		} else if conflict {
			_ = ipc.WriteError(w, http.StatusConflict, "REQUEST_ID_CONFLICT", "request ID 已用于不同请求", false, map[string]any{"requestId": requestID})
			return
		}

		recorder := &responseRecorder{target: w, header: make(http.Header), status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		if recorder.passthrough {
			return
		}
		copyHeader(w.Header(), recorder.header)
		w.WriteHeader(recorder.status)
		_, _ = w.Write(recorder.body.Bytes())
		if recorder.status < 500 && !isStreaming(recorder.header) {
			c.store(requestID, signature, recorder)
		}
	})
}

func (c *RequestCache) lookup(id string, signature [32]byte) (cacheEntry, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	c.prune(now)
	entry, ok := c.entries[id]
	if !ok {
		return cacheEntry{}, false, false
	}
	if entry.signature != signature {
		return cacheEntry{}, false, true
	}
	return entry, true, false
}

func (c *RequestCache) store(id string, signature [32]byte, recorder *responseRecorder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	c.prune(now)
	if len(c.entries) >= c.capacity {
		var oldestID string
		var oldest time.Time
		for entryID, entry := range c.entries {
			if oldestID == "" || entry.createdAt.Before(oldest) {
				oldestID, oldest = entryID, entry.createdAt
			}
		}
		delete(c.entries, oldestID)
	}
	c.entries[id] = cacheEntry{signature: signature, status: recorder.status, header: recorder.header.Clone(), body: append([]byte(nil), recorder.body.Bytes()...), createdAt: now, expiresAt: now.Add(c.ttl)}
}

func (c *RequestCache) prune(now time.Time) {
	for id, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, id)
		}
	}
}

type responseRecorder struct {
	target      http.ResponseWriter
	header      http.Header
	status      int
	body        bytes.Buffer
	wroteHeader bool
	passthrough bool
}

func (r *responseRecorder) Header() http.Header { return r.header }

func (r *responseRecorder) WriteHeader(statusCode int) {
	if r.wroteHeader {
		return
	}
	r.status = statusCode
	r.wroteHeader = true
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	if r.passthrough {
		return r.target.Write(data)
	}
	return r.body.Write(data)
}

func (r *responseRecorder) Flush() {
	if r.target == nil {
		return
	}
	if !r.passthrough {
		if !r.wroteHeader {
			r.WriteHeader(http.StatusOK)
		}
		copyHeader(r.target.Header(), r.header)
		r.target.WriteHeader(r.status)
		if r.body.Len() > 0 {
			_, _ = r.target.Write(r.body.Bytes())
			r.body.Reset()
		}
		r.passthrough = true
	}
	if flusher, ok := r.target.(http.Flusher); ok {
		flusher.Flush()
	}
}

func copyHeader(destination, source http.Header) {
	for key, values := range source {
		destination[key] = append([]string(nil), values...)
	}
}

func isStreaming(header http.Header) bool {
	return header.Get("Content-Type") == "application/x-ndjson"
}
