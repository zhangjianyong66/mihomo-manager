package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type NodeTestStatus string

const (
	NodeTestStatusSuccess NodeTestStatus = "success"
	NodeTestStatusFailed  NodeTestStatus = "failed"
)

var (
	ErrProxyNotFound   = errors.New("mihomo proxy not found")
	ErrNodeNotInGroup  = errors.New("node is not a member of the proxy group")
	ErrNodeNotTestable = errors.New("node is not testable")
)

type ProxyNodeState struct {
	Name     string
	Type     string
	Testable bool
	Latest   *NodeDelay
}

type proxyHistory struct {
	Time  time.Time `json:"time"`
	Delay int       `json:"delay"`
}

type proxyEntry struct {
	Type    string         `json:"type"`
	Now     string         `json:"now"`
	All     []string       `json:"all"`
	History []proxyHistory `json:"history"`
}

type proxyListResponse struct {
	Proxies map[string]proxyEntry `json:"proxies"`
}

func (c *Client) GroupDetail(group string) (ProxyGroup, error) {
	return c.groupDetail(context.Background(), group)
}

func (c *Client) groupDetail(ctx context.Context, group string) (ProxyGroup, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return ProxyGroup{}, errors.New("proxy group must not be empty")
	}
	snapshot, err := c.proxySnapshot(ctx)
	if err != nil {
		return ProxyGroup{}, err
	}
	entry, ok := snapshot.Proxies[group]
	if !ok || len(entry.All) == 0 {
		return ProxyGroup{}, fmt.Errorf("proxy group %q: %w", group, ErrProxyNotFound)
	}
	nodes := dedupNonEmpty(entry.All)
	states := make([]ProxyNodeState, 0, len(nodes))
	for _, node := range nodes {
		state := proxyNodeState(node, snapshot.Proxies[node])
		states = append(states, state)
	}
	return ProxyGroup{Name: group, Type: entry.Type, Now: strings.TrimSpace(entry.Now), All: nodes, NodeStates: states}, nil
}

func (c *Client) TestNodeStreamWithStop(group, node string, stop <-chan struct{}) <-chan NodeTestEvent {
	output := make(chan NodeTestEvent, 2)
	go func() {
		defer close(output)
		ctx, cancel := contextFromStop(stop)
		defer cancel()

		node = strings.TrimSpace(node)
		if node == "" {
			output <- NodeTestEvent{Err: errors.New("node must not be empty"), Finished: true}
			return
		}
		snapshot, err := c.proxySnapshot(ctx)
		if err != nil {
			if ctx.Err() == nil {
				output <- NodeTestEvent{Err: err, Finished: true}
			}
			return
		}
		entry, ok := snapshot.Proxies[node]
		if !ok {
			output <- NodeTestEvent{Err: fmt.Errorf("node %q: %w", node, ErrProxyNotFound), Finished: true}
			return
		}
		if strings.TrimSpace(group) != "" {
			groupEntry, exists := snapshot.Proxies[strings.TrimSpace(group)]
			if !exists || len(groupEntry.All) == 0 {
				output <- NodeTestEvent{Err: fmt.Errorf("proxy group %q: %w", group, ErrProxyNotFound), Finished: true}
				return
			}
			if !stringSliceContains(groupEntry.All, node) {
				output <- NodeTestEvent{Err: fmt.Errorf("node %q: %w", node, ErrNodeNotInGroup), Finished: true}
				return
			}
		}
		if !proxyNodeState(node, entry).Testable {
			output <- NodeTestEvent{Err: fmt.Errorf("node %q: %w", node, ErrNodeNotTestable), Finished: true}
			return
		}

		result := c.testNode(ctx, node)
		if ctx.Err() != nil {
			return
		}
		output <- NodeTestEvent{Done: 1, Total: 1, Result: &result}
		output <- NodeTestEvent{Done: 1, Total: 1, Finished: true}
	}()
	return output
}

