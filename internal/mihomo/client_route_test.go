package mihomo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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

	if cfg["mode"] != "rule" {
		t.Fatalf("expected mode=rule, got %#v", cfg["mode"])
	}

	rules := anyToStrings(cfg["rules"])
	if len(rules) < len(managerLocalRules)+4 {
		t.Fatalf("expected manager rules, got %v", rules)
	}
	for i, want := range managerLocalRules {
		if rules[i] != want {
			t.Fatalf("rule %d = %q, want %q; rules: %v", i, rules[i], want, rules)
		}
	}

	matchCount := 0
	for _, r := range rules {
		ru := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(r), " ", ""))
		if strings.HasPrefix(ru, "MATCH,") {
			matchCount++
			if r != "MATCH,"+ProxyGroupName {
				t.Fatalf("unexpected match rule: %q", r)
			}
		}
		if strings.HasPrefix(ru, "GEOSITE,") || strings.HasPrefix(ru, "GEOIP,") {
			t.Fatalf("legacy geo rule remains: %q", r)
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
		ConfigDir:  tmpDir,
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
		MihomoBin:       "/usr/bin/true",
		ConfigDir:       tmpDir,
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
	if len(rules) < len(managerLocalRules)+5 || rules[len(rules)-1] != "MATCH,"+ProxyGroupName {
		t.Fatalf("unexpected rule order: %v", rules)
	}
	if !containsString(rules, "DOMAIN-SUFFIX,example.com,DIRECT") || !containsString(rules, "DOMAIN-SUFFIX,github.com,DIRECT") {
		t.Fatalf("whitelist rules missing: %v", rules)
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
		MihomoBin:       "/usr/bin/true",
		ConfigDir:       tmpDir,
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
		MihomoBin:       "/usr/bin/true",
		ConfigDir:       tmpDir,
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
			t.Fatalf("legacy geo rule remains, got rules: %v", rules)
		}
	}
	if len(rules) < len(managerLocalRules)+4 || rules[len(rules)-1] != "MATCH,"+ProxyGroupName || !containsString(rules, "RULE-SET,"+CNDomainProviderName+",DIRECT") {
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
		MihomoBin:       "/usr/bin/true",
		ConfigDir:       tmpDir,
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
		MihomoBin:     "/usr/bin/true",
		ConfigDir:     tmpDir,
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
		MihomoBin:     "/usr/bin/true",
		ConfigDir:     tmpDir,
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
		MihomoBin:     "/usr/bin/true",
		ConfigDir:     tmpDir,
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
	if len(rules) < len(managerLocalRules)+4 || !containsString(rules, "DOMAIN-SUFFIX,example.com,DIRECT") || rules[len(rules)-1] != "MATCH,"+ProxyGroupName {
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
		MihomoBin:     "/usr/bin/true",
		ConfigDir:     tmpDir,
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
	if len(rules) < len(managerLocalRules)+4 || !containsString(rules, "DOMAIN-SUFFIX,example.com,DIRECT") || rules[len(rules)-1] != "MATCH,"+ProxyGroupName {
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
		MihomoBin:     "/usr/bin/true",
		ConfigDir:     tmpDir,
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
	if len(rules) < len(managerLocalRules)+4 || !containsString(rules, "DOMAIN-SUFFIX,github.com,DIRECT") || rules[len(rules)-1] != "MATCH,"+ProxyGroupName {
		t.Fatalf("unexpected rules after remove: %v", rules)
	}
	for _, rule := range rules {
		if strings.Contains(rule, "example.com") {
			t.Fatalf("removed domain still present in rules: %v", rules)
		}
	}
}

func TestUpdateSubscription_PreservesRoutingOverlayAndRestoresSelections(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")
	subscriptionURLFile := filepath.Join(tmpDir, "subscription.url")

	subscription := `
proxies:
  - name: node-z
    type: socks5
    server: 127.0.0.1
    port: 1080
  - name: node-a
    type: socks5
    server: 127.0.0.1
    port: 1081
`
	subscriptionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(subscription))
	}))
	defer subscriptionServer.Close()

	var mu sync.Mutex
	reloadCount := 0
	selected := map[string]string{}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/GLOBAL":
			_, _ = w.Write([]byte(`{"all":["node-a","node-old","🌐 代理"],"now":"node-a"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/proxies/"+ProxyGroupName:
			_, _ = w.Write([]byte(`{"all":["node-a","node-old"],"now":"node-old"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/configs":
			mu.Lock()
			reloadCount++
			mu.Unlock()
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/proxies/"):
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			selected[strings.TrimPrefix(r.URL.Path, "/proxies/")] = payload["name"]
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()

	original := `
mode: rule
mixed-port: 17890
socks-port: 17891
external-controller: 127.0.0.1:19090
proxies:
  - name: node-old
    type: socks5
    server: 127.0.0.1
    port: 1082
rule-providers:
  custom:
    type: file
    behavior: classical
    path: ./custom.yaml
rules:
  - DOMAIN,custom.example,REJECT
  - DOMAIN-SUFFIX,white.example,DIRECT
  - MATCH,GLOBAL
dns:
  enhanced-mode: fake-ip
  fake-ip-filter:
    - +.internal.example
  nameserver-policy:
    +.corp.example:
      - 10.0.0.53
`
	if err := os.WriteFile(configFile, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, nil, 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(whitelistFile, []byte("domains:\n  - white.example\n"), 0o600); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}
	if err := os.WriteFile(subscriptionURLFile, []byte(subscriptionServer.URL+"\n"), 0o600); err != nil {
		t.Fatalf("write subscription URL: %v", err)
	}

	client := New(config.Paths{
		MihomoBin:       "/usr/bin/true",
		ConfigDir:       tmpDir,
		ConfigFile:      configFile,
		BackupFile:      backupFile,
		CNDomainRuleset: filepath.Join(tmpDir, "rulesets", "cn-domain.mrs"),
		CNIPRuleset:     filepath.Join(tmpDir, "rulesets", "cn-ip.mrs"),
		SubscriptionURL: subscriptionURLFile,
		WhitelistFile:   whitelistFile,
		APIAddr:         apiServer.URL,
	})
	result, err := client.UpdateSubscriptionWithResult()
	if err != nil {
		t.Fatalf("UpdateSubscriptionWithResult failed: %v", err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "node-old") || !strings.Contains(result.Warnings[0], "node-a") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}

	updated, err := client.readConfigMap()
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if updated["mode"] != "rule" || updated["mixed-port"] != 17890 || updated["socks-port"] != 17891 || updated["external-controller"] != "127.0.0.1:19090" {
		t.Fatalf("local mode or ports changed: %#v", updated)
	}
	rules := anyToStrings(updated["rules"])
	if !containsString(rules, "DOMAIN,custom.example,REJECT") || !containsString(rules, "DOMAIN-SUFFIX,white.example,DIRECT") || rules[len(rules)-1] != "MATCH,"+ProxyGroupName {
		t.Fatalf("routing rules changed: %v", rules)
	}
	providers := updated["rule-providers"].(map[string]any)
	if providers["custom"] == nil || providers[CNDomainProviderName] == nil || providers[CNIPProviderName] == nil {
		t.Fatalf("rule providers = %#v", providers)
	}
	dns := updated["dns"].(map[string]any)
	if dns["enhanced-mode"] != "fake-ip" || !reflect.DeepEqual(dns["fake-ip-filter"], []any{"+.internal.example"}) {
		t.Fatalf("dns compatibility fields changed: %#v", dns)
	}
	policy := dns["nameserver-policy"].(map[string]any)
	if !reflect.DeepEqual(policy["+.corp.example"], []any{"10.0.0.53"}) {
		t.Fatalf("custom DNS policy changed: %#v", policy)
	}

	mu.Lock()
	defer mu.Unlock()
	if reloadCount != 1 || selected["GLOBAL"] != "node-a" || selected[ProxyGroupName] != "node-a" {
		t.Fatalf("reload=%d selections=%#v", reloadCount, selected)
	}
}

func TestUpdateSubscription_RestoresConfigAndModeWhenValidationFails(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")
	subscriptionURLFile := filepath.Join(tmpDir, "subscription.url")
	subscriptionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("proxies:\n  - {name: node-new, type: socks5, server: 127.0.0.1, port: 1080}\n"))
	}))
	defer subscriptionServer.Close()

	original := []byte("mode: global\nrules:\n  - MATCH,GLOBAL\n")
	if err := os.WriteFile(configFile, original, 0o640); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, nil, 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(whitelistFile, []byte("domains: []\n"), 0o600); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}
	if err := os.WriteFile(subscriptionURLFile, []byte(subscriptionServer.URL+"\n"), 0o600); err != nil {
		t.Fatalf("write subscription URL: %v", err)
	}

	client := New(config.Paths{
		MihomoBin:       "/usr/bin/false",
		ConfigDir:       tmpDir,
		ConfigFile:      configFile,
		BackupFile:      backupFile,
		SubscriptionURL: subscriptionURLFile,
		WhitelistFile:   whitelistFile,
	})
	if _, err := client.UpdateSubscriptionWithResult(); err == nil {
		t.Fatal("expected validation failure")
	}
	after, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(after) != string(original) {
		t.Fatalf("config was not restored:\n%s", after)
	}
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatalf("stat restored config: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("config mode = %o, want 640", info.Mode().Perm())
	}
}

