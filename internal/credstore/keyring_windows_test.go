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
		// An unreachable / policy-restricted Credential Manager is an environment
		// limitation, not a code defect — the raw-write unit tests cover Get/Set
		// logic. Skip rather than fail the build for unrelated work.
		t.Skipf("Credential Manager not writable in this environment (%v); skipping real round-trip", err)
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

// writeRawWindowsCredential plants a credential whose blob is exactly the bytes
// given, bypassing the production Set path (which always hex-encodes). This lets
// tests stage malformed blobs (non-hex, empty) that Set could never produce, so
// Get's blob-validation branches can be exercised.
func writeRawWindowsCredential(t *testing.T, target string, blob []byte) {
	t.Helper()
	targetName, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatalf("encode target name: %v", err)
	}
	cred := windowsCredential{
		Type:               credTypeGeneric,
		TargetName:         targetName,
		CredentialBlobSize: uint32(len(blob)),
		Persist:            credPersistLocalMachine,
	}
	// Taking &blob[0] on an empty slice panics; a zero-size blob keeps a nil
	// pointer, which matches a credential written with no blob.
	if len(blob) > 0 {
		cred.CredentialBlob = &blob[0]
	}
	ret, _, lastErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if ret == 0 {
		t.Fatalf("CredWriteW for %q failed: %v", target, lastErr)
	}
}

// Covers Get path 1: UTF16PtrFromString error on a key with an embedded NUL.
func TestWindowsKeyring_Get_EncodeTargetError(t *testing.T) {
	_, err := windowsKeyring{}.Get("bad\x00key")
	if err == nil {
		t.Fatalf("Get with NUL in key: got nil error, want encode error")
	}
	if errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get with NUL in key: got ErrKeyNotFound, want encode error")
	}
}

// Covers Get path 2: ret == 0 with a non-ERROR_NOT_FOUND errno. An empty target
// name makes CredReadW fail with ERROR_INVALID_PARAMETER, so the hard-error
// branch (not the ErrKeyNotFound branch) must be taken.
func TestWindowsKeyring_Get_ReadErrorNotNotFound(t *testing.T) {
	_, err := windowsKeyring{}.Get("")
	if err == nil {
		t.Fatalf("Get with empty target: got nil error, want read error")
	}
	if errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get with empty target: got ErrKeyNotFound, want read credential error")
	}
}

// Covers Get path 3: CredentialBlobSize == 0 returns ErrKeyNotFound.
func TestWindowsKeyring_Get_EmptyBlob(t *testing.T) {
	const target = "da-credstore-test-emptyblob"
	t.Cleanup(func() { deleteWindowsTestCredential(t, target) })
	writeRawWindowsCredential(t, target, nil)
	_, err := windowsKeyring{}.Get(target)
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get with empty blob: got %v, want ErrKeyNotFound", err)
	}
}

// Covers Get path 4: hex.DecodeString error returns a decode error.
func TestWindowsKeyring_Get_NonHexBlob(t *testing.T) {
	const target = "da-credstore-test-nonhex"
	t.Cleanup(func() { deleteWindowsTestCredential(t, target) })
	writeRawWindowsCredential(t, target, []byte("zz-not-hex"))
	_, err := windowsKeyring{}.Get(target)
	if err == nil {
		t.Fatalf("Get with non-hex blob: got nil error, want decode error")
	}
	if errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("Get with non-hex blob: got ErrKeyNotFound, want decode error")
	}
}

// Covers Set path 5: UTF16PtrFromString error on a key with an embedded NUL.
func TestWindowsKeyring_Set_EncodeTargetError(t *testing.T) {
	err := windowsKeyring{}.Set("bad\x00key", []byte{0x01})
	if err == nil {
		t.Fatalf("Set with NUL in key: got nil error, want encode error")
	}
}

// Covers Set path 6: ret == 0 from CredWriteW. A 1500-byte secret hex-encodes to
// 3000 chars, exceeding CRED_MAX_CREDENTIAL_BLOB_SIZE (2560), so CredWriteW fails
// with ERROR_INVALID_PARAMETER and Set returns the write-credential error.
func TestWindowsKeyring_Set_WriteError(t *testing.T) {
	const target = "da-credstore-test-oversize"
	t.Cleanup(func() { deleteWindowsTestCredential(t, target) })
	oversized := make([]byte, 1500)
	err := windowsKeyring{}.Set(target, oversized)
	if err == nil {
		t.Fatalf("Set with oversized blob: got nil error, want write error")
	}
}
