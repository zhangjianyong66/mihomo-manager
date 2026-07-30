package ruleset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

const (
	StateInstalled = "installed"
	StateMissing   = "missing"
	StateInvalid   = "invalid"
	StateOutdated  = "outdated"
	maxAssetBytes  = 64 << 20
)

var (
	ErrProxyAuthUnsupported = errors.New("ruleset proxy authentication is unsupported")
	ErrInvalidTarget        = errors.New("invalid ruleset target")
	ErrNotReady             = errors.New("rulesets are not ready")
	ErrRestoreFailed        = errors.New("ruleset restore failed")
	installLocks            sync.Map
)

// ReferencesManagerProviders reports whether a configuration explicitly relies
// on either manager-owned CN provider. It deliberately ignores ordinary
// custom providers so global/direct and minimal rule configurations remain
// usable while the optional assets are absent.
func ReferencesManagerProviders(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	if providers, ok := cfg["rule-providers"].(map[string]any); ok {
		for name := range providers {
			if strings.EqualFold(strings.TrimSpace(name), "mm-cn-domain") || strings.EqualFold(strings.TrimSpace(name), "mm-cn-ip") {
				return true
			}
		}
	}
	if rules, ok := cfg["rules"].([]any); ok {
		for _, raw := range rules {
			if text, ok := raw.(string); ok && referencesManagerRule(text) {
				return true
			}
		}
	}
	if rules, ok := cfg["rules"].([]string); ok {
		for _, text := range rules {
			if referencesManagerRule(text) {
				return true
			}
		}
	}
	return false
}

func referencesManagerRule(rule string) bool {
	parts := strings.Split(rule, ",")
	return len(parts) >= 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "rule-set") && (strings.EqualFold(strings.TrimSpace(parts[1]), "mm-cn-domain") || strings.EqualFold(strings.TrimSpace(parts[1]), "mm-cn-ip"))
}

// EnsureReady returns ErrNotReady when a manager provider is referenced but
// the pair is not installed and verified.
func EnsureReady(ctx context.Context, cfg map[string]any, target Target, mihomoBin string) (Status, error) {
	status, err := Inspect(ctx, target, mihomoBin)
	if err != nil {
		return status, err
	}
	if ReferencesManagerProviders(cfg) && status.State != StateInstalled {
		return status, fmt.Errorf("%w: run mm ruleset install", ErrNotReady)
	}
	return status, nil
}

type Target struct {
	Source       string `json:"source"`
	Ref          string `json:"ref"`
	DomainSHA256 string `json:"domainSha256"`
	IPSHA256     string `json:"ipSha256"`
	DomainPath   string `json:"domainPath"`
	IPPath       string `json:"ipPath"`
	DomainName   string `json:"domainName,omitempty"`
	IPName       string `json:"ipName,omitempty"`
}

func (t Target) Validate() error {
	if strings.TrimSpace(t.Source) == "" || strings.TrimSpace(t.Ref) == "" || !validSHA(t.DomainSHA256) || !validSHA(t.IPSHA256) {
		return ErrInvalidTarget
	}
	if strings.ContainsAny(t.Ref, "\r\n") || strings.HasPrefix(t.Ref, "/") || strings.Contains("/"+t.Ref+"/", "/../") {
		return ErrInvalidTarget
	}
	u, err := url.Parse(t.Source)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Host == "" && u.Scheme != "file" {
		return ErrInvalidTarget
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "file" {
		return fmt.Errorf("%w: unsupported source scheme", ErrInvalidTarget)
	}
	return nil
}

type FileStatus struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	Digest      string `json:"digest,omitempty"`
	DigestValid bool   `json:"digestValid"`
	FormatValid bool   `json:"formatValid"`
	Mode        uint32 `json:"mode,omitempty"`
}

