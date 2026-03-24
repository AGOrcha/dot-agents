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
