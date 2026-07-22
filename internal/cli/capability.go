package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

func newCapabilityCommands(deps Dependencies) []*cobra.Command {
	if deps.Capabilities == nil {
		return nil
	}
	return []*cobra.Command{newModeCommand(deps), newCoreCommand(deps), newGroupCommand(deps), newNodeCommand(deps), newSubscriptionCommand(deps), newRouteCommand(deps), newConfigCommand(deps)}
}

func bindCapabilityOptions(command *cobra.Command, profile *string, options *OutputOptions) {
	command.Flags().StringVar(profile, "profile", "", "档案 ID（默认使用活动 legacy 档案）")
	BindOutputOptions(command, options)
}

func capabilityResult(command *cobra.Command, options OutputOptions, result Result) error {
	return NewPresenter(command.OutOrStdout(), command.ErrOrStderr(), options).WriteResult(result)
}

func capabilityMutation(command *cobra.Command, options OutputOptions, action string, run func() error) error {
	if err := run(); err != nil {
		return err
	}
	return capabilityResult(command, options, Result{
		Kind:  "OperationResult",
		Data:  func(bool) any { return map[string]string{"action": action, "status": "succeeded"} },
		Table: func(w io.Writer, _ bool) error { _, err := fmt.Fprintf(w, "%s 已完成\n", action); return err },
	})
}

func newCoreCommand(deps Dependencies) *cobra.Command {
	coreCommand := &cobra.Command{Use: "core", Short: "管理 mihomo core", Args: noArgs}
	status := &cobra.Command{Use: "status", Short: "查询 core 状态", Args: noArgs}
	var profile string
	var options OutputOptions
	bindCapabilityOptions(status, &profile, &options)
	status.RunE = func(command *cobra.Command, _ []string) error {
		value, err := deps.Capabilities.CoreStatus(command.Context(), profile)
		if err != nil {
			return err
		}
		return capabilityResult(command, options, Result{
			Kind: "CoreStatus",
			Data: func(bool) any {
				return map[string]any{"type": value.Type, "state": value.State, "profileId": value.ProfileID, "pid": value.PID}
			},
			Table: func(w io.Writer, _ bool) error {
				_, err := fmt.Fprintf(w, "状态: %s\n档案: %s\nPID: %d\n", value.State, value.ProfileID, value.PID)
				return err
			},
		})
	}
	coreCommand.AddCommand(status)
	for _, action := range []string{"start", "stop", "restart", "reload"} {
		action := action
		command := &cobra.Command{Use: action, Short: action + " core", Args: noArgs}
		var profile string
		var options OutputOptions
		bindCapabilityOptions(command, &profile, &options)
		command.RunE = func(command *cobra.Command, _ []string) error {
			return capabilityMutation(command, options, "core "+action, func() error { return deps.Capabilities.CoreAction(command.Context(), profile, action) })
		}
		coreCommand.AddCommand(command)
	}
	coreCommand.AddCommand(newCoreLogsCommand(deps))
	return coreCommand
}

func newCoreLogsCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "logs", Short: "查看 core 日志", Args: noArgs}
	var profile string
	var lines int
	var follow bool
	var filter string
	var output string
	var showSecrets bool
	command.Flags().StringVar(&profile, "profile", "", "档案 ID（默认使用活动 legacy 档案）")
	command.Flags().IntVar(&lines, "lines", 50, "初始日志行数")
	command.Flags().BoolVar(&follow, "follow", false, "持续跟随日志")
	command.Flags().StringVar(&filter, "filter", "", "日志正则过滤")
	command.Flags().StringVar(&output, "output", "table", "输出格式：table、json、text 或 ndjson")
	command.Flags().BoolVar(&showSecrets, "show-secrets", false, "显示完整敏感信息")
	command.SetFlagErrorFunc(invalidFlagError)
	command.RunE = func(command *cobra.Command, _ []string) error {
		if lines < 1 || lines > 10000 {
			return &app.Error{Code: app.ErrorCodeInvalidArgument, Message: "lines 必须在 1 到 10000 之间"}
		}
		matcher, err := compileFilter(filter)
		if err != nil {
			return err
		}
		if follow {
			if output != "text" && output != "ndjson" {
				return &app.Error{Code: app.ErrorCodeInvalidArgument, Message: "follow 只支持 text 或 ndjson 输出"}
			}
			return followLogs(command, deps.Capabilities, profile, lines, matcher, output == "ndjson", showSecrets)
		}
		if output != "table" && output != "json" {
			return &app.Error{Code: app.ErrorCodeInvalidArgument, Message: "非 follow 日志只支持 table 或 json 输出"}
		}
		values, err := deps.Capabilities.TailLogs(command.Context(), app.LogRequest{ProfileID: domain.ProfileID(profile), Lines: lines})
		if err != nil {
			return err
		}
		values = filterLogs(values, matcher)
		options := OutputOptions{Format: OutputFormat(output), ShowSecrets: showSecrets}
		return capabilityResult(command, options, Result{
			Kind:  "CoreLogs",
			Data:  func(show bool) any { return map[string]any{"lines": logData(values, show)} },
			Table: func(w io.Writer, show bool) error { return writeLogTable(w, values, show) },
		})
	}
	return command
}

func newGroupCommand(deps Dependencies) *cobra.Command {
	groupCommand := &cobra.Command{Use: "group", Short: "管理代理组", Args: noArgs}
	list := &cobra.Command{Use: "list", Short: "列出代理组", Args: noArgs}
	var profile string
	var options OutputOptions
	bindCapabilityOptions(list, &profile, &options)
	list.RunE = func(command *cobra.Command, _ []string) error {
		values, err := deps.Capabilities.Groups(command.Context(), profile)
		if err != nil {
			return err
		}
		return capabilityResult(command, options, Result{Kind: "GroupList", Data: func(bool) any { return groupData(values) }, Table: func(w io.Writer, show bool) error { return writeGroupTable(w, values, show) }})
	}
	show := &cobra.Command{Use: "show <group>", Short: "查看代理组", Args: exactArgs(1)}
	var showProfile string
	var showOptions OutputOptions
	bindCapabilityOptions(show, &showProfile, &showOptions)
	show.RunE = func(command *cobra.Command, args []string) error {
		value, err := deps.Capabilities.Group(command.Context(), showProfile, args[0])
		if err != nil {
			return err
		}
		return capabilityResult(command, showOptions, Result{Kind: "Group", Data: func(bool) any { return groupData([]app.Group{value})[0] }, Table: func(w io.Writer, show bool) error { return writeGroupTable(w, []app.Group{value}, show) }})
	}
	selectCommand := &cobra.Command{Use: "select <group> <node>", Short: "选择代理组节点", Args: exactArgs(2)}
	var selectProfile string
	var selectOptions OutputOptions
	bindCapabilityOptions(selectCommand, &selectProfile, &selectOptions)
	selectCommand.RunE = func(command *cobra.Command, args []string) error {
		return capabilityMutation(command, selectOptions, "group select", func() error {
			return deps.Capabilities.SelectGroupNode(command.Context(), selectProfile, args[0], args[1])
		})
	}
	groupCommand.AddCommand(list, show, selectCommand)
	return groupCommand
}

