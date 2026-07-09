package links

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// ownedManagedLink reports whether linkPath is a resolvable managed link
// (POSIX symlink / Windows junction) whose target resolves UNDER the
// canonical dot-agents storage root. Only such an entry is provably ours
// to replace destructively; a resolvable link pointing anywhere else is a
// user-owned link and must be treated as unmanaged.
func ownedManagedLink(linkPath string) bool {
	return IsManagedLinkUnder(linkPath, config.AgentsHome())
}

// ownedManagedHardlink reports whether linkPath is a managed *file hard
// link* we own. On Windows a managed file link is a hard link with no
// reparse point, so os.Readlink cannot resolve it and
// IsManagedLinkUnder (resolvable-link only) reports false — a stale
// managed hard link would otherwise be misclassified as an unmanaged
// occupant and refused. Identity here is the production-trusted file-link
// signal already used by IsManagedFileLink / RemoveIfHardlinkedToAny: a
// non-symlink regular file whose inode is shared (link count >= 2). A
// file dot-agents rendered itself has a link count of 1, so a user's
// plain config file is still correctly refused. On POSIX a symlink takes
// the os.Readlink branch before this is reached, so behavior there is
// unchanged for the common case and uniform for genuine hard links.
func ownedManagedHardlink(linkPath string) bool {
	info, err := os.Lstat(linkPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	return IsManagedFileLink(linkPath)
}

// handleUnmanagedOccupant applies the ownership contract to an existing
// entry at path that is NOT a managed link we own. An empty directory
// carries no data and is removed (single-entry) so idempotent re-link
// still works. Anything else (regular file, non-empty dir, a user-owned
// symlink/junction/hard link) is refused with ErrUnmanagedTarget unless
// backup != nil, in which case it is backed up then removed. backup
// failure leaves the entry intact and propagates the error (no data loss).
func handleUnmanagedOccupant(path string, backup func(string) error) error {
	info, statErr := os.Lstat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return nil // vanished between checks; nothing to protect
		}
		// A real Lstat failure (permission-denied, I/O error) is NOT the
		// same as "vanished" — we cannot prove there is nothing here to
		// protect, so fail safe and surface it rather than silently
		// proceeding to overwrite an entry we could not inspect.
		return fmt.Errorf("checking existing entry at %s before replace: %w", path, statErr)
	}
	if info.IsDir() {
		if entries, derr := os.ReadDir(path); derr == nil && len(entries) == 0 {
			if rmErr := fsopsRemove(path); rmErr != nil {
				return fmt.Errorf("removing empty dir %s: %w", path, rmErr)
			}
			return nil
		}
	}
	if backup == nil {
		return fmt.Errorf("%w: %s is not a dot-agents-managed link — back it up/import or use the explicit replace path", ErrUnmanagedTarget, path)
	}
	if bErr := backup(path); bErr != nil {
		return fmt.Errorf("backing up unmanaged entry %s before replace: %w", path, bErr)
	}
	if rmErr := fsopsRemoveAll(path); rmErr != nil {
		return fmt.Errorf("removing backed-up entry %s: %w", path, rmErr)
	}
	return nil
}

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
		// Resolvable link pointing elsewhere. Only remove it if it is
		// provably ours (resolves under the canonical agents root); a
		// user-owned symlink/junction at this path must NOT be silently
		// destroyed.
		if rErr := replaceManagedOrRefuse(linkPath, ownedManagedLink(linkPath), "link", backup); rErr != nil {
			return rErr
		}
	} else if !os.IsNotExist(err) {
		// Not a symlink/junction: a regular file, a real directory, or a
		// Windows managed file hard link (no reparse point). A hard link
		// we own (shared inode, link count >= 2) is provably ours and is
		// updated like a stale managed link; a plain user file is refused.
		if rErr := replaceManagedOrRefuse(linkPath, ownedManagedHardlink(linkPath), "hard link", backup); rErr != nil {
			return rErr
		}
	}

	if err := fsops.MkdirAll(filepath.Dir(linkPath), 0755); err != nil {
		return fmt.Errorf("creating parent dir for %s: %w", linkPath, err)
	}
	return createLink(target, linkPath)
}

