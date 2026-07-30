package app

import (
	"strings"
	"testing"
)

func TestRuleSetProxyPriorityAndRedaction(t *testing.T) {
	values := map[string]string{"HTTPS_PROXY": "http://127.0.0.1:8443", "ALL_PROXY": "socks5://127.0.0.1:1080", "HTTP_PROXY": "http://127.0.0.1:8080"}
	endpoint, err := rulesetProxyFromEnv(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
	if err != nil || endpoint != "http://127.0.0.1:8443" {
		t.Fatalf("endpoint=%q err=%v", endpoint, err)
	}
	secret := "do-not-print"
	_, err = rulesetProxyFromEnv(func(key string) (string, bool) {
		if key == "HTTPS_PROXY" {
			return "http://user:" + secret + "@127.0.0.1:8443/private?token=" + secret, true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("authenticated proxy should be rejected")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("proxy secret leaked: %v", err)
	}
}

func TestRuleSetTargetOverridesRequireDigests(t *testing.T) {
	c := &DaemonCapabilities{lookupEnv: func(key string) (string, bool) {
		if key == "MM_RULESET_BASE_URL" {
			return "https://example.invalid/mirror", true
		}
		return "", false
	}}
	if _, err := c.ruleSetTargetFromEnv(); err == nil {
		t.Fatal("custom source without digests should fail")
	}
}
