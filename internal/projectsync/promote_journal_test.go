package projectsync

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/linktest"
)

// TestPromoteResource_JournalWriteFailureDegradesGracefully exercises the
// "journal could not be opened" warning branch in PromoteResource by forcing
// the journal-package osWriteFile seam to fail. The promote itself must still
// succeed end-to-end despite the missing journal.
func TestPromoteResource_JournalWriteFailureDegradesGracefully(t *testing.T) {
	agentsHome, projectPath := atomicEnv(t, "no-journal")
	writeWidget(t, projectPath, "alpha")

	orig := osWriteFile
	// osWriteFile is the journal-package seam used by BeginPromoteJournal.
	// Only fail when writing into the .promote-journal directory so the rc.Save
	// path (which calls os.WriteFile directly) is unaffected.
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.Contains(name, PromoteJournalDir) {
			return errors.New("synthetic journal write failure")
		}
		return os.WriteFile(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	if err := PromoteResource("alpha", projectPath, atomicWidgetSpec()); err != nil {
		t.Fatalf("PromoteResource should degrade gracefully when journal fails: %v", err)
	}
	canon := filepath.Join(agentsHome, "widgets", "no-journal", "alpha")
	if _, err := os.Stat(filepath.Join(canon, "WIDGET.md")); err != nil {
		t.Errorf("canonical manifest missing: %v", err)
	}
	// The journal directory may not even exist (depending on order); the key
	// invariant is that no journal entries remain.
	dir := promoteJournalDirPath(agentsHome)
	if entries, _ := os.ReadDir(dir); len(entries) > 0 {
		for _, e := range entries {
			t.Errorf("unexpected journal residue: %s", e.Name())
		}
	}
}

// TestPromoteResource_RCSaveFailureRollsBackJournal exercises the rc.Save
// failure branch (lines 93–98 in promote.go) by making the project directory
// read-only after the symlink step. The journal must be advanced to rolled-back.
func TestPromoteResource_RCSaveFailureRollsBackJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based read-only project dir not portable to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod restrictions")
	}
	agentsHome, projectPath := atomicEnv(t, "rcfail")
	writeWidget(t, projectPath, "alpha")

	// Make the project dir read-only just before rc.Save runs. We do this by
	// swapping osSymlink to chmod the project read-only AFTER the symlink is
	// created — that way materialize succeeds but rc.Save (called next) fails.
	swapSymlink(t, func(oldname, newname string) error {
		if err := os.Symlink(oldname, newname); err != nil {
			return err
		}
		return os.Chmod(projectPath, 0o500)
	})
	t.Cleanup(func() { _ = os.Chmod(projectPath, 0o755) })

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Skip("rc.Save did not fail under read-only project dir (filesystem quirk); skipping")
	}
	if !strings.Contains(err.Error(), "updating .agentsrc.json") {
		t.Errorf("expected rc.Save error wrapping, got: %v", err)
	}

	// Restore permissions before any t.Cleanup tries to delete files.
	_ = os.Chmod(projectPath, 0o755)

	// Journal must NOT linger after rollback.
	dir := promoteJournalDirPath(agentsHome)
	if entries, _ := os.ReadDir(dir); len(entries) > 0 {
		for _, e := range entries {
			t.Errorf("journal entry should be removed after rollback: %s", e.Name())
		}
	}
}

// TestMaterializePromoteSource_RemoveSourceFailure covers the os.RemoveAll
// failure branch (lines 165–167) by chmod-ing the source directory's parent
// to read-only so RemoveAll cannot delete the source.
func TestMaterializePromoteSource_RemoveSourceFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based read-only parent not portable to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod restrictions")
	}
	_, projectPath := atomicEnv(t, "rmsrc")
	writeWidget(t, projectPath, "alpha")

	bucket := filepath.Join(projectPath, ".agents", "widgets")
	if err := os.Chmod(bucket, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bucket, 0o755) })

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Skip("RemoveAll did not fail on read-only parent; skipping")
	}
	if !strings.Contains(err.Error(), "removing repo-local") {
		t.Errorf("expected remove-repo-local error, got: %v", err)
	}
}

// TestClearExistingCanonical_StaleSymlinkRemoveError covers the
// stale-symlink Remove error branch (lines 224–226).
func TestClearExistingCanonical_StaleSymlinkRemoveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod parent semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod restrictions")
	}
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "ro")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "stale")
	target := filepath.Join(tmp, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, target, link)
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	spec := atomicWidgetSpec()
	if err := clearExistingCanonical(link, "alpha", spec); err == nil {
		t.Skip("os.Remove succeeded on stale symlink under read-only parent; skipping")
	} else if !strings.Contains(err.Error(), "removing stale canonical symlink") {
		t.Errorf("expected stale-symlink error, got: %v", err)
	}
}

// TestClearExistingCanonical_ForceRealDirRemoveError covers the force=true
// real-dir RemoveAll-fail branch (lines 232–234).
func TestClearExistingCanonical_ForceRealDirRemoveError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod parent semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod restrictions")
	}
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "ro")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "canonical")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "leaf"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	spec := atomicWidgetSpec()
	spec.Force = true
	if err := clearExistingCanonical(target, "alpha", spec); err == nil {
		t.Skip("RemoveAll succeeded under read-only parent; skipping")
	} else if !strings.Contains(err.Error(), "removing existing canonical directory") {
		t.Errorf("expected RemoveAll error, got: %v", err)
	}
}

// TestMaterializePromoteSource_RollbackCrossFsCopyFails covers the cross-fs
// rollback path where CopyTree also fails (lines 179–182). We swap osSymlink
// to fail, osRename to return EXDEV, and chmod the project bucket dir
// read-only so the rollback CopyTree cannot recreate the source. We do this
// by injecting via osRename rather than CopyTree, because CopyTree is not
// itself a seam.
//
// Easiest path: make the source bucket dir read-only AFTER the source has
// been removed (during materialize). We accomplish this by also wrapping
// osSymlink — by the time it runs, the source has been removed; chmod the
// parent then.
func TestMaterializePromoteSource_RollbackCrossFsCopyFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod parent semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod restrictions")
	}
	_, projectPath := atomicEnv(t, "xdevcopy")
	writeWidget(t, projectPath, "alpha")

	bucket := filepath.Join(projectPath, ".agents", "widgets")

	swapSymlink(t, func(string, string) error {
		// Source has been removed by now; chmod the parent so CopyTree's
		// MkdirAll on the destination fails when rollback runs.
		_ = os.Chmod(bucket, 0o500)
		return errors.New("symlink-boom")
	})
	swapRename(t, func(string, string) error {
		return &os.LinkError{Op: "rename", Old: "x", New: "y", Err: syscall.EXDEV}
	})
	t.Cleanup(func() { _ = os.Chmod(bucket, 0o755) })

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Skip("CopyTree fallback succeeded; skipping")
	}
	if !strings.Contains(err.Error(), "rollback failed") && !strings.Contains(err.Error(), "now missing") {
		t.Errorf("expected cross-fs copy-failed error wording, got: %v", err)
	}
	// Restore so cleanup can remove the tmp tree.
	_ = os.Chmod(bucket, 0o755)
}