func newNodeCommand(deps Dependencies) *cobra.Command {
	nodeCommand := &cobra.Command{Use: "node", Short: "管理代理节点", Args: noArgs}
	list := &cobra.Command{Use: "list", Short: "列出代理节点", Args: noArgs}
	var profile, group string
	var options OutputOptions
	bindCapabilityOptions(list, &profile, &options)
	list.Flags().StringVar(&group, "group", "", "按代理组过滤")
	list.RunE = func(command *cobra.Command, _ []string) error {
		values, err := deps.Capabilities.Nodes(command.Context(), profile, group)
		if err != nil {
			return err
		}
		return capabilityResult(command, options, Result{Kind: "NodeList", Data: func(show bool) any { return nodeData(values, show) }, Table: func(w io.Writer, show bool) error { return writeNodeTable(w, values, show) }})
	}
	selectCommand := &cobra.Command{Use: "select <group> <node>", Short: "选择节点", Args: exactArgs(2)}
	var selectProfile string
	var selectOptions OutputOptions
	bindCapabilityOptions(selectCommand, &selectProfile, &selectOptions)
	selectCommand.RunE = func(command *cobra.Command, args []string) error {
		return capabilityMutation(command, selectOptions, "node select", func() error {
			return deps.Capabilities.SelectGroupNode(command.Context(), selectProfile, args[0], args[1])
		})
	}
	test := &cobra.Command{Use: "test", Short: "测试节点延迟", Args: noArgs}
	var testProfile, testGroup, testOutput string
	var concurrency, limit int
	var timeout time.Duration
	var showSecrets bool
	test.Flags().StringVar(&testProfile, "profile", "", "档案 ID（默认使用活动 legacy 档案）")
	test.Flags().StringVar(&testGroup, "group", "", "按代理组测试")
	test.Flags().IntVar(&concurrency, "concurrency", 5, "并发数")
	test.Flags().IntVar(&limit, "limit", 120, "最多测试节点数")
	test.Flags().DurationVar(&timeout, "timeout", 0, "超时时间")
	test.Flags().StringVar(&testOutput, "output", "text", "输出格式：text 或 ndjson")
	test.Flags().BoolVar(&showSecrets, "show-secrets", false, "显示完整敏感信息")
	test.SetFlagErrorFunc(invalidFlagError)
	test.RunE = func(command *cobra.Command, _ []string) error {
		if concurrency < 1 || concurrency > 64 || limit < 0 || limit > 1000 || (testOutput != "text" && testOutput != "ndjson") {
			return &app.Error{Code: app.ErrorCodeInvalidArgument, Message: "node test 参数无效"}
		}
		ctx := command.Context()
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		return streamNodeTest(command, deps.Capabilities, ctx, app.NodeTestRequest{ProfileID: domain.ProfileID(testProfile), GroupID: domain.GroupID(testGroup), Concurrency: concurrency, Limit: limit}, testOutput == "ndjson", showSecrets)
	}
	nodeCommand.AddCommand(list, selectCommand, test)
	return nodeCommand
}

func newSubscriptionCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "subscription", Short: "管理 legacy 单来源订阅", Args: noArgs}
	show := &cobra.Command{Use: "show", Short: "查看订阅地址", Args: noArgs}
	var profile string
	var options OutputOptions
	bindCapabilityOptions(show, &profile, &options)
	show.RunE = func(command *cobra.Command, _ []string) error {
		value, err := deps.Capabilities.Subscription(command.Context(), profile)
		if err != nil {
			return err
		}
		secret := NewURLSecret(value.URL)
		return capabilityResult(command, options, Result{Kind: "Subscription", Data: func(show bool) any {
			return map[string]any{"id": value.ID, "name": value.Name, "url": secret.Display(show), "enabled": value.Enabled}
		}, Table: func(w io.Writer, show bool) error {
			_, err := fmt.Fprintf(w, "名称: %s\n地址: %s\n启用: %t\n", value.Name, secret.Display(show), value.Enabled)
			return err
		}})
	}
	set := &cobra.Command{Use: "set <url>", Short: "设置 legacy 订阅地址", Args: exactArgs(1)}
	var setProfile string
	var setOptions OutputOptions
	bindCapabilityOptions(set, &setProfile, &setOptions)
	set.RunE = func(command *cobra.Command, args []string) error {
		return capabilityMutation(command, setOptions, "subscription set", func() error { return deps.Capabilities.SetSubscription(command.Context(), setProfile, args[0]) })
	}
	update := &cobra.Command{Use: "update", Short: "更新 legacy 订阅", Args: noArgs}
	var updateProfile string
	var updateOptions OutputOptions
	bindCapabilityOptions(update, &updateProfile, &updateOptions)
	update.RunE = func(command *cobra.Command, _ []string) error {
		return capabilityMutation(command, updateOptions, "subscription update", func() error { return deps.Capabilities.UpdateSubscription(command.Context(), updateProfile) })
	}
	command.AddCommand(show, set, update)
	return command
}

func newRouteCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "route", Short: "管理 legacy 路由和白名单", Args: noArgs}
	whitelist := &cobra.Command{Use: "whitelist", Short: "管理白名单", Args: noArgs}
	list := &cobra.Command{Use: "list", Short: "列出白名单", Args: noArgs}
	var profile string
	var options OutputOptions
	bindCapabilityOptions(list, &profile, &options)
	list.RunE = func(command *cobra.Command, _ []string) error {
		values, err := deps.Capabilities.Whitelist(command.Context(), profile)
		if err != nil {
			return err
		}
		return capabilityResult(command, options, Result{Kind: "Whitelist", Data: func(bool) any { return values }, Table: func(w io.Writer, _ bool) error {
			for _, value := range values {
				if _, err := fmt.Fprintln(w, value); err != nil {
					return err
				}
			}
			return nil
		}})
	}
	add := &cobra.Command{Use: "add <domain>", Short: "添加白名单域名", Args: exactArgs(1)}
	var addProfile string
	var addOptions OutputOptions
	bindCapabilityOptions(add, &addProfile, &addOptions)
	add.RunE = func(command *cobra.Command, args []string) error {
		return capabilityMutation(command, addOptions, "whitelist add", func() error { return deps.Capabilities.AddWhitelist(command.Context(), addProfile, args[0]) })
	}
	remove := &cobra.Command{Use: "remove <domain>", Short: "删除白名单域名", Args: exactArgs(1)}
	var removeProfile string
	var removeOptions OutputOptions
	bindCapabilityOptions(remove, &removeProfile, &removeOptions)
	remove.RunE = func(command *cobra.Command, args []string) error {
		return capabilityMutation(command, removeOptions, "whitelist remove", func() error { return deps.Capabilities.RemoveWhitelist(command.Context(), removeProfile, args[0]) })
	}
	edit := &cobra.Command{Use: "edit <old-domain> <new-domain>", Short: "修改白名单域名", Args: exactArgs(2)}
	var editProfile string
	var editOptions OutputOptions
	bindCapabilityOptions(edit, &editProfile, &editOptions)
	edit.RunE = func(command *cobra.Command, args []string) error {
		return capabilityMutation(command, editOptions, "whitelist edit", func() error { return deps.Capabilities.EditWhitelist(command.Context(), editProfile, args[0], args[1]) })
	}
	whitelist.AddCommand(list, add, remove, edit)
	preset := &cobra.Command{Use: "preset <name>", Short: "应用路由预设（cn）", Args: exactArgs(1)}
	var presetProfile string
	var presetOptions OutputOptions
	bindCapabilityOptions(preset, &presetProfile, &presetOptions)
	preset.RunE = func(command *cobra.Command, args []string) error {
		return capabilityMutation(command, presetOptions, "route preset", func() error { return deps.Capabilities.ApplyRoutePreset(command.Context(), presetProfile, args[0]) })
	}
	diagnose := &cobra.Command{Use: "diagnose <target>", Short: "诊断路由命中", Args: exactArgs(1)}
	var diagnoseProfile string
	var diagnoseOptions OutputOptions
	bindCapabilityOptions(diagnose, &diagnoseProfile, &diagnoseOptions)
	diagnose.RunE = func(command *cobra.Command, args []string) error {
		value, err := deps.Capabilities.DiagnoseRoute(command.Context(), diagnoseProfile, args[0])
		if err != nil {
			return err
		}
		return capabilityResult(command, diagnoseOptions, Result{Kind: "RouteDiagnosis", Data: func(bool) any { return routeData(value) }, Table: func(w io.Writer, _ bool) error {
			_, err := fmt.Fprintf(w, "输入: %s\nHost: %s\n规则: %s\n目标: %s\n当前节点: %s\n置信度: %s\n备注: %s\n", value.Input, value.Host, value.MatchedRule, value.Target, value.CurrentNode, value.Confidence, value.Note)
			return err
		}})
	}
	command.AddCommand(whitelist, preset, diagnose, newRouteConnectionsCommand(deps))
	return command
}

func newConfigCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "管理 legacy 配置", Args: noArgs}
	validate := &cobra.Command{Use: "validate", Short: "校验配置", Args: noArgs}
	var profile string
	var options OutputOptions
	bindCapabilityOptions(validate, &profile, &options)
	validate.RunE = func(command *cobra.Command, _ []string) error {
		return capabilityMutation(command, options, "config validate", func() error { return deps.Capabilities.ValidateConfig(command.Context(), profile) })
	}
	for _, action := range []string{"backup", "restore"} {
		action := action
		actionCommand := &cobra.Command{Use: action, Short: action + " legacy 配置", Args: noArgs}
		var actionProfile string
		var actionOptions OutputOptions
		bindCapabilityOptions(actionCommand, &actionProfile, &actionOptions)
		actionCommand.RunE = func(command *cobra.Command, _ []string) error {
			var run func(context.Context, string) error
			switch action {
			case "backup":
				run = deps.Capabilities.ConfigBackup
			case "restore":
				run = deps.Capabilities.ConfigRestore
			}
			return capabilityMutation(command, actionOptions, "config "+action, func() error { return run(command.Context(), actionProfile) })
		}
		command.AddCommand(actionCommand)
	}
	edit := &cobra.Command{Use: "edit", Short: "编辑 legacy 配置", Args: noArgs}
	var editProfile string
	var editOptions OutputOptions
	bindCapabilityOptions(edit, &editProfile, &editOptions)
	edit.RunE = func(command *cobra.Command, _ []string) error {
		return capabilityMutation(command, editOptions, "config edit", func() error {
			return editConfig(command, deps.Capabilities, editProfile)
		})
	}
	command.AddCommand(edit)
	command.AddCommand(newConfigPortsCommand(deps), newConfigPortCommand(deps))
	command.AddCommand(validate)
	return command
}

func newConfigPortsCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "ports", Short: "查看监听端口", Args: noArgs}
	var profile string
	var options OutputOptions
	bindCapabilityOptions(command, &profile, &options)
	command.RunE = func(command *cobra.Command, _ []string) error {
		value, err := deps.Capabilities.ListenerPorts(command.Context(), profile)
		if err != nil {
			return err
		}
		return writeListenerPortResult(command, options, "ListenerPortStatus", value)
	}
	return command
}

func newConfigPortCommand(deps Dependencies) *cobra.Command {
	command := &cobra.Command{Use: "port", Short: "修改监听端口", Args: noArgs}
	set := &cobra.Command{Use: "set <field> <port>", Short: "设置一个监听端口", Args: exactArgs(2)}
	var profile string
	var options OutputOptions
	bindCapabilityOptions(set, &profile, &options)
	set.RunE = func(command *cobra.Command, args []string) error {
		port, err := strconv.Atoi(args[1])
		if err != nil {
			return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCodeInvalidArgument, Message: "端口必须是整数", Err: err}
		}
		request := app.SetListenerPortRequest{ProfileID: profile, Field: args[0], Port: port}
		if err := request.Validate(); err != nil {
			return &app.Error{Category: app.ErrorCategoryInvalidArgument, Code: app.ErrorCodeInvalidArgument, Message: err.Error(), Err: err}
		}
		value, err := deps.Capabilities.SetListenerPort(command.Context(), request)
		if err != nil {
			return err
		}
		return writeListenerPortResult(command, options, "ListenerPortChange", value)
	}
	command.AddCommand(set)
	return command
}