func (c *Client) testNodesStreamWithStop(group string, concurrency, limit int, stop <-chan struct{}) <-chan NodeTestEvent {
	output := make(chan NodeTestEvent, 32)
	go func() {
		defer close(output)
		ctx, cancel := contextFromStop(stop)
		defer cancel()

		snapshot, err := c.proxySnapshot(ctx)
		if err != nil {
			if ctx.Err() == nil {
				output <- NodeTestEvent{Err: err, Finished: true}
			}
			return
		}
		members, err := snapshot.groupMembers(group)
		if err != nil {
			output <- NodeTestEvent{Err: err, Finished: true}
			return
		}
		nodes := make([]string, 0, len(members))
		for _, node := range members {
			entry, ok := snapshot.Proxies[node]
			if ok && proxyNodeState(node, entry).Testable {
				nodes = append(nodes, node)
			}
		}
		if limit > 0 && len(nodes) > limit {
			nodes = nodes[:limit]
		}
		total := len(nodes)
		if total == 0 {
			output <- NodeTestEvent{Finished: true}
			return
		}
		if concurrency < 1 {
			concurrency = 1
		}
		if concurrency > total {
			concurrency = total
		}

		jobs := make(chan string)
		results := make(chan NodeDelay, concurrency)
		var workers sync.WaitGroup
		for i := 0; i < concurrency; i++ {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case node, ok := <-jobs:
						if !ok {
							return
						}
						result := c.testNode(ctx, node)
						select {
						case results <- result:
						case <-ctx.Done():
							return
						}
					}
				}
			}()
		}
		go func() {
			defer close(jobs)
			for _, node := range nodes {
				select {
				case jobs <- node:
				case <-ctx.Done():
					return
				}
			}
		}()
		go func() {
			workers.Wait()
			close(results)
		}()

		done := 0
		for result := range results {
			done++
			copyResult := result
			select {
			case output <- NodeTestEvent{Done: done, Total: total, Result: &copyResult}:
			case <-ctx.Done():
				return
			}
		}
		if ctx.Err() == nil {
			output <- NodeTestEvent{Done: done, Total: total, Finished: true}
		}
	}()
	return output
}

func (c *Client) testNode(ctx context.Context, node string) NodeDelay {
	result := NodeDelay{Name: node, Delay: -1, Status: NodeTestStatusFailed}
	var response struct {
		Delay int `json:"delay"`
	}
	path := "/proxies/" + url.PathEscape(node) + "/delay?timeout=3000&url=http://www.gstatic.com/generate_204"
	if err := c.callJSON(ctx, http.MethodGet, path, nil, &response); err == nil && response.Delay > 0 {
		result.Delay = response.Delay
		result.Status = NodeTestStatusSuccess
	}
	result.TestedAt = time.Now()
	return result
}

func (c *Client) proxySnapshot(ctx context.Context) (proxyListResponse, error) {
	var response proxyListResponse
	if err := c.callJSON(ctx, http.MethodGet, "/proxies", nil, &response); err != nil {
		return proxyListResponse{}, err
	}
	if response.Proxies == nil {
		response.Proxies = map[string]proxyEntry{}
	}
	return response, nil
}

func (r proxyListResponse) groupMembers(group string) ([]string, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		group = "GLOBAL"
	}
	entry, ok := r.Proxies[group]
	if !ok || len(entry.All) == 0 {
		return nil, fmt.Errorf("proxy group %q: %w", group, ErrProxyNotFound)
	}
	return dedupNonEmpty(entry.All), nil
}

func proxyNodeState(name string, entry proxyEntry) ProxyNodeState {
	state := ProxyNodeState{Name: name, Type: entry.Type, Testable: isTestableProxy(name, entry)}
	for _, history := range entry.History {
		if state.Latest != nil && history.Time.Before(state.Latest.TestedAt) {
			continue
		}
		status := NodeTestStatusFailed
		if history.Delay > 0 {
			status = NodeTestStatusSuccess
		}
		latest := NodeDelay{Name: name, Delay: history.Delay, Status: status, TestedAt: history.Time}
		state.Latest = &latest
	}
	return state
}

func isTestableProxy(name string, entry proxyEntry) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "DIRECT") || strings.EqualFold(name, "REJECT") || strings.HasPrefix(name, "官网") || strings.HasPrefix(name, "有效期") {
		return false
	}
	if len(entry.All) > 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(entry.Type)) {
	case "", "selector", "urltest", "fallback", "loadbalance", "direct", "reject", "rejectdrop", "pass", "compatible":
		return false
	default:
		return true
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func contextFromStop(stop <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if stop != nil {
		go func() {
			select {
			case <-stop:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	return ctx, cancel
}

func (c *Client) callJSON(ctx context.Context, method, path string, payload, target any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.paths.APIAddr+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, maxRuntimeResponseBytes+1))
	if err != nil {
		return err
	}
	if len(content) > maxRuntimeResponseBytes {
		return fmt.Errorf("mihomo response exceeds %d bytes", maxRuntimeResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("mihomo returned status %d", response.StatusCode)
	}
	if target == nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("mihomo returned multiple JSON values")
		}
		return err
	}
	return nil
}
