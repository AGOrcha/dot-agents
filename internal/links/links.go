package links

import (
	"fmt"
	"os"
	"path/filepath"

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
		if err := fsops.RemoveAll(linkPath); err != nil {
			return fmt.Errorf("removing old symlink %s: %w", linkPath, err)
		}
	} else if !os.IsNotExist(err) {
		// Not a symlink - check if regular file/dir
		if _, statErr := os.Lstat(linkPath); statErr == nil {
			if err := fsops.RemoveAll(linkPath); err != nil {
				return fmt.Errorf("removing existing file %s: %w", linkPath, err)
			}
		}
	}

	if err := fsops.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", linkPath, err)
	}
	return createLink(target, linkPath)
}

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

// IsSymlinkTo returns true if linkPath is a symlink that resolves to target.
func IsSymlinkTo(linkPath, target string) bool {
	dest, err := os.Readlink(linkPath)
	if err != nil {
		return false
	}
	return dest == target
}

// IsSymlinkUnder returns true if linkPath is a symlink whose target starts with prefix.
func IsSymlinkUnder(linkPath, prefix string) bool {
	dest, err := os.Readlink(linkPath)
	if err != nil {
		return false
	}
	// Compare with both raw value and expanded
	if len(dest) >= len(prefix) && dest[:len(prefix)] == prefix {
		return true
	}
	return false
}

// RemoveIfSymlinkUnder removes linkPath if it is a symlink whose target starts with prefix.
func RemoveIfSymlinkUnder(linkPath, prefix string) error {
	if IsSymlinkUnder(linkPath, prefix) {
		return os.Remove(linkPath)
	}
	return nil
}

// IsDirEntry reports whether the entry at path is a directory, following symlinks.
// Use this instead of e.IsDir() when entries may be symlinks to directories.
func IsDirEntry(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
