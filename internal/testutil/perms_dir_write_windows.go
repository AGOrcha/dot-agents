//go:build windows

package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

// makeDirWriteDenied on Windows installs a deny-ACE for the current process's
// user SID on the directory's DACL via SetNamedSecurityInfo. FILE_WRITE_DATA
// | FILE_APPEND_DATA | FILE_DELETE_CHILD are denied, so CreateFile-with-write
// and DeleteFile against a child fail with ERROR_ACCESS_DENIED until the
// original DACL is restored at t.Cleanup time. Read-side rights
// (FILE_LIST_DIRECTORY, FILE_TRAVERSE) are intentionally NOT in the deny mask
// — the caller wants to assert the write/delete denial path while still being
// able to verify directory contents afterwards.
//
// Why this instead of toggling FILE_ATTRIBUTE_READONLY: NTFS ignores the
// readonly attribute for directory child operations (it only blocks deletion
// of the directory itself), so Go's os.Chmod cannot deny child create/unlink
// on Windows. A deny-ACE on the DACL is checked on every access regardless of
// who else holds a handle — that is the Windows-native equivalent of POSIX's
// chmod-0500 directory write denial.
//
// The original DACL is captured before installation and restored in t.Cleanup
// so the enclosing t.TempDir teardown can delete the directory. A process
// running with SeBackupPrivilege / SeRestorePrivilege (typical for elevated
// admin contexts and some CI agents) bypasses DACL enforcement; the helper
// probes by attempting os.Create on a transient child after the deny-ACE
// install and t.Skips if the denial was not actually applied.
func makeDirWriteDenied(t *testing.T, dir string) {
	t.Helper()

	// Capture current user's SID so we can deny exactly the access this
	// process would otherwise have. Denying Everyone would also lock out the
	// t.Cleanup restore path on some runners.
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatalf("MakeDirWriteDenied: GetTokenUser: %v", err)
	}
	sid := user.User.Sid

	// Snapshot the existing security descriptor so we can recover the
	// original DACL and put it back during cleanup. origSD owns the underlying
	// memory backing origDACL; keep it referenced for the lifetime of the test
	// via the closure capture below.
	origSD, err := windows.GetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("MakeDirWriteDenied: GetNamedSecurityInfo %q: %v", dir, err)
	}
	origDACL, _, err := origSD.DACL()
	if err != nil {
		t.Fatalf("MakeDirWriteDenied: origSD.DACL %q: %v", dir, err)
	}

	// Build a new DACL containing a single deny-ACE for the write/delete bits
	// against our SID, merged with the existing entries via ACLFromEntries so
	// an explicit deny ordering wins over any inherited allow. Read-side bits
	// (FILE_LIST_DIRECTORY, FILE_TRAVERSE, FILE_READ_ATTRIBUTES, FILE_READ_EA)
	// are deliberately omitted — this helper is the write-only dual of
	// MakeDirUnreadable.
	//
	// FILE_DELETE_CHILD (0x00000040) is the directory-specific permission that
	// controls whether a caller can delete entries WITHIN the directory.
	// golang.org/x/sys/windows ships FILE_WRITE_DATA / FILE_APPEND_DATA but
	// not FILE_DELETE_CHILD (it is folder-context only — the same bit on a
	// file means FILE_WRITE_EA). We declare it inline rather than expanding
	// the upstream constant set; the value is fixed by the Win32 SDK.
	const fileDeleteChild = 0x00000040
	const denyMask = windows.FILE_WRITE_DATA |
		windows.FILE_APPEND_DATA |
		fileDeleteChild
	explicit := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.ACCESS_MASK(denyMask),
		AccessMode:        windows.DENY_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}
	newDACL, err := windows.ACLFromEntries(explicit, origDACL)
	if err != nil {
		t.Fatalf("MakeDirWriteDenied: ACLFromEntries %q: %v", dir, err)
	}

	if err := windows.SetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil, nil,
		newDACL,
		nil,
	); err != nil {
		t.Fatalf("MakeDirWriteDenied: SetNamedSecurityInfo %q: %v", dir, err)
	}

	t.Cleanup(func() {
		// Restore by reapplying the original DACL pointer recovered from the
		// snapshot. The closure capture of origSD keeps the backing memory
		// alive (origDACL aliases into it).
		_ = windows.SetNamedSecurityInfo(
			dir,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
			nil, nil,
			origDACL,
			nil,
		)
		runtime.KeepAlive(origSD)
	})

	// Probe: attempt to create a transient child. If the create succeeds the
	// deny-ACE was not applied (elevated SeBackup/SeRestore, non-NTFS volume);
	// skip rather than mislead the caller. Clean up the probe file on the
	// unexpected success path.
	probe := filepath.Join(dir, ".makedirwritedenied-probe")
	if f, err := os.OpenFile(probe, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
		_ = f.Close()
		_ = os.Remove(probe)
		t.Skip("DACL deny-ACE did not produce a write denial (elevated SeBackup/SeRestore or non-NTFS volume?); cannot exercise the assertion")
	}
}
