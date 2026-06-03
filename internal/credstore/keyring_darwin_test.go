//go:build darwin

package credstore

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeFakeSecurity writes a shell script named "security" to a temp dir and
// returns its full path. Tests point securityBin at this path for the duration
// of the test, exercising darwinKeyring without touching the real Keychain.
func writeFakeSecurity(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "security")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatalf("write fake security: %v", err)
	}
	return bin
}

// useFakeSecurity swaps securityBin for the duration of the test.
func useFakeSecurity(t *testing.T, bin string) {
	t.Helper()
	old := securityBin
	securityBin = bin
	t.Cleanup(func() { securityBin = old })
}

func TestNewOSKeyring_NonNil(t *testing.T) {
	if NewOSKeyring() == nil {
		t.Fatal("NewOSKeyring returned nil on darwin")
	}
}

func TestDarwinKeyring_Get_Success(t *testing.T) {
	want := []byte{0xca, 0xfe, 0xba, 0xbe}
	hexVal := hex.EncodeToString(want)
	useFakeSecurity(t, writeFakeSecurity(t, "echo '"+hexVal+"'"))

	got, err := darwinKeyring{}.Get("svc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %x, want %x", got, want)
	}
}

func TestDarwinKeyring_Get_NotFound(t *testing.T) {
	useFakeSecurity(t, writeFakeSecurity(t, "exit 44"))

	_, err := darwinKeyring{}.Get("svc")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("want ErrKeyNotFound, got: %v", err)
	}
}

func TestDarwinKeyring_Get_HardError(t *testing.T) {
	useFakeSecurity(t, writeFakeSecurity(t, "echo 'keychain locked' >&2; exit 1"))

	_, err := darwinKeyring{}.Get("svc")
	if err == nil {
		t.Fatal("expected hard error, got nil")
	}
	if errors.Is(err, ErrKeyNotFound) {
		t.Fatal("should not be ErrKeyNotFound for non-44 exit")
	}
}

func TestDarwinKeyring_Get_BadHex(t *testing.T) {
	useFakeSecurity(t, writeFakeSecurity(t, "echo 'not-valid-hex'"))

	_, err := darwinKeyring{}.Get("svc")
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestDarwinKeyring_Set_Success(t *testing.T) {
	useFakeSecurity(t, writeFakeSecurity(t, "exit 0"))

	k := darwinKeyring{}
	if err := k.Set("svc", []byte{0x01, 0x02}); err != nil {
		t.Fatalf("Set: %v", err)
	}
}

func TestDarwinKeyring_Set_Error(t *testing.T) {
	useFakeSecurity(t, writeFakeSecurity(t, "echo 'write failed' >&2; exit 1"))

	k := darwinKeyring{}
	if err := k.Set("svc", []byte{0x01, 0x02}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestDarwinKeyringRoundTrip writes a real entry to the macOS Keychain via the
// production security(1) binary, reads it back, and verifies the value. It
// validates the full invocation — stdin-passing for Set, -w stdout for Get —
// using the same binary and args the production code uses.
func TestDarwinKeyringRoundTrip(t *testing.T) {
	const svc = "da-credstore-roundtrip-test"
	want := []byte{0xca, 0xfe, 0xba, 0xbe, 0x01, 0x02, 0x03, 0x04}
	k := darwinKeyring{}
	t.Cleanup(func() {
		exec.Command(securityBin,
			"delete-generic-password",
			"-s", svc,
			"-a", svc,
		).Run() //nolint:errcheck — cleanup best-effort
	})
	if err := k.Set(svc, want); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := k.Get(svc)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("round-trip: got %x want %x", got, want)
	}
}
