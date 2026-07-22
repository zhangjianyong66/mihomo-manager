package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func newModeCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "mode", Short: "查看和切换三种路由模式", Args: noArgs}
	command.SetFlagErrorFunc(invalidFlagError)
	if deps.Capabilities == nil {
		return command
	}

	status := &cobra.Command{Use: "status", Short: "查看当前配置和运行模式", Args: noArgs}
	var statusProfile string
	var statusOptions OutputOptions
	bindModeOptions(status, &statusProfile, &statusOptions)
	status.RunE = func(cmd *cobra.Command, _ []string) error {
		value, err := deps.Capabilities.ModeStatus(cmd.Context(), statusProfile)
		if err != nil {
			return guideModeError(err)
		}
		return writeModeResult(cmd, statusOptions, value, "RoutingModeStatus")
	}

	set := &cobra.Command{Use: "set <global|rule|direct>", Short: "切换路由模式", Args: exactArgs(1)}
	var setProfile string
	var closeConnections bool
	var setOptions OutputOptions
	bindModeOptions(set, &setProfile, &setOptions)
	set.Flags().BoolVar(&closeConnections, "close-connections", false, "切换成功后关闭现有连接")
	set.RunE = func(cmd *cobra.Command, args []string) error {
		mode := domain.RoutingMode(strings.ToLower(strings.TrimSpace(args[0])))
		if err := mode.Validate(); err != nil {
			return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCode("INVALID_ROUTING_MODE"), Message: "路由模式必须为 global、rule 或 direct", Err: err}
		}
		value, err := deps.Capabilities.SetMode(cmd.Context(), app.SetRoutingModeRequest{
			ProfileID: domain.ProfileID(setProfile), Mode: mode, CloseConnections: closeConnections,
		})
		if err != nil {
			return guideModeError(attachModeStatus(err, value))
		}
		return writeModeResult(cmd, setOptions, value, "RoutingModeChange")
	}

	command.AddCommand(status, set)
	return command
}

func writeModeResult(command *cobra.Command, options OutputOptions, value app.RoutingModeStatus, kind string) error {
	result := modeResult(value, kind)
	if options.Format == OutputTable {
		result.Warnings = nil
	}
	return capabilityResult(command, options, result)
}

func bindModeOptions(command *cobra.Command, profile *string, options *OutputOptions) {
	command.Flags().StringVar(profile, "profile", "", "档案 ID（默认使用活动 legacy 档案）")
	if options.Format == "" {
		options.Format = OutputTable
	}
	command.Flags().Var(&options.Format, "output", "输出格式：table 或 json")
	command.SetFlagErrorFunc(invalidFlagError)
}

func guideModeError(err error) error {
	var appErr *app.Error
	if !errors.As(err, &appErr) {
		return err
	}
	copy := *appErr
	switch copy.Code {
	case app.ErrorCodeDaemonUnavailable:
		copy.Message = "daemon 不可用，请执行 mm daemon status；必要时执行 mm daemon start"
	case app.ErrorCodeNotFound:
		copy.Message = "尚未迁移 legacy 配置，请执行 mm migrate plan，然后执行 mm migrate apply"
	}
	return &copy
}

func attachModeStatus(err error, status app.RoutingModeStatus) error {
	if err == nil || (status.ProfileID == "" && status.ConfigMode == "" && status.OperationID == "") {
		return err
	}
	var appErr *app.Error
	if !errors.As(err, &appErr) {
		return err
	}
	copy := *appErr
	copy.Details = make(map[string]any, len(appErr.Details)+1)
	for key, value := range appErr.Details {
		copy.Details[key] = value
	}
	if _, exists := copy.Details["status"]; !exists {
		copy.Details["status"] = modeData(status)
	}
	return &copy
}

func modeResult(value app.RoutingModeStatus, kind string) Result {
	warnings := append([]string(nil), value.Warnings...)
	return Result{
		Kind:     kind,
		Warnings: warnings,
		Data: func(bool) any {
			return modeData(value)
		},
		Table: func(w io.Writer, _ bool) error {
			return writeModeTable(w, value)
		},
	}
}

func modeData(value app.RoutingModeStatus) map[string]any {
	rules := make([]map[string]any, 0, len(value.RuleSets))
	for _, item := range value.RuleSets {
		rules = append(rules, map[string]any{
			"name": item.Name, "available": item.Available, "loaded": item.Loaded,
		})
	}
	return map[string]any{
		"profileId":            value.ProfileID,
		"configMode":           value.ConfigMode,
		"runtimeMode":          value.RuntimeMode,
		"runtimeAvailable":     value.RuntimeAvailable,
		"coreState":            value.CoreState,
		"effectiveGroup":       value.EffectiveGroup,
		"effectiveNode":        value.EffectiveNode,
		"ruleSets":             rules,
		"activeConnections":    value.ActiveConnections,
		"connectionsAvailable": value.ConnectionsAvailable,
		"connectionsClosed":    value.ConnectionsClosed,
		"nextStart":            value.NextStart,
		"operationId":          value.OperationID,
		"operationPhase":       value.OperationPhase,
		"listeners":            proxyListenerData(value.Listeners),
		"systemProxy":          proxySourceData(value.SystemProxy),
		"environmentProxy":     proxySourceData(value.EnvironmentProxy),
	}
}

