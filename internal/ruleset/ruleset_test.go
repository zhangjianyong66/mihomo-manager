package ruleset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectReusesValidLegacyPairAndBackfillsMetadata(t *testing.T) {
	configDir := t.TempDir()
	rulesetDir := filepath.Join(configDir, "rulesets")
	if err := os.Mkdir(rulesetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	domainData := []byte("domain-rules")
	ipData := []byte("ip-rules")
	target := Target{
		Source:       "https://example.invalid/rules",
		Ref:          "fixed-ref",
		DomainSHA256: testDigest(domainData),
		IPSHA256:     testDigest(ipData),
		DomainPath:   filepath.Join(rulesetDir, "cn-domain.mrs"),
		IPPath:       filepath.Join(rulesetDir, "cn-ip.mrs"),
	}
	if err := os.WriteFile(target.DomainPath, domainData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.IPPath, ipData, 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := Inspect(context.Background(), target, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateInstalled || !status.Reused || status.MetadataValid {
		t.Fatalf("unexpected legacy status: %#v", status)
	}
	if err := WriteMetadata(target); err != nil {
		t.Fatal(err)
	}
	status, err = Inspect(context.Background(), target, "")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateInstalled || status.Reused || !status.MetadataValid {
		t.Fatalf("unexpected metadata status: %#v", status)
	}
}

func TestInspectDistinguishesMissingInvalidAndOutdated(t *testing.T) {
	configDir := t.TempDir()
	target := DefaultCatalog().Target(configDir)
	status, err := Inspect(context.Background(), target, "")
	if err != nil || status.State != StateMissing {
		t.Fatalf("missing status=%#v err=%v", status, err)
	}
	if err := os.Mkdir(filepath.Dir(target.DomainPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target.DomainPath, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err = Inspect(context.Background(), target, "")
	if err != nil || status.State != StateInvalid {
		t.Fatalf("invalid status=%#v err=%v", status, err)
	}
}

func TestValidateProxyRejectsCredentialsAndUnsafeSuffixes(t *testing.T) {
	if _, err := ValidateProxy("http://user:secret@127.0.0.1:7890"); !errors.Is(err, ErrProxyAuthUnsupported) {
		t.Fatalf("expected auth error, got %v", err)
	}
	for _, value := range []string{
		"http://127.0.0.1:7890/path",
		"socks5://127.0.0.1:abc",
		"http://127.0.0.1:7890?token=secret",
	} {
		if _, err := ValidateProxy(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
	proxyValue, err := ValidateProxy("socks5h://127.0.0.1:7891")
	if err != nil || proxyValue.Port != 7891 || proxyValue.Scheme != "socks5h" {
		t.Fatalf("unexpected proxy=%#v err=%v", proxyValue, err)
	}
}

func TestReferencesManagerProviders(t *testing.T) {
	if ReferencesManagerProviders(map[string]any{"rules": []any{"MATCH,DIRECT"}}) {
		t.Fatal("minimal rule config must not require manager rulesets")
	}
	if !ReferencesManagerProviders(map[string]any{"rules": []any{"RULE-SET,mm-cn-domain,DIRECT"}}) {
		t.Fatal("manager rule reference was not detected")
	}
	if ReferencesManagerProviders(map[string]any{"rule-providers": map[string]any{"custom": map[string]any{"type": "http"}}}) {
		t.Fatal("custom provider must not require manager rulesets")
	}
}

func testDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