type Metadata struct {
	Schema       int       `json:"schema"`
	Source       string    `json:"source"`
	Ref          string    `json:"ref"`
	DomainPath   string    `json:"domainPath"`
	IPPath       string    `json:"ipPath"`
	DomainSHA256 string    `json:"domainSha256"`
	IPSHA256     string    `json:"ipSha256"`
	InstalledAt  time.Time `json:"installedAt"`
}

type Status struct {
	State           string     `json:"state"`
	Target          Target     `json:"target"`
	Installed       *Metadata  `json:"installed,omitempty"`
	Domain          FileStatus `json:"domain"`
	IP              FileStatus `json:"ip"`
	MetadataPath    string     `json:"metadataPath"`
	MetadataValid   bool       `json:"metadataValid"`
	Reused          bool       `json:"reused"`
	NextStart       bool       `json:"nextStart"`
	RuntimeReloaded bool       `json:"runtimeReloaded"`
	Warnings        []string   `json:"warnings,omitempty"`
}

func MetadataPath(configDir string) string {
	return filepath.Join(configDir, "rulesets", ".mihomo-manager.json")
}

func Inspect(ctx context.Context, target Target, mihomoBin string) (Status, error) {
	if err := target.Validate(); err != nil {
		return Status{}, err
	}
	if err := safePath(target.DomainPath); err != nil {
		return Status{}, err
	}
	if err := safePath(target.IPPath); err != nil {
		return Status{}, err
	}
	status := Status{Target: target, MetadataPath: MetadataPath(filepath.Dir(filepath.Dir(target.DomainPath)))}
	status.Domain = inspectFile(target.DomainPath, target.DomainSHA256)
	status.IP = inspectFile(target.IPPath, target.IPSHA256)
	metadataExists := false
	metadataInvalid := false
	if data, err := os.ReadFile(status.MetadataPath); err == nil {
		metadataExists = true
		var metadata Metadata
		if json.Unmarshal(data, &metadata) == nil && metadata.Schema >= 1 {
			status.Installed = &metadata
			status.MetadataValid = metadata.Source == target.Source && metadata.Ref == target.Ref && metadata.DomainSHA256 == target.DomainSHA256 && metadata.IPSHA256 == target.IPSHA256
		} else {
			metadataInvalid = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return status, err
	}
	if !status.Domain.Exists && !status.IP.Exists {
		status.State = StateMissing
		return status, nil
	}
	if metadataInvalid || !status.Domain.Exists || !status.IP.Exists || !secureMode(target.DomainPath) || !secureMode(target.IPPath) || (metadataExists && !secureMode(status.MetadataPath)) {
		status.State = StateInvalid
		return status, nil
	}
	if status.Installed != nil && !status.MetadataValid && (!status.Domain.DigestValid || !status.IP.DigestValid) {
		if status.Domain.Exists && status.IP.Exists && status.Domain.Digest == status.Installed.DomainSHA256 && status.IP.Digest == status.Installed.IPSHA256 && secureMode(target.DomainPath) && secureMode(target.IPPath) {
			status.State = StateOutdated
			return status, nil
		}
	}
	if !status.Domain.DigestValid || !status.IP.DigestValid {
		status.State = StateInvalid
		return status, nil
	}
	status.Domain.FormatValid = validateAssetFormat(ctx, mihomoBin, target.DomainPath, target.IPPath, true)
	status.IP.FormatValid = status.Domain.FormatValid
	if !status.Domain.FormatValid {
		status.State = StateInvalid
		return status, nil
	}
	status.State = StateInstalled
	if !status.MetadataValid {
		status.Reused = true
		if !metadataExists {
			status.Warnings = append(status.Warnings, "规则集来自旧缓存，metadata 将在下次安装命令中补齐")
		} else {
			status.Warnings = append(status.Warnings, "规则集文件有效，但 metadata 需要更新")
		}
	}
	return status, nil
}

func WriteMetadata(target Target) error {
	if err := target.Validate(); err != nil {
		return err
	}
	metaPath := MetadataPath(filepath.Dir(filepath.Dir(target.DomainPath)))
	metadata := Metadata{Schema: 1, Source: target.Source, Ref: target.Ref, DomainPath: target.DomainPath, IPPath: target.IPPath, DomainSHA256: target.DomainSHA256, IPSHA256: target.IPSHA256, InstalledAt: time.Now().UTC()}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(metaPath), ".ruleset-meta-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, metaPath); err != nil {
		return err
	}
	return os.Chmod(metaPath, 0o600)
}

