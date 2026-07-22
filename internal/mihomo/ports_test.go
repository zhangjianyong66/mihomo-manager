package mihomo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zhangjianyong66/mihomo-manager/internal/core"
)

func TestParseAndUpdateListenerPorts(t *testing.T) {
	content := []byte(`allow-lan: false
mixed-port: 17890
port: 17892
socks-port: 17891
redir-port: 17893
tproxy-port: 17894
external-controller: http://[::1]:19090
proxies: [{name: demo}]
rules: ["MATCH,DIRECT"]
`)
	ports, err := ParseListenerPorts(content)
	if err != nil {
		t.Fatal(err)
	}
	want := []ListenerPort{
		{Field: PortFieldMixedPort, Host: "127.0.0.1", Port: 17890, Networks: []string{"tcp", "udp"}},
		{Field: PortFieldHTTPPort, Host: "127.0.0.1", Port: 17892, Networks: []string{"tcp"}},
		{Field: PortFieldSocksPort, Host: "127.0.0.1", Port: 17891, Networks: []string{"tcp", "udp"}},
		{Field: PortFieldRedirPort, Host: "127.0.0.1", Port: 17893, Networks: []string{"tcp"}},
		{Field: PortFieldTProxyPort, Host: "127.0.0.1", Port: 17894, Networks: []string{"tcp", "udp"}},
		{Field: PortFieldExternalController, Host: "::1", Port: 19090, Required: true, Networks: []string{"tcp"}},
	}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("ports=%+v want=%+v", ports, want)
	}

	updated, err := UpdateListenerPort(content, PortFieldSocksPort, 0)
	if err != nil {
		t.Fatal(err)
	}
	updated, err = UpdateListenerPort(updated, PortFieldExternalController, 29090)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := yaml.Unmarshal(updated, &config); err != nil {
		t.Fatal(err)
	}
	if config[PortFieldSocksPort] != 0 || config[PortFieldExternalController] != "http://[::1]:29090" {
		t.Fatalf("updated config=%+v", config)
	}

	for _, test := range []struct {
		field string
		port  int
	}{
		{field: "unknown", port: 7890},
		{field: PortFieldMixedPort, port: -1},
		{field: PortFieldExternalController, port: 0},
		{field: PortFieldHTTPPort, port: 17890},
	} {
		if _, err := UpdateListenerPort(content, test.field, test.port); !errors.Is(err, core.ErrInvalidConfig) {
			t.Fatalf("field=%s port=%d err=%v", test.field, test.port, err)
		}
	}
}

func TestPreflightListenerPortsReturnsAllTCPAndUDPConflicts(t *testing.T) {
	tcpListener, udpListener := occupiedTCPAndUDPPort(t)
	defer tcpListener.Close()
	defer udpListener.Close()
	proxyPort := tcpListener.Addr().(*net.TCPAddr).Port
	controller, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	controllerPort := controller.Addr().(*net.TCPAddr).Port

	path := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("mixed-port: %d\nexternal-controller: 127.0.0.1:%d\n", proxyPort, controllerPort)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	err = preflightListenerPorts(path)
	var conflictErr *core.PortConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected port conflict, got %v", err)
	}
	want := []core.PortConflict{
		{Field: PortFieldMixedPort, Network: "tcp", Host: "127.0.0.1", Port: proxyPort},
		{Field: PortFieldMixedPort, Network: "udp", Host: "127.0.0.1", Port: proxyPort},
		{Field: PortFieldExternalController, Network: "tcp", Host: "127.0.0.1", Port: controllerPort},
	}
	if !reflect.DeepEqual(conflictErr.Conflicts, want) {
		t.Fatalf("conflicts=%+v want=%+v", conflictErr.Conflicts, want)
	}
}

func TestAdapterStartPortConflictDoesNotCreateProcess(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	root := t.TempDir()
	marker := filepath.Join(root, "started")
	binary := writeScript(t, root, "mihomo", "#!/bin/sh\nprintf started >\"$MM_STARTED\"\n")
	t.Setenv("MM_STARTED", marker)
	configPath := filepath.Join(root, "config.yaml")
	content := fmt.Sprintf("port: %d\nexternal-controller: 127.0.0.1:19090\n", port)
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := NewAdapter(AdapterOptions{Binary: binary})
	_, err = adapter.Start(context.Background(), core.RuntimeSpec{ConfigPath: configPath, ConfigDir: root})
	if !errors.Is(err, core.ErrPortConflict) {
		t.Fatalf("expected port conflict, got %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("core process was created: %v", err)
	}
}

func occupiedTCPAndUDPPort(t *testing.T) (net.Listener, net.PacketConn) {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := tcpListener.Addr().(*net.TCPAddr).Port
		udpListener, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			return tcpListener, udpListener
		}
		_ = tcpListener.Close()
	}
	t.Fatal("could not reserve one TCP/UDP port")
	return nil, nil
}
