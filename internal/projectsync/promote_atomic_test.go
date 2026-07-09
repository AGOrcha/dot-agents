package projectsync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// atomicEnv mirrors promote_test.go's promoteEnv but lives inside the
// projectsync package so it can swap the unexported osSymlink/osRename seams
// installed by promote.go.
func atomicEnv(t *testing.T, projectName string) (agentsHome, projectPath string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, "agentshome")
	projectPath = filepath.Join(tmp, "repo")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	rc := &config.AgentsRC{
		Version: 1,
		Project: projectName,
		Sources: []config.Source{{Type: "local"}},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatalf("rc.Save: %v", err)
	}
	return agentsHome, projectPath
}

func atomicWidgetSpec() PromoteSpec {
	return PromoteSpec{
		BucketSpec: BucketSpec{
			Bucket:       "widgets",
			ManifestName: "WIDGET.md",
			Singular:     "widget",
			Plural:       "Widgets",
		},
		ExistingRealDirHint: "; cannot promote",
		RegisterInRC: func(rc *config.AgentsRC, n string) int {
			rc.Skills = config.AppendUnique(rc.Skills, n)
			return len(rc.Skills)
		},
	}
}

func writeWidget(t *testing.T, projectPath, name string) {
	t.Helper()
	dir := filepath.Join(projectPath, ".agents", "widgets", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "WIDGET.md"),
		[]byte("---\nname: "+name+"\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// swapSymlink installs a test double for the package-level osSymlink seam
// and restores the original via t.Cleanup.
func swapSymlink(t *testing.T, fn func(oldname, newname string) error) {
	t.Helper()
	orig := osSymlink
	osSymlink = fn
	t.Cleanup(func() { osSymlink = orig })
}

// swapRename installs a test double for the package-level osRename seam.
func swapRename(t *testing.T, fn func(oldpath, newpath string) error) {
	t.Helper()
	orig := osRename
	osRename = fn
	t.Cleanup(func() { osRename = orig })
}

// TestMaterializePromoteSource_RollbackOnSymlinkFailure verifies that when
// Symlink fails, the canonical copy is renamed back to the repo-local source
// so the on-disk layout is preserved.
func TestMaterializePromoteSource_RollbackOnSymlinkFailure(t *testing.T) {
	agentsHome, projectPath := atomicEnv(t, "rollback")
	writeWidget(t, projectPath, "alpha")

	swapSymlink(t, func(string, string) error {
		return errors.New("synthetic symlink failure")
	})

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Fatal("expected error from PromoteResource when symlink fails")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("expected 'rolled back' in error, got: %v", err)
	}

	// Source restored from canonical.
	sourcePath := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	info, err := os.Lstat(sourcePath)
	if err != nil {
		t.Fatalf("source path should be restored: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("source should be a real directory after rollback, got symlink")
	}
	if _, err := os.Stat(filepath.Join(sourcePath, "WIDGET.md")); err != nil {
		t.Errorf("manifest missing after rollback: %v", err)
	}

	// Canonical should be gone (we renamed it back).
	canonicalPath := filepath.Join(agentsHome, "widgets", "rollback", "alpha")
	if _, err := os.Stat(canonicalPath); !os.IsNotExist(err) {
		t.Errorf("canonical should be empty after rollback, got err=%v", err)
	}
}

// TestMaterializePromoteSource_DoubleFailureClearError verifies the error
// message when both Symlink and the rollback Rename fail.
func TestMaterializePromoteSource_DoubleFailureClearError(t *testing.T) {
	_, projectPath := atomicEnv(t, "doubl")
	writeWidget(t, projectPath, "alpha")

	swapSymlink(t, func(string, string) error {
		return errors.New("symlink-boom")
	})
	swapRename(t, func(string, string) error {
		return errors.New("rename-boom")
	})

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Fatal("expected double-failure error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "symlink=") {
		t.Errorf("missing symlink= substring in %q", msg)
	}
	if !strings.Contains(msg, "rollback=") {
		t.Errorf("missing rollback= substring in %q", msg)
	}
}

// TestMaterializePromoteSource_HappyPath end-to-end exercise: source dir
// becomes a managed symlink, canonical holds the real files.
func TestMaterializePromoteSource_HappyPath(t *testing.T) {
	agentsHome, projectPath := atomicEnv(t, "happy")
	writeWidget(t, projectPath, "alpha")

	if err := PromoteResource("alpha", projectPath, atomicWidgetSpec()); err != nil {
		t.Fatalf("PromoteResource: %v", err)
	}
	canonical := filepath.Join(agentsHome, "widgets", "happy", "alpha")
	if _, err := os.Stat(filepath.Join(canonical, "WIDGET.md")); err != nil {
		t.Errorf("canonical manifest missing: %v", err)
	}
	repoLocal := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	target, err := os.Readlink(repoLocal)
	if err != nil {
		t.Fatalf("repo-local should be a symlink: %v", err)
	}
	if target != canonical {
		t.Errorf("symlink target %q want %q", target, canonical)
	}
}

// TestMaterializePromoteSource_ExistingSymlinkBranch covers the "already a
// managed symlink" early-return branch (validatePromoteSymlink happy path).
func TestMaterializePromoteSource_ExistingSymlinkBranch(t *testing.T) {
	agentsHome, projectPath := atomicEnv(t, "existing")
	canonical := filepath.Join(agentsHome, "widgets", "existing", "alpha")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "WIDGET.md"),
		[]byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	repoBucket := filepath.Join(projectPath, ".agents", "widgets")
	if err := os.MkdirAll(repoBucket, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, canonical, filepath.Join(repoBucket, "alpha"))
	if err := PromoteResource("alpha", projectPath, atomicWidgetSpec()); err != nil {
		t.Fatalf("expected idempotent success: %v", err)
	}
}

// TestMaterializePromoteSource_MissingManifest exercises the "no manifest
// file in source dir" branch.
func TestMaterializePromoteSource_MissingManifest(t *testing.T) {
	_, projectPath := atomicEnv(t, "nomanifest")
	dir := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil || !strings.Contains(err.Error(), "WIDGET.md") {
		t.Errorf("expected missing-manifest error, got %v", err)
	}
}

// TestMaterializePromoteSource_ClearExistingCanonicalError exercises the
// error path where clearExistingCanonical returns an error (real dir at
// canonical without Force).
func TestMaterializePromoteSource_ClearExistingCanonicalError(t *testing.T) {
	agentsHome, projectPath := atomicEnv(t, "clash")
	writeWidget(t, projectPath, "alpha")
	dest := filepath.Join(agentsHome, "widgets", "clash", "alpha")
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "WIDGET.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Errorf("expected real-directory error, got %v", err)
	}
}

