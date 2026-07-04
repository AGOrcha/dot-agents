//go:build windows

package agentslock

import (
	"io"
	"os"
	"syscall"
)

// init routes every sidecar-lock read through the FILE_SHARE_DELETE reader on
// Windows. The lock name is freed by an atomic rename-away (displaceLock) that a
// contender's concurrent read must not block; Go's os.ReadFile opens with only
// FILE_SHARE_READ|FILE_SHARE_WRITE, so on Windows a plain read pins the name
// against MoveFileEx with ERROR_SHARING_VIOLATION. Sharing DELETE lets the
// release/reclaim rename proceed while the identity is read.
func init() { readLockFile = readLockFileShareDelete }

// readLockFileShareDelete reads path with a handle that shares DELETE, so a
// holder's atomic rename-away is never blocked by this read. It mirrors
// os.ReadFile's contract: it returns the full file contents, and a missing name
// yields an *os.PathError wrapping the Windows not-found errno, which
// os.IsNotExist classifies correctly. The handle carries GENERIC_READ only, so
// it is a pure read (not an fsguard-policed mutation).
func readLockFileShareDelete(path string) ([]byte, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(
		p,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	return io.ReadAll(f)
}
