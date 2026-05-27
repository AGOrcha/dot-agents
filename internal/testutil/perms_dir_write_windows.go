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

// makeDirWriteDenied on Windows installs a deny-ACE for the current process's
// user SID on the directory's DACL via SetNamedSecurityInfo. FILE_WRITE_DATA
// | FILE_APPEND_DATA | FILE_DELETE_CHILD are denied on the parent directory
// itself, so CreateFile-with-write against a NEW child fails with
// ERROR_ACCESS_DENIED. To also block DeleteFile / RemoveDirectory against
// existing children, the helper additionally installs a DELETE deny-ACE on
// each pre-existing child (recursively for directories) — Windows resolves
// DeleteFile by checking either DELETE on the child OR FILE_DELETE_CHILD on
// the parent, and a normal child created with default ACLs inherits DELETE
// from its parent, so the parent-only deny does not intercept the path the
// 9 catalogued tests exercise (os.Remove / os.RemoveAll of an existing
// child). Read-side rights (FILE_LIST_DIRECTORY, FILE_TRAVERSE) are
// intentionally NOT in the deny mask — the caller wants to assert the
// write/delete denial path while still being able to verify directory
// contents afterwards.
//
// Why this instead of toggling FILE_ATTRIBUTE_READONLY: NTFS ignores the
// readonly attribute for directory child operations (it only blocks deletion
// of the directory itself), so Go's os.Chmod cannot deny child create/unlink
// on Windows. A deny-ACE on the DACL is checked on every access regardless of
// who else holds a handle — that is the Windows-native equivalent of POSIX's
// chmod-0500 directory write denial.
//
// The original DACLs (parent + each touched child) are captured before
// installation and restored in t.Cleanup so the enclosing t.TempDir teardown
// can delete the directory. A process running with SeBackupPrivilege /
// SeRestorePrivilege (typical for elevated admin contexts and some CI
// agents) bypasses DACL enforcement; the helper probes by attempting
// os.Create on a transient child after the deny-ACE install and t.Skips if
// the denial was not actually applied.
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

	// FILE_DELETE_CHILD (0x00000040) is the directory-specific permission
	// that controls whether a caller can delete entries WITHIN the directory.
	// golang.org/x/sys/windows ships FILE_WRITE_DATA / FILE_APPEND_DATA but
	// not FILE_DELETE_CHILD (it is folder-context only — the same bit on a
	// file means FILE_WRITE_EA). We declare it inline rather than expanding
	// the upstream constant set; the value is fixed by the Win32 SDK.
	const fileDeleteChild = 0x00000040
	const parentDenyMask = windows.FILE_WRITE_DATA |
		windows.FILE_APPEND_DATA |
		fileDeleteChild

	// Install a per-target deny-ACE on path with the given access mask,
	// returning the original DACL/SD so cleanup can restore it. The SD
	// pointer must be kept alive until cleanup runs (origDACL aliases into
	// it) — the caller stashes it in a closure.
	installDeny := func(path string, mask uint32) (*windows.SECURITY_DESCRIPTOR, *windows.ACL, error) {
		origSD, err := windows.GetNamedSecurityInfo(
			path,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
		)
		if err != nil {
			return nil, nil, err
		}
		origDACL, _, err := origSD.DACL()
		if err != nil {
			return nil, nil, err
		}
		explicit := []windows.EXPLICIT_ACCESS{{
			AccessPermissions: windows.ACCESS_MASK(mask),
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
			return nil, nil, err
		}
		if err := windows.SetNamedSecurityInfo(
			path,
			windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION,
			nil, nil,
			newDACL,
			nil,
		); err != nil {
			return nil, nil, err
		}
		return origSD, origDACL, nil
	}

	// Apply DELETE-deny to each existing child (recursively for subdirs) so
	// DeleteFile / RemoveDirectory against the children fails even though
	// they hold DELETE via inherited ACEs. Collect the (path, origSD,
	// origDACL) tuples in walk-from-leaves order — cleanup restores in
	// reverse so a parent's deny is lifted only after its children's are
	// gone, mirroring how the OS evaluates the chain.
	type savedACL struct {
		path string
		sd   *windows.SECURITY_DESCRIPTOR
		dacl *windows.ACL
	}
	var saved []savedACL

	// Pre-walk: gather all descendants so we can deny-walk leaves-first.
	// We tolerate any individual walk error (e.g. a child the test already
	// made unreadable) — the helper's contract is best-effort write denial,
	// and the post-install probe catches failure.
	var descendants []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if p == dir {
			return nil
		}
		descendants = append(descendants, p)
		return nil
	})
	// Reverse so child files are denied before their parent subdirs, and
	// subdirs before the root dir we deny last.
	for i, j := 0, len(descendants)-1; i < j; i, j = i+1, j-1 {
		descendants[i], descendants[j] = descendants[j], descendants[i]
	}

	const childDeleteMask = windows.DELETE
	for _, child := range descendants {
		sd, dacl, err := installDeny(child, childDeleteMask)
		if err != nil {
			// Best-effort: skip children we cannot deny; the post-install
			// probe will catch the case where the denial doesn't take.
			continue
		}
		saved = append(saved, savedACL{path: child, sd: sd, dacl: dacl})
	}

	// Finally deny on the parent (covers FILE_WRITE_DATA + FILE_APPEND_DATA
	// + FILE_DELETE_CHILD — the latter handles future children that didn't
	// exist when we walked, and the former two block new-file create).
	parentSD, parentDACL, err := installDeny(dir, parentDenyMask)
	if err != nil {
		t.Fatalf("MakeDirWriteDenied: install parent deny on %q: %v", dir, err)
	}
	saved = append(saved, savedACL{path: dir, sd: parentSD, dacl: parentDACL})

	t.Cleanup(func() {
		// Restore in REVERSE order (parent first, then leaves) so the parent's
		// FILE_DELETE_CHILD restore happens before t.TempDir tries to walk
		// into the children — children's DELETE-deny will then be lifted
		// before t.TempDir attempts os.Remove on each.
		for i := len(saved) - 1; i >= 0; i-- {
			s := saved[i]
			_ = windows.SetNamedSecurityInfo(
				s.path,
				windows.SE_FILE_OBJECT,
				windows.DACL_SECURITY_INFORMATION,
				nil, nil,
				s.dacl,
				nil,
			)
			runtime.KeepAlive(s.sd)
		}
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
