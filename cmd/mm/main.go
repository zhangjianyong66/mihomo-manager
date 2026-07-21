package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/zhangjianyong66/mihomo-manager/internal/app"
	"github.com/zhangjianyong66/mihomo-manager/internal/cli"
	"github.com/zhangjianyong66/mihomo-manager/internal/config"
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
	code := cli.Execute(
		ctx,
		cli.Dependencies{
			Daemon:       daemonService,
			Migrations:   migrationService,
			Capabilities: capabilityService,
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
