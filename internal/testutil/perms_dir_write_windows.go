//go:build windows

package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

// makeDirWriteDenied on Windows uses a two-pronged mechanism: a deny-ACE on
// the parent directory's DACL blocks NEW child creation (FILE_WRITE_DATA |
// FILE_APPEND_DATA | FILE_DELETE_CHILD against the current user's SID), and
// FILE_ATTRIBUTE_READONLY is set on each pre-existing child so DeleteFile /
// RemoveDirectory against them fails with ERROR_ACCESS_DENIED. Read-side
// rights are intentionally preserved on the parent — the caller wants to
// assert write/delete denial while still verifying directory contents
// afterwards.
//
// Why this hybrid. DACL deny-ACE on the parent is the right tool for
// child-create (FILE_WRITE_DATA on the parent is checked when CreateFile
// targets a path inside that parent). It is NOT sufficient to block
// DeleteFile on an existing child: Windows resolves DeleteFile by checking
// either DELETE on the child OR FILE_DELETE_CHILD on the parent, and a
// child created with default ACLs inherits DELETE from the parent's
// pre-existing inheritable ACEs. Installing a per-child DELETE deny-ACE via
// SetEntriesInAcl is in principle correct but empirically did not deny
// DeleteFile on the GitHub-actions windows-latest runner (the runner user's
// effective token bypassed the user-SID deny). The FILE_ATTRIBUTE_READONLY
// approach is the documented Win32 way to prevent file/directory deletion
// — DeleteFile returns failure when the attribute is set, full stop — and
// it is enforced regardless of token privilege.
//
// Why this instead of toggling FILE_ATTRIBUTE_READONLY on the parent alone:
// NTFS ignores the readonly attribute for directory child operations (it
// only blocks deletion of the directory itself), so setting READONLY on the
// parent does not deny child create. The DACL-based parent deny is needed
// to cover the CREATE side.
//
// The original parent DACL is captured before installation and restored in
// t.Cleanup; each touched child's READONLY attribute is also restored so
// the enclosing t.TempDir teardown can recursively delete the directory. A
// process running with SeBackupPrivilege / SeRestorePrivilege (typical for
// elevated admin contexts and some CI agents) bypasses DACL enforcement;
// the helper probes by attempting os.Create on a transient child after the
// install and t.Skips if the denial was not actually applied.
func makeDirWriteDenied(t *testing.T, dir string) {
	t.Helper()

	// --- Phase 1: set FILE_ATTRIBUTE_READONLY on each existing child so
	// DeleteFile / RemoveDirectory against them fails. Walk the tree once,
	// chmod 0o444 each entry, and remember the original mode for cleanup.
	type savedMode struct {
		path string
		mode os.FileMode
	}
	var savedModes []savedMode
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if p == dir {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Go's os.Chmod on Windows toggles FILE_ATTRIBUTE_READONLY based on
		// the user-write bit (0o200). 0o444 clears it; restore to 0o644
		// (file) or 0o755 (dir) in cleanup.
		if err := os.Chmod(p, 0o444); err == nil {
			savedModes = append(savedModes, savedMode{path: p, mode: info.Mode().Perm()})
		}
		return nil
	})

	// --- Phase 2: install a DACL deny-ACE on the parent so NEW child create
	// fails. FILE_WRITE_DATA + FILE_APPEND_DATA deny the CreateFile-with-
	// write path; FILE_DELETE_CHILD denies DeleteFile against children added
	// after the helper runs (best-effort coverage for tests that create then
	// delete inside the denied dir).

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
	// original DACL and put it back during cleanup. origSD owns the
	// underlying memory backing origDACL; keep it referenced for the
	// lifetime of the test via the closure capture below.
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

	// FILE_DELETE_CHILD (0x00000040) is the directory-specific permission
	// that controls whether a caller can delete entries WITHIN the
	// directory. golang.org/x/sys/windows ships FILE_WRITE_DATA /
	// FILE_APPEND_DATA but not FILE_DELETE_CHILD (it is folder-context only
	// — the same bit on a file means FILE_WRITE_EA). We declare it inline
	// rather than expanding the upstream constant set; the value is fixed
	// by the Win32 SDK.
	const fileDeleteChild = 0x00000040
	const parentDenyMask = windows.FILE_WRITE_DATA |
		windows.FILE_APPEND_DATA |
		fileDeleteChild
	explicit := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.ACCESS_MASK(parentDenyMask),
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
		// Restore parent DACL first so subsequent child-mode restores can
		// run (they need write access on each child's containing dir).
		_ = windows.SetNamedSecurityInfo(
			dir,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
			nil, nil,
			origDACL,
			nil,
		)
		runtime.KeepAlive(origSD)
		// Then clear FILE_ATTRIBUTE_READONLY on each child so t.TempDir
		// teardown can delete them. Restore the originally-observed mode so
		// the perm bits are not silently coerced to 0o644/0o755.
		for _, s := range savedModes {
			restore := s.mode
			if restore == 0 {
				restore = 0o644
			}
			// Always ensure the W bit is set so READONLY clears; that is
			// the only effective change on Windows.
			restore |= 0o200
			_ = os.Chmod(s.path, restore)
		}
	})

	// Probe: attempt to create a transient child. If the create succeeds the
	// deny-ACE was not applied (elevated SeBackup/SeRestore, non-NTFS
	// volume); skip rather than mislead the caller. Clean up the probe file
	// on the unexpected success path.
	probe := filepath.Join(dir, ".makedirwritedenied-probe")
	if f, err := os.OpenFile(probe, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
		_ = f.Close()
		_ = os.Remove(probe)
		t.Skip("DACL deny-ACE did not produce a write denial (elevated SeBackup/SeRestore or non-NTFS volume?); cannot exercise the assertion")
	}
}
