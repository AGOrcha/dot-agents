package links

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/fsops"
)

// ErrUnmanagedTarget is returned by Symlink when linkPath is occupied by an
// entry dot-agents does not own (a regular file, or a non-empty directory)
// rather than a managed link or an idempotent re-link of the same canonical
// file. Callers can errors.Is() this to distinguish "user data is in the
// way" from an I/O failure, and decide to surface a conflict or route
// through SymlinkReplacing with an explicit backup.
var ErrUnmanagedTarget = errors.New("link path occupied by an unmanaged entry")

// Symlink creates or updates a managed link at linkPath pointing to target.
// It is idempotent for managed state (correct link / same canonical inode /
// stale managed link / empty squat dir) but, by contract, NEVER destroys
// unmanaged user data: an existing regular file or non-empty directory that
// is not a managed link yields ErrUnmanagedTarget and is left intact. A
// caller that legitimately intends to replace such an entry must go through
// SymlinkReplacing with an explicit backup.
//
// "Managed link" is OS-specific: a POSIX symlink, or on Windows a directory
// junction (dirs) / hard link (files). The hard-link case has no reparse
// point, so idempotency for it is detected by inode identity (os.SameFile)
// rather than os.Readlink.
func Symlink(target, linkPath string) error {
	return symlinkWithPolicy(target, linkPath, nil)
}

// SymlinkReplacing behaves like Symlink but, when linkPath holds an
// unmanaged regular file or non-empty directory, first invokes backup(path)
// (a caller-supplied preservation step — e.g. mirror/backup at the commands
// layer; links must not depend on it) and only then removes the entry and
// installs the managed link. If backup returns an error the original entry
// is left untouched and the error is propagated (no data loss). A nil
// backup is identical to Symlink (refuse).
func SymlinkReplacing(target, linkPath string, backup func(path string) error) error {
	return symlinkWithPolicy(target, linkPath, backup)
}

func symlinkWithPolicy(target, linkPath string, backup func(path string) error) error {
	if same, err := pathsResolveToSameFile(target, linkPath); err == nil && same {
		return nil // already a hard link / junction to the same canonical file
	}

	existing, err := os.Readlink(linkPath)
	if err == nil {
		if existing == target {
			return nil // already correct
		}
		// Stale MANAGED link (symlink/junction) pointing elsewhere:
		// removing it deletes only the link, never a target's contents.
		if err := fsopsRemoveAll(linkPath); err != nil {
			return fmt.Errorf("removing old symlink %s: %w", linkPath, err)
		}
	} else if !os.IsNotExist(err) {
		// Not a symlink/junction: a regular file or a real directory.
		if info, statErr := os.Lstat(linkPath); statErr == nil {
			unmanaged := !info.IsDir() // any non-managed regular file
			if info.IsDir() {
				if entries, derr := os.ReadDir(linkPath); derr == nil && len(entries) > 0 {
					unmanaged = true // non-empty dir may hold local work
				}
			}
			if unmanaged {
				if backup == nil {
					return fmt.Errorf("%w: %s is a regular file or non-empty directory, not a managed link — back it up/import or use the explicit replace path", ErrUnmanagedTarget, linkPath)
				}
				if bErr := backup(linkPath); bErr != nil {
					return fmt.Errorf("backing up unmanaged entry %s before replace: %w", linkPath, bErr)
				}
				if rmErr := fsopsRemoveAll(linkPath); rmErr != nil {
					return fmt.Errorf("removing backed-up entry %s: %w", linkPath, rmErr)
				}
			} else if rmErr := fsopsRemove(linkPath); rmErr != nil {
				// Empty dir: single-entry removal, no data, idempotent.
				return fmt.Errorf("removing existing entry %s: %w", linkPath, rmErr)
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

// fsopsRemove is the single-entry removal seam (regular file or EMPTY
// dir; never recursive) used by Symlink's non-symlink replace branch.
var fsopsRemove = fsops.Remove

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
// resolved target starts with prefix. Resolvable links (POSIX symlink /
// Windows junction) are handled here; a Windows hard-linked *file* has no
// resolvable target, so callers that manage file links and know the
// candidate canonical sources should additionally use
// RemoveIfHardlinkedToAny.
func RemoveIfSymlinkUnder(linkPath, prefix string) error {
	if IsManagedLinkUnder(linkPath, prefix) {
		return fsops.RemoveAll(linkPath)
	}
	return nil
}

// RemoveIfHardlinkedToAny removes path and returns true if path is hard
// linked to any of the given candidate source files (same inode / file
// index). This is the file-link analogue of RemoveIfSymlinkUnder for the
// Windows model, where managed files are hard links with no reparse point
// to resolve against a prefix — the caller supplies the canonical sources
// it manages. Promoted from the cursor platform, which has used this
// pattern in production since .mdc rule files cannot be symlinks.
// Returns (matched, err): matched=true once a hard-linked source is
// found; err is non-nil only when removal of a matched managed link
// failed. Callers MUST distinguish (false,nil)=not-managed,
// (true,nil)=removed, (true,err)=managed-but-removal-failed — silently
// dropping the error makes da remove/doctor report success while an
// active managed file is left behind.
func RemoveIfHardlinkedToAny(path string, sources []string) (bool, error) {
	for _, src := range sources {
		if linked, _ := AreHardlinked(path, src); linked {
			if err := fsops.RemoveAll(path); err != nil {
				return true, fmt.Errorf("removing managed hard link %s: %w", path, err)
			}
			return true, nil
		}
	}
	return false, nil
}

// IsManagedFileLink reports whether the entry at path is a managed link to a
// file when the canonical target is unknown to the caller. It is the
// target-free companion to IsManagedLink for code paths (e.g. removal of
// rendered managed files) that must distinguish "a managed link we must
// preserve" from "a plain regular file we wrote and may delete".
//
//   - POSIX:   the entry is a symlink (os.Lstat reports os.ModeSymlink).
//   - Windows: the entry is a hard link — no reparse point, but a managed
//     file link always shares its inode with the canonical source, so its
//     link count is >= 2. A file dot-agents rendered itself has a link
//     count of 1.
//
// A directory junction is reported by the symlink branch on Windows (Go's
// os.Lstat sets os.ModeSymlink for IO_REPARSE_TAG_MOUNT_POINT).
func IsManagedFileLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if !info.Mode().IsRegular() {
		return false
	}
	return hasMultipleHardLinks(path)
}

// IsDirEntry reports whether the entry at path is a directory, following symlinks.
// Use this instead of e.IsDir() when entries may be symlinks to directories.
func IsDirEntry(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
