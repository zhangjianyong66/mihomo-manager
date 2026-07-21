package ipc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	ProtocolVersion = 1
	ProtocolMin     = "MM-Protocol-Min"
	ProtocolMax     = "MM-Protocol-Max"
	ProtocolHeader  = "MM-Protocol-Version"
	RequestIDHeader = "MM-Request-ID"
	MaxBodySize     = 1 << 20
)

var (
	ErrDaemonUnavailable = errors.New("daemon unavailable")
	ErrPermissionDenied  = errors.New("daemon permission denied")
	ErrProtocolMismatch  = errors.New("protocol incompatible")
	ErrRequestCancelled  = errors.New("request cancelled")
	ErrStreamInvalid     = errors.New("invalid ndjson stream")
)

type ErrorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retryable bool           `json:"retryable"`
	Details   map[string]any `json:"details,omitempty"`
}

type Response struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Data       any        `json:"data,omitempty"`
	Warnings   []string   `json:"warnings"`
	Error      *ErrorBody `json:"error,omitempty"`
}

type Error struct {
	Body       ErrorBody
	StatusCode int
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Body.Message != "" {
		return e.Body.Message
	}
	return e.Body.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	switch e.Body.Code {
	case "PROTOCOL_INCOMPATIBLE":
		return ErrProtocolMismatch
	case "PERMISSION_DENIED":
		return ErrPermissionDenied
	default:
		return nil
	}
}

type Server struct {
	MinVersion int
	MaxVersion int
	MaxBody    int64
	Handler    http.Handler
}

func NewServer(handler http.Handler) *Server {
	return &Server{MinVersion: ProtocolVersion, MaxVersion: ProtocolVersion, MaxBody: MaxBodySize, Handler: handler}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	min, max, err := parseVersionRange(r.Header.Get(ProtocolMin), r.Header.Get(ProtocolMax))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "协议版本范围无效", false, nil)
		return
	}
	selected, ok := negotiate(min, max, s.MinVersion, s.MaxVersion)
	if !ok {
		WriteError(w, http.StatusUpgradeRequired, "PROTOCOL_INCOMPATIBLE", "客户端与 daemon 协议不兼容", false, map[string]any{"min": s.MinVersion, "max": s.MaxVersion})
		return
	}
	w.Header().Set(ProtocolHeader, fmt.Sprint(selected))
	if requestID := r.Header.Get(RequestIDHeader); requestID != "" {
		w.Header().Set(RequestIDHeader, requestID)
	}
	limit := s.MaxBody
	if limit <= 0 {
		limit = MaxBodySize
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if r.ContentLength > limit {
		WriteError(w, http.StatusRequestEntityTooLarge, "INVALID_REQUEST", "请求体超过限制", false, nil)
		return
	}
	if s.Handler == nil {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "资源不存在", false, nil)
		return
	}
	s.Handler.ServeHTTP(w, r)
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			_ = WriteError(w, http.StatusRequestEntityTooLarge, "INVALID_REQUEST", "请求体超过限制", false, nil)
		} else {
			_ = WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "JSON 请求体无效", false, nil)
		}
		return err
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, kind string, data any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(Response{APIVersion: "mm/v1", Kind: kind, Data: data, Warnings: []string{}})
}

func WriteError(w http.ResponseWriter, status int, code, message string, retryable bool, details map[string]any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(Response{APIVersion: "mm/v1", Kind: "Error", Warnings: []string{}, Error: &ErrorBody{Code: code, Message: message, Retryable: retryable, Details: details}})
}

type Client struct {
	SocketPath string
	HTTPClient *http.Client
	MinVersion int
	MaxVersion int
}

func NewClient(socketPath string) *Client {
	transport := &http.Transport{DisableKeepAlives: true}
	transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
	}
	return &Client{SocketPath: socketPath, HTTPClient: &http.Client{Transport: transport}, MinVersion: ProtocolVersion, MaxVersion: ProtocolVersion}
}

