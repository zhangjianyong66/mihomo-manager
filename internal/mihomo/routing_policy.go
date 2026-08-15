package mihomo

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	ProxyGroupName       = "🌐 代理"
	DirectGroupName      = "🎯 直连"
	CNDomainProviderName = "mm-cn-domain"
	CNIPProviderName     = "mm-cn-ip"
	// Kept for compatibility with callers that display the historical source;
	// manager providers are now local file providers and never use these URLs.
	rulesetUpdateInterval  = 86400
	cnDomainRulesetURL     = "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geosite/cn.mrs"
	cnIPRulesetURL         = "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/geoip/cn.mrs"
	domesticResolver       = "223.5.5.5"
	domesticBackupResolver = "119.29.29.29"
	overseasResolver       = "https://1.1.1.1/dns-query"
	overseasBackupResolver = "https://8.8.8.8/dns-query"
)

var managerLocalRules = []string{
	"DOMAIN,localhost,DIRECT",
	"DOMAIN-SUFFIX,local,DIRECT",
	"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
	"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
	"IP-CIDR6,::1/128,DIRECT,no-resolve",
	"IP-CIDR6,fc00::/7,DIRECT,no-resolve",
	"IP-CIDR6,fe80::/10,DIRECT,no-resolve",
}

var managerDNSPolicyKeys = []string{
	"localhost",
	"+.local",
	"+.lan",
	"rule-set:" + CNDomainProviderName,
}

type RoutingPolicy struct {
	Mode            domain.RoutingMode
	CustomRules     []string
	CustomProviders map[string]any
	Whitelist       []string
	Direct          []string
	Proxy           []string
	DNS             map[string]any
	Warnings        []string
}

// BuildBootstrapConfig returns a private, global-mode copy of a legacy
// configuration suitable for fetching manager-owned rule sets. The source
// bytes are never modified. Manager providers, owned rules and DNS policy
// entries are removed while ordinary user providers/rules/listeners remain.
func BuildBootstrapConfig(content []byte) ([]byte, error) {
	var cfg map[string]any
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("decode bootstrap config: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("bootstrap config must be a mapping")
	}
	policy, err := ParseRoutingPolicyWithRules(cfg, RouteRules{})
	if err != nil {
		return nil, fmt.Errorf("parse bootstrap routing policy: %w", err)
	}
	policy.Mode = domain.RoutingModeGlobal
	if err := ApplyRoutingPolicy(cfg, policy, config.Paths{}); err != nil {
		return nil, fmt.Errorf("apply bootstrap routing policy: %w", err)
	}

	// Global mode must use the GLOBAL selector for the terminal rule. Custom
	// rules are retained, but any manager-generated terminal match is replaced.
	rules := anyToStrings(cfg["rules"])
	filtered := make([]string, 0, len(rules)+1)
	for _, rule := range rules {
		if len(ruleParts(rule)) > 0 && strings.EqualFold(ruleParts(rule)[0], "MATCH") {
			continue
		}
		filtered = append(filtered, rule)
	}
	filtered = append(filtered, "MATCH,GLOBAL")
	cfg["rules"] = filtered

	// Remove DNS fields owned by the manager. User DNS settings remain intact.
	if dns, ok := cfg["dns"].(map[string]any); ok {
		for _, key := range managerDNSPolicyKeys {
			if key == "rule-set:"+CNDomainProviderName {
				if policies, ok := dns["nameserver-policy"].(map[string]any); ok {
					delete(policies, key)
				}
				continue
			}
			delete(dns, key)
		}
		if policies, ok := dns["nameserver-policy"].(map[string]any); ok {
			for key := range policies {
				if isManagerDNSPolicyKey(key) {
					delete(policies, key)
				}
			}
		}
	}

	result, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode bootstrap config: %w", err)
	}
	return bytes.TrimSpace(result), nil
}

