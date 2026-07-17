//go:build linux

package platform

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// atomicSwapRename atomically exchanges the entries a and b (RENAME_EXCHANGE):
// after it returns nil, a names what b named and vice versa, with no window in
// which either path is absent. Both must exist. It is the Linux backing for
// atomicSwapReplaceManagedLink's safe managed-link repoint (defect 2): the
// former occupant ends up at the caller's private tmp name where it can be
// inspected free of any external race.
func atomicSwapRename(a, b string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE); err != nil {
		return fmt.Errorf("renameat2(RENAME_EXCHANGE) %s <-> %s: %w", a, b, err)
	}
	return nil
}
