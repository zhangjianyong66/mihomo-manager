package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/zhangjianyong66/mihomo-manager/internal/app"
)

type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

type TUIRunner interface {
	Run(context.Context, IOStreams) error
}

type TUIRunnerFunc func(context.Context, IOStreams) error

func (f TUIRunnerFunc) Run(ctx context.Context, streams IOStreams) error {
	return f(ctx, streams)
}

type Dependencies struct {
	TUI          TUIRunner
	Daemon       *app.DaemonService
	Migrations   *app.MigrationService
	Capabilities app.CapabilityAPI
}

func NewRoot(deps Dependencies) *cobra.Command {
	runTUI := func(cmd *cobra.Command, _ []string) error {
		if deps.TUI == nil {
			return &app.Error{Code: app.ErrorCodeInternal, Message: "TUI 运行器未配置"}
		}
		return deps.TUI.Run(cmd.Context(), IOStreams{
			In:     cmd.InOrStdin(),
			Out:    cmd.OutOrStdout(),
			ErrOut: cmd.ErrOrStderr(),
		})
	}

	root := &cobra.Command{
		Use:           "mm",
		Short:         "终端代理客户端管理器",
		Args:          noArgs,
		RunE:          runTUI,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetFlagErrorFunc(invalidFlagError)

	tuiCommand := &cobra.Command{
		Use:   "tui",
		Short: "打开交互式终端界面",
		Args:  noArgs,
		RunE:  runTUI,
	}
	tuiCommand.SetFlagErrorFunc(invalidFlagError)
	root.AddCommand(tuiCommand)
	root.AddCommand(newDaemonCommand(deps))
	root.AddCommand(newMigrationCommand(deps))
	root.AddCommand(newCapabilityCommands(deps)...)

	return root
}

func newDaemonCommand(deps Dependencies) *cobra.Command {
	daemonCommand := &cobra.Command{Use: "daemon", Short: "管理 mihomo-manager daemon", Args: noArgs}
	daemonCommand.SetFlagErrorFunc(invalidFlagError)

	run := &cobra.Command{Use: "run", Short: "前台运行 daemon", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if deps.Daemon == nil {
			return &app.Error{Code: app.ErrorCodeInternal, Message: "daemon 服务未配置"}
		}
		return deps.Daemon.Run(cmd.Context(), cmd.ErrOrStderr())
	}}
	status := &cobra.Command{Use: "status", Short: "查询 daemon 状态", Args: noArgs}
	addDaemonOutput(status, func(cmd *cobra.Command) error {
		if deps.Daemon == nil {
			return &app.Error{Code: app.ErrorCodeInternal, Message: "daemon 服务未配置"}
		}
		value, err := deps.Daemon.Status(cmd.Context())
		if err != nil {
			return err
		}
		return daemonPresenter(cmd).WriteResult(Result{
			Kind: "DaemonStatus",
			Data: func(bool) any { return value },
			Table: func(w io.Writer, _ bool) error {
				_, err := fmt.Fprintf(w, "状态: %s\nPID: %d\n协议版本: %d\n启动时间: %s\nSchema 版本: %d\nCore 状态: %s\nCore PID: %d\n", value.State, value.PID, value.ProtocolVersion, value.StartedAt.Format(time.RFC3339Nano), value.SchemaVersion, value.Core.State, value.Core.PID)
				return err
			},
		})
	})
	daemonCommand.AddCommand(run, status)
	for _, action := range []string{"enable", "disable", "start", "stop"} {
		action := action
		command := &cobra.Command{Use: action, Short: "daemon " + action, Args: noArgs}
		addDaemonOutput(command, func(cmd *cobra.Command) error {
			if deps.Daemon == nil {
				return &app.Error{Code: app.ErrorCodeInternal, Message: "daemon 服务未配置"}
			}
			value, err := deps.Daemon.Control(cmd.Context(), action)
			if err != nil {
				return err
			}
			return daemonPresenter(cmd).WriteResult(Result{
				Kind: "DaemonControl",
				Data: func(bool) any { return value },
				Table: func(w io.Writer, _ bool) error {
					if value.Hint != "" {
						_, err := fmt.Fprintf(w, "%s\n提示: %s\n", value.Message, value.Hint)
						return err
					}
					_, err := fmt.Fprintln(w, value.Message)
					return err
				},
			})
		})
		daemonCommand.AddCommand(command)
	}
	return daemonCommand
}

func addDaemonOutput(command *cobra.Command, run func(*cobra.Command) error) {
	var format OutputFormat
	command.Flags().Var(&format, "output", "输出格式：table 或 json")
	command.SetFlagErrorFunc(invalidFlagError)
	command.RunE = func(cmd *cobra.Command, args []string) error {
		if format == "" {
			format = OutputTable
		}
		if _, err := ParseOutputFormat(format.String()); err != nil {
			return err
		}
		return run(cmd)
	}
}

func daemonPresenter(cmd *cobra.Command) Presenter {
	format := OutputFormat(cmd.Flags().Lookup("output").Value.String())
	return NewPresenter(cmd.OutOrStdout(), cmd.ErrOrStderr(), OutputOptions{Format: format})
}

func Execute(ctx context.Context, deps Dependencies, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := NewRoot(deps)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.ExecuteContext(ctx); err != nil {
		presenter := NewPresenter(stdout, stderr, OutputOptions{Format: requestedOutputFormat(args)})
		_ = presenter.WriteError(err)
		return ExitCode(err)
	}
	return 0
}

func requestedOutputFormat(args []string) OutputFormat {
	for index, arg := range args {
		if arg == "--output=json" || arg == "--output=ndjson" {
			return OutputJSON
		}
		if arg == "--output" && index+1 < len(args) && (args[index+1] == "json" || args[index+1] == "ndjson") {
			return OutputJSON
		}
	}
	return OutputTable
}

func noArgs(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return &app.Error{
		Code:    app.ErrorCodeInvalidArgument,
		Message: "未知命令或存在多余参数",
		Details: map[string]any{"argumentCount": len(args)},
	}
}

func invalidFlagError(_ *cobra.Command, err error) error {
	return &app.Error{
		Code:    app.ErrorCodeInvalidArgument,
		Message: "命令参数无效",
		Details: map[string]any{"reason": "flag parsing failed"},
		Err:     err,
	}
}
