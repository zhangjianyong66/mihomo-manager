package mihomo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
)

func TestApplyRouteCN_EnforcesGlobalFallbackAndCleansCNRules(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")

	original := `
mixed-port: 10808
rules:
  - GEOSITE,cn,DIRECT
  - GEOIP, CN, DIRECT, no-resolve
  - MATCH,DIRECT
  - DOMAIN-SUFFIX,example.com,DIRECT
`
	if err := os.WriteFile(configFile, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}

	c := New(config.Paths{
		MihomoBin:  "/usr/bin/true",
		ConfigFile: configFile,
		BackupFile: backupFile,
	})

	if err := c.ApplyRouteCN(); err != nil {
		t.Fatalf("ApplyRouteCN failed: %v", err)
	}

	raw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	if cfg["geodata-mode"] != true {
		t.Fatalf("expected geodata-mode=true, got %#v", cfg["geodata-mode"])
	}

	rules := anyToStrings(cfg["rules"])
	if len(rules) < 4 {
		t.Fatalf("expected at least 4 rules, got %v", rules)
	}
	if rules[0] != "GEOSITE,CN,DIRECT" || rules[1] != "GEOIP,CN,DIRECT,no-resolve" || rules[2] != "MATCH,GLOBAL" {
		t.Fatalf("unexpected managed rule prefix: %v", rules[:3])
	}

	matchCount := 0
	for _, r := range rules {
		ru := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(r), " ", ""))
		if strings.HasPrefix(ru, "MATCH,") {
			matchCount++
			if ru != "MATCH,GLOBAL" {
				t.Fatalf("unexpected match rule: %q", r)
			}
		}
		if strings.HasPrefix(ru, "GEOSITE,CN,DIRECT") && r != "GEOSITE,CN,DIRECT" {
			t.Fatalf("stale geosite cn rule remains: %q", r)
		}
		if strings.HasPrefix(ru, "GEOIP,CN,DIRECT") && r != "GEOIP,CN,DIRECT,no-resolve" {
			t.Fatalf("stale geoip cn rule remains: %q", r)
		}
	}
	if matchCount != 1 {
		t.Fatalf("expected exactly one MATCH rule, got %d", matchCount)
	}
}

func TestApplyRouteCN_RestoreConfigWhenPostWriteTestFails(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")

	original := "rules:\n  - DOMAIN-SUFFIX,example.com,DIRECT\n"
	if err := os.WriteFile(configFile, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}

	c := New(config.Paths{
		MihomoBin:  "/usr/bin/false",
		ConfigFile: configFile,
		BackupFile: backupFile,
	})

	if err := c.ApplyRouteCN(); err == nil {
		t.Fatal("expected error when test config fails")
	}

	after, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(after) != original {
		t.Fatalf("expected config restored to original, got:\n%s", string(after))
	}
}
