//go:build windows

package credstore

import (
	"encoding/hex"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         uint32  = 1
	credPersistLocalMachine uint32  = 2
	errNotFound             uintptr = 1168 // ERROR_NOT_FOUND
)

var (
	modadvapi32    = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW  = modadvapi32.NewProc("CredReadW")
	procCredWriteW = modadvapi32.NewProc("CredWriteW")
	procCredFree   = modadvapi32.NewProc("CredFree")
)

// windowsCredential mirrors the CREDENTIAL struct from wincred.h.
// Go's alignment rules match MSVC on 64-bit Windows: the four implicit padding
// bytes between CredentialBlobSize (uint32) and CredentialBlob (*byte) are
// inserted automatically because *byte requires 8-byte alignment on amd64.
type windowsCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type windowsKeyring struct{}

// NewOSKeyring returns a Keyring backed by the Windows Credential Manager.
func NewOSKeyring() Keyring { return windowsKeyring{} }

func (windowsKeyring) Get(key string) ([]byte, error) {
	targetName, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return nil, fmt.Errorf("%s: encode target name: %w", errPrefix, err)
	}
	var credPtr *windowsCredential
	ret, _, lastErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(targetName)),
		uintptr(credTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&credPtr)),
	)
	if ret == 0 {
		if errno, ok := lastErr.(windows.Errno); ok && uintptr(errno) == errNotFound {
			return nil, ErrKeyNotFound
		}
		return nil, fmt.Errorf("%s: read credential: %w", errPrefix, lastErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credPtr)))
	if credPtr.CredentialBlobSize == 0 {
		return nil, ErrKeyNotFound
	}
	// Copy the blob before CredFree is called at function exit.
	blobBytes := unsafe.Slice(credPtr.CredentialBlob, credPtr.CredentialBlobSize)
	b, decErr := hex.DecodeString(string(blobBytes))
	if decErr != nil {
		return nil, fmt.Errorf("%s: decode credential blob: %w", errPrefix, decErr)
	}
	return b, nil
}

func (windowsKeyring) Set(key string, secret []byte) error {
	targetName, err := windows.UTF16PtrFromString(key)
	if err != nil {
		return fmt.Errorf("%s: encode target name: %w", errPrefix, err)
	}
	encoded := []byte(hex.EncodeToString(secret))
	cred := windowsCredential{
		Type:               credTypeGeneric,
		TargetName:         targetName,
		CredentialBlobSize: uint32(len(encoded)),
		CredentialBlob:     &encoded[0],
		Persist:            credPersistLocalMachine,
	}
	ret, _, lastErr := procCredWriteW.Call(
		uintptr(unsafe.Pointer(&cred)),
		0,
	)
	if ret == 0 {
		return fmt.Errorf("%s: write credential: %w", errPrefix, lastErr)
	}
	return nil
}
