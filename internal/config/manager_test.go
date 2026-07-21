package config

import (
	"path/filepath"
	"testing"
)

func TestLoad_RulesetPathsFollowConfigDir(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "mihomo")
	t.Setenv("CONFIG_DIR", configDir)
	t.Setenv("MIHOMO_API_PORT", "invalid")

	paths := Load()
	if paths.ConfigDir != configDir || paths.RulesetDir != filepath.Join(configDir, "rulesets") {
		t.Fatalf("unexpected config paths: %+v", paths)
	}
	if paths.CNDomainRuleset != filepath.Join(configDir, "rulesets", "cn-domain.mrs") {
		t.Fatalf("unexpected domain ruleset path: %s", paths.CNDomainRuleset)
	}
	if paths.CNIPRuleset != filepath.Join(configDir, "rulesets", "cn-ip.mrs") {
		t.Fatalf("unexpected IP ruleset path: %s", paths.CNIPRuleset)
	}
	if paths.APIAddr != "http://127.0.0.1:9090" {
		t.Fatalf("invalid API port should use fallback: %s", paths.APIAddr)
	}
}

func TestResolveManagerPaths_XDGAndFallback(t *testing.T) {
	home := t.TempDir()
	tests := []struct {
		name  string
		env   ManagerEnvironment
		run   string
		data  string
		state string
	}{
		{
			name:  "fallback",
			env:   ManagerEnvironment{HomeDir: home},
			run:   filepath.Join(home, ".local", "state", "mihomo-manager", "run"),
			data:  filepath.Join(home, ".local", "share", "mihomo-manager"),
			state: filepath.Join(home, ".local", "state", "mihomo-manager"),
		},
		{
			name: "xdg",
			env: ManagerEnvironment{
				HomeDir: home, DataHome: filepath.Join(home, "data"), StateHome: filepath.Join(home, "state"),
				RuntimeDir: filepath.Join(home, "runtime"), ConfigHome: filepath.Join(home, "config"),
			},
			run:   filepath.Join(home, "runtime", "mihomo-manager"),
			data:  filepath.Join(home, "data", "mihomo-manager"),
			state: filepath.Join(home, "state", "mihomo-manager"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, err := ResolveManagerPaths(tt.env)
			if err != nil {
				t.Fatal(err)
			}
			if paths.RuntimeDir != tt.run || paths.DataDir != tt.data || paths.StateDir != tt.state {
				t.Fatalf("unexpected paths: %+v", paths)
			}
			if paths.Socket != filepath.Join(tt.run, "mm.sock") || paths.Database != filepath.Join(tt.data, "state.db") {
				t.Fatalf("unexpected derived paths: %+v", paths)
			}
			if paths.GenerationsDir != filepath.Join(tt.data, "generations") || paths.CoreStateDir != filepath.Join(tt.state, "core") {
				t.Fatalf("unexpected core paths: %+v", paths)
			}
			if paths.BackupsDir != filepath.Join(tt.data, "backups") {
				t.Fatalf("unexpected backup path: %+v", paths)
			}
			if paths.CoreLog != filepath.Join(tt.state, "core", "mihomo.log") || paths.RuntimeState != filepath.Join(tt.state, "core", "runtime.json") {
				t.Fatalf("unexpected core files: %+v", paths)
			}
		})
	}
}

func TestResolveManagerPaths_RejectsRelativeValues(t *testing.T) {
	for _, env := range []ManagerEnvironment{
		{HomeDir: "relative"},
		{HomeDir: t.TempDir(), DataHome: "relative"},
		{HomeDir: t.TempDir(), RuntimeDir: "relative"},
	} {
		if _, err := ResolveManagerPaths(env); err == nil {
			t.Fatal("expected relative path rejection")
		}
	}
}
