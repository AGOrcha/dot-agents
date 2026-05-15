//go:build windows

package links

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// createLink creates a managed link at linkPath pointing to target using a
// Windows-native mechanism that needs no SeCreateSymbolicLinkPrivilege
// (works without Developer Mode or admin): a directory junction for
// directory targets, a hard link for file targets. This is the
// pnpm-analogous model.
//
// There is deliberately no copy fallback. A copied file is not a managed
// reference — it would not resolve to the canonical inode, so the link
// contract (IsManagedLink / CollectBrokenUserLinks / doctor repair) would
// silently treat it as unmanaged. Failing loudly is correct here.
func createLink(target, linkPath string) error {
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("stat link target %s: %w", target, err)
	}
	if info.IsDir() {
		return createJunction(linkPath, target)
	}
	if err := os.Link(target, linkPath); err != nil {
		return fmt.Errorf("hardlink %s -> %s: %w", linkPath, target, err)
	}
	return nil
}

// createJunction creates an NTFS directory junction at linkPath pointing
// to target. `mklink /J` requires an absolute target and, unlike a true
// symlink, needs no special privilege. Go's os.Readlink and os.Lstat
// resolve junctions (IO_REPARSE_TAG_MOUNT_POINT) as symlinks, so the
// existing readlink-based contract checks see a junction as a managed link.
func createJunction(linkPath, target string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve junction target %s: %w", target, err)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", linkPath, absTarget)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mklink /J %s -> %s: %s", linkPath, absTarget, strings.TrimSpace(string(out)))
	}
	return nil
}
