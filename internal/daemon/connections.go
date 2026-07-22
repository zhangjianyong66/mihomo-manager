package daemon

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
)

var ErrConnectionRuntimeUnavailable = errors.New("mihomo connection runtime is unavailable")

const defaultConnectionPollInterval = time.Second

type ConnectionInfo struct {
	ID              string    `json:"id"`
	Host            string    `json:"host,omitempty"`
	DestinationIP   string    `json:"destinationIp,omitempty"`
	DestinationPort int       `json:"destinationPort,omitempty"`
	Network         string    `json:"network,omitempty"`
	Rule            string    `json:"rule,omitempty"`
	RulePayload     string    `json:"rulePayload,omitempty"`
	Chains          []string  `json:"chains"`
	FinalNode       string    `json:"finalNode,omitempty"`
	Upload          int64     `json:"upload"`
	Download        int64     `json:"download"`
	Start           time.Time `json:"start,omitempty"`
	Warnings        []string  `json:"warnings"`
}

type ConnectionEvent struct {
	Action     string         `json:"action"`
	Connection ConnectionInfo `json:"connection"`
	Time       time.Time      `json:"time"`
	Err        error          `json:"-"`
	Finished   bool           `json:"-"`
}

func (s *CapabilityService) Connections(ctx context.Context, profileID string) ([]ConnectionInfo, error) {
	profile, _, err := s.profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if !profile.Active {
		return nil, fmt.Errorf("profile %s is not active: %w", profile.ID, ErrCapabilityUnsupported)
	}
	state, runtime, err := s.routingRuntime(profile.ID)
	if err != nil {
		return nil, err
	}
	if state != domain.CoreStateRunning || runtime == nil {
		return nil, ErrConnectionRuntimeUnavailable
	}
	values, err := runtime.Connections(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ConnectionInfo, 0, len(values))
	for _, value := range values {
		result = append(result, convertConnection(value))
	}
	return result, nil
}

func convertConnection(value mihomo.RuntimeConnection) ConnectionInfo {
	chains := append([]string(nil), value.Chains...)
	warnings := append([]string(nil), value.Warnings...)
	if chains == nil {
		chains = []string{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	finalNode := ""
	if len(chains) > 0 {
		finalNode = strings.TrimSpace(chains[0])
	}
	return ConnectionInfo{
		ID: value.ID, Host: value.Host, DestinationIP: value.DestinationIP, DestinationPort: value.DestinationPort,
		Network: value.Network, Rule: value.Rule, RulePayload: value.RulePayload,
		Chains: chains, FinalNode: finalNode, Upload: value.Upload, Download: value.Download,
		Start: value.Start, Warnings: warnings,
	}
}

func (s *CapabilityService) FollowConnections(ctx context.Context, profileID string) (<-chan ConnectionEvent, error) {
	profile, _, err := s.profile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if !profile.Active {
		return nil, fmt.Errorf("profile %s is not active: %w", profile.ID, ErrCapabilityUnsupported)
	}
	state, runtime, err := s.routingRuntime(profile.ID)
	if err != nil {
		return nil, err
	}
	if state != domain.CoreStateRunning || runtime == nil {
		return nil, ErrConnectionRuntimeUnavailable
	}
	interval := s.connectionPollInterval
	if interval <= 0 {
		interval = defaultConnectionPollInterval
	}
	clock := s.clock
	if clock == nil {
		clock = time.Now
	}
	result := make(chan ConnectionEvent, 64)
	go produceConnectionEvents(ctx, result, interval, clock, runtime.Connections)
	return result, nil
}

func produceConnectionEvents(ctx context.Context, output chan<- ConnectionEvent, interval time.Duration, clock func() time.Time, fetch func(context.Context) ([]mihomo.RuntimeConnection, error)) {
	defer close(output)
	previous := map[string]ConnectionInfo{}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		values, err := fetch(ctx)
		if err != nil {
			sendDaemonConnectionEvent(ctx, output, ConnectionEvent{Err: err, Finished: true})
			return
		}
		current := make(map[string]ConnectionInfo, len(values))
		for _, value := range values {
			info := convertConnection(value)
			if strings.TrimSpace(info.ID) == "" {
				continue
			}
			current[info.ID] = info
		}
		if len(current) > mihomo.MaxRuntimeConnections {
			sendDaemonConnectionEvent(ctx, output, ConnectionEvent{Err: fmt.Errorf("connection follow limit exceeded: %d", len(current)), Finished: true})
			return
		}
		emitConnectionDiff(ctx, output, clock().UTC(), previous, current)
		previous = current
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func emitConnectionDiff(ctx context.Context, output chan<- ConnectionEvent, now time.Time, previous, current map[string]ConnectionInfo) {
	ids := make([]string, 0, len(previous)+len(current))
	seen := make(map[string]struct{}, len(previous)+len(current))
	for id := range previous {
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for id := range current {
		if _, ok := seen[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		before, existed := previous[id]
		after, exists := current[id]
		switch {
		case !existed && exists:
			if !sendDaemonConnectionEvent(ctx, output, ConnectionEvent{Action: "open", Connection: after, Time: now}) {
				return
			}
		case existed && !exists:
			if !sendDaemonConnectionEvent(ctx, output, ConnectionEvent{Action: "closed", Connection: before, Time: now}) {
				return
			}
		case existed && exists && !reflect.DeepEqual(before, after):
			if !sendDaemonConnectionEvent(ctx, output, ConnectionEvent{Action: "update", Connection: after, Time: now}) {
				return
			}
		}
	}
}

func sendDaemonConnectionEvent(ctx context.Context, output chan<- ConnectionEvent, event ConnectionEvent) bool {
	select {
	case output <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
