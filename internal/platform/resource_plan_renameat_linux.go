//go:build linux

package platform

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// renameNoClobber renames tmp to target using RENAME_NOREPLACE (Linux only):
// the kernel rejects the rename with EEXIST if ANY entry has appeared at
// target since the caller unlinked the previously-verified managed link,
// rather than silently replacing whatever landed there (t2b TOCTOU
// hardening — see atomicManagedSymlinkSwap). src is unused on this path; it
// is accepted only to keep the signature identical to the non-Linux fallback
// (resource_plan_renameat_other.go), which needs it for its read-back
// verification.
func renameNoClobber(tmp, target, _ string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, tmp, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("renameat2(RENAME_NOREPLACE) %s -> %s: %w (a foreign entry may have raced into the target slot)", tmp, target, err)
	}
	return nil
}
