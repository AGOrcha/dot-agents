//go:build !windows

package lifecycle

import (
	"os"
	"syscall"
)

// defaultHasMultipleHardLinks reports whether path has more than one directory
// entry referencing its inode (link count > 1). A managed file link on Windows
// is a hard link (no reparse point), so this distinguishes a managed
// hard-linked file from a standalone regular file when no canonical source
// path is available to compare against. On POSIX a managed reference is a
// symlink, so this is a fallback that returns false for the common case.
//
// Wired into the HasMultipleHardLinks func-var seam in backup.go so
// backup_test.go can override the link-count behavior without touching the
// real syscalls.
func defaultHasMultipleHardLinks(path string) bool {
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
