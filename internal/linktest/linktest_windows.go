//go:build windows

package linktest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// createManagedLink mirrors the production Windows link model: a directory
// junction for directory targets, a hard link for file targets. Neither
// needs SeCreateSymbolicLinkPrivilege.
func createManagedLink(target, link string) error {
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat link target %s: %w", target, err)
	}
	if info.IsDir() {
		return junction(link, target)
	}
	if err := os.Link(target, link); err != nil {
		return fmt.Errorf("hardlink %s -> %s: %w", link, target, err)
	}
	return nil
}

// createDanglingLink produces a broken reparse point: create a real
// directory, junction to it, then remove the directory. os.Readlink on the
// junction still returns missingTarget, but os.Stat of it now fails — the
// same shape the broken-link detectors expect. A hard link cannot dangle
// (its target must exist), so a junction is the only viable analogue.
func createDanglingLink(link, missingTarget string) error {
	if err := os.MkdirAll(missingTarget, 0o755); err != nil {
		return fmt.Errorf("seed dangling target %s: %w", missingTarget, err)
	}
	if err := junction(link, missingTarget); err != nil {
		return err
	}
	if err := os.RemoveAll(missingTarget); err != nil {
		return fmt.Errorf("remove dangling target %s: %w", missingTarget, err)
	}
	return nil
}

func junction(link, target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve junction target %s: %w", target, err)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, absTarget)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mklink /J %s -> %s: %s", link, absTarget, strings.TrimSpace(string(out)))
	}
	return nil
}
