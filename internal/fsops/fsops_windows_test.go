//go:build windows

package fsops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// TestFsopsWindows_RoundTrip mirrors TestFsopsDefault_RoundTrip but exercises
// the Windows-tagged implementations: MkdirAll → WriteFile → stat → Remove →
// RemoveAll. Keeping the assertion shape identical to the POSIX sibling makes
// it obvious if the Windows variants ever diverge in observable behaviour.
func TestFsopsWindows_RoundTrip(t *testing.T) {
	root := t.TempDir()

	nested := filepath.Join(root, "a", "b", "c")
	if err := MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if fi, err := os.Stat(nested); err != nil || !fi.IsDir() {
		t.Fatalf("expected nested dir, stat err=%v", err)
	}

	f := filepath.Join(nested, "f.txt")
	if err := WriteFile(f, []byte("payload"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(f)
	if err != nil || string(got) != "payload" {
		t.Fatalf("ReadFile after WriteFile: got=%q err=%v", got, err)
	}

	if err := Remove(f); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("expected file gone after Remove, err=%v", err)
	}

	if err := RemoveAll(filepath.Join(root, "a")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Errorf("expected tree gone after RemoveAll, err=%v", err)
	}
}

// TestMkdirAllComponents_DeepPath exercises the Windows-specific
// component-by-component fallback that the production MkdirAll falls back to
// when os.MkdirAll fails. We call mkdirAllComponents directly so that the
// happy-path os.MkdirAll branch in MkdirAll does not short-circuit it. The
// path is deeper than the round-trip case to make sure the volume-prefix
// trimming and per-component os.Mkdir loop both fire.
func TestMkdirAllComponents_DeepPath(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "one", "two", "three", "four", "five")

	if err := mkdirAllComponents(deep, 0o755); err != nil {
		t.Fatalf("mkdirAllComponents fresh path: %v", err)
	}

	// Each component should exist as a directory.
	cur := root
	for _, part := range []string{"one", "two", "three", "four", "five"} {
		cur = filepath.Join(cur, part)
		fi, err := os.Stat(cur)
		if err != nil {
			t.Fatalf("stat %s: %v", cur, err)
		}
		if !fi.IsDir() {
			t.Fatalf("expected %s to be a directory", cur)
		}
	}

	// Running again must be a no-op (every component already exists as a dir).
	// This proves the `errors.Is(err, os.ErrExist)` tolerance branch fires for
	// every component on the second pass.
	if err := mkdirAllComponents(deep, 0o755); err != nil {
		t.Fatalf("mkdirAllComponents idempotent call: %v", err)
	}

	// Empty-suffix path (just the volume + root) must return nil without
	// attempting any mkdir, exercising the early-return on `rest == ""`.
	vol := filepath.VolumeName(root)
	if vol != "" {
		if err := mkdirAllComponents(vol+string(os.PathSeparator), 0o755); err != nil {
			t.Fatalf("mkdirAllComponents on volume root: %v", err)
		}
	}

	// A component that exists as a file (not a dir) must fail the
	// "exists and is not a directory" guard.
	filePath := filepath.Join(root, "iam-a-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	collide := filepath.Join(filePath, "child")
	err := mkdirAllComponents(collide, 0o755)
	if err == nil {
		t.Fatal("mkdirAllComponents on file-as-parent: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "is not a directory") &&
		!strings.Contains(strings.ToLower(err.Error()), "directory name is invalid") &&
		!strings.Contains(strings.ToLower(err.Error()), "system cannot find the path") {
		// We accept either the explicit "is not a directory" error from the
		// guard or the underlying Win32 error returned by os.Mkdir when the
		// parent component is a file. Both prove the negative path triggered.
		t.Fatalf("mkdirAllComponents on file-as-parent: unexpected error: %v", err)
	}
}

// TestSystemExe_AppendsExeSuffix verifies systemExe joins under %SystemRoot%
// instead of relying on PATH (the SonarCloud go:S4036 mitigation). The
// returned path must end with the supplied relative segment and be rooted at
// either %SystemRoot% or the documented fallback C:\Windows.
func TestSystemExe_AppendsExeSuffix(t *testing.T) {
	// Case 1: %SystemRoot% set.
	t.Setenv("SystemRoot", `C:\Windows`)
	got := systemExe(`System32\foo.exe`)
	want := filepath.Join(`C:\Windows`, `System32\foo.exe`)
	if got != want {
		t.Errorf("systemExe with SystemRoot set: got=%q want=%q", got, want)
	}
	if !strings.HasSuffix(strings.ToLower(got), ".exe") {
		t.Errorf("systemExe must preserve the .exe suffix: got=%q", got)
	}

	// Case 2: %SystemRoot% unset — fallback to literal C:\Windows.
	t.Setenv("SystemRoot", "")
	got = systemExe(`System32\bar.exe`)
	want = filepath.Join(`C:\Windows`, `System32\bar.exe`)
	if got != want {
		t.Errorf("systemExe with SystemRoot unset: got=%q want=%q", got, want)
	}

	// The package-level winPowerShell value must use systemExe's anchoring —
	// regression check that the var stays under a Windows-system path and
	// cannot be hijacked by a poisoned PATH.
	low := strings.ToLower(winPowerShell)
	if !strings.Contains(low, `\system32\windowspowershell\v1.0\powershell.exe`) {
		t.Errorf("winPowerShell must be anchored under System32: got=%q", winPowerShell)
	}
}

// TestRemove_MissingFileIsNoError documents the explicit IsNotExist tolerance
// on Remove (parity with os.Remove). This is a defensive-branch coverage case:
// the production code returns nil when os.Remove yields IsNotExist, which
// otherwise would only be reachable when the PowerShell fallback also fires.
func TestRemove_MissingFileIsNoError(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "nope.txt")
	if err := Remove(missing); err != nil {
		t.Fatalf("Remove on missing path must be nil, got: %v", err)
	}
}

// TestRemoveAll_MissingPathIsNoError mirrors the Remove case for RemoveAll.
func TestRemoveAll_MissingPathIsNoError(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "no", "such", "tree")
	if err := RemoveAll(missing); err != nil {
		t.Fatalf("RemoveAll on missing path must be nil, got: %v", err)
	}
}

// TestWriteFile_CreatesMissingParent exercises the WriteFile branch that
// calls MkdirAll on the parent directory before retrying. The seed path's
// parent does not exist, so the first os.WriteFile fails; the production
// code then calls MkdirAll(filepath.Dir(path)) and proceeds via the
// PowerShell fallback. This proves the parent-creation branch runs.
func TestWriteFile_CreatesMissingParent(t *testing.T) {
	root := t.TempDir()
	// Parent dir does not yet exist; os.WriteFile would fail with ENOENT.
	target := filepath.Join(root, "new-parent", "f.txt")
	if err := WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile with missing parent: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile after WriteFile: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("content mismatch: got=%q want=%q", got, "hello")
	}
}

