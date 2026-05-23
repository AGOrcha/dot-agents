//go:build windows

package testutil

import (
	"testing"

	"golang.org/x/sys/windows"
)

// makeFileUnreadable on Windows takes an exclusive byte-range lock on the
// entire file via LockFileEx. Read requests to a locked range from any handle
// fail with ERROR_LOCK_VIOLATION until the lock is released — that is the
// Windows-native equivalent of POSIX's chmod-0 read denial.
//
// Why this instead of "open with FILE_SHARE_NONE": Windows CI runners have
// antivirus that opens new files for scanning with maximum sharing, so a
// dwShareMode=0 CreateFile call may succeed but not actually exclude
// concurrent opens — the test target then reads through the antivirus's
// permissive handle. Byte-range locks are not subject to that interference;
// the kernel rejects reads to a locked range no matter who is holding the
// file open.
//
// The handle is kept with FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE
// so the surrounding t.TempDir teardown can delete the file once we unlock —
// share semantics for our handle are intentionally lax; the byte-range lock
// is what enforces the denial.
func makeFileUnreadable(t *testing.T, path string) {
	t.Helper()
	utf16Path, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("MakeFileUnreadable: utf16 %q: %v", path, err)
	}
	h, err := windows.CreateFile(
		utf16Path,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("MakeFileUnreadable: CreateFile %q: %v", path, err)
	}
	// Lock the entire file exclusively. nNumberOfBytesToLockLow/High set to
	// the 32-bit max each → 0xFFFFFFFF_FFFFFFFF byte range, which covers the
	// entire file (and beyond — locking past EOF is legal and harmless).
	// LOCKFILE_FAIL_IMMEDIATELY makes the call non-blocking; if a real
	// concurrent locker exists we want to fail fast, not stall the test.
	var ol windows.Overlapped
	if err := windows.LockFileEx(
		h,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		^uint32(0), ^uint32(0),
		&ol,
	); err != nil {
		_ = windows.CloseHandle(h)
		t.Fatalf("MakeFileUnreadable: LockFileEx %q: %v", path, err)
	}
	t.Cleanup(func() {
		_ = windows.UnlockFileEx(h, 0, ^uint32(0), ^uint32(0), &ol)
		_ = windows.CloseHandle(h)
	})
}