func ParseRoutingPolicy(cfg map[string]any, whitelist []string) (RoutingPolicy, error) {
	return ParseRoutingPolicyWithRules(cfg, RouteRules{Direct: whitelist})
}

func ParseRoutingPolicyWithRules(cfg map[string]any, managed RouteRules) (RoutingPolicy, error) {
	if cfg == nil {
		return RoutingPolicy{}, errors.New("routing config must not be nil")
	}
	mode := domain.RoutingModeRule
	if raw, ok := cfg["mode"]; ok && strings.TrimSpace(fmt.Sprint(raw)) != "" {
		mode = domain.RoutingMode(strings.ToLower(strings.TrimSpace(fmt.Sprint(raw))))
	}
	if err := mode.Validate(); err != nil {
		return RoutingPolicy{}, err
	}

	managed.Direct = normalizeRouteRuleList(managed.Direct)
	managed.Proxy = normalizeRouteRuleList(managed.Proxy)
	customRules := make([]string, 0)
	for _, rule := range anyToStrings(cfg["rules"]) {
		if isManagerOwnedRule(rule, managed) {
			continue
		}
		customRules = append(customRules, rule)
	}

	providers, err := stringMap(cfg["rule-providers"], "rule-providers")
	if err != nil {
		return RoutingPolicy{}, err
	}
	customProviders := make(map[string]any, len(providers))
	for name, provider := range providers {
		if isManagerProvider(name) {
			continue
		}
		customProviders[name] = cloneYAMLValue(provider)
	}

	dns, err := stringMap(cfg["dns"], "dns")
	if err != nil {
		return RoutingPolicy{}, err
	}
	warnings := make([]string, 0, 1)
	if _, ok := dns["fallback"]; ok {
		warnings = append(warnings, "DNS fallback 已迁移为明确的 Rule 分流")
	} else if _, ok := dns["fallback-filter"]; ok {
		warnings = append(warnings, "DNS fallback-filter 已迁移为明确的 Rule 分流")
	}

	return RoutingPolicy{
		Mode:            mode,
		CustomRules:     customRules,
		CustomProviders: customProviders,
		Whitelist:       append([]string(nil), managed.Direct...),
		Direct:          append([]string(nil), managed.Direct...),
		Proxy:           append([]string(nil), managed.Proxy...),
		DNS:             cloneStringMap(dns),
		Warnings:        warnings,
	}, nil
}

func ApplyRoutingPolicy(cfg map[string]any, policy RoutingPolicy, paths config.Paths) error {
	if cfg == nil {
		return errors.New("routing config must not be nil")
	}
	if err := policy.Mode.Validate(); err != nil {
		return err
	}

	cfg["mode"] = policy.Mode.String()
	providers := make(map[string]any, len(policy.CustomProviders)+2)
	for name, provider := range policy.CustomProviders {
		if isManagerProvider(name) {
			continue
		}
		providers[name] = cloneYAMLValue(provider)
	}
	if policy.Mode == domain.RoutingModeRule {
		providers[CNDomainProviderName] = managerRuleProvider(
			"domain",
			rulesetConfigPath(paths.ConfigDir, paths.CNDomainRuleset, "cn-domain.mrs"),
		)
		providers[CNIPProviderName] = managerRuleProvider(
			"ipcidr",
			rulesetConfigPath(paths.ConfigDir, paths.CNIPRuleset, "cn-ip.mrs"),
		)
	}
	cfg["rule-providers"] = providers

	rules := make([]string, 0, len(managerLocalRules)+len(policy.CustomRules)+len(policy.Direct)+len(policy.Proxy)+3)
	if policy.Mode == domain.RoutingModeRule {
		rules = append(rules, managerLocalRules...)
	}
	for _, rule := range policy.CustomRules {
		if !isManagerOwnedRule(rule, RouteRules{Direct: policy.Direct, Proxy: policy.Proxy}) {
			rules = append(rules, rule)
		}
	}
	rules = append(rules, renderManagedRules(policy.Direct, policy.Proxy)...)
	if policy.Mode == domain.RoutingModeRule {
		rules = append(rules,
			"RULE-SET,"+CNDomainProviderName+",DIRECT",
			"RULE-SET,"+CNIPProviderName+",DIRECT,no-resolve",
			"MATCH,"+ProxyGroupName,
		)
		cfg["rules"] = rules
		dns, err := applyManagerDNS(policy.DNS)
		if err != nil {
			return err
		}
		cfg["dns"] = dns
	} else {
		cfg["rules"] = append([]string(nil), rules...)
		if len(rules) == 0 {
			cfg["rules"] = []string{"MATCH," + map[bool]string{true: "DIRECT", false: ProxyGroupName}[policy.Mode == domain.RoutingModeDirect]}
		}
		cfg["dns"] = cloneStringMap(policy.DNS)
	}
	return nil
}

