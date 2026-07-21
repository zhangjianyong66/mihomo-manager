package app

import (
	"context"
	"os"
	"strings"
	"testing"
)

type fakeInteractiveAPI struct {
	CapabilityAPI
	document ConfigDocument
	profile  string
	digest   string
	content  []byte
}

func (f *fakeInteractiveAPI) ReadConfig(context.Context, string) (ConfigDocument, error) {
	return f.document, nil
}

func (f *fakeInteractiveAPI) ReplaceConfig(_ context.Context, profileID, digest string, content []byte) error {
	f.profile = profileID
	f.digest = digest
	f.content = append([]byte(nil), content...)
	return nil
}

func TestInteractiveCapabilitiesEditConfigUsesPrivateFileAndExpectedDigest(t *testing.T) {
	fake := &fakeInteractiveAPI{document: ConfigDocument{Content: []byte("mode: rule\n"), SHA256: strings.Repeat("a", 64)}}
	capabilities := NewInteractiveCapabilities(fake, "fake-editor")
	capabilities.run = func(_ context.Context, name string, args ...string) error {
		if name != "fake-editor" || len(args) != 1 {
			t.Fatalf("unexpected editor invocation: %q %#v", name, args)
		}
		info, err := os.Stat(args[0])
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("temporary config mode=%#o", info.Mode().Perm())
		}
		return os.WriteFile(args[0], []byte("mode: direct\n"), 0o600)
	}
	if err := capabilities.EditConfig(context.Background(), "legacy-mihomo"); err != nil {
		t.Fatal(err)
	}
	if fake.profile != "legacy-mihomo" || fake.digest != strings.Repeat("a", 64) || string(fake.content) != "mode: direct\n" {
		t.Fatalf("unexpected replace: profile=%q digest=%q content=%q", fake.profile, fake.digest, fake.content)
	}
}
