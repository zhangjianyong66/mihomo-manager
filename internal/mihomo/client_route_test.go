package mihomo

import (
	"net/http"
	"net/http/httptest"
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
	if rules[0] != "DOMAIN-SUFFIX,example.com,DIRECT" || rules[1] != "GEOSITE,CN,DIRECT" || rules[2] != "GEOIP,CN,DIRECT,no-resolve" || rules[3] != "MATCH,GLOBAL" {
		t.Fatalf("unexpected rule prefix: %v", rules[:4])
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

func TestDiagnoseRoute_MatchesDomainWildcardRule(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")

	original := `
rules:
  - DOMAIN-WILDCARD,*.example.com,DIRECT
  - MATCH,GLOBAL
`
	if err := os.WriteFile(configFile, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}

	c := New(config.Paths{
		ConfigFile: configFile,
		BackupFile: backupFile,
	})

	got, err := c.DiagnoseRoute("api.example.com")
	if err != nil {
		t.Fatalf("DiagnoseRoute failed: %v", err)
	}
	if got.MatchedRule != "DOMAIN-WILDCARD,*.example.com,DIRECT" || got.Target != "DIRECT" || got.CurrentNode != "DIRECT" {
		t.Fatalf("unexpected diagnosis: %+v", got)
	}
}

func TestUpdateSubscription_PreservesWhitelistFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	subscriptionURLFile := filepath.Join(tmpDir, "subscription.url")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	subscription := `
proxies:
  - name: node-a
    type: ss
    server: 127.0.0.1
    port: 8388
    cipher: aes-128-gcm
    pass` + `word: pass
rules:
  - MATCH,DIRECT
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(subscription))
	}))
	defer server.Close()

	if err := os.WriteFile(configFile, []byte("rules:\n  - MATCH,DIRECT\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}
	if err := os.WriteFile(subscriptionURLFile, []byte(server.URL+"\n"), 0644); err != nil {
		t.Fatalf("write subscription url: %v", err)
	}
	if err := os.WriteFile(whitelistFile, []byte("domains:\n  - example.com\n  - github.com\n"), 0644); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:      configFile,
		BackupFile:      backupFile,
		SubscriptionURL: subscriptionURLFile,
		WhitelistFile:   whitelistFile,
	})

	if err := c.UpdateSubscription(); err != nil {
		t.Fatalf("UpdateSubscription failed: %v", err)
	}

	raw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	rules := anyToStrings(cfg["rules"])
	wantPrefix := []string{
		"DOMAIN-SUFFIX,example.com,DIRECT",
		"DOMAIN-SUFFIX,github.com,DIRECT",
		"MATCH,🌐 代理",
	}
	if len(rules) < len(wantPrefix) {
		t.Fatalf("expected at least %d rules, got %v", len(wantPrefix), rules)
	}
	for i, want := range wantPrefix {
		if rules[i] != want {
			t.Fatalf("rule %d mismatch: got %q want %q; all rules: %v", i, rules[i], want, rules)
		}
	}
}

func TestUpdateSubscription_AcceptsVLESSURIList(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	subscriptionURLFile := filepath.Join(tmpDir, "subscription.url")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	subscription := "vless://11111111-1111-1111-1111-111111111111@example.com:443?security=tls&sni=example.com&type=ws&path=%2Fws#test-node\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(subscription))
	}))
	defer server.Close()

	if err := os.WriteFile(configFile, []byte("rules:\n  - MATCH,DIRECT\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}
	if err := os.WriteFile(subscriptionURLFile, []byte(server.URL+"\n"), 0644); err != nil {
		t.Fatalf("write subscription url: %v", err)
	}
	if err := os.WriteFile(whitelistFile, []byte("domains: []\n"), 0644); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:      configFile,
		BackupFile:      backupFile,
		SubscriptionURL: subscriptionURLFile,
		WhitelistFile:   whitelistFile,
	})

	if err := c.UpdateSubscription(); err != nil {
		t.Fatalf("UpdateSubscription failed: %v", err)
	}

	raw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	proxies, ok := cfg["proxies"].([]any)
	if !ok || len(proxies) != 1 {
		t.Fatalf("expected one proxy, got %#v", cfg["proxies"])
	}
	proxy, ok := proxies[0].(map[string]any)
	if !ok {
		t.Fatalf("proxy has unexpected type: %#v", proxies[0])
	}
	if proxy["name"] != "test-node" || proxy["type"] != "vless" || proxy["server"] != "example.com" {
		t.Fatalf("unexpected proxy: %#v", proxy)
	}
	groups, ok := cfg["proxy-groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatalf("expected proxy groups, got %#v", cfg["proxy-groups"])
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("group has unexpected type: %#v", groups[0])
	}
	if got := anyToStrings(group["proxies"]); len(got) != 1 || got[0] != "test-node" {
		t.Fatalf("expected test-node in proxy group, got %v", got)
	}
}

func TestUpdateSubscription_DoesNotRequireGeoData(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	subscriptionURLFile := filepath.Join(tmpDir, "subscription.url")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	subscription := `
proxies:
  - name: node-a
    type: ss
    server: 127.0.0.1
    port: 8388
    cipher: aes-128-gcm
    pass` + `word: pass
rules:
  - MATCH,DIRECT
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(subscription))
	}))
	defer server.Close()

	if err := os.WriteFile(configFile, []byte("rules:\n  - MATCH,DIRECT\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}
	if err := os.WriteFile(subscriptionURLFile, []byte(server.URL+"\n"), 0644); err != nil {
		t.Fatalf("write subscription url: %v", err)
	}
	if err := os.WriteFile(whitelistFile, []byte("domains:\n  - example.com\n"), 0644); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:      configFile,
		BackupFile:      backupFile,
		SubscriptionURL: subscriptionURLFile,
		WhitelistFile:   whitelistFile,
	})

	if err := c.UpdateSubscription(); err != nil {
		t.Fatalf("UpdateSubscription failed: %v", err)
	}

	raw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	rules := anyToStrings(cfg["rules"])
	for _, rule := range rules {
		upper := strings.ToUpper(rule)
		if strings.HasPrefix(upper, "GEOIP,") || strings.HasPrefix(upper, "GEOSITE,") {
			t.Fatalf("subscription update should not require geo data, got rules: %v", rules)
		}
	}
	if len(rules) < 2 || rules[0] != "DOMAIN-SUFFIX,example.com,DIRECT" || rules[len(rules)-1] != "MATCH,🌐 代理" {
		t.Fatalf("unexpected rules: %v", rules)
	}
}