func (c *Client) Do(ctx context.Context, method, path, requestID string, request any, result any) error {
	if !strings.HasPrefix(path, "/v1/") {
		return fmt.Errorf("ipc path must begin with /v1/: %w", ErrProtocolMismatch)
	}
	var body io.Reader
	if request != nil {
		encoded, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("encode ipc request: %w", err)
		}
		if len(encoded) > MaxBodySize {
			return fmt.Errorf("encode ipc request: %w", ErrStreamInvalid)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://mihomo-manager"+path, body)
	if err != nil {
		return err
	}
	min, max := c.MinVersion, c.MaxVersion
	if min == 0 {
		min = ProtocolVersion
	}
	if max == 0 {
		max = ProtocolVersion
	}
	req.Header.Set(ProtocolMin, fmt.Sprint(min))
	req.Header.Set(ProtocolMax, fmt.Sprint(max))
	if requestID != "" {
		req.Header.Set(RequestIDHeader, requestID)
	}
	client := c.HTTPClient
	if client == nil {
		client = NewClient(c.SocketPath).HTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("ipc request: %w", ErrRequestCancelled)
		}
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("ipc connect: %w", ErrPermissionDenied)
		}
		return fmt.Errorf("ipc connect: %w", ErrDaemonUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 && resp.Header.Get(ProtocolHeader) != fmt.Sprint(ProtocolVersion) {
		return fmt.Errorf("ipc response protocol: %w", ErrProtocolMismatch)
	}
	var raw struct {
		APIVersion string          `json:"apiVersion"`
		Kind       string          `json:"kind"`
		Data       json.RawMessage `json:"data"`
		Warnings   []string        `json:"warnings"`
		Error      *ErrorBody      `json:"error"`
	}
	encoded, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodySize+1))
	if err != nil || len(encoded) > MaxBodySize {
		return fmt.Errorf("read ipc response: %w", ErrDaemonUnavailable)
	}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return fmt.Errorf("decode ipc response: %w", ErrDaemonUnavailable)
	}
	if raw.Error != nil || resp.StatusCode >= 400 {
		if raw.Error == nil {
			raw.Error = &ErrorBody{Code: "INTERNAL", Message: "daemon 请求失败"}
		}
		if resp.StatusCode == http.StatusUpgradeRequired || raw.Error.Code == "PROTOCOL_INCOMPATIBLE" {
			return &Error{Body: *raw.Error, StatusCode: resp.StatusCode}
		}
		return &Error{Body: *raw.Error, StatusCode: resp.StatusCode}
	}
	if result != nil && len(raw.Data) > 0 && string(raw.Data) != "null" {
		if err := json.Unmarshal(raw.Data, result); err != nil {
			return fmt.Errorf("decode ipc data: %w", err)
		}
	}
	return nil
}

// OpenStream opens a daemon NDJSON response. The caller owns and must close the body.
func (c *Client) OpenStream(ctx context.Context, method, path, requestID string, request any) (io.ReadCloser, error) {
	if !strings.HasPrefix(path, "/v1/") {
		return nil, fmt.Errorf("ipc path must begin with /v1/: %w", ErrProtocolMismatch)
	}
	var body io.Reader
	if request != nil {
		encoded, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("encode ipc request: %w", err)
		}
		if len(encoded) > MaxBodySize {
			return nil, fmt.Errorf("encode ipc request: %w", ErrStreamInvalid)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://mihomo-manager"+path, body)
	if err != nil {
		return nil, err
	}
	min, max := c.MinVersion, c.MaxVersion
	if min == 0 {
		min = ProtocolVersion
	}
	if max == 0 {
		max = ProtocolVersion
	}
	req.Header.Set(ProtocolMin, fmt.Sprint(min))
	req.Header.Set(ProtocolMax, fmt.Sprint(max))
	if requestID != "" {
		req.Header.Set(RequestIDHeader, requestID)
	}
	client := c.HTTPClient
	if client == nil {
		client = NewClient(c.SocketPath).HTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("ipc stream: %w", ErrRequestCancelled)
		}
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return nil, fmt.Errorf("ipc stream: %w", ErrPermissionDenied)
		}
		return nil, fmt.Errorf("ipc stream: %w", ErrDaemonUnavailable)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		encoded, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxBodySize+1))
		if readErr != nil || len(encoded) > MaxBodySize {
			return nil, fmt.Errorf("read ipc error: %w", ErrDaemonUnavailable)
		}
		var raw Response
		if json.Unmarshal(encoded, &raw) != nil || raw.Error == nil {
			return nil, fmt.Errorf("decode ipc error: %w", ErrDaemonUnavailable)
		}
		return nil, &Error{Body: *raw.Error, StatusCode: resp.StatusCode}
	}
	if resp.Header.Get(ProtocolHeader) != fmt.Sprint(ProtocolVersion) {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("ipc stream protocol: %w", ErrProtocolMismatch)
	}
	return resp.Body, nil
}

