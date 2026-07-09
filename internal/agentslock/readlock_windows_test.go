//go:build windows

package agentslock

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestReadLockFileShareDelete_UTF16Error covers the defensive early-return in
// readLockFileShareDelete: syscall.UTF16PtrFromString rejects any path that
// contains an embedded NUL byte, so the reader must surface that as an
// *os.PathError (mirroring os.ReadFile) without ever reaching CreateFile and
// without panicking. This is the only branch of the Windows reader that a
// real, existing lock path can never exercise, so it needs a direct probe.
func TestReadLockFileShareDelete_UTF16Error(t *testing.T) {
	// Embedded NUL — UTF16PtrFromString returns EINVAL for this input.
	bad := "a\x00b.lock"

	data, err := readLockFileShareDelete(bad)
	if err == nil {
		t.Fatalf("expected an error for a NUL-containing path, got nil (data=%q)", data)
	}
	if data != nil {
		t.Fatalf("expected nil bytes on error, got %q", data)
	}

	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("expected *os.PathError, got %T: %v", err, err)
	}
	if pathErr.Op != "open" {
		t.Errorf("PathError.Op = %q, want %q", pathErr.Op, "open")
	}
	if !strings.Contains(pathErr.Path, "b.lock") {
		t.Errorf("PathError.Path = %q, want it to include the offending path", pathErr.Path)
	}
	if pathErr.Err == nil {
		t.Errorf("PathError.Err is nil, want the wrapped UTF16PtrFromString error")
	}
}
