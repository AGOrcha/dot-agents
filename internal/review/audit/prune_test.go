package audit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// restorePruneSeams snapshots the prune-specific filesystem seams and restores
// them after the test, mirroring restoreSeams for the shared ones.
func restorePruneSeams(t *testing.T) {
	t.Helper()
	origDir, origRemove := readDirFunc, removeFunc
	t.Cleanup(func() { readDirFunc, removeFunc = origDir, origRemove })
}

// seedYearArchives appends one record per year through the real rotation path,
// leaving a dated archive for each fully-rotated earlier year and the final year
// live in the active log.
func seedYearArchives(t *testing.T, l *Log, years ...int) {
	t.Helper()
	for _, y := range years {
		if _, err := l.Append(Event{
			Actor:  "admin@example.com",
			Role:   "admin",
			Action: ActionUserCreate,
			Target: "user/seed",
			Now:    time.Date(y, 2, 3, 4, 5, 6, 0, time.UTC),
		}); err != nil {
			t.Fatalf("seed year %d: %v", y, err)
		}
	}
}

func TestLeadingYear(t *testing.T) {
	cases := []struct {
		in   string
		year int
		ok   bool
	}{
		{"2025", 2025, true},
		{"2025.1", 2025, true}, // second same-year (size-triggered) rotation
		{"notyear", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		y, ok := leadingYear(c.in)
		if y != c.year || ok != c.ok {
			t.Fatalf("leadingYear(%q) = %d,%v want %d,%v", c.in, y, ok, c.year, c.ok)
		}
	}
}

func TestPruneArchivesBeforeCompacts(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2023, 2024, 2026)
	base := archiveBase(l.Path())

	removed, err := l.PruneArchivesBefore(2024)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	want := map[string]bool{base + ".2022.jsonl": true, base + ".2023.jsonl": true}
	if len(removed) != 2 {
		t.Fatalf("removed = %v, want 2 archives", removed)
	}
	for _, p := range removed {
		if !want[p] {
			t.Fatalf("unexpected removed archive %q", p)
		}
	}
	for _, gone := range []string{
		base + ".2022.jsonl", base + ".2022.jsonl.head",
		base + ".2023.jsonl", base + ".2023.jsonl.head",
	} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected %s removed, stat err=%v", gone, err)
		}
	}
	for _, kept := range []string{base + ".2024.jsonl", l.Path()} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("expected %s kept: %v", kept, err)
		}
	}
	if res, err := Open(base + ".2024.jsonl").Verify(); err != nil || !res.OK {
		t.Fatalf("kept archive should still verify: %+v err=%v", res, err)
	}
}

func TestPruneArchivesBeforeSkipsCorruptArchive(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2021, 2022, 2026)
	base := archiveBase(l.Path())

	// Rewrite the 2021 archive's record so it stays valid JSON but no longer
	// matches its head anchor: a chain break (OK=false), not a parse error.
	corruptPath := base + ".2021.jsonl"
	data, err := os.ReadFile(corruptPath)
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if err := os.WriteFile(corruptPath, bytes.Replace(data, []byte("user/seed"), []byte("user/evil"), 1), 0o600); err != nil {
		t.Fatalf("corrupt archive: %v", err)
	}

	removed, err := l.PruneArchivesBefore(2025)
	if !errors.Is(err, ErrCorruptArchive) {
		t.Fatalf("want ErrCorruptArchive, got %v", err)
	}
	if !strings.Contains(err.Error(), corruptPath) {
		t.Fatalf("error should name %s: %v", corruptPath, err)
	}
	if len(removed) != 1 || removed[0] != base+".2022.jsonl" {
		t.Fatalf("removed = %v, want only the intact 2022 archive", removed)
	}
	if _, err := os.Stat(corruptPath); err != nil {
		t.Fatalf("corrupt archive must be left in place: %v", err)
	}
}

func TestPruneArchivesBeforeMissingDirIsNoop(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	readDirFunc = func(string) ([]os.DirEntry, error) { return nil, os.ErrNotExist }
	l := tempLog(t)
	removed, err := l.PruneArchivesBefore(2025)
	if err != nil || len(removed) != 0 {
		t.Fatalf("missing dir should be a no-op: removed=%v err=%v", removed, err)
	}
}

