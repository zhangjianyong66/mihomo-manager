package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func newRouteConnectionsCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "connections", Short: "查看 mihomo 实时活动连接", Args: noArgs}
	var profile string
	var follow bool
	var output string
	command.Flags().StringVar(&profile, "profile", "", "档案 ID（默认使用活动 legacy 档案）")
	command.Flags().BoolVar(&follow, "follow", false, "持续跟随连接变化")
	command.Flags().StringVar(&output, "output", "table", "输出格式：table、json、text 或 ndjson")
	command.SetFlagErrorFunc(invalidFlagError)
	command.RunE = func(command *cobra.Command, _ []string) error {
		if follow {
			if output != "text" && output != "ndjson" {
				return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCodeInvalidArgument, Message: "连接 follow 只支持 text 或 ndjson 输出"}
			}
			return followConnections(command, deps.Capabilities, app.ConnectionRequest{ProfileID: domain.ProfileID(profile)}, output == "ndjson")
		}
		if output != "table" && output != "json" {
			return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCodeInvalidArgument, Message: "连接快照只支持 table 或 json 输出"}
		}
		values, err := deps.Capabilities.Connections(command.Context(), app.ConnectionRequest{ProfileID: domain.ProfileID(profile)})
		if err != nil {
			return err
		}
		warnings := connectionWarnings(values)
		return capabilityResult(command, OutputOptions{Format: OutputFormat(output)}, Result{
			Kind: "RouteConnections", Warnings: warnings,
			Data:  func(bool) any { return connectionData(values) },
			Table: func(w io.Writer, _ bool) error { return writeConnectionTable(w, values, warnings) },
		})
	}
	return command
}

func followConnections(command *cobra.Command, service app.CapabilityAPI, request app.ConnectionRequest, ndjson bool) error {
	stream := service.FollowConnections(command.Context(), request)
	lastSeq := uint64(0)
	for event := range stream {
		seq := event.Seq
		if seq <= lastSeq {
			seq = lastSeq + 1
		}
		lastSeq = seq
		if event.Err != nil {
			if errors.Is(event.Err, context.Canceled) {
				return writeConnectionDone(command, ndjson, lastSeq)
			}
			if ndjson {
				if err := writeJSON(command.OutOrStdout(), map[string]any{"apiVersion": APIVersion, "kind": "ConnectionStreamError", "seq": seq, "error": map[string]any{"message": event.Err.Error()}, "warnings": []string{}}); err != nil {
					return err
				}
				return &alreadyWrittenError{err: event.Err}
			}
			return event.Err
		}
		if event.Finished {
			return writeConnectionDone(command, ndjson, seq)
		}
		if event.Connection == nil {
			continue
		}
		if ndjson {
			if err := writeJSON(command.OutOrStdout(), map[string]any{
				"apiVersion": APIVersion, "kind": "ConnectionEvent", "seq": seq,
				"data":     map[string]any{"action": event.Action, "time": event.Time.UTC().Format(time.RFC3339Nano), "connection": connectionData([]app.Connection{*event.Connection})[0]},
				"warnings": []string{},
			}); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(command.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\t%s/%s\n",
			event.Time.Local().Format("15:04:05"), event.Action, connectionTarget(*event.Connection),
			connectionRule(*event.Connection), connectionChain(*event.Connection), formatBytes(event.Connection.Upload), formatBytes(event.Connection.Download)); err != nil {
			return err
		}
	}
	return writeConnectionDone(command, ndjson, lastSeq+1)
}

func writeConnectionDone(command *cobra.Command, ndjson bool, seq uint64) error {
	if seq < 1 {
		seq = 1
	}
	if ndjson {
		return writeJSON(command.OutOrStdout(), map[string]any{"apiVersion": APIVersion, "kind": "ConnectionStreamDone", "seq": seq, "data": map[string]string{"status": "done"}, "warnings": []string{}})
	}
	return nil
}

func connectionData(values []app.Connection) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		started := ""
		if !value.Start.IsZero() {
			started = value.Start.UTC().Format(time.RFC3339Nano)
		}
		result = append(result, map[string]any{
			"id": value.ID, "host": value.Host, "destinationIp": value.DestinationIP,
			"destinationPort": value.DestinationPort, "target": connectionTarget(value),
			"network": value.Network, "rule": value.Rule, "rulePayload": value.RulePayload,
			"chains": append([]string(nil), value.Chains...), "finalNode": value.FinalNode,
			"upload": value.Upload, "download": value.Download, "start": started,
			"warnings": append([]string(nil), value.Warnings...),
		})
	}
	return result
}

func writeConnectionTable(w io.Writer, values []app.Connection, warnings []string) error {
	if _, err := fmt.Fprintln(w, "ID\t目标\t网络\t规则\t完整链路\t最终节点\t上传\t下载"); err != nil {
		return err
	}
	for _, value := range values {
		rule := connectionRule(value)
		chains := connectionChain(value)
		finalNode := value.FinalNode
		if finalNode == "" {
			finalNode = "-"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", value.ID, connectionTarget(value), emptyDash(value.Network), rule, chains, finalNode, formatBytes(value.Upload), formatBytes(value.Download)); err != nil {
			return err
		}
	}
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(w, "警告: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func connectionRule(value app.Connection) string {
	rule := value.Rule
	if value.RulePayload != "" {
		rule += ":" + value.RulePayload
	}
	return emptyDash(rule)
}

func connectionChain(value app.Connection) string {
	return emptyDash(strings.Join(value.Chains, " -> "))
}

func connectionTarget(value app.Connection) string {
	host := strings.TrimSpace(value.Host)
	if host == "" {
		host = strings.TrimSpace(value.DestinationIP)
	}
	if host == "" {
		host = "-"
	}
	if value.DestinationPort <= 0 {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(value.DestinationPort))
}

func connectionWarnings(values []app.Connection) []string {
	result := make([]string, 0)
	for _, value := range values {
		for _, warning := range value.Warnings {
			result = append(result, fmt.Sprintf("连接 %s: %s", emptyDash(value.ID), warning))
		}
	}
	return result
}

func formatBytes(value int64) string {
	const unit = int64(1024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor, suffix := unit, "KiB"
	if value >= unit*unit {
		divisor, suffix = unit*unit, "MiB"
	}
	if value >= unit*unit*unit {
		divisor, suffix = unit*unit*unit, "GiB"
	}
	return fmt.Sprintf("%.1f %s", float64(value)/float64(divisor), suffix)
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