// TestClearExistingCanonical_RealLstatErrorSurfaces exercises the
// should-be-ATOMIC fix: clearExistingCanonical runs immediately before a
// destructive CopyTree, so a real (non-NotExist) Lstat failure must abort
// rather than being read as "nothing there, safe to write."
func TestClearExistingCanonical_RealLstatErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "bucket")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(parent, "widget")
	testutil.MakeDirUnreadable(t, parent)

	if err := clearExistingCanonical(canonical, "widget", PromoteSpec{BucketSpec: BucketSpec{Singular: "widget"}}); err == nil {
		t.Fatal("want a surfaced error for a real Lstat failure, not a silent nil")
	}
}

// TestClearExistingCanonical_AbsentPathIsNoop covers the legitimate-absence
// case unchanged by the fix: a canonical path that has never been created
// yet is safe to proceed past.
func TestClearExistingCanonical_AbsentPathIsNoop(t *testing.T) {
	canonical := filepath.Join(t.TempDir(), "widget")
	if err := clearExistingCanonical(canonical, "widget", PromoteSpec{BucketSpec: BucketSpec{Singular: "widget"}}); err != nil {
		t.Fatalf("want nil for a legitimately absent canonical path, got %v", err)
	}
}

// TestMaterializePromoteSource_CopyTreeError exercises the CopyTree-fails
// branch by making the source manifest file unreadable. CopyTree should
// fail on the read; we restore mode in cleanup so the temp dir can be
// removed.
func TestMaterializePromoteSource_CopyTreeError(t *testing.T) {
	_, projectPath := atomicEnv(t, "copyfail")
	writeWidget(t, projectPath, "alpha")
	manifest := filepath.Join(projectPath, ".agents", "widgets", "alpha", "WIDGET.md")
	if err := os.Chmod(manifest, 0); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(manifest, 0644)
	})

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Skip("CopyTree did not error on chmod-0 manifest (likely root); skipping")
	}
	if !strings.Contains(err.Error(), "copying") {
		t.Errorf("expected copying error, got %v", err)
	}
}

