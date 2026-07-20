package cli

import (
	"encoding/json"
	"net/url"
)

const Redacted = "[REDACTED]"

type Secret struct {
	value    string
	redacted string
}

func NewSecret(value string) Secret {
	return Secret{value: value, redacted: Redacted}
}

func NewURLSecret(value string) Secret {
	return Secret{value: value, redacted: redactURL(value)}
}

func (s Secret) Display(showSecrets bool) string {
	if showSecrets {
		return s.value
	}
	if s.redacted == "" {
		return Redacted
	}
	return s.redacted
}

func (s Secret) String() string { return s.Display(false) }

func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Display(false))
}

func redactURL(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return Redacted
	}
	u.User = nil
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	if u.Path != "" && u.Path != "/" {
		u.Path = "/redacted"
		u.RawPath = ""
	}
	return u.String()
}
