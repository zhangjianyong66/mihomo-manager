package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/cli"
	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/tui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	paths, _ := config.LoadManagerPaths()
	var daemonService *app.DaemonService
	var migrationService *app.MigrationService
	var capabilityService *app.DaemonCapabilities
	if paths.Database != "" {
		daemonService = app.NewDaemonService(paths)
		migrationService = app.NewMigrationService(paths)
		capabilityService = app.NewCapabilityService(paths)
	}
	interactiveCapabilities := app.NewInteractiveCapabilities(capabilityService, os.Getenv("EDITOR"))
	code := cli.Execute(
		ctx,
		cli.Dependencies{
			Daemon:       daemonService,
			Migrations:   migrationService,
			Capabilities: capabilityService,
			TUI: cli.TUIRunnerFunc(func(ctx context.Context, streams cli.IOStreams) error {
				if capabilityService == nil {
					return &app.Error{Code: app.ErrorCodeInternal, Message: "无法解析 daemon 运行路径"}
				}
				model := tui.NewWithContext(ctx, interactiveCapabilities)
				return app.RunInteractiveContext(ctx, streams.In, streams.Out, model)
			}),
		},
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
	)
	os.Exit(code)
}
