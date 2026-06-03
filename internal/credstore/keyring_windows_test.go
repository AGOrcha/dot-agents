//go:build windows

package credstore

import (
	"bytes"
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procCredDeleteW = modadvapi32.NewProc("CredDeleteW")

func deleteWindowsTestCredential(t *testing.T, name string) {
	t.Helper()
	target, _ := windows.UTF16PtrFromString(name)
	procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), uintptr(credTypeGeneric), 0)
}

func TestNewOSKeyring_NonNil_Windows(t *testing.T) {
	if NewOSKeyring() == nil {
		t.Fatal("NewOSKeyring returned nil on Windows")
	}
}

func TestWindowsKeyring_Get_NotFound(t *testing.T) {
	_, err := windowsKeyring{}.Get("da-credstore-test-notfound-" + t.Name())
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("want ErrKeyNotFound, got %v", err)
	}
}

func TestWindowsKeyringRoundTrip(t *testing.T) {
	const svc = "da-credstore-roundtrip-test"
	want := []byte{0xca, 0xfe, 0xba, 0xbe, 0x01, 0x02, 0x03, 0x04}
	k := windowsKeyring{}
	t.Cleanup(func() { deleteWindowsTestCredential(t, svc) })
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

func TestWindowsCredentialStructSize(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("size assertion only valid on 64-bit")
	}
	if got, want := unsafe.Sizeof(windowsCredential{}), uintptr(80); got != want {
		t.Fatalf("windowsCredential size = %d want %d: struct layout may diverge from Windows CREDENTIAL", got, want)
	}
}