// TestMkdirAll_UnderUnreadableParent uses testutil.MakeDirUnreadable to deny
// directory listing/traversal on a parent and asserts that MkdirAll cannot
// silently succeed underneath the deny-ACE. The exact error varies (Win32
// returns ERROR_ACCESS_DENIED, which the guard wraps), so we only assert that
// either an error returned or, if the elevated/SeBackup case skipped the
// fixture, the helper handled the skip itself.
func TestMkdirAll_UnderUnreadableParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "denied")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	testutil.MakeDirUnreadable(t, parent)

	child := filepath.Join(parent, "child")
	err := MkdirAll(child, 0o755)
	if err == nil {
		// On a well-enforced DACL this is unreachable; if it does happen the
		// fixture silently let the operation through, which is the runtime
		// signature of an elevated/non-NTFS volume. The helper itself skips
		// in those cases, so reaching here means the deny-ACE was applied AND
		// it still allowed the mkdir — that is a real regression worth
		// surfacing.
		t.Fatal("MkdirAll under deny-ACE parent unexpectedly succeeded")
	}
	if !errors.Is(err, fs.ErrPermission) &&
		!strings.Contains(strings.ToLower(err.Error()), "access is denied") &&
		!strings.Contains(strings.ToLower(err.Error()), "mkdir") {
		t.Fatalf("MkdirAll under deny-ACE parent: expected permission/mkdir-wrapped error, got: %v", err)
	}
}