func inspectFile(path, expected string) FileStatus {
	value := FileStatus{Path: path}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return value
	}
	value.Exists, value.Mode = true, uint32(info.Mode().Perm())
	value.Digest = digestFile(path)
	value.DigestValid = strings.EqualFold(value.Digest, expected)
	return value
}

func secureMode(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm() == 0o600
}

func digestFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validateAssetFormat(ctx context.Context, mihomoBin, domainPath, ipPath string, requirePair bool) bool {
	if strings.TrimSpace(mihomoBin) == "" {
		return true
	}
	if _, err := os.Stat(mihomoBin); err != nil {
		return false
	}
	dir := filepath.Dir(domainPath)
	tmp, err := os.MkdirTemp(dir, ".ruleset-format-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "cn-domain.mrs"), mustRead(domainPath), 0o600); err != nil {
		return false
	}
	if requirePair {
		if err := os.WriteFile(filepath.Join(tmp, "cn-ip.mrs"), mustRead(ipPath), 0o600); err != nil {
			return false
		}
	}
	config := "rule-providers:\n  mm-cn-domain:\n    type: file\n    behavior: domain\n    format: mrs\n    path: ./cn-domain.mrs\n  mm-cn-ip:\n    type: file\n    behavior: ipcidr\n    format: mrs\n    path: ./cn-ip.mrs\nrules:\n  - RULE-SET,mm-cn-domain,DIRECT\n  - RULE-SET,mm-cn-ip,DIRECT,no-resolve\n  - MATCH,DIRECT\n"
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return false
	}
	command := exec.CommandContext(ctx, mihomoBin, "-t", "-d", tmp, "-f", configPath)
	return command.Run() == nil
}

func mustRead(path string) []byte { data, _ := os.ReadFile(path); return data }

type Downloader struct {
	Client   *http.Client
	MaxBytes int64
	Timeout  time.Duration
	Proxy    string
}

func (d Downloader) Download(ctx context.Context, target Target, dir string, progress func(string, int64)) (string, string, error) {
	if err := target.Validate(); err != nil {
		return "", "", err
	}
	if d.MaxBytes <= 0 {
		d.MaxBytes = maxAssetBytes
	}
	if d.Timeout <= 0 {
		d.Timeout = 45 * time.Second
	}
	client := d.Client
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if d.Proxy != "" {
			proxyURL, err := url.Parse(strings.TrimSpace(d.Proxy))
			if err != nil {
				return "", "", err
			}
			if _, err := ValidateProxy(d.Proxy); err != nil {
				return "", "", err
			}
			if proxyURL.Scheme == "http" || proxyURL.Scheme == "https" {
				transport.Proxy = http.ProxyURL(proxyURL)
			} else {
				dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
				if err != nil {
					return "", "", err
				}
				transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
					return dialContext(ctx, dialer, network, address)
				}
			}
		}
		client = &http.Client{Timeout: d.Timeout, Transport: transport}
	}
	if d.Proxy != "" {
		parsed, err := ValidateProxy(d.Proxy)
		if err != nil {
			return "", "", err
		}
		_ = parsed
	}
	base := strings.TrimRight(target.Source, "/") + "/" + strings.Trim(target.Ref, "/")
	domain, ip := base+"/geo/geosite/cn.mrs", base+"/geo/geoip/cn.mrs"
	domainTmp, err := downloadWithRetry(ctx, client, domain, dir, "domain", d.MaxBytes, progress)
	if err != nil {
		return "", "", err
	}
	ipTmp, err := downloadWithRetry(ctx, client, ip, dir, "ip", d.MaxBytes, progress)
	if err != nil {
		os.Remove(domainTmp)
		return "", "", err
	}
	return domainTmp, ipTmp, nil
}

