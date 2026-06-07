package mihomo

import (
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
)

func TestStartCommandRunsInNewSession(t *testing.T) {
	c := New(config.Paths{
		MihomoBin:  "/tmp/mihomo",
		ConfigDir:  "/tmp/mihomo-config",
		ConfigFile: "/tmp/mihomo-config/config.yaml",
	})

	cmd := c.startCommand()
	if cmd.SysProcAttr == nil {
		t.Fatal("expected start command to set SysProcAttr")
	}
	if !cmd.SysProcAttr.Setsid {
		t.Fatal("expected start command to run in a new session")
	}
}
