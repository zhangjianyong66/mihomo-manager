package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

const maxRuntimeResponseBytes = 1 << 20

type runtimeClient struct {
	baseURL string
	http    *http.Client
}

// RuntimeRule is the stable subset of mihomo's /rules payload needed for
// routing-mode verification. Unknown upstream fields stay outside the daemon
// contract.
type RuntimeRule struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
	Proxy   string `json:"proxy"`
}

type runtimeModeRequest struct {
	Mode domain.RoutingMode `json:"mode"`
}

type runtimeReloadRequest struct {
	Path string `json:"path"`
}

// RoutingRuntime is implemented by the typed mihomo runtime client. It is
// intentionally separate from core.RuntimeClient so adapters for other cores
// only need to implement readiness until they opt into routing capabilities.
type RoutingRuntime interface {
	Mode(context.Context) (domain.RoutingMode, error)
	SetMode(context.Context, domain.RoutingMode) error
	Reload(context.Context, string) error
	LoadedRules(context.Context) ([]RuntimeRule, error)
	Connections(context.Context) ([]RuntimeConnection, error)
	ConnectionCount(context.Context) (int, error)
	CloseConnections(context.Context) error
	SelectedProxy(context.Context, string) (string, error)
}

func newRuntimeClient(endpoint string, client *http.Client) (*runtimeClient, error) {
	baseURL, _, err := parseControllerEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	return &runtimeClient{baseURL: baseURL, http: client}, nil
}

func (c *runtimeClient) Ready(ctx context.Context) error {
	delay := 20 * time.Millisecond
	for {
		if err := c.probe(ctx); err == nil {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 200*time.Millisecond {
			delay *= 2
		}
	}
}

func (c *runtimeClient) probe(ctx context.Context) error {
	var version struct {
		Version string `json:"version"`
	}
	if err := c.getJSON(ctx, "/version", &version); err != nil {
		return err
	}
	if strings.TrimSpace(version.Version) == "" {
		return fmt.Errorf("mihomo version response is incomplete")
	}
	var configs struct {
		Mode string `json:"mode"`
	}
	if err := c.getJSON(ctx, "/configs", &configs); err != nil {
		return err
	}
	if strings.TrimSpace(configs.Mode) == "" {
		return fmt.Errorf("mihomo configs response is incomplete")
	}
	return nil
}

func (c *runtimeClient) Mode(ctx context.Context) (domain.RoutingMode, error) {
	var configs struct {
		Mode domain.RoutingMode `json:"mode"`
	}
	if err := c.getJSON(ctx, "/configs", &configs); err != nil {
		return "", err
	}
	if err := configs.Mode.Validate(); err != nil {
		return "", fmt.Errorf("mihomo configs mode: %w", err)
	}
	return configs.Mode, nil
}

func (c *runtimeClient) SetMode(ctx context.Context, mode domain.RoutingMode) error {
	if err := mode.Validate(); err != nil {
		return err
	}
	return c.sendJSON(ctx, http.MethodPatch, "/configs", runtimeModeRequest{Mode: mode}, nil)
}

func (c *runtimeClient) Reload(ctx context.Context, configPath string) error {
	if strings.TrimSpace(configPath) == "" {
		return fmt.Errorf("mihomo config path must not be empty")
	}
	return c.sendJSON(ctx, http.MethodPut, "/configs?force=true", runtimeReloadRequest{Path: configPath}, nil)
}

func (c *runtimeClient) LoadedRules(ctx context.Context) ([]RuntimeRule, error) {
	var response struct {
		Rules []RuntimeRule `json:"rules"`
	}
	if err := c.getJSON(ctx, "/rules", &response); err != nil {
		return nil, err
	}
	return response.Rules, nil
}

func (c *runtimeClient) ConnectionCount(ctx context.Context) (int, error) {
	connections, err := c.Connections(ctx)
	if err != nil {
		return 0, err
	}
	return len(connections), nil
}

func (c *runtimeClient) CloseConnections(ctx context.Context) error {
	return c.sendJSON(ctx, http.MethodDelete, "/connections", nil, nil)
}

func (c *runtimeClient) SelectedProxy(ctx context.Context, group string) (string, error) {
	if strings.TrimSpace(group) == "" {
		return "", fmt.Errorf("mihomo proxy group must not be empty")
	}
	var response struct {
		Now string `json:"now"`
	}
	if err := c.getJSON(ctx, "/proxies/"+url.PathEscape(group), &response); err != nil {
		return "", err
	}
	return response.Now, nil
}

func (c *runtimeClient) getJSON(ctx context.Context, path string, target any) error {
	return c.sendJSON(ctx, http.MethodGet, path, nil, target)
}

func (c *runtimeClient) sendJSON(ctx context.Context, method, path string, payload, target any) error {
	var requestBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode mihomo runtime request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxRuntimeResponseBytes))
		return fmt.Errorf("mihomo runtime returned status %d", resp.StatusCode)
	}
	if target == nil {
		read, err := io.Copy(io.Discard, io.LimitReader(resp.Body, maxRuntimeResponseBytes+1))
		if err != nil {
			return fmt.Errorf("read mihomo runtime response: %w", err)
		}
		if read > maxRuntimeResponseBytes {
			return fmt.Errorf("mihomo runtime response exceeds %d bytes", maxRuntimeResponseBytes)
		}
		return nil
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxRuntimeResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read mihomo runtime response: %w", err)
	}
	if len(responseBody) > maxRuntimeResponseBytes {
		return fmt.Errorf("mihomo runtime response exceeds %d bytes", maxRuntimeResponseBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(string(responseBody)))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode mihomo runtime response: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("mihomo runtime returned multiple JSON values")
		}
		return fmt.Errorf("read mihomo runtime response: %w", err)
	}
	return nil
}
