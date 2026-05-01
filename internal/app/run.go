package app

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/tui"
)

func RunInteractive() error {
	paths := config.Load()
	_ = os.MkdirAll(paths.ConfigDir, 0755)
	client := mihomo.New(paths)
	m := tui.New(client)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