func TestPruneArchivesBeforeReadDirError(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	readDirFunc = func(string) ([]os.DirEntry, error) { return nil, errors.New("io boom") }
	l := tempLog(t)
	_, err := l.PruneArchivesBefore(2025)
	if err == nil || !strings.Contains(err.Error(), "read log dir") {
		t.Fatalf("want read-log-dir error, got %v", err)
	}
}

func TestPruneArchivesBeforeLockError(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	boom := errors.New("lock stuck")
	acquireFileLock = func(string) (func() error, error) { return nil, boom }
	l := tempLog(t)
	if _, err := l.PruneArchivesBefore(2025); !errors.Is(err, boom) {
		t.Fatalf("want lock error, got %v", err)
	}
}

func TestPruneArchivesBeforeRemoveArchiveError(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	orig := removeFunc
	removeFunc = func(p string) error {
		if strings.HasSuffix(p, ".2022.jsonl") {
			return errors.New("rm boom")
		}
		return orig(p)
	}
	removed, err := l.PruneArchivesBefore(2025)
	if err == nil || !strings.Contains(err.Error(), "remove archive") {
		t.Fatalf("want remove-archive error, got %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none before the failure", removed)
	}
}

func TestPruneArchivesBeforeRemoveHeadError(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	orig := removeFunc
	removeFunc = func(p string) error {
		if strings.HasSuffix(p, ".head") {
			return errors.New("head boom")
		}
		return orig(p)
	}
	_, err := l.PruneArchivesBefore(2025)
	if err == nil || !strings.Contains(err.Error(), "remove archive head") {
		t.Fatalf("want remove-head error, got %v", err)
	}
}

func TestPruneArchivesBeforeHeadMissingIsOK(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	base := archiveBase(l.Path())
	if err := os.Remove(base + ".2022.jsonl.head"); err != nil {
		t.Fatalf("drop head sidecar: %v", err)
	}
	removed, err := l.PruneArchivesBefore(2025)
	if err != nil {
		t.Fatalf("prune with absent head sidecar: %v", err)
	}
	if len(removed) != 1 || removed[0] != base+".2022.jsonl" {
		t.Fatalf("removed = %v, want the 2022 archive", removed)
	}
}

func TestPruneArchivesBeforeVerifyError(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	orig := readFile
	readFile = func(p string) ([]byte, error) {
		if strings.HasSuffix(p, ".2022.jsonl") {
			return nil, errors.New("read boom")
		}
		return orig(p)
	}
	_, err := l.PruneArchivesBefore(2025)
	if err == nil || !strings.Contains(err.Error(), "verify archive") || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("want verify-archive error, got %v", err)
	}
}

func TestPruneArchivesBeforeSkipsNonArchiveSiblings(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2020, 2026) // archive 2020, active 2026
	dir := filepath.Dir(l.Path())
	base := archiveBase(l.Path())

	// Siblings that share the directory but must never be pruned.
	for _, name := range []string{"audit.log.notyear.jsonl", "other.jsonl", "audit.log.2019.jsonl.bak"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o600); err != nil {
			t.Fatalf("write sibling %s: %v", name, err)
		}
	}
	// A directory whose name looks like an archive exercises the IsDir skip.
	if err := os.Mkdir(filepath.Join(dir, "audit.log.2018.jsonl"), 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}

	removed, err := l.PruneArchivesBefore(2025)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 1 || removed[0] != base+".2020.jsonl" {
		t.Fatalf("removed = %v, want only the 2020 archive", removed)
	}
	for _, kept := range []string{
		filepath.Join(dir, "audit.log.notyear.jsonl"),
		filepath.Join(dir, "other.jsonl"),
		filepath.Join(dir, "audit.log.2019.jsonl.bak"),
		filepath.Join(dir, "audit.log.2018.jsonl"),
	} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("sibling %s should survive prune: %v", kept, err)
		}
	}
}
