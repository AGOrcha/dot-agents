//go:build !windows

package platform

import (
	"os"
	"syscall"
)

// hasMultipleHardLinks reports whether path has more than one directory entry
// referencing its inode (link count > 1). Used by claude's CountLinks /
// Badge implementations: a managed .claude/rules entry on Windows is a hard
// link with no reparse point, so this distinguishes a managed hard-linked
// file from a standalone regular file when no canonical source path is
// available to compare against. On POSIX a managed reference is a symlink,
// so this is a fallback that returns false for the common case.
//
// Relocated from commands/internal/lifecycle/linkcount_unix.go (the lifecycle
// HasMultipleHardLinks func-var seam) per the P1 fold-back at
// .agents/active/fold-back/p1-hasmultiplehardlinks-move-deferred-to-p3.md.
// The lifecycle seam is now backed by this helper so backup.go and
// status.go cannot silently diverge.
func hasMultipleHardLinks(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return st.Nlink > 1
}

// HasMultipleHardLinks is the exported entry point that the
// commands/internal/lifecycle HasMultipleHardLinks func-var seam now
// delegates to. Exported (not package-private) because lifecycle's
// backup.go and status.go need to read it across the package boundary, and
// because backup_test.go overrides the lifecycle-side seam directly — both
// callers are within the dot-agents binary so the surface stays internal.
func HasMultipleHardLinks(path string) bool { return hasMultipleHardLinks(path) }