func TestUpdateSubscription_RestoresConfigAndSelectionsWhenSelectionFails(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := filepath.Join(tmpDir, "config.yaml.bak")
	whitelistFile := filepath.Join(tmpDir, "whitelist.yaml")
	subscriptionURLFile := filepath.Join(tmpDir, "subscription.url")
	subscriptionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("proxies:\n  - {name: node-new, type: socks5, server: 127.0.0.1, port: 1080}\n"))
	}))
	defer subscriptionServer.Close()

	var mu sync.Mutex
	reloadCount := 0
	failNewProxySelection := true
	selected := map[string]string{}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/proxies/GLOBAL" || r.URL.Path == "/proxies/"+ProxyGroupName):
			_, _ = w.Write([]byte(`{"all":["node-old"],"now":"node-old"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/configs":
			mu.Lock()
			reloadCount++
			mu.Unlock()
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/proxies/"):
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			group := strings.TrimPrefix(r.URL.Path, "/proxies/")
			mu.Lock()
			if group == ProxyGroupName && payload["name"] == "node-new" && failNewProxySelection {
				failNewProxySelection = false
				mu.Unlock()
				http.Error(w, "selection failed", http.StatusBadGateway)
				return
			}
			selected[group] = payload["name"]
			mu.Unlock()
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()

	original := []byte("mode: rule\nproxies:\n  - {name: node-old, type: socks5, server: 127.0.0.1, port: 1081}\nrules:\n  - MATCH,🌐 代理\n")
	if err := os.WriteFile(configFile, original, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(backupFile, nil, 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	if err := os.WriteFile(whitelistFile, []byte("domains: []\n"), 0o600); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}
	if err := os.WriteFile(subscriptionURLFile, []byte(subscriptionServer.URL+"\n"), 0o600); err != nil {
		t.Fatalf("write subscription URL: %v", err)
	}

	client := New(config.Paths{
		MihomoBin:       "/usr/bin/true",
		ConfigDir:       tmpDir,
		ConfigFile:      configFile,
		BackupFile:      backupFile,
		SubscriptionURL: subscriptionURLFile,
		WhitelistFile:   whitelistFile,
		APIAddr:         apiServer.URL,
	})
	if _, err := client.UpdateSubscriptionWithResult(); err == nil {
		t.Fatal("expected selection failure")
	}
	after, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(after) != string(original) {
		t.Fatalf("config was not restored:\n%s", after)
	}
	mu.Lock()
	defer mu.Unlock()
	if reloadCount != 2 || selected["GLOBAL"] != "node-old" || selected[ProxyGroupName] != "node-old" {
		t.Fatalf("reload=%d selections=%#v", reloadCount, selected)
	}
}

func TestDiagnoseRoute_RuleSetIsLowConfidence(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("rules:\n  - RULE-SET,mm-cn-domain,DIRECT\n  - MATCH,🌐 代理\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	result, err := New(config.Paths{ConfigFile: configFile}).DiagnoseRoute("example.com")
	if err != nil {
		t.Fatalf("DiagnoseRoute failed: %v", err)
	}
	if result.MatchedRule != "RULE-SET,mm-cn-domain,DIRECT" || result.Target != "DIRECT" || result.Confidence != "low" || !strings.Contains(result.Note, "live connections") {
		t.Fatalf("diagnosis = %+v", result)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
