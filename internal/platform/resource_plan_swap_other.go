//go:build !linux && !darwin

package platform

// atomicSwapRename has no implementation on an OS without an atomic path
// EXCHANGE syscall (Windows and the remaining BSDs lack a stdlib-reachable
// equivalent of Linux RENAME_EXCHANGE / Darwin RENAME_SWAP). It returns
// errSwapUnsupported so atomicSwapReplaceManagedLink fails a managed-link
// REPOINT closed rather than falling back to an unsafe unlink-by-pathname
// that could delete a racer's user file (defect 2). The CREATE path
// (os.Symlink no-clobber) still works everywhere, so only in-place repoints
// of an already-projected sourced unit are affected on these platforms.
func atomicSwapRename(_, _ string) error {
	return errSwapUnsupported
}
