//go:build windows

package fsops

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func MkdirAll(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err == nil {
		return err
	}
	cmd := exec.Command("cmd", "/c", "mkdir", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mkdir %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := os.WriteFile(path, data, perm); err == nil {
		return err
	}
	if err := MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		"[IO.File]::WriteAllBytes($env:FSOPS_TARGET,[Convert]::FromBase64String($env:FSOPS_B64))",
	)
	cmd.Env = append(os.Environ(), "FSOPS_TARGET="+path, "FSOPS_B64="+encoded)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("write %s via powershell: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Remove(path string) error {
	if err := os.Remove(path); err == nil || os.IsNotExist(err) {
		return err
	}
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		"if (Test-Path -LiteralPath $env:FSOPS_TARGET) { Remove-Item -LiteralPath $env:FSOPS_TARGET -Force }",
	)
	cmd.Env = append(os.Environ(), "FSOPS_TARGET="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove %s via powershell: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func RemoveAll(path string) error {
	if err := os.RemoveAll(path); err == nil || os.IsNotExist(err) {
		return err
	}
	cmd := exec.Command("cmd", "/c", "rmdir", "/s", "/q", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove tree %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}