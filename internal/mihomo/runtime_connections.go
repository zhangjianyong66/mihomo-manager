package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const MaxRuntimeConnections = 4096

// RuntimeConnection is the stable subset of mihomo's /connections payload.
// Raw controller fields are normalized here so downstream consumers never
// need to know whether mihomo encoded a port as a string or a number.
type RuntimeConnection struct {
	ID              string
	Host            string
	DestinationIP   string
	DestinationPort int
	Network         string
	Rule            string
	RulePayload     string
	Chains          []string
	Upload          int64
	Download        int64
	Start           time.Time
	Warnings        []string
}

type runtimeConnectionsResponse struct {
	Connections []runtimeConnectionPayload `json:"connections"`
}

type runtimeConnectionPayload struct {
	ID       string `json:"id"`
	Metadata struct {
		Network         string          `json:"network"`
		Host            string          `json:"host"`
		DestinationIP   string          `json:"destinationIP"`
		DestinationPort json.RawMessage `json:"destinationPort"`
	} `json:"metadata"`
	Upload      int64    `json:"upload"`
	Download    int64    `json:"download"`
	Start       string   `json:"start"`
	Chains      []string `json:"chains"`
	Rule        string   `json:"rule"`
	RulePayload string   `json:"rulePayload"`
}

func (c *runtimeClient) Connections(ctx context.Context) ([]RuntimeConnection, error) {
	var response runtimeConnectionsResponse
	if err := c.getJSON(ctx, "/connections", &response); err != nil {
		return nil, err
	}
	if len(response.Connections) > MaxRuntimeConnections {
		return nil, fmt.Errorf("mihomo returned %d connections, limit is %d", len(response.Connections), MaxRuntimeConnections)
	}
	result := make([]RuntimeConnection, 0, len(response.Connections))
	for _, value := range response.Connections {
		connection := RuntimeConnection{
			ID: value.ID, Host: strings.TrimSpace(value.Metadata.Host), DestinationIP: strings.TrimSpace(value.Metadata.DestinationIP),
			Network: strings.ToLower(strings.TrimSpace(value.Metadata.Network)), Rule: strings.TrimSpace(value.Rule), RulePayload: strings.TrimSpace(value.RulePayload),
			Chains: normalizeChains(value.Chains), Upload: value.Upload, Download: value.Download,
		}
		if connection.Upload < 0 {
			connection.Upload = 0
			connection.Warnings = append(connection.Warnings, "mihomo 返回了负数上传计数，已按 0 处理")
		}
		if connection.Download < 0 {
			connection.Download = 0
			connection.Warnings = append(connection.Warnings, "mihomo 返回了负数下载计数，已按 0 处理")
		}
		port, err := parseRuntimePort(value.Metadata.DestinationPort)
		if err != nil {
			connection.Warnings = append(connection.Warnings, "mihomo 返回的目标端口无效")
		} else {
			connection.DestinationPort = port
		}
		if strings.TrimSpace(value.Start) != "" {
			started, err := time.Parse(time.RFC3339Nano, value.Start)
			if err != nil {
				connection.Warnings = append(connection.Warnings, "mihomo 返回的连接开始时间无效")
			} else {
				connection.Start = started
			}
		}
		result = append(result, connection)
	}
	return result, nil
}

func parseRuntimePort(raw json.RawMessage) (int, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, nil
	}
	var number json.Number
	if trimmed[0] == '"' {
		var value string
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return 0, err
		}
		number = json.Number(strings.TrimSpace(value))
	} else {
		number = json.Number(string(trimmed))
	}
	port64, err := strconv.ParseInt(number.String(), 10, 32)
	if err != nil || port64 < 0 || port64 > 65535 {
		return 0, fmt.Errorf("invalid destination port")
	}
	return int(port64), nil
}

func normalizeChains(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
