//go:build linux || darwin

package http

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unixPair returns both ends of a live Unix-domain socket connection.
func unixPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.sock")
	ln, err := net.Listen(unixNetwork, path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("accept: %v", err)
			close(accepted)
			return
		}
		accepted <- conn
	}()
	client, err = net.Dial(unixNetwork, path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	server, ok := <-accepted
	if !ok {
		t.Fatal("no accepted conn")
	}
	t.Cleanup(func() { server.Close() })
	return client, server
}

// TestPeerUIDReal proves the kernel attests the connecting peer: over a real
// UDS connection the reported uid is this process's own effective uid.
func TestPeerUIDReal(t *testing.T) {
	_, server := unixPair(t)
	uid, err := defaultPeerUID(server)
	if err != nil {
		t.Fatalf("defaultPeerUID: %v", err)
	}
	if want := os.Geteuid(); int(uid) != want {
		t.Fatalf("peer uid = %d, want own euid %d", uid, want)
	}
}

// TestPeerUIDNotUnix rejects a connection that is not a Unix socket — there
// is no kernel credential to read.
func TestPeerUIDNotUnix(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if _, err := peerUIDFromConn(a, getsockoptPeerUID); err == nil ||
		!strings.Contains(err.Error(), "requires a unix socket") {
		t.Fatalf("peerUIDFromConn(net.Pipe) = %v, want unix-socket requirement", err)
	}
}

// TestPeerUIDGetErr surfaces a getsockopt failure as a wrapped credential
// error (the injected getUID stands in for a platform refusal).
func TestPeerUIDGetErr(t *testing.T) {
	_, server := unixPair(t)
	boom := errors.New("getsockopt boom")
	_, err := peerUIDFromConn(server, func(int) (uint32, error) { return 0, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("peerUIDFromConn = %v, want wrapped %v", err, boom)
	}
}

// TestPeerUIDClosedConn covers the raw-control failure arm: a closed
// connection has no descriptor to interrogate.
func TestPeerUIDClosedConn(t *testing.T) {
	_, server := unixPair(t)
	server.Close()
	if _, err := defaultPeerUID(server); err == nil {
		t.Fatal("defaultPeerUID on a closed conn = nil error, want failure")
	}
}

// TestGetsockoptNotSocket proves the raw reader itself fails cleanly on a
// descriptor that is not a socket (ENOTSOCK), covering its error arm.
func TestGetsockoptNotSocket(t *testing.T) {
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := getsockoptPeerUID(int(f.Fd())); err == nil {
		t.Fatal("getsockoptPeerUID on a non-socket fd = nil error, want ENOTSOCK")
	}
}
