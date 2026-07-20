package main

import (
	"context"
	"os"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/cli"
)

func main() {
	code := cli.Execute(
		context.Background(),
		cli.Dependencies{
			TUI: cli.TUIRunnerFunc(func(ctx context.Context, streams cli.IOStreams) error {
				return app.RunInteractiveContext(ctx, streams.In, streams.Out)
			}),
		},
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
	)
	os.Exit(code)
}