func TestUpdateSubscription_PreservesLocalPorts(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	subscriptionURLFile := filepath.Join(tmpDir, "subscription.url")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	subscription := `
proxies:
  - name: node-a
    type: ss
    server: 127.0.0.1
    port: 8388
    cipher: aes-128-gcm
    pass` + `word: pass
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(subscription))
	}))
	defer server.Close()

	original := `
mixed-port: 7890
socks-port: 7891
external-controller: 127.0.0.1:9090
rules:
  - MATCH,DIRECT
`
	if err := os.WriteFile(configFile, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}
	if err := os.WriteFile(subscriptionURLFile, []byte(server.URL+"\n"), 0644); err != nil {
		t.Fatalf("write subscription url: %v", err)
	}
	if err := os.WriteFile(whitelistFile, []byte("domains: []\n"), 0644); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:      configFile,
		BackupFile:      backupFile,
		SubscriptionURL: subscriptionURLFile,
		WhitelistFile:   whitelistFile,
	})

	if err := c.UpdateSubscription(); err != nil {
		t.Fatalf("UpdateSubscription failed: %v", err)
	}

	raw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if cfg["mixed-port"] != 7890 || cfg["socks-port"] != 7891 || cfg["external-controller"] != "127.0.0.1:9090" {
		t.Fatalf("local ports not preserved: %#v", cfg)
	}
}

func TestListWhitelist_MigratesLegacyConfigRulesToFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	original := `
rules:
  - DOMAIN-SUFFIX,example.com,DIRECT
  - DOMAIN,api.github.com,🎯 直连
  - GEOSITE,CN,DIRECT
  - MATCH,GLOBAL
`
	if err := os.WriteFile(configFile, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:    configFile,
		BackupFile:    backupFile,
		WhitelistFile: whitelistFile,
	})

	got, err := c.ListWhitelist()
	if err != nil {
		t.Fatalf("ListWhitelist failed: %v", err)
	}
	want := []string{"api.github.com", "example.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected whitelist: got %v want %v", got, want)
	}

	raw, err := os.ReadFile(whitelistFile)
	if err != nil {
		t.Fatalf("expected whitelist file written: %v", err)
	}
	var stored struct {
		Domains []string `yaml:"domains"`
	}
	if err := yaml.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal whitelist file: %v", err)
	}
	if strings.Join(stored.Domains, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected stored whitelist: got %v want %v", stored.Domains, want)
	}
}

func TestListWhitelist_MigratesLegacyWildcardRulesToFile(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	original := `
rules:
  - DOMAIN-WILDCARD,*.example.com,DIRECT
  - MATCH,GLOBAL
`
	if err := os.WriteFile(configFile, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:    configFile,
		BackupFile:    backupFile,
		WhitelistFile: whitelistFile,
	})

	got, err := c.ListWhitelist()
	if err != nil {
		t.Fatalf("ListWhitelist failed: %v", err)
	}
	if strings.Join(got, ",") != "example.com" {
		t.Fatalf("unexpected whitelist: got %v", got)
	}

	raw, err := os.ReadFile(whitelistFile)
	if err != nil {
		t.Fatalf("expected whitelist file written: %v", err)
	}
	var stored struct {
		Domains []string `yaml:"domains"`
	}
	if err := yaml.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal whitelist file: %v", err)
	}
	if strings.Join(stored.Domains, ",") != "example.com" {
		t.Fatalf("unexpected stored whitelist: got %v", stored.Domains)
	}
}

func TestAddWhitelist_WritesFileAndInjectsConfigRule(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	if err := os.WriteFile(configFile, []byte("rules:\n  - MATCH,GLOBAL\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:    configFile,
		BackupFile:    backupFile,
		WhitelistFile: whitelistFile,
	})

	if err := c.AddWhitelist("https://Example.com/path"); err != nil {
		t.Fatalf("AddWhitelist failed: %v", err)
	}

	got, err := c.ListWhitelist()
	if err != nil {
		t.Fatalf("ListWhitelist failed: %v", err)
	}
	if strings.Join(got, ",") != "example.com" {
		t.Fatalf("unexpected whitelist: %v", got)
	}
	rules := readRules(t, configFile)
	if len(rules) < 2 || rules[0] != "DOMAIN-SUFFIX,example.com,DIRECT" || rules[1] != "MATCH,GLOBAL" {
		t.Fatalf("unexpected rules after add: %v", rules)
	}
}

func TestAddWhitelist_WildcardDomainUsesSuffixRule(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	if err := os.WriteFile(configFile, []byte("rules:\n  - MATCH,GLOBAL\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:    configFile,
		BackupFile:    backupFile,
		WhitelistFile: whitelistFile,
	})

	if err := c.AddWhitelist("*.Example.com"); err != nil {
		t.Fatalf("AddWhitelist failed: %v", err)
	}

	got, err := c.ListWhitelist()
	if err != nil {
		t.Fatalf("ListWhitelist failed: %v", err)
	}
	if strings.Join(got, ",") != "example.com" {
		t.Fatalf("unexpected whitelist: %v", got)
	}
	rules := readRules(t, configFile)
	if len(rules) < 2 || rules[0] != "DOMAIN-SUFFIX,example.com,DIRECT" || rules[1] != "MATCH,GLOBAL" {
		t.Fatalf("unexpected rules after wildcard add: %v", rules)
	}
}

func TestRemoveWhitelist_UpdatesFileAndConfigRules(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")

	original := `
rules:
  - DOMAIN-SUFFIX,example.com,DIRECT
  - DOMAIN-SUFFIX,github.com,DIRECT
  - MATCH,GLOBAL
`
	if err := os.WriteFile(configFile, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, []byte(""), 0644); err != nil {
		t.Fatalf("init backup: %v", err)
	}
	if err := os.WriteFile(whitelistFile, []byte("domains:\n  - example.com\n  - github.com\n"), 0644); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}

	c := New(config.Paths{
		ConfigFile:    configFile,
		BackupFile:    backupFile,
		WhitelistFile: whitelistFile,
	})

	if err := c.RemoveWhitelist("example.com"); err != nil {
		t.Fatalf("RemoveWhitelist failed: %v", err)
	}

	got, err := c.ListWhitelist()
	if err != nil {
		t.Fatalf("ListWhitelist failed: %v", err)
	}
	if strings.Join(got, ",") != "github.com" {
		t.Fatalf("unexpected whitelist: %v", got)
	}
	rules := readRules(t, configFile)
	if len(rules) < 2 || rules[0] != "DOMAIN-SUFFIX,github.com,DIRECT" || rules[1] != "MATCH,GLOBAL" {
		t.Fatalf("unexpected rules after remove: %v", rules)
	}
	for _, rule := range rules {
		if strings.Contains(rule, "example.com") {
			t.Fatalf("removed domain still present in rules: %v", rules)
		}
	}
}

func readRules(t *testing.T, configFile string) []string {
	t.Helper()
	raw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	return anyToStrings(cfg["rules"])
}
