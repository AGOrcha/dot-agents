//go:build darwin

package platform

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// atomicSwapRename atomically exchanges the entries a and b (RENAME_SWAP via
// renamex_np): after it returns nil, a names what b named and vice versa, with
// no window in which either path is absent. Both must exist. It is the Darwin
// backing for atomicSwapReplaceManagedLink's safe managed-link repoint
// (defect 2), the peer of Linux's RENAME_EXCHANGE.
func atomicSwapRename(a, b string) error {
	if err := unix.RenamexNp(a, b, unix.RENAME_SWAP); err != nil {
		return fmt.Errorf("renamex_np(RENAME_SWAP) %s <-> %s: %w", a, b, err)
	}
	return nil
}
