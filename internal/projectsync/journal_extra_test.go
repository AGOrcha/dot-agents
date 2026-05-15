package projectsync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPromoteJournal_BeginMkdirAllError forces a write-failure that surfaces as
// "creating promote-journal dir" by passing an agents-home rooted at an
// existing regular file. os.MkdirAll then fails because a path component is
// not a directory.
func TestPromoteJournal_BeginMkdirAllError(t *testing.T) {
	tmp := t.TempDir()
	notADir := filepath.Join(tmp, "regular-file")
	if err := os.WriteFile(notADir, []byte("oops"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginPromoteJournal(notADir, sampleJournalEntry()); err == nil {
		t.Fatal("expected MkdirAll failure when agents-home is a regular file")
	} else if !strings.Contains(err.Error(), "creating promote-journal dir") {
		t.Errorf("expected wrapped MkdirAll error, got %v", err)
	}
}

// TestPromoteJournal_AdvanceWriteError reuses the osWriteFile seam to force
// the post-marshal write failure in AdvancePromoteJournal.
func TestPromoteJournal_AdvanceWriteError(t *testing.T) {
	home := journalAgentsHome(t)
	path, err := BeginPromoteJournal(home, sampleJournalEntry())
	if err != nil {
		t.Fatalf("BeginPromoteJournal: %v", err)
	}

	orig := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error {
		return errors.New("synthetic advance write failure")
	}
	t.Cleanup(func() { osWriteFile = orig })

	if err := AdvancePromoteJournal(path, PromoteStateSourceRemoved); err == nil {
		t.Fatal("expected synthetic write failure to propagate")
	} else if !strings.Contains(err.Error(), "synthetic advance write failure") {
		t.Errorf("expected synthetic error in %v", err)
	}
}

// TestPromoteJournal_RemoveErrorNonNotExist forces a non-NotExist error on
// Remove by passing a directory path (Remove on a non-empty dir returns
// ENOTEMPTY, which is not IsNotExist).
func TestPromoteJournal_RemoveErrorNonNotExist(t *testing.T) {
	dir := t.TempDir()
	// Put a file inside so the dir is non-empty; os.Remove on a non-empty dir
	// returns a non-IsNotExist error.
	if err := os.WriteFile(filepath.Join(dir, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemovePromoteJournal(dir); err == nil {
		t.Fatal("expected Remove to fail on non-empty dir")
	} else if !strings.Contains(err.Error(), "removing journal entry") {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

// TestPromoteJournal_ListPendingReadDirError forces the non-NotExist branch
// in os.ReadDir by passing an agents-home that is a regular file (its
// .promote-journal subpath cannot be read as a directory).
func TestPromoteJournal_ListPendingReadDirError(t *testing.T) {
	// Use a path that exists as a file so the journal subdir resolves to a
	// non-directory inode, producing a non-IsNotExist error.
	tmp := t.TempDir()
	regular := filepath.Join(tmp, "file")
	if err := os.WriteFile(regular, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// promoteJournalDirPath(regular) = regular/.promote-journal — ReadDir of a
	// path under a regular file returns ENOTDIR.
	_, err := ListPendingPromoteJournals(regular)
	if err == nil {
		t.Fatal("expected ReadDir error when agents-home is a regular file")
	}
	if !strings.Contains(err.Error(), "reading promote-journal dir") {
		t.Errorf("expected wrapped error, got %v", err)
	}
}

// TestPromoteJournal_ListPendingSkipsNonEntries covers three "continue"
// branches: directory entries, non-.json files, and corrupt JSON files.
func TestPromoteJournal_ListPendingSkipsNonEntries(t *testing.T) {
	home := journalAgentsHome(t)
	dir := promoteJournalDirPath(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	// (a) subdirectory under journal dir → IsDir branch
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	// (b) non-.json file → extension filter branch
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}
	// (c) corrupt .json → unmarshal-error branch
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	// One real pending entry so we can confirm the rest of the loop still
	// returns expected data after skips.
	entry := sampleJournalEntry()
	good := PromoteJournalEntry{ID: "ok", Singular: "skill", Bucket: "skills", Name: "ok", State: PromoteStatePrepared}
	good.StartedAt = entry.StartedAt
	data, _ := json.MarshalIndent(good, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "ok.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	pending, err := ListPendingPromoteJournals(home)
	if err != nil {
		t.Fatalf("ListPendingPromoteJournals: %v", err)
	}
	if len(pending) != 1 || pending[0].Name != "ok" {
		t.Errorf("expected only the ok entry to survive, got %+v", pending)
	}
}

// TestPromoteJournal_ListPendingReadFileSkip forces the rerr branch in the
// per-entry read by creating an unreadable .json file (chmod 0000). Skipped
// on platforms where chmod does not restrict read for the owner (root, etc.).
func TestPromoteJournal_ListPendingReadFileSkip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-0 unreadable semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read 0o000 files; skipping")
	}
	home := journalAgentsHome(t)
	dir := promoteJournalDirPath(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dir, "locked.json")
	if err := os.WriteFile(unreadable, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	pending, err := ListPendingPromoteJournals(home)
	if err != nil {
		t.Fatalf("ListPendingPromoteJournals: %v", err)
	}
	// The locked entry must be silently skipped (not returned, no error).
	for _, p := range pending {
		if p.ID == "locked" {
			t.Errorf("expected locked entry to be skipped, got %+v", p)
		}
	}
}

// TestRecoverPendingPromote_PreparedRemoveError covers the error branch from
// RemovePromoteJournal in the "prepared" case by pointing the entry at a
// path under a non-empty directory so os.Remove fails.
func TestRecoverPendingPromote_PreparedRemoveError(t *testing.T) {
	home := journalAgentsHome(t)
	dir := promoteJournalDirPath(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a directory with the journal's expected path so RemovePromoteJournal
	// (which calls os.Remove, not RemoveAll) fails with ENOTEMPTY.
	e := sampleJournalEntry()
	e.ID = "stuck"
	e.State = PromoteStatePrepared
	stuck := filepath.Join(dir, e.ID+".json")
	if err := os.MkdirAll(stuck, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverPendingPromote(home, e); err == nil {
		t.Error("expected error from RemovePromoteJournal on non-empty dir")
	}
}

// TestRecoverPendingPromote_CanonicalCopiedRemoveAllError covers the
// RemoveAll error branch by pointing canonical at a path whose parent is
// read-only — but the more portable trick is to set CanonicalPath to a file
// path whose parent dir has been chmoded to 0500.
func TestRecoverPendingPromote_CanonicalCopiedRemoveAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod read-only parent semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod restrictions")
	}
	home := journalAgentsHome(t)
	parent := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "canonical")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	// Make parent read-only so RemoveAll cannot delete target.
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	e := sampleJournalEntry()
	e.ID = "cc"
	e.State = PromoteStateCanonicalCopied
	e.CanonicalPath = target
	if _, err := RecoverPendingPromote(home, e); err == nil {
		t.Error("expected RemoveAll to fail on read-only parent")
	}
}

// TestRecoverPendingPromote_CanonicalCopiedRemoveJournalError forces the
// RemovePromoteJournal error after a successful canonical remove, by parking
// the journal entry at a non-empty directory path.
func TestRecoverPendingPromote_CanonicalCopiedRemoveJournalError(t *testing.T) {
	home := journalAgentsHome(t)
	dir := promoteJournalDirPath(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	e := sampleJournalEntry()
	e.ID = "cc2"
	e.State = PromoteStateCanonicalCopied
	// Canonical points to an absent path — RemoveAll on a missing path is nil,
	// so we proceed to RemovePromoteJournal.
	e.CanonicalPath = filepath.Join(t.TempDir(), "absent")
	stuck := filepath.Join(dir, e.ID+".json")
	if err := os.MkdirAll(stuck, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverPendingPromote(home, e); err == nil {
		t.Error("expected RemovePromoteJournal error after canonical remove")
	}
}

// TestRecoverPendingPromote_SourceRemovedSymlinkError covers the Symlink
// failure branch by pointing SourcePath at an existing file — os.Symlink
// refuses to overwrite.
func TestRecoverPendingPromote_SourceRemovedSymlinkError(t *testing.T) {
	home := journalAgentsHome(t)
	tmp := t.TempDir()
	canonical := filepath.Join(tmp, "canonical")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(tmp, "src-exists")
	if err := os.WriteFile(source, []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	e := sampleJournalEntry()
	e.ID = "sr"
	e.State = PromoteStateSourceRemoved
	e.SourcePath = source
	e.CanonicalPath = canonical
	if _, err := RecoverPendingPromote(home, e); err == nil {
		t.Error("expected Symlink to fail when source path is occupied")
	} else if !strings.Contains(err.Error(), "recreating symlink") {
		t.Errorf("expected wrapped symlink error, got %v", err)
	}
}

// TestRecoverPendingPromote_SourceRemovedRemoveJournalError covers the
// RemovePromoteJournal error after a successful symlink, by parking the
// journal entry at a non-empty directory path.
func TestRecoverPendingPromote_SourceRemovedRemoveJournalError(t *testing.T) {
	home := journalAgentsHome(t)
	dir := promoteJournalDirPath(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	canonical := filepath.Join(tmp, "canonical")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(tmp, "fresh-src")
	e := sampleJournalEntry()
	e.ID = "sr2"
	e.State = PromoteStateSourceRemoved
	e.SourcePath = source
	e.CanonicalPath = canonical
	stuck := filepath.Join(dir, e.ID+".json")
	if err := os.MkdirAll(stuck, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverPendingPromote(home, e); err == nil {
		t.Error("expected RemovePromoteJournal error after symlink")
	}
}

// TestRecoverPendingPromote_TerminalRemoveJournalError covers the journal-
// remove error branch in the terminal-state arm.
func TestRecoverPendingPromote_TerminalRemoveJournalError(t *testing.T) {
	home := journalAgentsHome(t)
	dir := promoteJournalDirPath(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	e := sampleJournalEntry()
	e.ID = "term"
	e.State = PromoteStateRCSaved
	stuck := filepath.Join(dir, e.ID+".json")
	if err := os.MkdirAll(stuck, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverPendingPromote(home, e); err == nil {
		t.Error("expected RemovePromoteJournal error on terminal state")
	}
}