// TestValidatePromoteSymlink_UnresolvableSource exercises the final
// generic-mismatch branch: a source that is neither the canonical managed
// link nor a resolvable foreign link (here, a non-existent path — the
// OS-aware analogue of a Windows hard link to some other file, which has no
// reparse point to resolve). It must still be fatal, matching the prior
// fatal-on-mismatch contract.
func TestValidatePromoteSymlink_UnresolvableSource(t *testing.T) {
	spec := atomicWidgetSpec()
	err := validatePromoteSymlink(filepath.Join(t.TempDir(), "ghost"),
		filepath.Join(t.TempDir(), "canon"), "alpha", spec)
	if err == nil || !strings.Contains(err.Error(), "does not point to the canonical path") {
		t.Errorf("expected unresolvable-source mismatch error, got %v", err)
	}
}

// TestValidatePromoteSymlink_Mismatch exercises the symlink-points-elsewhere
// branch.
func TestValidatePromoteSymlink_Mismatch(t *testing.T) {
	tmp := t.TempDir()
	wrong := filepath.Join(tmp, "wrong")
	if err := os.MkdirAll(wrong, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(tmp, "src")
	linktest.Link(t, wrong, src)
	spec := atomicWidgetSpec()
	err := validatePromoteSymlink(src, filepath.Join(tmp, "canonical"), "alpha", spec)
	if err == nil || !strings.Contains(err.Error(), "already a symlink but points to") {
		t.Errorf("expected mispoint error, got %v", err)
	}
}

// TestMaterializePromoteSource_RollbackCrossFsFallback verifies that an EXDEV
// error from os.Rename triggers the CopyTree fallback path. The repo-local
// source must be restored from canonical, and canonical removed afterward.
func TestMaterializePromoteSource_RollbackCrossFsFallback(t *testing.T) {
	agentsHome, projectPath := atomicEnv(t, "exdev")
	writeWidget(t, projectPath, "alpha")

	swapSymlink(t, func(string, string) error {
		return errors.New("synthetic symlink failure")
	})
	swapRename(t, func(string, string) error {
		// Wrap EXDEV in the same shape Go's os package produces so errors.Is
		// finds it.
		return &os.LinkError{Op: "rename", Old: "x", New: "y", Err: syscall.EXDEV}
	})

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Fatal("expected error from PromoteResource on symlink failure")
	}
	if !strings.Contains(err.Error(), "via cross-fs copy") {
		t.Errorf("expected 'via cross-fs copy' in error message, got: %v", err)
	}

	// Source must be restored as a real directory from the canonical copy.
	sourcePath := filepath.Join(projectPath, ".agents", "widgets", "alpha")
	info, err := os.Lstat(sourcePath)
	if err != nil {
		t.Fatalf("source path should be restored: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("source should be a real directory after rollback, got symlink")
	}
	if _, err := os.Stat(filepath.Join(sourcePath, "WIDGET.md")); err != nil {
		t.Errorf("manifest missing after cross-fs rollback: %v", err)
	}

	// Canonical must be removed.
	canonicalPath := filepath.Join(agentsHome, "widgets", "exdev", "alpha")
	if _, err := os.Stat(canonicalPath); !os.IsNotExist(err) {
		t.Errorf("canonical should be removed after cross-fs rollback, got err=%v", err)
	}
}

// TestValidatePromoteSymlink_HappyPath covers the matched-target return.
func TestValidatePromoteSymlink_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	canon := filepath.Join(tmp, "canon")
	if err := os.MkdirAll(canon, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(tmp, "src")
	linktest.Link(t, canon, src)
	if err := validatePromoteSymlink(src, canon, "alpha", atomicWidgetSpec()); err != nil {
		t.Errorf("happy path: %v", err)
	}
}
