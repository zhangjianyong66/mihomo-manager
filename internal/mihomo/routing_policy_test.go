package mihomo

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"gopkg.in/yaml.v3"
)

func TestRoutingPolicy_GeneratesStableRuleOrderAndProviders(t *testing.T) {
	configDir := t.TempDir()
	cfg := map[string]any{
		"mode": "rule",
		"rule-providers": map[string]any{
			"custom":             map[string]any{"type": "file", "path": "./custom.yaml", "behavior": "classical"},
			CNDomainProviderName: map[string]any{"type": "file", "path": "./old-domain.mrs"},
			CNIPProviderName:     map[string]any{"type": "file", "path": "./old-ip.mrs"},
		},
		"rules": []any{
			"DOMAIN,localhost,DIRECT",
			"IP-CIDR, 10.0.0.0/8, DIRECT, no-resolve",
			"DOMAIN,custom.example,REJECT",
			"DOMAIN-SUFFIX,whitelist.example,DIRECT",
			"DOMAIN-SUFFIX,user-direct.example,DIRECT",
			"GEOSITE,CN,DIRECT",
			"GEOIP,CN,DIRECT,no-resolve",
			"RULE-SET,mm-cn-domain,DIRECT",
			"MATCH,GLOBAL",
			"DOMAIN,after-match.example,DIRECT",
			"MATCH,DIRECT",
		},
		"dns": map[string]any{
			"enhanced-mode":  "fake-ip",
			"fake-ip-range":  "198.18.0.1/16",
			"fake-ip-filter": []any{"+.example.test"},
			"fallback":       []any{"tls://9.9.9.9"},
			"nameserver-policy": map[string]any{
				"+.corp.example":        []any{"10.0.0.53"},
				"rule-set:mm-cn-domain": []any{"old-resolver"},
			},
		},
	}

	policy, err := ParseRoutingPolicy(cfg, []string{"whitelist.example"})
	if err != nil {
		t.Fatalf("ParseRoutingPolicy failed: %v", err)
	}
	if policy.Mode != domain.RoutingModeRule {
		t.Fatalf("mode = %q", policy.Mode)
	}
	wantCustomRules := []string{
		"DOMAIN,custom.example,REJECT",
		"DOMAIN-SUFFIX,user-direct.example,DIRECT",
		"DOMAIN,after-match.example,DIRECT",
	}
	if !reflect.DeepEqual(policy.CustomRules, wantCustomRules) {
		t.Fatalf("custom rules = %#v, want %#v", policy.CustomRules, wantCustomRules)
	}
	if len(policy.CustomProviders) != 1 || policy.CustomProviders["custom"] == nil {
		t.Fatalf("custom providers = %#v", policy.CustomProviders)
	}
	if len(policy.Warnings) != 1 {
		t.Fatalf("warnings = %#v", policy.Warnings)
	}

	paths := config.Paths{
		ConfigDir:       configDir,
		CNDomainRuleset: filepath.Join(configDir, "rulesets", "cn-domain.mrs"),
		CNIPRuleset:     filepath.Join(configDir, "rulesets", "cn-ip.mrs"),
	}
	if err := ApplyRoutingPolicy(cfg, policy, paths); err != nil {
		t.Fatalf("ApplyRoutingPolicy failed: %v", err)
	}

	wantRules := append([]string{}, managerLocalRules...)
	wantRules = append(wantRules, wantCustomRules...)
	wantRules = append(wantRules,
		"DOMAIN-SUFFIX,whitelist.example,DIRECT",
		"RULE-SET,mm-cn-domain,DIRECT",
		"RULE-SET,mm-cn-ip,DIRECT,no-resolve",
		"MATCH,🌐 代理",
	)
	if got := anyToStrings(cfg["rules"]); !reflect.DeepEqual(got, wantRules) {
		t.Fatalf("rules =\n%v\nwant\n%v", got, wantRules)
	}
	matchCount := 0
	for index, rule := range anyToStrings(cfg["rules"]) {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(rule)), "MATCH,") {
			matchCount++
			if index != len(wantRules)-1 {
				t.Fatalf("MATCH is not last: %v", cfg["rules"])
			}
		}
	}
	if matchCount != 1 {
		t.Fatalf("MATCH count = %d", matchCount)
	}

	providers := cfg["rule-providers"].(map[string]any)
	assertManagerProvider(t, providers[CNDomainProviderName], "domain", "./rulesets/cn-domain.mrs", cnDomainRulesetURL)
	assertManagerProvider(t, providers[CNIPProviderName], "ipcidr", "./rulesets/cn-ip.mrs", cnIPRulesetURL)
	if !reflect.DeepEqual(providers["custom"], map[string]any{"type": "file", "path": "./custom.yaml", "behavior": "classical"}) {
		t.Fatalf("custom provider changed: %#v", providers["custom"])
	}

	dns := cfg["dns"].(map[string]any)
	if dns["enable"] != true || dns["respect-rules"] != true || dns["enhanced-mode"] != "fake-ip" || dns["fake-ip-range"] != "198.18.0.1/16" {
		t.Fatalf("dns fields = %#v", dns)
	}
	if _, ok := dns["fallback"]; ok {
		t.Fatalf("fallback was not removed: %#v", dns)
	}
	dnsPolicy := dns["nameserver-policy"].(map[string]any)
	if !reflect.DeepEqual(dnsPolicy["+.corp.example"], []any{"10.0.0.53"}) {
		t.Fatalf("custom nameserver policy changed: %#v", dnsPolicy)
	}
	if !reflect.DeepEqual(dnsPolicy["rule-set:mm-cn-domain"], []any{domesticResolver, domesticBackupResolver}) {
		t.Fatalf("manager nameserver policy = %#v", dnsPolicy)
	}
}