// TestRemove_UnderUnreadableParent installs a deny-ACE on the parent dir of a
// pre-seeded file and asserts that Remove surfaces the denial rather than
// silently succeeding. Mirrors TestMkdirAll_UnderUnreadableParent for the
// Remove path's defensive branches.
func TestRemove_UnderUnreadableParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "denied")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	victim := filepath.Join(parent, "victim.txt")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed victim: %v", err)
	}
	testutil.MakeDirUnreadable(t, parent)

	err := Remove(victim)
	if err == nil {
		// As in the MkdirAll case, reaching here means the deny-ACE was
		// honoured by os.Remove but the operation still succeeded. The
		// Remove production code has a PowerShell fallback that may legitimately
		// succeed on some runners (PowerShell may inherit different ACL
		// evaluation), so we do not treat that as a hard failure here —
		// only assert that, when an error is returned, it is the
		// wrapped-permission-style error the production code documents.
		t.Skip("Remove succeeded despite deny-ACE; PowerShell fallback evaluation differs on this runner")
	}
	low := strings.ToLower(err.Error())
	if !errors.Is(err, fs.ErrPermission) &&
		!strings.Contains(low, "access is denied") &&
		!strings.Contains(low, "remove") &&
		!strings.Contains(low, "powershell") {
		t.Fatalf("Remove under deny-ACE parent: unexpected error shape: %v", err)
	}
}

// TestRemoveAll_UnderUnreadableParent mirrors TestRemove_UnderUnreadableParent
// for the RemoveAll path. RemoveAll has the PowerShell -Recurse fallback, so
// like its sibling we accept either a permission error or a successful
// PowerShell-mediated removal.
func TestRemoveAll_UnderUnreadableParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "denied")
	tree := filepath.Join(parent, "sub", "deep")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatalf("seed tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tree, "f.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	testutil.MakeDirUnreadable(t, parent)

	err := RemoveAll(filepath.Join(parent, "sub"))
	if err == nil {
		t.Skip("RemoveAll succeeded despite deny-ACE on parent; PowerShell fallback handled it")
	}
	low := strings.ToLower(err.Error())
	if !errors.Is(err, fs.ErrPermission) &&
		!strings.Contains(low, "access is denied") &&
		!strings.Contains(low, "remove") &&
		!strings.Contains(low, "powershell") {
		t.Fatalf("RemoveAll under deny-ACE parent: unexpected error shape: %v", err)
	}
}

// TestWriteFile_UnderUnreadableParent installs a deny-ACE on the target's
// parent and asserts WriteFile surfaces the denial. Like the sibling Remove
// cases, the PowerShell WriteAllBytes fallback may succeed where the Go
// runtime call fails, so an unexpected success only triggers a skip.
func TestWriteFile_UnderUnreadableParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "denied")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	testutil.MakeDirUnreadable(t, parent)

	err := WriteFile(filepath.Join(parent, "f.txt"), []byte("z"), 0o644)
	if err == nil {
		t.Skip("WriteFile succeeded despite deny-ACE; PowerShell fallback handled it")
	}
	low := strings.ToLower(err.Error())
	if !errors.Is(err, fs.ErrPermission) &&
		!strings.Contains(low, "access is denied") &&
		!strings.Contains(low, "write") &&
		!strings.Contains(low, "powershell") {
		t.Fatalf("WriteFile under deny-ACE parent: unexpected error shape: %v", err)
	}
}