func managerRuleProvider(behavior, path string) map[string]any {
	return map[string]any{
		"type":     "file",
		"behavior": behavior,
		"format":   "mrs",
		"path":     path,
	}
}

func rulesetConfigPath(configDir, configured, name string) string {
	if configured == "" {
		configured = filepath.Join(configDir, "rulesets", name)
	}
	if configDir != "" {
		if relative, err := filepath.Rel(configDir, configured); err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			relative = filepath.ToSlash(relative)
			if !strings.HasPrefix(relative, ".") {
				relative = "./" + relative
			}
			return relative
		}
	}
	if configured == "" {
		return "./rulesets/" + name
	}
	return filepath.ToSlash(configured)
}

func applyManagerDNS(existing map[string]any) (map[string]any, error) {
	dns := cloneStringMap(existing)
	delete(dns, "fallback")
	delete(dns, "fallback-filter")
	dns["enable"] = true
	dns["respect-rules"] = true
	dns["default-nameserver"] = []any{domesticResolver, domesticBackupResolver}
	dns["nameserver"] = []any{overseasResolver, overseasBackupResolver}
	dns["proxy-server-nameserver"] = []any{domesticResolver, domesticBackupResolver}

	rawPolicy, err := stringMap(dns["nameserver-policy"], "nameserver-policy")
	if err != nil {
		return nil, err
	}
	policy := make(map[string]any, len(rawPolicy)+len(managerDNSPolicyKeys))
	for key, value := range rawPolicy {
		if isManagerDNSPolicyKey(key) {
			continue
		}
		policy[key] = cloneYAMLValue(value)
	}
	policy["localhost"] = []any{"system"}
	policy["+.local"] = []any{"system"}
	policy["+.lan"] = []any{"system"}
	policy["rule-set:"+CNDomainProviderName] = []any{domesticResolver, domesticBackupResolver}
	dns["nameserver-policy"] = policy
	return dns, nil
}

func isManagerOwnedRule(rule string, managed RouteRules) bool {
	parts := ruleParts(rule)
	if len(parts) == 0 {
		return false
	}
	typ := strings.ToUpper(parts[0])
	if typ == "MATCH" {
		return true
	}
	if isManagerLocalRule(rule) {
		return true
	}
	if typ == "RULE-SET" && len(parts) >= 2 && isManagerProvider(parts[1]) {
		return true
	}
	if (typ == "GEOSITE" || typ == "GEOIP") && len(parts) >= 3 && strings.EqualFold(parts[1], "CN") && isDirectTarget(parts[2]) {
		return true
	}
	if len(parts) >= 3 && (typ == "DOMAIN" || typ == "DOMAIN-SUFFIX" || typ == "DOMAIN-WILDCARD" || typ == "IP-CIDR" || typ == "IP-CIDR6") {
		value, err := normalizeRouteRule(parts[1])
		if err != nil {
			return false
		}
		if isDirectTarget(parts[2]) {
			return containsRouteRule(managed.Direct, value)
		}
		if strings.TrimSpace(parts[2]) == ProxyGroupName {
			return containsRouteRule(managed.Proxy, value)
		}
	}
	return false
}

