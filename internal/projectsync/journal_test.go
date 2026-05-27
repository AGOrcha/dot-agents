package projectsync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

// journalAgentsHome returns an isolated AGENTS_HOME tempdir for journal tests.
// Tests do not need a full project layout — only the journal subdir.
func journalAgentsHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", tmp)
	return tmp
}

func sampleJournalEntry() PromoteJournalEntry {
	return PromoteJournalEntry{
		Singular:      "skill",
		Bucket:        "skills",
		Name:          "alpha",
		SourcePath:    "/repo/.agents/skills/alpha",
		CanonicalPath: "/home/.agents/skills/proj/alpha",
	}
}

// TestPromoteJournal_BeginAdvanceRemove exercises the happy-path lifecycle.
func TestPromoteJournal_BeginAdvanceRemove(t *testing.T) {
	home := journalAgentsHome(t)

	path, err := BeginPromoteJournal(home, sampleJournalEntry())
	if err != nil {
		t.Fatalf("BeginPromoteJournal: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty journal path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("journal file missing on disk: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var e PromoteJournalEntry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.State != PromoteStatePrepared {
		t.Errorf("initial state = %q, want %q", e.State, PromoteStatePrepared)
	}
	if e.ID == "" {
		t.Error("expected ID to be populated by Begin")
	}
	if e.StartedAt.IsZero() {
		t.Error("expected StartedAt to be populated")
	}

	if err := AdvancePromoteJournal(path, PromoteStateCanonicalCopied); err != nil {
		t.Fatalf("AdvancePromoteJournal: %v", err)
	}
	data, _ = os.ReadFile(path)
	_ = json.Unmarshal(data, &e)
	if e.State != PromoteStateCanonicalCopied {
		t.Errorf("after advance state = %q, want %q", e.State, PromoteStateCanonicalCopied)
	}

	if err := RemovePromoteJournal(path); err != nil {
		t.Fatalf("RemovePromoteJournal: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("journal file should be removed, got err=%v", err)
	}
	// Idempotent on second remove.
	if err := RemovePromoteJournal(path); err != nil {
		t.Errorf("RemovePromoteJournal on missing file should be nil, got %v", err)
	}
}

// TestPromoteJournal_BeginWriteError exercises the osWriteFile seam: a synthetic
// write failure must propagate.
func TestPromoteJournal_BeginWriteError(t *testing.T) {
	home := journalAgentsHome(t)
	orig := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error {
		return errors.New("synthetic write failure")
	}
	t.Cleanup(func() { osWriteFile = orig })

	_, err := BeginPromoteJournal(home, sampleJournalEntry())
	if err == nil {
		t.Fatal("expected error from synthetic write failure")
	}
	if !strings.Contains(err.Error(), "synthetic write failure") {
		t.Errorf("missing synthetic error in %v", err)
	}
}

// TestPromoteJournal_AdvanceMissingFile verifies AdvancePromoteJournal surfaces
// a clear error when the file is gone.
func TestPromoteJournal_AdvanceMissingFile(t *testing.T) {
	if err := AdvancePromoteJournal(filepath.Join(t.TempDir(), "ghost.json"), PromoteStateSourceRemoved); err == nil {
		t.Fatal("expected error advancing missing journal file")
	}
}

// TestPromoteJournal_AdvanceCorruptFile verifies parse errors are reported.
func TestPromoteJournal_AdvanceCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := AdvancePromoteJournal(path, PromoteStateSourceRemoved); err == nil {
		t.Fatal("expected parse error for corrupt journal")
	}
}

// TestPromoteJournal_ListPendingFiltersTerminal verifies terminal states are
// filtered out and pending entries are sorted by StartedAt.
func TestPromoteJournal_ListPendingFiltersTerminal(t *testing.T) {
	home := journalAgentsHome(t)

	// Two pending + one terminal.
	p1, err := BeginPromoteJournal(home, PromoteJournalEntry{Singular: "skill", Bucket: "skills", Name: "older"})
	if err != nil {
		t.Fatal(err)
	}
	// Force StartedAt rewrite to ensure deterministic ordering.
	rewriteStartedAt(t, p1, time.Now().Add(-2*time.Hour))

	p2, err := BeginPromoteJournal(home, PromoteJournalEntry{Singular: "skill", Bucket: "skills", Name: "newer"})
	if err != nil {
		t.Fatal(err)
	}
	rewriteStartedAt(t, p2, time.Now().Add(-1*time.Hour))

	p3, err := BeginPromoteJournal(home, PromoteJournalEntry{Singular: "skill", Bucket: "skills", Name: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if err := AdvancePromoteJournal(p3, PromoteStateRCSaved); err != nil {
		t.Fatal(err)
	}

	pending, err := ListPendingPromoteJournals(home)
	if err != nil {
		t.Fatalf("ListPendingPromoteJournals: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d (%v)", len(pending), pending)
	}
	if pending[0].Name != "older" || pending[1].Name != "newer" {
		t.Errorf("expected older-first ordering, got %s / %s", pending[0].Name, pending[1].Name)
	}
}

// TestPromoteJournal_ListPendingMissingDir returns nil when the journal dir
// has never been created.
func TestPromoteJournal_ListPendingMissingDir(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	pending, err := ListPendingPromoteJournals(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error on missing dir, got %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected empty pending list, got %v", pending)
	}
}

// TestPromoteJournal_ConcurrentBeginUniqueIDs spawns many concurrent Begin
// calls and asserts every returned path is unique.
func TestPromoteJournal_ConcurrentBeginUniqueIDs(t *testing.T) {
	home := journalAgentsHome(t)
	const n = 32
	var wg sync.WaitGroup
	paths := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := BeginPromoteJournal(home, sampleJournalEntry())
			if err != nil {
				t.Errorf("BeginPromoteJournal: %v", err)
				return
			}
			paths <- p
		}()
	}
	wg.Wait()
	close(paths)

	seen := make(map[string]struct{}, n)
	for p := range paths {
		if _, dup := seen[p]; dup {
			t.Errorf("duplicate journal path: %s", p)
		}
		seen[p] = struct{}{}
	}
	if len(seen) != n {
		t.Errorf("expected %d unique paths, got %d", n, len(seen))
	}
}

// TestRecoverPendingPromote_Prepared deletes the journal and reports no work.
func TestRecoverPendingPromote_Prepared(t *testing.T) {
	home := journalAgentsHome(t)
	e := sampleJournalEntry()
	path, _ := BeginPromoteJournal(home, e)
	loaded := loadJournal(t, path)

	msg, err := RecoverPendingPromote(home, loaded)
	if err != nil {
		t.Fatalf("RecoverPendingPromote: %v", err)
	}
	if !strings.Contains(msg, "prepared") {
		t.Errorf("expected 'prepared' in msg, got %q", msg)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("journal should be removed for prepared state, got err=%v", err)
	}
}

// TestRecoverPendingPromote_CanonicalCopied removes the partial canonical dir.
func TestRecoverPendingPromote_CanonicalCopied(t *testing.T) {
	home := journalAgentsHome(t)
	canonical := filepath.Join(t.TempDir(), "canonical")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	e := sampleJournalEntry()
	e.CanonicalPath = canonical
	path, _ := BeginPromoteJournal(home, e)
	_ = AdvancePromoteJournal(path, PromoteStateCanonicalCopied)
	loaded := loadJournal(t, path)

	if _, err := RecoverPendingPromote(home, loaded); err != nil {
		t.Fatalf("RecoverPendingPromote: %v", err)
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Errorf("canonical should be removed, got err=%v", err)
	}
}

// TestRecoverPendingPromote_SourceRemoved recreates the symlink.
func TestRecoverPendingPromote_SourceRemoved(t *testing.T) {
	home := journalAgentsHome(t)
	tmp := t.TempDir()
	canonical := filepath.Join(tmp, "canonical")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(tmp, "src")
	e := sampleJournalEntry()
	e.SourcePath = source
	e.CanonicalPath = canonical
	path, _ := BeginPromoteJournal(home, e)
	_ = AdvancePromoteJournal(path, PromoteStateSourceRemoved)
	loaded := loadJournal(t, path)

	if _, err := RecoverPendingPromote(home, loaded); err != nil {
		t.Fatalf("RecoverPendingPromote: %v", err)
	}
	target, err := os.Readlink(source)
	if err != nil {
		t.Fatalf("expected symlink at source, got %v", err)
	}
	if target != canonical {
		t.Errorf("symlink target %q want %q", target, canonical)
	}
}

// TestRecoverPendingPromote_Symlinked surfaces a manual-rerun notice and does
// NOT delete the journal — only the user can complete the rc update.
func TestRecoverPendingPromote_Symlinked(t *testing.T) {
	home := journalAgentsHome(t)
	e := sampleJournalEntry()
	path, _ := BeginPromoteJournal(home, e)
	_ = AdvancePromoteJournal(path, PromoteStateSymlinked)
	loaded := loadJournal(t, path)

	msg, err := RecoverPendingPromote(home, loaded)
	if err != nil {
		t.Fatalf("RecoverPendingPromote: %v", err)
	}
	if !strings.Contains(msg, "rerun") {
		t.Errorf("expected rerun hint, got %q", msg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("journal should be preserved on symlinked state, got err=%v", err)
	}
}

// TestRecoverPendingPromote_TerminalStates deletes the journal for RCSaved and
// RolledBack states.
func TestRecoverPendingPromote_TerminalStates(t *testing.T) {
	home := journalAgentsHome(t)
	for _, state := range []string{PromoteStateRCSaved, PromoteStateRolledBack} {
		state := state
		t.Run(state, func(t *testing.T) {
			path, _ := BeginPromoteJournal(home, sampleJournalEntry())
			_ = AdvancePromoteJournal(path, state)
			loaded := loadJournal(t, path)
			if _, err := RecoverPendingPromote(home, loaded); err != nil {
				t.Fatalf("RecoverPendingPromote: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("journal should be removed for %s, got err=%v", state, err)
			}
		})
	}
}

// TestRecoverPendingPromote_UnknownState surfaces a clear error.
func TestRecoverPendingPromote_UnknownState(t *testing.T) {
	e := sampleJournalEntry()
	e.ID = "ghost"
	e.State = "not-a-real-state"
	_, err := RecoverPendingPromote(t.TempDir(), e)
	if err == nil || !strings.Contains(err.Error(), "unknown promote-journal state") {
		t.Errorf("expected unknown-state error, got %v", err)
	}
}

// loadJournal reads a journal file off disk for use in recovery tests.
func loadJournal(t *testing.T, path string) PromoteJournalEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loadJournal read: %v", err)
	}
	var e PromoteJournalEntry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatalf("loadJournal unmarshal: %v", err)
	}
	return e
}

// rewriteStartedAt rewrites StartedAt on an existing journal file so list-order
// tests stay deterministic without sleeping.
func rewriteStartedAt(t *testing.T, path string, when time.Time) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e PromoteJournalEntry
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	e.StartedAt = when
	out, _ := json.MarshalIndent(e, "", "  ")
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatal(err)
	}
}

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

	tmp := t.TempDir()
	regular := filepath.Join(tmp, "file")
	if err := os.WriteFile(regular, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

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

	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("nope"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}

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
	home := journalAgentsHome(t)
	dir := promoteJournalDirPath(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dir, "locked.json")
	if err := os.WriteFile(unreadable, []byte(`{}`), 0644); err != nil {
		t.Fatal(err)
	}
	// testutil.MakeFileUnreadable covers both POSIX (chmod 0) and Windows
	// (byte-range lock) and t.Skips on root-on-POSIX where mode bits are
	// bypassed.
	testutil.MakeFileUnreadable(t, unreadable)

	pending, err := ListPendingPromoteJournals(home)
	if err != nil {
		t.Fatalf("ListPendingPromoteJournals: %v", err)
	}

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
