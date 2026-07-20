package cli

import (
	"context"
	"io"

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
	TUI TUIRunner
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

	return root
}

func Execute(ctx context.Context, deps Dependencies, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := NewRoot(deps)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.ExecuteContext(ctx); err != nil {
		presenter := NewPresenter(stdout, stderr, OutputOptions{Format: OutputTable})
		_ = presenter.WriteError(err)
		return ExitCode(err)
	}
	return 0
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
