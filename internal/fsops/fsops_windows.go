//go:build windows

// Package fsops provides filesystem operations with OS-appropriate
// implementations. The Windows variants fall back to cmd/PowerShell when
// the Go runtime's syscall path is rejected — most commonly RemoveAll on a
// tree that contains an NTFS junction, which the Go runtime can refuse to
// traverse while `rmdir /s /q` handles it natively.
package fsops

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MkdirAll creates a directory path and all missing parents, falling back
// to `cmd /c mkdir` (which also creates intermediate directories) when the
// Go runtime call fails.
func MkdirAll(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err == nil {
		return nil
	}
	cmd := exec.Command("cmd", "/c", "mkdir", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkdir %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// WriteFile writes data to path, falling back to a PowerShell
// WriteAllBytes (data passed base64 via env to avoid quoting issues) when
// the Go runtime call fails.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err == nil {
		return nil
	}
	if err := MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command",
		"[IO.File]::WriteAllBytes($env:FSOPS_TARGET,[Convert]::FromBase64String($env:FSOPS_B64))",
	)
	cmd.Env = append(os.Environ(), "FSOPS_TARGET="+path, "FSOPS_B64="+encoded)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write %s via powershell: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Remove removes a single file, falling back to PowerShell Remove-Item.
// A missing path is not an error (parity with os.Remove on IsNotExist).
func Remove(path string) error {
	if err := os.Remove(path); err == nil || os.IsNotExist(err) {
		return nil
	}
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command",
		"if (Test-Path -LiteralPath $env:FSOPS_TARGET) { Remove-Item -LiteralPath $env:FSOPS_TARGET -Force }",
	)
	cmd.Env = append(os.Environ(), "FSOPS_TARGET="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("remove %s via powershell: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveAll removes path and its children, falling back to `rmdir /s /q`
// which traverses junction-containing trees the Go runtime can refuse.
func RemoveAll(path string) error {
	if err := os.RemoveAll(path); err == nil || os.IsNotExist(err) {
		return nil
	}
	cmd := exec.Command("cmd", "/c", "rmdir", "/s", "/q", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("remove tree %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
