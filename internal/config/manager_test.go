package config

import (
	"path/filepath"
	"testing"
)

func TestResolveManagerPaths_XDGAndFallback(t *testing.T) {
	home := t.TempDir()
	tests := []struct {
		name string
		env  ManagerEnvironment
		run  string
		data string
	}{
		{
			name: "fallback",
			env:  ManagerEnvironment{HomeDir: home},
			run:  filepath.Join(home, ".local", "state", "mihomo-manager", "run"),
			data: filepath.Join(home, ".local", "share", "mihomo-manager"),
		},
		{
			name: "xdg",
			env: ManagerEnvironment{
				HomeDir: home, DataHome: filepath.Join(home, "data"), StateHome: filepath.Join(home, "state"),
				RuntimeDir: filepath.Join(home, "runtime"), ConfigHome: filepath.Join(home, "config"),
			},
			run:  filepath.Join(home, "runtime", "mihomo-manager"),
			data: filepath.Join(home, "data", "mihomo-manager"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, err := ResolveManagerPaths(tt.env)
			if err != nil {
				t.Fatal(err)
			}
			if paths.RuntimeDir != tt.run || paths.DataDir != tt.data {
				t.Fatalf("unexpected paths: %+v", paths)
			}
			if paths.Socket != filepath.Join(tt.run, "mm.sock") || paths.Database != filepath.Join(tt.data, "state.db") {
				t.Fatalf("unexpected derived paths: %+v", paths)
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
