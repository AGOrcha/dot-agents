//go:build !windows

package links

import (
	"fmt"
	"os"
	"syscall"
)

// AreHardlinked checks whether two paths share the same inode.
func AreHardlinked(a, b string) (bool, error) {
	infoA, err := os.Lstat(a)
	if err != nil {
		return false, err
	}
	infoB, err := os.Lstat(b)
	if err != nil {
		return false, err
	}

	sysA, okA := infoA.Sys().(*syscall.Stat_t)
	sysB, okB := infoB.Sys().(*syscall.Stat_t)
	if !okA || !okB {
		return false, fmt.Errorf("stat_t unavailable")
	}
	return sysA.Ino == sysB.Ino, nil
}

// hasMultipleHardLinks reports whether the regular file at path participates
// in more than one directory entry (nlink > 1). On POSIX a managed file link
// is a symlink (nlink == 1 for the symlink itself, so this is naturally
// false) — this primarily backs the Windows path where a managed file link
// is a hard link with no reparse point and nlink >= 2.
func hasMultipleHardLinks(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return sys.Nlink > 1
}
