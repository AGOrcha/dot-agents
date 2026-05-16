package links

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/fsops"
)

// Symlink creates or updates a managed link at linkPath pointing to target.
// It is idempotent: if the correct link already exists, it is a no-op.
//
// "Managed link" is OS-specific: a POSIX symlink, or on Windows a directory
// junction (dirs) / hard link (files). The hard-link case has no reparse
// point, so idempotency for it is detected by inode identity (os.SameFile)
// rather than os.Readlink.
func Symlink(target, linkPath string) error {
	if same, err := pathsResolveToSameFile(target, linkPath); err == nil && same {
		return nil // already a hard link / junction to the same canonical file
	}

	existing, err := os.Readlink(linkPath)
	if err == nil {
		if existing == target {
			return nil // already correct
		}
		// points elsewhere - remove and recreate
		if err := fsopsRemoveAll(linkPath); err != nil {
			return fmt.Errorf("removing old symlink %s: %w", linkPath, err)
		}
	} else if !os.IsNotExist(err) {
		// Not a symlink - check if regular file/dir
		if _, statErr := os.Lstat(linkPath); statErr == nil {
			if err := fsopsRemoveAll(linkPath); err != nil {
				return fmt.Errorf("removing existing file %s: %w", linkPath, err)
			}
		}
	}

	if err := fsops.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", linkPath, err)
	}
	return createLink(target, linkPath)
}

// fsopsRemoveAll is a seam over fsops.RemoveAll so tests can fault-inject
// the otherwise-unreachable "failed to remove a stale/occupying entry"
// error returns in Symlink. Production always uses the real implementation.
var fsopsRemoveAll = fsops.RemoveAll

// pathsResolveToSameFile reports whether target and linkPath are the same
// underlying file (same inode / file index). True when linkPath is a hard
// link to target, or a junction/symlink that resolves to it.
func pathsResolveToSameFile(target, linkPath string) (bool, error) {
	targetInfo, err := os.Stat(target)
	if err != nil {
		return false, err
	}
	linkInfo, err := os.Stat(linkPath)
	if err != nil {
		return false, err
	}
	return os.SameFile(targetInfo, linkInfo), nil
}

// Hardlink creates a hard link at dstPath pointing to the same inode as srcPath.
// It is idempotent: if the dst is already hard-linked to src, it is a no-op.
func Hardlink(srcPath, dstPath string) error {
	if already, err := AreHardlinked(srcPath, dstPath); err == nil && already {
		return nil
	}

	// Remove existing dst if present
	if _, err := os.Lstat(dstPath); err == nil {
		if err := os.Remove(dstPath); err != nil {
			return fmt.Errorf("removing existing %s: %w", dstPath, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", dstPath, err)
	}
	return os.Link(srcPath, dstPath)
}

// FindFile tries each extension suffix in order and returns the first match,
// or empty string if none found.
func FindFile(basePath string, exts []string) string {
	for _, ext := range exts {
		candidate := basePath + "." + ext
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// ManagedLinkTarget returns the path linkPath references, and true, when
// linkPath is a *resolvable* managed link — a POSIX symlink or a Windows
// junction (Go's os.Readlink resolves IO_REPARSE_TAG_MOUNT_POINT). A hard
// link has no reparse point and therefore no resolvable target; use
// IsManagedLink with a known target for the hard-link case.
func ManagedLinkTarget(linkPath string) (string, bool) {
	dest, err := os.Readlink(linkPath)
	if err != nil {
		return "", false
	}
	return dest, true
}

// IsManagedLink reports whether linkPath is a managed reference to target.
// This is the single cross-platform predicate the link contract is built
// on:
//
//   - POSIX:   a symlink whose target == target.
//   - Windows: a symlink or junction resolving to target (junction targets
//     are absolute, so an absolute/clean-normalized compare is also tried),
//     OR a hard link to the same canonical file (os.SameFile).
//
// On POSIX the hard-link branch is still honored (a hardlinked managed file
// is a valid managed reference there too), so behavior is uniform.
func IsManagedLink(linkPath, target string) bool {
	if dest, ok := ManagedLinkTarget(linkPath); ok {
		if dest == target {
			return true
		}
		// Junctions store an absolute, cleaned target; tolerate that.
		if absTarget, err := filepath.Abs(target); err == nil {
			if dest == absTarget || filepath.Clean(dest) == filepath.Clean(absTarget) {
				return true
			}
		}
	}
	// Hard link: no reparse point, identity is a shared inode / file index
	// across two *distinct* directory entries. The degenerate case where
	// linkPath and target are the same path is a plain file, not a link.
	if !samePath(linkPath, target) {
		if same, err := pathsResolveToSameFile(target, linkPath); err == nil && same {
			return true
		}
	}
	return false
}

// samePath reports whether two paths denote the same location after
// absolute+clean normalization (so a/./b and a/b compare equal).
func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}

// IsManagedLinkUnder reports whether linkPath is a managed link whose
// resolved target lies under prefix. Only resolvable links (symlink /
// junction) can answer this; a hard link has no target path to test
// against prefix, so it is reported false (parity with the prior
// symlink-only behavior on POSIX).
func IsManagedLinkUnder(linkPath, prefix string) bool {
	dest, ok := ManagedLinkTarget(linkPath)
	if !ok {
		return false
	}
	if strings.HasPrefix(dest, prefix) {
		return true
	}
	// Junction targets are absolute; also test the absolute prefix form.
	if absPrefix, err := filepath.Abs(prefix); err == nil {
		if strings.HasPrefix(dest, absPrefix) {
			return true
		}
	}
	return false
}

// IsSymlinkTo is retained for callers/tests that specifically assert the
// POSIX-symlink contract; it now delegates to the OS-aware predicate.
func IsSymlinkTo(linkPath, target string) bool {
	return IsManagedLink(linkPath, target)
}

// IsSymlinkUnder delegates to the OS-aware predicate.
func IsSymlinkUnder(linkPath, prefix string) bool {
	return IsManagedLinkUnder(linkPath, prefix)
}

// RemoveIfSymlinkUnder removes linkPath if it is a managed link whose
// resolved target starts with prefix.
func RemoveIfSymlinkUnder(linkPath, prefix string) error {
	if IsManagedLinkUnder(linkPath, prefix) {
		return fsops.RemoveAll(linkPath)
	}
	return nil
}

// IsDirEntry reports whether the entry at path is a directory, following symlinks.
// Use this instead of e.IsDir() when entries may be symlinks to directories.
func IsDirEntry(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
