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

// makeDirWriteDenied on Windows uses a two-pronged mechanism:
//
//  1. A deny-ACE on the parent directory's DACL blocks NEW child creation
//     (FILE_WRITE_DATA | FILE_APPEND_DATA | FILE_DELETE_CHILD against the
//     current process's user SID).
//  2. For each EXISTING child (recursively), an exclusive
//     no-FILE_SHARE_DELETE handle is opened and kept alive for the duration
//     of the test. While that handle is held the kernel rejects DeleteFile /
//     RemoveDirectory against the child with ERROR_SHARING_VIOLATION,
//     regardless of DACL, privilege escalation, or Go's read-only-strip
//     retry path in os.Remove.
//
// Why the per-child handle instead of more DACL work. Go's os.Remove on
// Windows is not a thin wrapper around DeleteFile: when DeleteFile fails and
// the file has FILE_ATTRIBUTE_READONLY set, Go strips the attribute via
// SetFileAttributes and retries DeleteFile (see runtime
// src/os/file_windows.go). That defeats the FILE_ATTRIBUTE_READONLY-on-child
// approach our earlier iteration tried. DACL deny-ACE on parent for
// FILE_DELETE_CHILD is also insufficient on its own — Windows resolves
// DeleteFile by checking either DELETE on the child OR FILE_DELETE_CHILD on
// the parent, and children created with default ACLs inherit DELETE from the
// parent's pre-existing allow ACEs, so the user SID still has DELETE on the
// child. Even adding per-child DELETE deny-ACEs (the iter-2 attempt) was
// empirically bypassed on the GitHub-actions windows-latest runner — the
// runner's process token grants enough privilege that user-SID deny ACEs do
// not always take effect. The sharing-mode approach is enforced by the
// kernel's file object table and cannot be overridden by token privilege:
// holding a handle without FILE_SHARE_DELETE always causes other callers'
// delete attempts to return ERROR_SHARING_VIOLATION.
//
// Why the DACL deny-ACE on parent is kept. The sharing-mode mechanism
// covers only children that exist at install time. Tests that ALSO need to
// assert "create new file under denied dir fails" depend on the parent DACL
// deny-ACE; the existing self-test TestMakeDirWriteDeniedBlocksChildCreate
// exercises that path and is the reason we keep both legs.
//
// Cleanup. The parent's original DACL is restored first so the per-child
// handles can be closed without re-entering the deny path; then each child
// handle is closed, releasing the sharing lock and freeing the file for
// t.TempDir's recursive RemoveAll teardown.
func makeDirWriteDenied(t *testing.T, dir string) {
	t.Helper()

	heldHandles, heldPaths := openHeldHandles(t, dir)
	sid, origSD, origDACL := getSIDAndSnapshot(t, dir, heldHandles)
	installDenyACL(t, dir, heldHandles, sid, origSD, origDACL)
	probeInstallation(t, dir, heldPaths)
}

// openHeldHandles walks dir and opens a no-sharing handle on each descendant.
// Returns the list of held handles and paths in walk order (parent-before-child).
// Returns early on walk errors by closing handles and calling t.Fatalf.
func openHeldHandles(t *testing.T, dir string) ([]windows.Handle, []string) {
	var heldHandles []windows.Handle
	var heldPaths []string
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || p == dir {
			return nil
		}
		utf16Path, uErr := windows.UTF16PtrFromString(p)
		if uErr != nil {
			return nil
		}
		// FILE_FLAG_BACKUP_SEMANTICS required to open directory handle;
		// FILE_SHARE_READ allows reading, absence of FILE_SHARE_DELETE
		// causes DeleteFile / RemoveDirectory to fail with ERROR_SHARING_VIOLATION.
		h, cErr := windows.CreateFile(
			utf16Path,
			windows.GENERIC_READ,
			windows.FILE_SHARE_READ,
			nil,
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS,
			0,
		)
		if cErr != nil {
			return nil // Best effort: skip entries we can't open
		}
		heldHandles = append(heldHandles, h)
		heldPaths = append(heldPaths, p)
		return nil
	})
	if walkErr != nil {
		for _, h := range heldHandles {
			_ = windows.CloseHandle(h)
		}
		t.Fatalf("MakeDirWriteDenied: WalkDir %q: %v", dir, walkErr)
	}
	return heldHandles, heldPaths
}

// getSIDAndSnapshot retrieves the current process's user SID and snapshots
// the existing security descriptor and DACL on dir. Closes heldHandles
// and calls t.Fatalf on any error.
func getSIDAndSnapshot(t *testing.T, dir string, heldHandles []windows.Handle) (
	*windows.SID, *windows.SECURITY_DESCRIPTOR, *windows.ACL) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		for _, h := range heldHandles {
			_ = windows.CloseHandle(h)
		}
		t.Fatalf("MakeDirWriteDenied: GetTokenUser: %v", err)
	}

	origSD, err := windows.GetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		for _, h := range heldHandles {
			_ = windows.CloseHandle(h)
		}
		t.Fatalf("MakeDirWriteDenied: GetNamedSecurityInfo %q: %v", dir, err)
	}
	origDACL, _, err := origSD.DACL()
	if err != nil {
		for _, h := range heldHandles {
			_ = windows.CloseHandle(h)
		}
		t.Fatalf("MakeDirWriteDenied: origSD.DACL %q: %v", dir, err)
	}

	return user.User.Sid, origSD, origDACL
}

// installDenyACL constructs and installs a DACL deny-ACE on dir, then
// registers cleanup to restore the original DACL and close all handles.
func installDenyACL(t *testing.T, dir string, heldHandles []windows.Handle,
	sid *windows.SID, origSD *windows.SECURITY_DESCRIPTOR, origDACL *windows.ACL) {
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
		for _, h := range heldHandles {
			_ = windows.CloseHandle(h)
		}
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
		for _, h := range heldHandles {
			_ = windows.CloseHandle(h)
		}
		t.Fatalf("MakeDirWriteDenied: SetNamedSecurityInfo %q: %v", dir, err)
	}

	t.Cleanup(func() {
		_ = windows.SetNamedSecurityInfo(
			dir,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
			nil, nil,
			origDACL,
			nil,
		)
		runtime.KeepAlive(origSD)
		for i := len(heldHandles) - 1; i >= 0; i-- {
			_ = windows.CloseHandle(heldHandles[i])
		}
	})
}

// probeInstallation verifies the deny-ACE and sharing-mode locks work.
// Skips the test if installation failed (elevated privilege or non-NTFS volume).
func probeInstallation(t *testing.T, dir string, heldPaths []string) {
	probe := filepath.Join(dir, ".makedirwritedenied-probe")
	if f, err := os.OpenFile(probe, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
		_ = f.Close()
		_ = os.Remove(probe)
		t.Skip("DACL deny-ACE did not produce a write denial (elevated SeBackup/SeRestore or non-NTFS volume?); cannot exercise the assertion")
	}

	if len(heldPaths) > 0 {
		if err := os.Remove(heldPaths[0]); err == nil {
			t.Skip("sharing-mode no-FILE_SHARE_DELETE handle did not produce a delete denial; cannot exercise the assertion")
		}
	}
}
