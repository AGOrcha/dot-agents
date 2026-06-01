package projectsync

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/testutil"
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
	agentsHome, projectPath := atomicEnv(t, "rcfail")
	writeWidget(t, projectPath, "alpha")

	// Make the project dir write-denied just before rc.Save runs. We do this
	// by swapping osSymlink to call MakeDirWriteDenied AFTER the symlink is
	// created — that way materialize succeeds but rc.Save (called next)
	// fails. MakeDirWriteDenied registers its own t.Cleanup to restore the
	// directory's permissions for t.TempDir teardown.
	swapSymlink(t, func(oldname, newname string) error {
		if err := os.Symlink(oldname, newname); err != nil {
			return err
		}
		testutil.MakeDirWriteDenied(t, projectPath)
		return nil
	})

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Skip("rc.Save did not fail under read-only project dir (filesystem quirk); skipping")
	}
	if !strings.Contains(err.Error(), "updating .agentsrc.json") {
		t.Errorf("expected rc.Save error wrapping, got: %v", err)
	}

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
	_, projectPath := atomicEnv(t, "rmsrc")
	writeWidget(t, projectPath, "alpha")

	bucket := filepath.Join(projectPath, ".agents", "widgets")
	testutil.MakeDirWriteDenied(t, bucket)

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
	testutil.MakeDirWriteDenied(t, parent)

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
	testutil.MakeDirWriteDenied(t, parent)

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
// to fail, osRename to return EXDEV, and deny writes on the project bucket
// dir so the rollback CopyTree cannot recreate the source. We do this by
// injecting via osRename rather than CopyTree, because CopyTree is not
// itself a seam.
//
// Easiest path: deny writes on the source bucket dir AFTER the source has
// been removed (during materialize). We accomplish this by also wrapping
// osSymlink — by the time it runs, the source has been removed;
// MakeDirWriteDenied the parent then.
//
// Windows-skipped: the rollback path is gated on MkdirAll(bucket/alpha)
// failing. On the github-actions windows-latest runner, the DACL deny-ACE
// blocks file create (FILE_ADD_FILE = 0x0002) but the runner's effective
// token still grants directory create (FILE_ADD_SUBDIRECTORY = 0x0004),
// so CopyTree's MkdirAll succeeds and the rollback completes — no error
// to assert. The full sharing-mode lock used by MakeDirWriteDenied for
// child-deletion denial doesn't apply here because the bucket has zero
// children at install time (alpha was already removed). The behaviour the
// test exercises is POSIX-specific and was acknowledged as such by the
// original author with an explicit runtime.GOOS == "windows" skip; the
// dir-write-denied migration accidentally dropped that gate.
func TestMaterializePromoteSource_RollbackCrossFsCopyFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows runner grants FILE_ADD_SUBDIRECTORY despite the parent DACL deny on FILE_ADD_FILE / FILE_DELETE_CHILD, so CopyTree's MkdirAll succeeds and the asserted rollback-failure path cannot be reached; POSIX-only assertion")
	}

	_, projectPath := atomicEnv(t, "xdevcopy")
	writeWidget(t, projectPath, "alpha")

	bucket := filepath.Join(projectPath, ".agents", "widgets")

	swapSymlink(t, func(string, string) error {
		// Source has been removed by now; deny writes on the parent so
		// CopyTree's MkdirAll on the destination fails when rollback runs.
		// MakeDirWriteDenied registers its own t.Cleanup to restore
		// permissions for t.TempDir teardown.
		testutil.MakeDirWriteDenied(t, bucket)
		return errors.New("symlink-boom")
	})
	swapRename(t, func(string, string) error {
		return &os.LinkError{Op: "rename", Old: "x", New: "y", Err: syscall.EXDEV}
	})

	err := PromoteResource("alpha", projectPath, atomicWidgetSpec())
	if err == nil {
		t.Skip("CopyTree fallback succeeded; skipping")
	}
	if !strings.Contains(err.Error(), "rollback failed") && !strings.Contains(err.Error(), "now missing") {
		t.Errorf("expected cross-fs copy-failed error wording, got: %v", err)
	}
}
