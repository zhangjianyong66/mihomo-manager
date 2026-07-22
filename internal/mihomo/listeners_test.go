package mihomo

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadProxyListenersNormalizesLocalEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("bind-address: '*'\nmixed-port: 7890\nport: 7892\nsocks-port: 7891\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadProxyListeners(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []ProxyListener{{"mixed", "127.0.0.1", 7890}, {"http", "127.0.0.1", 7892}, {"socks", "127.0.0.1", 7891}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("listeners=%+v want=%+v", got, want)
	}
}