func writeModeTable(w io.Writer, value app.RoutingModeStatus) error {
	if _, err := fmt.Fprintf(w, "档案: %s\n配置模式: %s\n运行模式: %s\nCore 状态: %s\n", value.ProfileID, modeLabel(value.ConfigMode), runtimeModeLabel(value), coreStateLabel(value.CoreState)); err != nil {
		return err
	}
	group := value.EffectiveGroup
	if group == "" {
		group = "-"
	}
	node := value.EffectiveNode
	if node == "" {
		node = "-"
	}
	if _, err := fmt.Fprintf(w, "有效组: %s\n有效节点: %s\n", group, node); err != nil {
		return err
	}
	if value.ConnectionsAvailable {
		if _, err := fmt.Fprintf(w, "活动连接: %d\n", value.ActiveConnections); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w, "活动连接: 不可用"); err != nil {
		return err
	}
	if value.ConfigMode != domain.RoutingModeRule || len(value.RuleSets) == 0 {
		if _, err := fmt.Fprintln(w, "规则集: 无需核验"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(w, "规则集:"); err != nil {
			return err
		}
		for _, item := range value.RuleSets {
			state := "异常"
			if !value.RuntimeAvailable {
				state = "待 Core 启动后核验"
			} else if item.Available && item.Loaded {
				state = "正常"
			}
			if _, err := fmt.Fprintf(w, "  %s: %s\n", item.Name, state); err != nil {
				return err
			}
		}
	}
	if value.NextStart {
		if _, err := fmt.Fprintln(w, "生效时机: 下次启动 mihomo 时生效"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(w, "生效时机: 已在运行中生效"); err != nil {
			return err
		}
	}
	if value.ConnectionsClosed {
		if _, err := fmt.Fprintln(w, "连接处理: 已关闭现有连接"); err != nil {
			return err
		}
	}
	if value.OperationID != "" {
		if _, err := fmt.Fprintf(w, "操作: %s (%s)\n", value.OperationID, value.OperationPhase); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "mihomo listeners:"); err != nil {
		return err
	}
	if len(value.Listeners) == 0 {
		if _, err := fmt.Fprintln(w, "  无"); err != nil {
			return err
		}
	}
	for _, listener := range value.Listeners {
		if _, err := fmt.Fprintf(w, "  %s: %s\n", listener.Protocol, net.JoinHostPort(listener.Host, strconv.Itoa(listener.Port))); err != nil {
			return err
		}
	}
	if err := writeProxySources(w, "GNOME 系统代理", value.SystemProxy); err != nil {
		return err
	}
	if err := writeProxySources(w, "当前 CLI 环境代理", value.EnvironmentProxy); err != nil {
		return err
	}
	for _, warning := range value.Warnings {
		if _, err := fmt.Fprintf(w, "警告: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

func proxyListenerData(values []app.ProxyListener) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"protocol": value.Protocol, "host": value.Host, "port": value.Port})
	}
	return result
}

func proxySourceData(values []app.ProxySourceStatus) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		var endpoint any
		if value.Endpoint != nil {
			endpoint = map[string]any{"scheme": value.Endpoint.Scheme, "host": value.Endpoint.Host, "port": value.Endpoint.Port}
		}
		result = append(result, map[string]any{
			"source": value.Source, "expectedProtocol": value.ExpectedProtocol, "state": value.State,
			"endpoint": endpoint, "warning": value.Warning,
		})
	}
	return result
}

func writeProxySources(w io.Writer, title string, values []app.ProxySourceStatus) error {
	if _, err := fmt.Fprintln(w, title+":"); err != nil {
		return err
	}
	if len(values) == 0 {
		_, err := fmt.Fprintln(w, "  未检测")
		return err
	}
	for _, value := range values {
		endpoint := "-"
		if value.Endpoint != nil {
			endpoint = value.Endpoint.Scheme + "://" + net.JoinHostPort(value.Endpoint.Host, strconv.Itoa(value.Endpoint.Port))
		}
		if _, err := fmt.Fprintf(w, "  %s: %s (%s)\n", value.Source, value.State, endpoint); err != nil {
			return err
		}
	}
	return nil
}

func modeLabel(mode domain.RoutingMode) string {
	switch mode {
	case domain.RoutingModeGlobal:
		return "全局代理 (global)"
	case domain.RoutingModeRule:
		return "规则分流 (rule)"
	case domain.RoutingModeDirect:
		return "全局直连 (direct)"
	default:
		return string(mode)
	}
}

func runtimeModeLabel(value app.RoutingModeStatus) string {
	if !value.RuntimeAvailable || value.RuntimeMode == nil {
		return "不可用 (Core 已停止)"
	}
	return modeLabel(*value.RuntimeMode)
}

func coreStateLabel(state domain.CoreState) string {
	switch state {
	case domain.CoreStateRunning:
		return "运行中 (running)"
	case domain.CoreStateStopped:
		return "已停止 (stopped)"
	case domain.CoreStateFailed:
		return "失败 (failed)"
	default:
		return string(state)
	}
}
