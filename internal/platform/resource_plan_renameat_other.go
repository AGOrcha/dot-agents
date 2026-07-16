//go:build !linux

package platform

import (
	"fmt"
	"os"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// renameNoClobber is the non-Linux fallback for the t2b atomicManagedSymlinkSwap
// hardening: there is no portable no-clobber rename primitive in the stdlib
// on macOS/BSD/Windows (Linux's RENAME_NOREPLACE — resource_plan_renameat_linux.go
// — has no equivalent used here), so this performs a plain rename and then
// reads the result back to confirm target now resolves to src. A mismatch
// means a concurrent writer raced into target between the rename and this
// read-back and is surfaced as an error rather than silently believed
// successful. This is a strictly WEAKER guarantee than Linux's: it cannot
// prevent a racer's content from being clobbered by our own rename (that
// content is already gone by the time we read back), it can only detect that
// OUR write was itself subsequently clobbered by a second racer. Closing the
// gap fully on these platforms would require OS-specific syscalls
// (e.g. Darwin's renamex_np(RENAME_EXCL)) that are deliberately out of scope
// here.
func renameNoClobber(tmp, target, src string) error {
	if err := fsops.Rename(tmp, target); err != nil {
		return err
	}
	got, err := os.Readlink(target)
	if err != nil {
		return fmt.Errorf("verify-after-rename: reading back %s: %w", target, err)
	}
	if got != src {
		return fmt.Errorf("verify-after-rename: %s resolves to %q, not %q — a concurrent writer raced this swap", target, got, src)
	}
	return nil
}
