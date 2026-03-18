package links

import (
	"fmt"
	"os"
	"path/filepath"
)

// Symlink creates or updates a symlink at linkPath pointing to target.
// It is idempotent: if the correct symlink already exists, it is a no-op.
func Symlink(target, linkPath string) error {
	existing, err := os.Readlink(linkPath)
	if err == nil {
		if existing == target {
			return nil // already correct
		}
		// points elsewhere - remove and recreate
		if err := os.Remove(linkPath); err != nil {
			return fmt.Errorf("removing old symlink %s: %w", linkPath, err)
		}
	} else if !os.IsNotExist(err) {
		// Not a symlink - check if regular file/dir
		if _, statErr := os.Lstat(linkPath); statErr == nil {
			if err := os.Remove(linkPath); err != nil {
				return fmt.Errorf("removing existing file %s: %w", linkPath, err)
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", linkPath, err)
	}
	return os.Symlink(target, linkPath)
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
