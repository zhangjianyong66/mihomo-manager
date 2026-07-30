package ruleset

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Catalog is the trusted, non-secret source description shared by Go and Shell.
type Catalog struct {
	Schema     int               `json:"schema"`
	Source     string            `json:"source"`
	Ref        string            `json:"ref"`
	DomainPath string            `json:"domain_path"`
	IPPath     string            `json:"ip_path"`
	DomainSHA  string            `json:"domain_sha256"`
	IPSHA      string            `json:"ip_sha256"`
	DomainName string            `json:"domain_name"`
	IPName     string            `json:"ip_name"`
	Behavior   map[string]string `json:"behavior"`
}

//go:embed catalog.json
var catalogBytes []byte

func DefaultCatalog() Catalog {
	var value Catalog
	if err := json.Unmarshal(catalogBytes, &value); err != nil {
		panic("invalid embedded ruleset catalog: " + err.Error())
	}
	return value
}

func (c Catalog) Validate() error {
	if c.Schema < 1 || strings.TrimSpace(c.Source) == "" || strings.TrimSpace(c.Ref) == "" {
		return fmt.Errorf("ruleset catalog is incomplete")
	}
	if !validSHA(c.DomainSHA) || !validSHA(c.IPSHA) {
		return fmt.Errorf("ruleset catalog contains invalid sha256")
	}
	if c.DomainPath == "" || c.IPPath == "" || c.DomainName == "" || c.IPName == "" {
		return fmt.Errorf("ruleset catalog paths are incomplete")
	}
	return nil
}

func (c Catalog) Target(configDir string) Target {
	return Target{
		Source: c.Source, Ref: c.Ref, DomainSHA256: c.DomainSHA, IPSHA256: c.IPSHA,
		DomainPath: join(configDir, "rulesets", c.DomainName), IPPath: join(configDir, "rulesets", c.IPName),
		DomainName: c.DomainName, IPName: c.IPName,
	}
}

func (c Catalog) URLs() (string, string) {
	return strings.TrimRight(c.Source, "/") + "/" + c.Ref + "/" + strings.TrimLeft(c.DomainPath, "/"),
		strings.TrimRight(c.Source, "/") + "/" + c.Ref + "/" + strings.TrimLeft(c.IPPath, "/")
}