func parseVersionRange(minText, maxText string) (int, int, error) {
	if minText == "" {
		minText = fmt.Sprint(ProtocolVersion)
	}
	if maxText == "" {
		maxText = fmt.Sprint(ProtocolVersion)
	}
	var min, max int
	min, err := strconv.Atoi(minText)
	if err != nil {
		return 0, 0, err
	}
	max, err = strconv.Atoi(maxText)
	if err != nil || min < 1 || max < min {
		if err == nil {
			err = errors.New("invalid protocol range")
		}
		return 0, 0, err
	}
	return min, max, nil
}

func negotiate(clientMin, clientMax, serverMin, serverMax int) (int, bool) {
	low, high := clientMin, clientMax
	if serverMin > low {
		low = serverMin
	}
	if serverMax < high {
		high = serverMax
	}
	return high, low <= high
}

type StreamEvent struct {
	Kind  string          `json:"kind"`
	Seq   uint64          `json:"seq"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error *ErrorBody      `json:"error,omitempty"`
}

type StreamDecoder struct {
	reader   *bufio.Reader
	source   io.Reader
	nextSeq  uint64
	finished bool
}

func NewStreamDecoder(reader io.Reader) *StreamDecoder {
	return &StreamDecoder{reader: bufio.NewReader(reader), source: reader, nextSeq: 1}
}

func (d *StreamDecoder) Next(ctx context.Context) (StreamEvent, error) {
	if d.finished {
		return StreamEvent{}, io.EOF
	}
	select {
	case <-ctx.Done():
		return StreamEvent{}, fmt.Errorf("read stream: %w", ErrRequestCancelled)
	default:
	}
	type readResult struct {
		line []byte
		err  error
	}
	var line []byte
	var err error
	if closer, ok := d.source.(io.ReadCloser); ok && ctx.Done() != nil {
		result := make(chan readResult, 1)
		go func() {
			value, readErr := readLimitedLine(d.reader)
			result <- readResult{line: value, err: readErr}
		}()
		select {
		case value := <-result:
			line, err = value.line, value.err
		case <-ctx.Done():
			_ = closer.Close()
			return StreamEvent{}, fmt.Errorf("read stream: %w", ErrRequestCancelled)
		}
	} else {
		line, err = readLimitedLine(d.reader)
	}
	if err != nil && len(line) == 0 {
		if errors.Is(err, io.EOF) {
			return StreamEvent{}, fmt.Errorf("stream ended without terminal event: %w", ErrStreamInvalid)
		}
		return StreamEvent{}, err
	}
	if len(line) > MaxBodySize {
		return StreamEvent{}, fmt.Errorf("stream event too large: %w", ErrStreamInvalid)
	}
	var event StreamEvent
	if json.Unmarshal(bytes.TrimSpace(line), &event) != nil || event.Seq != d.nextSeq || event.Kind == "" {
		return StreamEvent{}, fmt.Errorf("decode stream event: %w", ErrStreamInvalid)
	}
	if event.Kind != "event" && event.Kind != "done" && event.Kind != "error" {
		return StreamEvent{}, fmt.Errorf("decode stream event kind: %w", ErrStreamInvalid)
	}
	d.nextSeq++
	if event.Kind == "done" || event.Kind == "error" {
		d.finished = true
	}
	return event, nil
}

func readLimitedLine(reader *bufio.Reader) ([]byte, error) {
	line := make([]byte, 0, 4096)
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(line)+len(fragment) > MaxBodySize {
			return nil, fmt.Errorf("stream event too large: %w", ErrStreamInvalid)
		}
		line = append(line, fragment...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return line, err
	}
}

type StreamWriter struct {
	writer io.Writer
	seq    uint64
	ended  bool
}

func NewStreamWriter(writer io.Writer) *StreamWriter { return &StreamWriter{writer: writer} }

func (w *StreamWriter) Write(ctx context.Context, event StreamEvent) error {
	if w.ended {
		return fmt.Errorf("write stream after terminal event: %w", ErrStreamInvalid)
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("write stream: %w", ErrRequestCancelled)
	default:
	}
	if event.Kind != "event" && event.Kind != "done" && event.Kind != "error" {
		return fmt.Errorf("write stream: %w", ErrStreamInvalid)
	}
	w.seq++
	event.Seq = w.seq
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(encoded) > MaxBodySize {
		return fmt.Errorf("write stream event: %w", ErrStreamInvalid)
	}
	_, err = fmt.Fprintf(w.writer, "%s\n", encoded)
	if event.Kind == "done" || event.Kind == "error" {
		w.ended = true
	}
	return err
}