type renderedManagedRule struct {
	value  string
	target string
	prefix int
	domain bool
}

func renderManagedRules(direct, proxy []string) []string {
	items := make([]renderedManagedRule, 0, len(direct)+len(proxy))
	appendRules := func(values []string, target string) {
		for _, value := range values {
			if prefix, err := netip.ParsePrefix(value); err == nil {
				items = append(items, renderedManagedRule{value: prefix.String(), target: target, prefix: prefix.Bits()})
				continue
			}
			items = append(items, renderedManagedRule{value: value, target: target, prefix: strings.Count(value, ".") + 1, domain: true})
		}
	}
	appendRules(direct, "DIRECT")
	appendRules(proxy, ProxyGroupName)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].domain != items[j].domain {
			return items[i].domain
		}
		if items[i].prefix != items[j].prefix {
			return items[i].prefix > items[j].prefix
		}
		if items[i].value != items[j].value {
			return items[i].value < items[j].value
		}
		return items[i].target < items[j].target
	})
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item.domain {
			out = append(out, "DOMAIN-SUFFIX,"+item.value+","+item.target)
		} else if strings.Contains(item.value, ":") {
			out = append(out, "IP-CIDR6,"+item.value+","+item.target+",no-resolve")
		} else {
			out = append(out, "IP-CIDR,"+item.value+","+item.target+",no-resolve")
		}
	}
	return out
}

func stripWhitelistRules(cfg map[string]any, domains []string) {
	rules := anyToStrings(cfg["rules"])
	result := make([]string, 0, len(rules))
	for _, rule := range rules {
		if isWhitelistDomainRule(rule, domains) {
			continue
		}
		result = append(result, rule)
	}
	cfg["rules"] = result
}

func stripManagedRouteRules(cfg map[string]any, managed RouteRules) {
	rules := anyToStrings(cfg["rules"])
	result := make([]string, 0, len(rules))
	for _, rule := range rules {
		if !isManagerOwnedRule(rule, managed) {
			result = append(result, rule)
		}
	}
	cfg["rules"] = result
}

func isWhitelistDomainRule(rule string, domains []string) bool {
	parts := ruleParts(rule)
	if len(parts) < 3 || !isDirectTarget(parts[2]) {
		return false
	}
	typ := strings.ToUpper(parts[0])
	if typ != "DOMAIN" && typ != "DOMAIN-SUFFIX" && typ != "DOMAIN-WILDCARD" {
		return false
	}
	domainName := normalizeDomain(parts[1])
	for _, candidate := range domains {
		if strings.EqualFold(domainName, normalizeDomain(candidate)) {
			return true
		}
	}
	return false
}

func isManagerLocalRule(rule string) bool {
	normalized := normalizedRule(rule)
	for _, candidate := range managerLocalRules {
		if normalized == normalizedRule(candidate) {
			return true
		}
	}
	return false
}

func isManagerProvider(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), CNDomainProviderName) || strings.EqualFold(strings.TrimSpace(name), CNIPProviderName)
}

func isManagerDNSPolicyKey(key string) bool {
	for _, candidate := range managerDNSPolicyKeys {
		if strings.EqualFold(strings.TrimSpace(key), candidate) {
			return true
		}
	}
	return false
}

func ruleParts(rule string) []string {
	raw := strings.TrimSpace(strings.Trim(rule, "\""))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

func normalizedRule(rule string) string {
	parts := ruleParts(rule)
	for index := range parts {
		parts[index] = strings.ToUpper(parts[index])
	}
	return strings.Join(parts, ",")
}

func stringMap(value any, name string) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping", name)
	}
	return result, nil
}

func cloneStringMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = cloneYAMLValue(value)
	}
	return result
}

func cloneYAMLValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneStringMap(typed)
	case []any:
		result := make([]any, len(typed))
		for index := range typed {
			result[index] = cloneYAMLValue(typed[index])
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
