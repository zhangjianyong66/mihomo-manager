package mihomo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/domain"
)

const (
	ProxyGroupName         = "🌐 代理"
	DirectGroupName        = "🎯 直连"
	CNDomainProviderName   = "mm-cn-domain"
	CNIPProviderName       = "mm-cn-ip"
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
	DNS             map[string]any
	Warnings        []string
}

func ParseRoutingPolicy(cfg map[string]any, whitelist []string) (RoutingPolicy, error) {
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

	domains := normalizeDomainList(whitelist)
	customRules := make([]string, 0)
	for _, rule := range anyToStrings(cfg["rules"]) {
		if isManagerOwnedRule(rule, domains) {
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
		Whitelist:       domains,
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
	providers[CNDomainProviderName] = managerRuleProvider(
		"domain",
		rulesetConfigPath(paths.ConfigDir, paths.CNDomainRuleset, "cn-domain.mrs"),
		cnDomainRulesetURL,
	)
	providers[CNIPProviderName] = managerRuleProvider(
		"ipcidr",
		rulesetConfigPath(paths.ConfigDir, paths.CNIPRuleset, "cn-ip.mrs"),
		cnIPRulesetURL,
	)
	cfg["rule-providers"] = providers

	rules := make([]string, 0, len(managerLocalRules)+len(policy.CustomRules)+len(policy.Whitelist)+3)
	rules = append(rules, managerLocalRules...)
	for _, rule := range policy.CustomRules {
		if !isManagerOwnedRule(rule, policy.Whitelist) {
			rules = append(rules, rule)
		}
	}
	for _, domainName := range normalizeDomainList(policy.Whitelist) {
		rules = append(rules, "DOMAIN-SUFFIX,"+domainName+",DIRECT")
	}
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
	return nil
}

func managerRuleProvider(behavior, path, url string) map[string]any {
	return map[string]any{
		"type":     "http",
		"behavior": behavior,
		"format":   "mrs",
		"path":     path,
		"url":      url,
		"interval": rulesetUpdateInterval,
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

func isManagerOwnedRule(rule string, whitelist []string) bool {
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
	if len(parts) >= 3 && (typ == "DOMAIN" || typ == "DOMAIN-SUFFIX" || typ == "DOMAIN-WILDCARD") && isDirectTarget(parts[2]) {
		domainName := normalizeDomain(parts[1])
		for _, managedDomain := range whitelist {
			if strings.EqualFold(domainName, normalizeDomain(managedDomain)) {
				return true
			}
		}
	}
	return false
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
