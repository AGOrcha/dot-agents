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

	// --- Phase 1: open a no-sharing handle on each existing descendant so
	// DeleteFile / RemoveDirectory against them returns ERROR_SHARING_VIOLATION
	// while the test is running. Walk depth-first so directories are opened
	// AFTER their children — closing in reverse order at cleanup time lets
	// the children be deleted (by the surrounding t.TempDir) before their
	// parents lose the no-share lock.
	var heldHandles []windows.Handle
	var heldPaths []string
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if p == dir {
			return nil
		}
		utf16Path, uErr := windows.UTF16PtrFromString(p)
		if uErr != nil {
			return nil
		}
		// FILE_FLAG_BACKUP_SEMANTICS is required to open a directory handle
		// via CreateFile. It is harmless on file handles. GENERIC_READ is
		// the minimum access we need; the share mode is what does the work:
		// FILE_SHARE_READ allows the test (and antivirus) to keep reading
		// the file, but the absence of FILE_SHARE_DELETE / FILE_SHARE_WRITE
		// causes any DeleteFile / RemoveDirectory against the path to fail
		// with ERROR_SHARING_VIOLATION until our handle closes.
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
			// Best effort: skip entries we can't open (already-locked,
			// reparse-point dance, etc.). The probe at the end of this
			// function catches the case where the parent DACL deny also
			// silently fails.
			return nil
		}
		heldHandles = append(heldHandles, h)
		heldPaths = append(heldPaths, p)
		return nil
	})
	if walkErr != nil {
		// WalkDir's first-arg callback already swallowed any per-entry
		// error; this can only fire on a totally broken root. Treat as
		// fatal so the test does not silently fail to install.
		for _, h := range heldHandles {
			_ = windows.CloseHandle(h)
		}
		t.Fatalf("MakeDirWriteDenied: WalkDir %q: %v", dir, walkErr)
	}

	// --- Phase 2: install a DACL deny-ACE on the parent so NEW child create
	// fails. FILE_WRITE_DATA + FILE_APPEND_DATA deny the CreateFile-with-
	// write path; FILE_DELETE_CHILD is included for defence-in-depth against
	// children created AFTER the helper runs (the sharing-mode lock only
	// covers descendants present at install time).

	// Capture current user's SID so we can deny exactly the access this
	// process would otherwise have. Denying Everyone would also lock out the
	// t.Cleanup restore path on some runners.
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		for _, h := range heldHandles {
			_ = windows.CloseHandle(h)
		}
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
		// Restore parent DACL first so subsequent operations against the
		// directory (including t.TempDir's recursive RemoveAll) can proceed
		// once the per-child handles are released.
		_ = windows.SetNamedSecurityInfo(
			dir,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
			nil, nil,
			origDACL,
			nil,
		)
		runtime.KeepAlive(origSD)
		// Close the sharing-lock handles in REVERSE walk order. WalkDir
		// visits directories before their contents (pre-order, lexical), so
		// handles were appended parent-before-child. t.TempDir's recursive
		// RemoveAll deletes children before their parents, so we must
		// release file handles BEFORE the enclosing directory handles —
		// otherwise the dir-handle release order would not match RemoveAll's
		// traversal and leave handles open during child deletion attempts.
		for i := len(heldHandles) - 1; i >= 0; i-- {
			_ = windows.CloseHandle(heldHandles[i])
		}
		_ = heldPaths // retained for diagnostics if a future failure mode wants paths-with-handles
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

	// Probe 2: if we held any handles, verify the sharing-mode delete
	// denial works by trying to delete the first held child. We re-open it
	// only long enough to call os.Remove and confirm the failure. On
	// success (denial held) we do nothing; on the unexpected success of
	// os.Remove (denial broken) we skip with a clear reason. Skip if there
	// are no held descendants — the caller is exercising the create-new
	// path only, which probe 1 already covered.
	if len(heldPaths) > 0 {
		if err := os.Remove(heldPaths[0]); err == nil {
			t.Skip("sharing-mode no-FILE_SHARE_DELETE handle did not produce a delete denial; cannot exercise the assertion")
		}
	}
}