func writeListenerPortResult(command *cobra.Command, options OutputOptions, kind string, value app.ListenerPortStatus) error {
	return capabilityResult(command, options, Result{
		Kind: kind,
		Data: func(bool) any {
			ports := make([]map[string]any, 0, len(value.Ports))
			for _, item := range value.Ports {
				ports = append(ports, map[string]any{
					"field": item.Field, "host": item.Host, "port": item.Port, "required": item.Required,
					"enabled": item.Enabled, "networks": append([]string(nil), item.Networks...),
				})
			}
			conflicts := make([]map[string]any, 0, len(value.PortConflicts))
			for _, item := range value.PortConflicts {
				conflicts = append(conflicts, map[string]any{
					"field": item.Field, "network": item.Network, "host": item.Host, "port": item.Port,
				})
			}
			return map[string]any{
				"profileId": value.ProfileID, "coreState": value.CoreState, "restarted": value.Restarted,
				"nextStart": value.NextStart, "ports": ports, "portConflicts": conflicts,
			}
		},
		Table: func(w io.Writer, _ bool) error { return writeListenerPortTable(w, value) },
	})
}

func writeListenerPortTable(w io.Writer, value app.ListenerPortStatus) error {
	if _, err := fmt.Fprintf(w, "档案: %s\nCore: %s\n", value.ProfileID, value.CoreState); err != nil {
		return err
	}
	for _, item := range value.Ports {
		state := strconv.Itoa(item.Port)
		if !item.Enabled {
			state = "禁用"
		}
		conflict := ""
		for _, current := range value.PortConflicts {
			if current.Field == item.Field && current.Port == item.Port {
				conflict = " [冲突]"
				break
			}
		}
		if _, err := fmt.Fprintf(w, "%s: %s (%s %s)%s\n", item.Field, state, strings.Join(item.Networks, "/"), item.Host, conflict); err != nil {
			return err
		}
	}
	if value.Restarted {
		_, err := fmt.Fprintln(w, "生效状态: 已重启并生效")
		return err
	}
	if value.NextStart {
		_, err := fmt.Fprintln(w, "生效状态: 已保存，下次启动生效")
		return err
	}
	return nil
}

func editConfig(command *cobra.Command, service app.CapabilityAPI, profileID string) error {
	document, err := service.ReadConfig(command.Context(), profileID)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "mm-config-*.yaml")
	if err != nil {
		return &app.Error{Code: app.ErrorCodeInternal, Message: "无法创建配置编辑临时文件", Err: err}
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := file.Write(document.Content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	process := exec.CommandContext(command.Context(), editor, path)
	process.Stdin = command.InOrStdin()
	process.Stdout = command.OutOrStdout()
	process.Stderr = command.ErrOrStderr()
	if err := process.Run(); err != nil {
		return &app.Error{Category: app.ErrorCategoryUpstreamFailure, Code: app.ErrorCodeUpstreamFailure, Message: "配置编辑器执行失败", Err: err}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if bytes.Equal(content, document.Content) {
		return nil
	}
	return service.ReplaceConfig(command.Context(), profileID, document.SHA256, content)
}

func exactArgs(count int) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != count {
			return &app.Error{Code: app.ErrorCodeInvalidArgument, Message: fmt.Sprintf("需要 %d 个参数", count)}
		}
		return nil
	}
}

func compileFilter(value string) (*regexp.Regexp, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	matcher, err := regexp.Compile(value)
	if err != nil {
		return nil, &app.Error{Code: app.ErrorCodeInvalidArgument, Message: "日志过滤正则无效", Err: err}
	}
	return matcher, nil
}

func filterLogs(values []app.LogLine, matcher *regexp.Regexp) []app.LogLine {
	if matcher == nil {
		return values
	}
	result := make([]app.LogLine, 0, len(values))
	for _, value := range values {
		if matcher.MatchString(value.Message) {
			result = append(result, value)
		}
	}
	return result
}