func dialContext(ctx context.Context, dialer proxy.Dialer, network, address string) (net.Conn, error) {
	type contextDialer interface {
		DialContext(context.Context, string, string) (net.Conn, error)
	}
	if value, ok := dialer.(contextDialer); ok {
		return value.DialContext(ctx, network, address)
	}
	result := make(chan struct {
		conn net.Conn
		err  error
	}, 1)
	go func() {
		conn, err := dialer.Dial(network, address)
		result <- struct {
			conn net.Conn
			err  error
		}{conn, err}
	}()
	select {
	case value := <-result:
		return value.conn, value.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func downloadWithRetry(ctx context.Context, client *http.Client, source, dir, label string, max int64, progress func(string, int64)) (string, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		path, err := downloadOne(ctx, client, source, dir, label, max, progress)
		if err == nil {
			return path, nil
		}
		last = err
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 150 * time.Millisecond):
		}
	}
	return "", last
}

func downloadOne(ctx context.Context, client *http.Client, source, dir, label string, max int64, progress func(string, int64)) (string, error) {
	tmp, err := os.CreateTemp(dir, ".ruleset-download-")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	_ = tmp.Chmod(0o600)
	defer func() {
		if err != nil {
			os.Remove(path)
		}
	}()
	if strings.HasPrefix(source, "file://") {
		u, e := url.Parse(source)
		if e != nil {
			err = e
		} else {
			var input *os.File
			input, err = os.Open(u.Path)
			if err == nil {
				_, err = copyLimited(tmp, input, max, progress, label)
				input.Close()
			}
		}
	} else {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if e != nil {
			err = e
		} else {
			var response *http.Response
			response, err = client.Do(req)
			if err == nil {
				defer response.Body.Close()
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					err = fmt.Errorf("ruleset download returned HTTP %d", response.StatusCode)
				} else {
					_, err = copyLimited(tmp, response.Body, max, progress, label)
				}
			}
		}
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func copyLimited(dst io.Writer, src io.Reader, max int64, progress func(string, int64), label string) (int64, error) {
	reader := io.LimitReader(src, max+1)
	var total int64
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if total+int64(n) > max {
				return total, fmt.Errorf("ruleset asset exceeds %d bytes", max)
			}
			written, werr := dst.Write(buf[:n])
			total += int64(written)
			if werr != nil {
				return total, werr
			}
			if progress != nil {
				progress(label, total)
			}
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, err
		}
	}
}

type Proxy struct {
	Scheme, Host string
	Port         int
}

func ValidateProxy(raw string) (Proxy, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Proxy{}, fmt.Errorf("invalid ruleset proxy")
	}
	if u.User != nil {
		return Proxy{}, ErrProxyAuthUnsupported
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return Proxy{}, fmt.Errorf("unsupported ruleset proxy")
	}
	if u.Hostname() == "" {
		return Proxy{}, fmt.Errorf("ruleset proxy host is required")
	}
	if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return Proxy{}, fmt.Errorf("ruleset proxy must not contain path, query, or fragment")
	}
	port := 0
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return Proxy{}, fmt.Errorf("ruleset proxy port is invalid")
		}
	}
	if port == 0 {
		if u.Scheme == "socks5" || u.Scheme == "socks5h" {
			port = 1080
		} else if u.Scheme == "http" {
			port = 80
		} else {
			port = 443
		}
	}
	return Proxy{Scheme: u.Scheme, Host: u.Hostname(), Port: port}, nil
}

func Publish(ctx context.Context, target Target, domainTmp, ipTmp string, mihomoBin string) (Status, error) {
	return PublishWithHook(ctx, target, domainTmp, ipTmp, mihomoBin, nil)
}

