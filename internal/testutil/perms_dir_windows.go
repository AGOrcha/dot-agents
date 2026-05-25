//go:build windows

package testutil

import (
	"os"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

// makeDirUnreadable on Windows installs a deny-ACE for the current process's
// user SID on the directory's DACL via SetNamedSecurityInfo. FILE_LIST_DIRECTORY
// is denied, so FindFirstFile / FindNextFile (the syscalls behind os.ReadDir)
// fail with ERROR_ACCESS_DENIED until the original DACL is restored at
// t.Cleanup time.
//
// Why this instead of "open the directory with FILE_FLAG_BACKUP_SEMANTICS and
// dwShareMode=0": share-mode-based denial only blocks subsequent CreateFile
// calls that request incompatible access; it does NOT block the kernel's
// readdir path against a directory whose handle a separate caller has already
// opened with permissive sharing (which most antivirus and Explorer do for
// freshly-created dirs on CI runners). A deny-ACE on the DACL is checked on
// every access regardless of who else holds a handle — that is the
// Windows-native equivalent of POSIX's chmod-0 directory denial.
//
// The original DACL is captured before installation and restored in t.Cleanup
// so the enclosing t.TempDir teardown can delete the directory. A process
// running with SeBackupPrivilege / SeRestorePrivilege (typical for elevated
// admin contexts and some CI agents) bypasses DACL enforcement; the helper
// probes by calling os.ReadDir after the deny-ACE install and t.Skips if the
// denial was not actually applied.
func makeDirUnreadable(t *testing.T, dir string) {
	t.Helper()

	// Capture current user's SID so we can deny exactly the access this
	// process would otherwise have. Denying Everyone would also lock out the
	// t.Cleanup restore path on some runners.
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		t.Fatalf("MakeDirUnreadable: GetTokenUser: %v", err)
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
		t.Fatalf("MakeDirUnreadable: GetNamedSecurityInfo %q: %v", dir, err)
	}
	origDACL, _, err := origSD.DACL()
	if err != nil {
		t.Fatalf("MakeDirUnreadable: origSD.DACL %q: %v", dir, err)
	}

	// Build a new DACL containing a single deny-ACE for FILE_LIST_DIRECTORY
	// (and the related list/traverse bits) against our SID, merged with the
	// existing entries via ACLFromEntries so an explicit deny ordering wins
	// over any inherited allow.
	const denyMask = windows.FILE_LIST_DIRECTORY |
		windows.FILE_TRAVERSE |
		windows.FILE_READ_ATTRIBUTES |
		windows.FILE_READ_EA
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
		t.Fatalf("MakeDirUnreadable: ACLFromEntries %q: %v", dir, err)
	}

	if err := windows.SetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil, nil,
		newDACL,
		nil,
	); err != nil {
		t.Fatalf("MakeDirUnreadable: SetNamedSecurityInfo %q: %v", dir, err)
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

	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("DACL deny-ACE did not produce a denial (elevated SeBackup/SeRestore or non-NTFS volume?); cannot exercise the assertion")
	}
}
