//go:build !windows

package lifecycle

import (
	"os"
	"syscall"
)

// HasMultipleHardLinks reports whether path has more than one directory entry
// referencing its inode (link count > 1). A managed file link on Windows is a
// hard link (no reparse point), so this distinguishes a managed hard-linked
// file from a standalone regular file when no canonical source path is
// available to compare against. On POSIX a managed reference is a symlink, so
// this is a fallback that returns false for the common case.
//
// Exported during the t08→t09 window per SHAPE.md OD-2 so doctor.go (still in
// root before t09) can keep importing the helper via lifecycle.HasMultipleHardLinks.
// Once t09 lands doctor.go in this same package, the cross-package consumer
// disappears and this becomes the only call site — at which point t09 can
// lowercase the name back to package-private.
func HasMultipleHardLinks(path string) bool {
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