func TestRoutingPolicy_DefaultsMissingModeToRule(t *testing.T) {
	policy, err := ParseRoutingPolicy(map[string]any{}, nil)
	if err != nil {
		t.Fatalf("ParseRoutingPolicy failed: %v", err)
	}
	if policy.Mode != domain.RoutingModeRule {
		t.Fatalf("mode = %q", policy.Mode)
	}
	if err := ApplyRoutingPolicy(map[string]any{}, policy, config.Paths{}); err != nil {
		t.Fatalf("ApplyRoutingPolicy failed: %v", err)
	}
}

func TestRoutingPolicy_RejectsInvalidOwnedMappings(t *testing.T) {
	for name, cfg := range map[string]map[string]any{
		"mode":             {"mode": "unknown"},
		"providers":        {"rule-providers": []any{"invalid"}},
		"dns":              {"dns": []any{"invalid"}},
		"nameserverPolicy": {"dns": map[string]any{"nameserver-policy": []any{"invalid"}}},
	} {
		t.Run(name, func(t *testing.T) {
			policy, err := ParseRoutingPolicy(cfg, nil)
			if name == "nameserverPolicy" {
				if err != nil {
					t.Fatalf("ParseRoutingPolicy failed early: %v", err)
				}
				err = ApplyRoutingPolicy(cfg, policy, config.Paths{})
			}
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestBuildBootstrapConfigRemovesManagerOwnershipAndUsesGlobal(t *testing.T) {
	source := []byte("mode: rule\n" +
		"mixed-port: 7890\n" +
		"external-controller: 127.0.0.1:9090\n" +
		"proxies:\n  - {name: node, type: socks5, server: 127.0.0.1, port: 1080}\n" +
		"proxy-groups:\n  - {name: GLOBAL, type: select, proxies: [node]}\n" +
		"rule-providers:\n  mm-cn-domain: {type: file, path: ./rulesets/cn-domain.mrs}\n  custom: {type: file, path: ./custom.yaml}\n" +
		"rules:\n  - DOMAIN-SUFFIX,custom.example,DIRECT\n  - RULE-SET,mm-cn-domain,DIRECT\n  - MATCH,GLOBAL\n" +
		"dns:\n  enable: true\n  nameserver-policy:\n    rule-set:mm-cn-domain: [223.5.5.5]\n    +.example: [1.1.1.1]\n")
	got, err := BuildBootstrapConfig(source)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(got, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["mode"] != "global" {
		t.Fatalf("mode = %#v", cfg["mode"])
	}
	providers, _ := cfg["rule-providers"].(map[string]any)
	if _, ok := providers[CNDomainProviderName]; ok {
		t.Fatal("manager provider retained")
	}
	if _, ok := providers["custom"]; !ok {
		t.Fatal("custom provider removed")
	}
	rules := anyToStrings(cfg["rules"])
	for _, rule := range rules {
		if strings.Contains(strings.ToLower(rule), "mm-cn-") {
			t.Fatalf("manager rule retained: %s", rule)
		}
	}
	if rules[len(rules)-1] != "MATCH,GLOBAL" {
		t.Fatalf("terminal rule = %q", rules[len(rules)-1])
	}
	dns, _ := cfg["dns"].(map[string]any)
	policy, _ := dns["nameserver-policy"].(map[string]any)
	if _, ok := policy["rule-set:mm-cn-domain"]; ok {
		t.Fatal("manager DNS policy retained")
	}
	if _, ok := policy["+.example"]; !ok {
		t.Fatal("custom DNS policy removed")
	}
}

func TestRoutingPolicy_MihomoNativeValidation(t *testing.T) {
	if os.Getenv("MIHOMO_NATIVE_TEST") != "1" {
		t.Skip("set MIHOMO_NATIVE_TEST=1 to run the installed mihomo validation")
	}
	paths := config.Load()
	if _, err := os.Stat(paths.MihomoBin); err != nil {
		t.Skipf("mihomo binary is unavailable: %v", err)
	}
	domainRules, err := os.ReadFile(paths.CNDomainRuleset)
	if err != nil {
		t.Skipf("CN domain ruleset is unavailable: %v", err)
	}
	ipRules, err := os.ReadFile(paths.CNIPRuleset)
	if err != nil {
		t.Skipf("CN IP ruleset is unavailable: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, "rulesets"), 0o700); err != nil {
		t.Fatalf("create ruleset dir: %v", err)
	}
	domainPath := filepath.Join(tmpDir, "rulesets", "cn-domain.mrs")
	ipPath := filepath.Join(tmpDir, "rulesets", "cn-ip.mrs")
	if err := os.WriteFile(domainPath, domainRules, 0o600); err != nil {
		t.Fatalf("write domain ruleset: %v", err)
	}
	if err := os.WriteFile(ipPath, ipRules, 0o600); err != nil {
		t.Fatalf("write IP ruleset: %v", err)
	}
	cfg := map[string]any{
		"mixed-port": 17890,
		"socks-port": 17891,
		"proxies":    []any{map[string]any{"name": "native-test", "type": "socks5", "server": "127.0.0.1", "port": 1080}},
		"proxy-groups": []any{
			map[string]any{"name": ProxyGroupName, "type": "select", "proxies": []any{"native-test"}},
			map[string]any{"name": DirectGroupName, "type": "select", "proxies": []any{"DIRECT"}},
		},
	}
	policy, err := ParseRoutingPolicy(cfg, []string{"example.com"})
	if err != nil {
		t.Fatalf("ParseRoutingPolicy failed: %v", err)
	}
	if err := ApplyRoutingPolicy(cfg, policy, config.Paths{ConfigDir: tmpDir, CNDomainRuleset: domainPath, CNIPRuleset: ipPath}); err != nil {
		t.Fatalf("ApplyRoutingPolicy failed: %v", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	command := exec.Command(paths.MihomoBin, "-t", "-d", tmpDir, "-f", configPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("mihomo native validation failed: %v\n%s", err, output)
	}
}

func assertManagerProvider(t *testing.T, value any, behavior, path, url string) {
	t.Helper()
	provider, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("provider = %#v", value)
	}
	if provider["type"] != "file" || provider["behavior"] != behavior || provider["format"] != "mrs" || provider["path"] != path {
		t.Fatalf("provider = %#v", provider)
	}
	if _, ok := provider["url"]; ok {
		t.Fatalf("manager provider unexpectedly has remote url: %#v", provider)
	}
}
