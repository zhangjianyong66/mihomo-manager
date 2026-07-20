package mihomo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxRuntimeResponseBytes = 1 << 20

type runtimeClient struct {
	baseURL string
	http    *http.Client
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

func (c *runtimeClient) getJSON(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRuntimeResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read mihomo runtime response: %w", err)
	}
	if len(body) > maxRuntimeResponseBytes {
		return fmt.Errorf("mihomo runtime response exceeds %d bytes", maxRuntimeResponseBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
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
