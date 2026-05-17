//go:build windows

// Package fsops provides filesystem operations with OS-appropriate
// implementations. The Windows variants fall back to PowerShell when the Go
// runtime's syscall path is rejected — most commonly RemoveAll on a tree
// that contains an NTFS junction, which the Go runtime can refuse to
// traverse while PowerShell's Remove-Item handles natively. Every fallback
// passes the caller-controlled path through an environment variable and
// -LiteralPath (never through a shell command line), so a path containing
// shell metacharacters cannot be reinterpreted as a command.
package fsops

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// systemExe resolves a Windows system executable to its absolute path under
// %SystemRoot% (a fixed, unwriteable directory) instead of relying on a
// PATH lookup, which a poisoned PATH could hijack (SonarCloud go:S4036).
func systemExe(rel string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, rel)
}

var winPowerShell = systemExe(`System32\WindowsPowerShell\v1.0\powershell.exe`)

// PowerShell invocation flags and the env-var prefix used to pass the
// caller-controlled target path to every fallback (defined once to avoid
// duplicated string literals — SonarCloud go:S1192).
const (
	psNoProfile      = "-NoProfile"
	psNonInteractive = "-NonInteractive"
	psExecPolicy     = "-ExecutionPolicy"
	psCommand        = "-Command"
	fsopsTargetEnv   = "FSOPS_TARGET="
)

// MkdirAll creates a directory path and all missing parents. When the Go
// runtime call fails it is retried component-by-component with os.Mkdir
// (which absorbs benign EEXIST / racing-creator cases); there is no shell
// fallback because no shell `mkdir` can succeed where os.MkdirAll cannot,
// and routing a caller path through cmd.exe would be an injection vector.
func MkdirAll(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err == nil {
		return nil
	}
	if err := mkdirAllComponents(path, perm); err != nil {
		return fmt.Errorf("mkdir %s: %w", path, err)
	}
	return nil
}

// mkdirAllComponents walks the parents of path from the root down, creating
// each missing component with os.Mkdir and tolerating components that
// already exist as directories.
func mkdirAllComponents(path string, perm os.FileMode) error {
	clean := filepath.Clean(path)
	vol := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, vol)
	rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	if rest == "" {
		return nil
	}

	cur := vol + string(os.PathSeparator)
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		if err := os.Mkdir(cur, perm); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if info, statErr := os.Stat(cur); statErr != nil {
			return statErr
		} else if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", cur)
		}
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
		winPowerShell,
		psNoProfile, psNonInteractive, psExecPolicy, "Bypass",
		psCommand,
		"[IO.File]::WriteAllBytes($env:FSOPS_TARGET,[Convert]::FromBase64String($env:FSOPS_B64))",
	)
	cmd.Env = append(os.Environ(), fsopsTargetEnv+path, "FSOPS_B64="+encoded)
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
		winPowerShell,
		psNoProfile, psNonInteractive, psExecPolicy, "Bypass",
		psCommand,
		"if (Test-Path -LiteralPath $env:FSOPS_TARGET) { Remove-Item -LiteralPath $env:FSOPS_TARGET -Force }",
	)
	cmd.Env = append(os.Environ(), fsopsTargetEnv+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("remove %s via powershell: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveAll removes path and its children, falling back to PowerShell
// Remove-Item -Recurse, which traverses junction-containing trees the Go
// runtime can refuse. The path is passed via an environment variable and
// -LiteralPath (never on a shell command line), so shell metacharacters in
// the path cannot be reinterpreted as commands.
func RemoveAll(path string) error {
	if err := os.RemoveAll(path); err == nil || os.IsNotExist(err) {
		return nil
	}
	cmd := exec.Command(
		winPowerShell,
		psNoProfile, psNonInteractive, psExecPolicy, "Bypass",
		psCommand,
		"if (Test-Path -LiteralPath $env:FSOPS_TARGET) { Remove-Item -LiteralPath $env:FSOPS_TARGET -Recurse -Force }",
	)
	cmd.Env = append(os.Environ(), fsopsTargetEnv+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("remove tree %s via powershell: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
