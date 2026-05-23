//go:build windows

package testutil

import (
	"testing"

	"golang.org/x/sys/windows"
)

// makeFileUnreadable on Windows opens an exclusive handle to path with
// dwShareMode = 0. While the handle is held, any other process or library —
// including the test target's os.Open call — that tries to open path fails
// with ERROR_SHARING_VIOLATION. The handle is closed at t.Cleanup time so the
// enclosing t.TempDir can finish its own teardown.
//
// This is the Windows-native equivalent of POSIX's "chmod 0": both ensure
// subsequent reads return an error to the test target, just through different
// mechanisms (DAC vs share semantics). The chmod-0 trick does not work here
// because NTFS does not honour POSIX mode bits — that is the gap this whole
// helper exists to close.
func makeFileUnreadable(t *testing.T, path string) {
	t.Helper()
	utf16Path, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("MakeFileUnreadable: utf16 %q: %v", path, err)
	}
	h, err := windows.CreateFile(
		utf16Path,
		windows.GENERIC_READ,
		0, // dwShareMode = 0 (FILE_SHARE_NONE): no concurrent access until we close
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("MakeFileUnreadable: CreateFile %q: %v", path, err)
	}
	t.Cleanup(func() {
		_ = windows.CloseHandle(h)
	})
}
