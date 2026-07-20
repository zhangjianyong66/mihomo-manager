//go:build linux

package platform

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePrivateDir_SecuresAndRejectsSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("unexpected mode: %v %v", info.Mode(), err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateDir(filepath.Join(link, "child")); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("expected unsafe path, got %v", err)
	}
}

func TestAcquireFileLock_ExclusiveAndNoFollow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "daemon.lock")
	lock, err := AcquireFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if _, err := AcquireFileLock(path); !errors.Is(err, ErrAlreadyLocked) {
		t.Fatalf("expected lock conflict, got %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected lock mode: %v %v", info.Mode(), err)
	}
	link := filepath.Join(t.TempDir(), "link.lock")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireFileLock(link); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestListenUnix_PermissionsPeerAndUnsafeTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "mm.sock")
	listener, err := ListenUnix(path, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected socket mode: %v %v", info.Mode(), err)
	}
	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
		accepted <- acceptErr
	}()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}

	bad := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(bad, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenUnix(bad, uint32(os.Geteuid())); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("expected unsafe target, got %v", err)
	}
}

func TestListenUnix_RejectsUnexpectedPeerUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "wrong-peer.sock")
	listener, err := ListenUnix(path, uint32(os.Geteuid()+1))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan error, 1)
	go func() {
		_, err := listener.Accept()
		accepted <- err
	}()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1)
	_, readErr := conn.Read(buffer)
	_ = conn.Close()
	if readErr == nil {
		t.Fatal("unexpected peer connection remained open")
	}
	_ = listener.Close()
	<-accepted
}