func PublishWithHook(ctx context.Context, target Target, domainTmp, ipTmp string, mihomoBin string, afterPublish func(context.Context) error) (Status, error) {
	if err := target.Validate(); err != nil {
		return Status{}, err
	}
	if err := safePath(target.DomainPath); err != nil {
		return Status{}, err
	}
	if err := safePath(target.IPPath); err != nil {
		return Status{}, err
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	dir := filepath.Dir(target.DomainPath)
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return Status{}, fmt.Errorf("%w: ruleset directory is a symlink", ErrInvalidTarget)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Status{}, err
	}
	_ = os.Chmod(dir, 0o700)
	mutexValue, _ := installLocks.LoadOrStore(dir, &sync.Mutex{})
	mutex := mutexValue.(*sync.Mutex)
	mutex.Lock()
	defer mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	oldDomain, oldIP, oldMeta := readOptional(target.DomainPath), readOptional(target.IPPath), readOptional(MetadataPath(filepath.Dir(filepath.Dir(target.DomainPath))))
	oldDomainMode, oldIPMode, oldMetaMode := fileMode(target.DomainPath), fileMode(target.IPPath), fileMode(MetadataPath(filepath.Dir(filepath.Dir(target.DomainPath))))
	restore := func() error {
		return errors.Join(
			restoreFile(target.DomainPath, oldDomain, oldDomainMode),
			restoreFile(target.IPPath, oldIP, oldIPMode),
			restoreFile(MetadataPath(filepath.Dir(filepath.Dir(target.DomainPath))), oldMeta, oldMetaMode),
		)
	}
	if digestFile(domainTmp) != target.DomainSHA256 || digestFile(ipTmp) != target.IPSHA256 {
		return Status{}, fmt.Errorf("ruleset digest mismatch")
	}
	if !validateAssetFormat(ctx, mihomoBin, domainTmp, ipTmp, true) {
		return Status{}, fmt.Errorf("ruleset format validation failed")
	}
	if err := replaceFile(domainTmp, target.DomainPath); err != nil {
		restoreErr := restore()
		if restoreErr != nil {
			return Status{}, errors.Join(ErrRestoreFailed, err, restoreErr)
		}
		return Status{}, err
	}
	if err := replaceFile(ipTmp, target.IPPath); err != nil {
		restoreErr := restore()
		if restoreErr != nil {
			return Status{}, errors.Join(ErrRestoreFailed, err, restoreErr)
		}
		return Status{}, err
	}
	if err := WriteMetadata(target); err != nil {
		restoreErr := restore()
		if restoreErr != nil {
			return Status{}, errors.Join(ErrRestoreFailed, err, restoreErr)
		}
		return Status{}, err
	}
	status, err := Inspect(ctx, target, mihomoBin)
	if err != nil || status.State != StateInstalled {
		restoreErr := restore()
		if restoreErr != nil {
			return Status{}, errors.Join(ErrRestoreFailed, err, restoreErr)
		}
		return Status{}, fmt.Errorf("published ruleset failed verification: %w", err)
	}
	if afterPublish != nil {
		if err := afterPublish(ctx); err != nil {
			restoreErr := restore()
			reloadErr := afterPublish(ctx)
			if restoreErr != nil || reloadErr != nil {
				return Status{}, errors.Join(ErrRestoreFailed, err, restoreErr, reloadErr)
			}
			return Status{}, err
		}
		status.RuntimeReloaded = true
	}
	return status, nil
}

func replaceFile(src, dst string) error {
	if err := os.Chmod(src, 0o600); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func safePath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("ruleset path must be absolute")
	}
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("ruleset path contains symlink: %w", os.ErrPermission)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}
func readOptional(path string) []byte { data, _ := os.ReadFile(path); return data }
func fileMode(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Mode().Perm()
}
func restoreFile(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

var shaPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func validSHA(value string) bool  { return shaPattern.MatchString(strings.TrimSpace(value)) }
func join(parts ...string) string { return filepath.Join(parts...) }