// replaceManagedOrRefuse removes linkPath when it is provably a managed
// link/hard link we own (owned == true), so a stale one can be re-pointed.
// Otherwise it applies the unmanaged-occupant contract (caller backup then
// replace, or refuse) so a user-owned entry is never silently destroyed.
// kind ("link" / "hard link") only flavors the removal error message.
func replaceManagedOrRefuse(linkPath string, owned bool, kind string, backup func(path string) error) error {
	if owned {
		if err := fsopsRemoveAll(linkPath); err != nil {
			return fmt.Errorf("removing old managed %s %s: %w", kind, linkPath, err)
		}
		return nil
	}
	return handleUnmanagedOccupant(linkPath, backup)
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

// Hardlink creates a hard link at dstPath pointing to the same inode as
// srcPath. Idempotent (already-hard-linked → no-op) and, by the same
// ownership contract as Symlink, NEVER destroys unmanaged user data: an
// existing dst that is not a managed link we own (regular file, non-empty
// dir, user-owned link) yields ErrUnmanagedTarget. A caller that
// legitimately intends to replace such an entry uses HardlinkReplacing
// with an explicit backup.
func Hardlink(srcPath, dstPath string) error {
	return hardlinkWithPolicy(srcPath, dstPath, nil)
}

// HardlinkReplacing behaves like Hardlink but backs up an unmanaged
// occupant (via the caller-supplied backup) before replacing it; a backup
// failure aborts and preserves the entry.
func HardlinkReplacing(srcPath, dstPath string, backup func(path string) error) error {
	return hardlinkWithPolicy(srcPath, dstPath, backup)
}

func hardlinkWithPolicy(srcPath, dstPath string, backup func(path string) error) error {
	if already, err := AreHardlinked(srcPath, dstPath); err == nil && already {
		return nil
	}
	if _, err := os.Lstat(dstPath); err == nil {
		if ownedManagedLink(dstPath) {
			if rmErr := fsopsRemoveAll(dstPath); rmErr != nil {
				return fmt.Errorf("removing old managed link %s: %w", dstPath, rmErr)
			}
		} else if uErr := handleUnmanagedOccupant(dstPath, backup); uErr != nil {
			return uErr
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
	// A POSIX symlink target may be relative to the link's own directory;
	// resolve it before the containment test.
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(linkPath), dest)
	}
	return pathUnder(dest, prefix)
}

// pathUnder reports whether target is the same path as, or strictly
// nested within, root. It uses a path-boundary check (clean+abs, then
// filepath.Rel) NOT raw string prefixing: raw HasPrefix wrongly treats a
// sibling like ".../.agents-old" as under ".../.agents", which would let
// a user-owned link in a sibling directory be misclassified as a managed
// link and destroyed without backup.
func pathUnder(target, root string) bool {
	at, err1 := filepath.Abs(target)
	ar, err2 := filepath.Abs(root)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(ar), filepath.Clean(at))
	if err != nil {
		return false
	}
	if rel == "." {
		return true // target == root
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
		linked, err := AreHardlinked(path, src)
		if err != nil {
			if os.IsNotExist(err) {
				continue // path or this candidate source doesn't exist
			}
			// A real syscall error (permission-denied, I/O) must not read
			// as "not hard-linked" — that would let the caller conclude
			// "not managed" while never having actually determined
			// ownership.
			return false, fmt.Errorf("checking hard-link identity of %s against %s: %w", path, src, err)
		}
		if linked {
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
		if os.IsNotExist(err) {
			return false // legitimately absent — nothing to protect
		}
		// A real Lstat failure must not be read as "not ours, safe to
		// delete/overwrite" — that is the unsafe fail direction callers
		// (platform/hooks.go, platform/claude.go) rely on this predicate to
		// avoid. Fail safe: report the entry as managed/protected instead.
		return true
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