func writeGroupTable(w io.Writer, values []app.Group, _ bool) error {
	for _, value := range values {
		if _, err := fmt.Fprintf(w, "%s\t%s\t当前=%s\t节点=%d\n", value.Name, value.Type, value.SelectedNodeID, len(value.NodeIDs)); err != nil {
			return err
		}
	}
	return nil
}
func writeNodeTable(w io.Writer, values []app.Node, _ bool) error {
	for _, value := range values {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", value.Name, value.Protocol); err != nil {
			return err
		}
	}
	return nil
}
func groupData(values []app.Group) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		nodes := make([]string, 0, len(value.NodeIDs))
		for _, node := range value.NodeIDs {
			nodes = append(nodes, node.String())
		}
		result = append(result, map[string]any{"id": value.ID, "name": value.Name, "type": value.Type, "selectedNodeId": value.SelectedNodeID, "nodes": nodes})
	}
	return result
}
func nodeData(values []app.Node, show bool) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"id": value.ID, "name": RedactTextOrShow(value.Name, show), "protocol": value.Protocol})
	}
	return result
}

func routeData(value app.RouteDiagnosis) map[string]any {
	return map[string]any{"input": value.Input, "host": value.Host, "matchedRule": value.MatchedRule, "target": value.Target, "currentNode": value.CurrentNode, "confidence": value.Confidence, "note": value.Note}
}

func RedactTextOrShow(value string, show bool) string {
	if show {
		return value
	}
	return RedactText(value)
}
func logData(values []app.LogLine, show bool) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, RedactTextOrShow(value.Message, show))
	}
	return result
}
func writeLogTable(w io.Writer, values []app.LogLine, show bool) error {
	for _, value := range values {
		if _, err := fmt.Fprintln(w, RedactTextOrShow(value.Message, show)); err != nil {
			return err
		}
	}
	return nil
}

func followLogs(command *cobra.Command, service app.CapabilityAPI, profile string, lines int, matcher *regexp.Regexp, ndjson, show bool) error {
	stream := service.FollowLogs(command.Context(), app.LogRequest{ProfileID: domain.ProfileID(profile), Lines: lines})
	for event := range stream {
		if event.Err != nil {
			if errors.Is(event.Err, context.Canceled) {
				return nil
			}
			return event.Err
		}
		if event.Finished {
			return nil
		}
		if event.Line == nil || (matcher != nil && !matcher.MatchString(event.Line.Message)) {
			continue
		}
		line := RedactTextOrShow(event.Line.Message, show)
		if ndjson {
			if err := writeJSON(command.OutOrStdout(), map[string]any{"apiVersion": APIVersion, "kind": "LogLine", "data": map[string]string{"line": line}, "warnings": []string{}}); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintln(command.OutOrStdout(), line); err != nil {
			return err
		}
	}
	return nil
}

func streamNodeTest(command *cobra.Command, service app.CapabilityAPI, ctx context.Context, request app.NodeTestRequest, ndjson, show bool) error {
	stream := service.TestNodes(ctx, request)
	for event := range stream {
		if event.Err != nil {
			if errors.Is(event.Err, context.Canceled) {
				return nil
			}
			return event.Err
		}
		if event.Finished {
			if ndjson {
				return writeJSON(command.OutOrStdout(), map[string]any{"apiVersion": APIVersion, "kind": "NodeTestComplete", "data": map[string]int{"done": event.Done, "total": event.Total}, "warnings": []string{}})
			}
			_, err := fmt.Fprintf(command.OutOrStdout(), "测速完成: %d/%d\n", event.Done, event.Total)
			return err
		}
		if event.Result == nil {
			continue
		}
		if ndjson {
			if err := writeJSON(command.OutOrStdout(), map[string]any{"apiVersion": APIVersion, "kind": "NodeTestEvent", "data": map[string]any{"done": event.Done, "total": event.Total, "nodeId": RedactTextOrShow(event.Result.NodeID.String(), show), "delayMs": event.Result.Delay.Milliseconds()}, "warnings": []string{}}); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(command.OutOrStdout(), "%d/%d\t%s\t%dms\n", event.Done, event.Total, RedactTextOrShow(event.Result.NodeID.String(), show), event.Result.Delay.Milliseconds()); err != nil {
			return err
		}
	}
	return nil
}
