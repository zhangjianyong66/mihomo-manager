package daemon

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhangjianyong66/mihomo-manager/internal/config"
	"github.com/zhangjianyong66/mihomo-manager/internal/mihomo"
	"github.com/zhangjianyong66/mihomo-manager/internal/platform"
)

func TestSelectProxyListenerUsesProtocolPriority(t *testing.T) {
	listeners := []mihomo.ListenerPort{
		{Field: mihomo.PortFieldHTTPPort, Host: "127.0.0.1", Port: 7892},
		{Field: mihomo.PortFieldMixedPort, Host: "127.0.0.1", Port: 7890},
		{Field: mihomo.PortFieldSocksPort, Host: "::1", Port: 7891},
	}
	httpListener, ok := selectProxyListener(listeners, platform.ProxyTargetHTTP)
	if !ok || httpListener.Field != mihomo.PortFieldMixedPort || httpListener.Port != 7890 {
		t.Fatalf("http listener=%+v ok=%t", httpListener, ok)
	}
	httpsListener, ok := selectProxyListener(listeners, platform.ProxyTargetHTTPS)
	if !ok || httpsListener.Field != mihomo.PortFieldMixedPort {
		t.Fatalf("https listener=%+v ok=%t", httpsListener, ok)
	}
	socksListener, ok := selectProxyListener(listeners, platform.ProxyTargetSocks)
	if !ok || socksListener.Field != mihomo.PortFieldSocksPort || socksListener.Port != 7891 {
		t.Fatalf("socks listener=%+v ok=%t", socksListener, ok)
	}
}

func TestProxyErrorsUseStableClientCodes(t *testing.T) {
	status, code, _, _ := classifyCapabilityError(ErrProxyEndpointMissing)
	if status != http.StatusBadRequest || code != "INVALID_REQUEST" {
		t.Fatalf("missing listener status=%d code=%s", status, code)
	}
	status, code, _, _ = classifyCapabilityError(platform.ErrProxyAuthUnsupported)
	if status != http.StatusConflict || code != "PROXY_AUTH_UNSUPPORTED" {
		t.Fatalf("auth status=%d code=%s", status, code)
	}
	status, code, _, _ = classifyCapabilityError(ErrProxySnapshotMissing)
	if status != http.StatusNotFound || code != "PROXY_SNAPSHOT_NOT_FOUND" {
		t.Fatalf("snapshot status=%d code=%s", status, code)
	}
}

func TestProxyStateUsesPrivateAtomicFileAndRejectsCorruption(t *testing.T) {
	root := t.TempDir()
	paths, err := config.ResolveManagerPaths(config.ManagerEnvironment{HomeDir: root, StateHome: filepath.Join(root, "state")})
	if err != nil {
		t.Fatal(err)
	}
	service := &CapabilityService{managerPaths: paths}
	state := proxyPersistentState{Version: 1, Env: &envProxyState{BlockHash: "abc"}}
	if err := service.saveProxyState(state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.ProxyState)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("proxy state mode=%#o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(paths.ProxyState))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("proxy state dir mode=%#o", dirInfo.Mode().Perm())
	}
	loaded, err := service.loadProxyState()
	if err != nil || loaded.Env == nil || loaded.Env.BlockHash != "abc" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	if err := os.WriteFile(paths.ProxyState, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.loadProxyState(); !errors.Is(err, ErrProxyStateCorrupt) {
		t.Fatalf("corrupt state err=%v", err)
	}
}
