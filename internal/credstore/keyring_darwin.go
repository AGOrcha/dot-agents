//go:build darwin

package credstore

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// securityErrItemNotFound is the exit code security(1) returns when the
// requested keychain item does not exist (errSecItemNotFound = -25300 = 0x44).
const securityErrItemNotFound = 44

// securityBin is the absolute path to the macOS security(1) CLI tool.
// Using a fixed path avoids PATH-lookup ambiguity (go:S4036). It is a
// package-level variable so tests can substitute a fake binary without
// PATH manipulation.
var securityBin = "/usr/bin/security"

// darwinKeyring implements Keyring using the macOS security(1) CLI tool.
// The binary seed blob is hex-encoded for keychain storage. The write path
// feeds the hex value through the command's stdin (not argv) so the seed
// never appears in the process argument list where other local processes
// could observe it via ps or the procfs equivalent.
type darwinKeyring struct{}

// NewOSKeyring returns a Keyring backed by the macOS Keychain via security(1).
func NewOSKeyring() Keyring { return darwinKeyring{} }

func (darwinKeyring) Get(key string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(securityBin,
		"find-generic-password",
		"-s", key,
		"-a", key,
		"-w",
	)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == securityErrItemNotFound {
			return nil, ErrKeyNotFound
		}
		// Locked keychain, permission denied, missing binary, etc. — hard error
		// so callers do not misinterpret a transient operational failure as a
		// first-run condition and overwrite a valid keyring seed.
		return nil, fmt.Errorf("%s: read keychain entry: %w (stderr: %s)", errPrefix, err, strings.TrimSpace(stderr.String()))
	}
	encoded := strings.TrimRight(string(out), "\n")
	b, decErr := hex.DecodeString(encoded)
	if decErr != nil {
		return nil, fmt.Errorf("%s: decode keychain entry: %w", errPrefix, decErr)
	}
	return b, nil
}

func (darwinKeyring) Set(key string, secret []byte) error {
	// Pass the hex-encoded secret through stdin rather than -w <value> so the
	// seed never appears in the process argument list.
	encoded := hex.EncodeToString(secret) + "\n"
	var stderr bytes.Buffer
	cmd := exec.Command(securityBin,
		"add-generic-password",
		"-U",
		"-s", key,
		"-a", key,
	)
	cmd.Stdin = strings.NewReader(encoded)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: write keychain entry: %w (stderr: %s)", errPrefix, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
