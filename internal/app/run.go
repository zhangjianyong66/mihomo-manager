package app

import (
	"context"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/tui"
)

func RunInteractive() error {
	return RunInteractiveContext(context.Background(), os.Stdin, os.Stdout)
}

func RunInteractiveContext(ctx context.Context, input io.Reader, output io.Writer) error {
	paths := config.Load()
	_ = os.MkdirAll(paths.ConfigDir, 0755)
	client := mihomo.New(paths)
	m := tui.New(client)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output))
	_, err := p.Run()
	return err
}
