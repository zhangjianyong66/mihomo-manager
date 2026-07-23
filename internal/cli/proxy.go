package cli

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

func newProxyCommand(deps Dependencies) *cobra.Command {
	root := &cobra.Command{Use: "proxy", Short: "管理 GNOME 与 Bash 代理", Args: noArgs}
	root.AddCommand(newProxyLayerCommand(deps, "system", "GNOME 系统代理", true), newProxyLayerCommand(deps, "env", "Bash 环境代理", false))
	return root
}

func newProxyLayerCommand(deps Dependencies, layer, title string, restore bool) *cobra.Command {
	command := &cobra.Command{Use: layer, Short: title, Args: noArgs}
	var profile string
	var options OutputOptions
	status := &cobra.Command{Use: "status", Short: "查看当前代理状态", Args: noArgs}
	bindCapabilityOptions(status, &profile, &options)
	status.RunE = func(cmd *cobra.Command, _ []string) error {
		value, err := deps.Capabilities.ProxyStatus(cmd.Context(), layer, profile)
		if err != nil {
			return err
		}
		return writeProxyCLIResult(cmd, options, value, "ProxyConfigStatus", title)
	}
	set := &cobra.Command{Use: "set <http|https|socks|all> [ip port]", Short: "设置代理入口", Args: proxySetArgs}
	var setProfile string
	var setOptions OutputOptions
	bindCapabilityOptions(set, &setProfile, &setOptions)
	set.RunE = func(cmd *cobra.Command, args []string) error {
		request := app.ProxyRequest{Layer: layer, ProfileID: setProfile, Target: args[0]}
		if len(args) == 3 {
			request.Host = strings.Trim(args[1], "[]")
			if net.ParseIP(request.Host) == nil {
				return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCodeInvalidArgument, Message: "代理地址必须是 IPv4 或 IPv6 字面量"}
			}
			port, err := strconv.Atoi(args[2])
			if err != nil {
				return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCodeInvalidArgument, Message: "端口必须是数字", Err: err}
			}
			request.Port = port
		}
		value, err := deps.Capabilities.SetProxy(cmd.Context(), request)
		if err != nil {
			return err
		}
		return writeProxyCLIResult(cmd, setOptions, value, "ProxyConfigSet", title)
	}
	command.AddCommand(status, set)
	if restore {
		restoreCommand := &cobra.Command{Use: "restore", Short: "恢复 GNOME 启用前设置", Args: noArgs}
		var restoreProfile string
		var restoreOptions OutputOptions
		bindCapabilityOptions(restoreCommand, &restoreProfile, &restoreOptions)
		restoreCommand.RunE = func(cmd *cobra.Command, _ []string) error {
			value, err := deps.Capabilities.RestoreProxy(cmd.Context(), app.ProxyRequest{Layer: layer, ProfileID: restoreProfile})
			if err != nil {
				return err
			}
			return writeProxyCLIResult(cmd, restoreOptions, value, "ProxyConfigRestore", title)
		}
		command.AddCommand(restoreCommand)
	} else {
		disableCommand := &cobra.Command{Use: "disable", Short: "删除 Bash 管理区块", Args: noArgs}
		var disableProfile string
		var disableOptions OutputOptions
		bindCapabilityOptions(disableCommand, &disableProfile, &disableOptions)
		disableCommand.RunE = func(cmd *cobra.Command, _ []string) error {
			value, err := deps.Capabilities.DisableProxy(cmd.Context(), app.ProxyRequest{Layer: layer, ProfileID: disableProfile})
			if err != nil {
				return err
			}
			return writeProxyCLIResult(cmd, disableOptions, value, "ProxyConfigDisable", title)
		}
		command.AddCommand(disableCommand)
	}
	return command
}

func proxySetArgs(_ *cobra.Command, args []string) error {
	if len(args) != 1 && len(args) != 3 {
		return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCodeInvalidArgument, Message: "set 需要 target，或同时提供 IP 和端口"}
	}
	target := strings.ToLower(strings.TrimSpace(args[0]))
	if target != "http" && target != "https" && target != "socks" && target != "all" {
		return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCodeInvalidArgument, Message: "代理类型必须为 http、https、socks 或 all"}
	}
	return nil
}

func writeProxyCLIResult(command *cobra.Command, options OutputOptions, value app.ProxyConfigStatus, kind, title string) error {
	return capabilityResult(command, options, Result{
		Kind:     kind,
		Warnings: value.Warnings,
		Data: func(bool) any {
			endpoints := make([]map[string]any, 0, len(value.Endpoints))
			for _, item := range value.Endpoints {
				endpoints = append(endpoints, map[string]any{"target": item.Target, "scheme": item.Scheme, "host": item.Host, "port": item.Port, "state": item.State, "warning": item.Warning})
			}
			return map[string]any{"layer": value.Layer, "profileId": value.ProfileID, "coreState": value.CoreState, "managed": value.Managed, "snapshotAvailable": value.SnapshotAvailable, "nextStart": value.NextStart, "endpoints": endpoints, "updatedAt": value.UpdatedAt}
		},
		Table: func(w io.Writer, _ bool) error {
			if _, err := fmt.Fprintf(w, "%s\n层: %s\n档案: %s\n已由 manager 管理: %t\n恢复快照: %t\n", title, value.Layer, value.ProfileID, value.Managed, value.SnapshotAvailable); err != nil {
				return err
			}
			for _, item := range value.Endpoints {
				endpoint := "禁用"
				if item.Host != "" && item.Port > 0 {
					endpoint = item.Scheme + "://" + net.JoinHostPort(item.Host, strconv.Itoa(item.Port))
				}
				if _, err := fmt.Fprintf(w, "%s: %s [%s]\n", item.Target, endpoint, item.State); err != nil {
					return err
				}
				if item.Warning != "" {
					if _, err := fmt.Fprintln(w, "  警告: "+item.Warning); err != nil {
						return err
					}
				}
			}
			if value.Layer == "env" {
				_, _ = fmt.Fprintln(w, "生效范围: 新 Bash 终端；当前终端请执行 source ~/.bashrc")
			} else {
				_, _ = fmt.Fprintln(w, "生效范围: GNOME 桌面应用")
			}
			if value.NextStart {
				_, _ = fmt.Fprintln(w, "Core 状态: 当前未运行，代理配置不会自动启动 Core")
			}
			return nil
		},
	})
}
