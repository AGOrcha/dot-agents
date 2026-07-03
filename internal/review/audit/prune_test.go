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
	origDir, origRemove, origRename := readDirFunc, removeFunc, renameArchiveFunc
	t.Cleanup(func() {
		readDirFunc, removeFunc, renameArchiveFunc = origDir, origRemove, origRename
	})
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
		{"2025.1", 2025, true},    // second same-year (size-triggered) rotation
		{"2024.backup", 0, false}, // non-integer suffix must be rejected (Bug 1)
		{"2024.tmp", 0, false},    // another non-integer suffix variant
		{"notyear", 0, false},
		{"", 0, false},
		{"-2024", 0, false}, // signed year: 5 chars, rejected by length check
		{"-024", 0, false},  // signed 4-char: '-' not a digit, rejected by char check
		{"999", 0, false},   // fewer than 4 digits
		{"10000", 0, false}, // more than 4 digits
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
	if !errors.Is(err, ErrUnprunableArchive) {
		t.Fatalf("want ErrUnprunableArchive, got %v", err)
	}
	if !strings.Contains(err.Error(), corruptPath) || !strings.Contains(err.Error(), "corrupt chain") {
		t.Fatalf("error should name %s with a corrupt-chain reason: %v", corruptPath, err)
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

// assertBothPresentNoTombstones pins the both-present state: the archive and
// its head are at their real paths and NEITHER ".pruning" tombstone remains.
func assertBothPresentNoTombstones(t *testing.T, archivePath, headPath string) {
	t.Helper()
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive must be present at its real path: %v", err)
	}
	if _, err := os.Stat(headPath); err != nil {
		t.Fatalf("head must be present at its real path: %v", err)
	}
	for _, tomb := range []string{archivePath + pruningSuffix, headPath + pruningSuffix} {
		if _, err := os.Stat(tomb); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("no tombstone must remain (%s): stat err=%v", tomb, err)
		}
	}
}

// TestRemoveArchiveHeadTombstoneFailureIsAtomic — scenario (b): the FIRST
// transaction step (tombstone the head) fails. Nothing is moved or removed, the
// error names the step, and removed stays empty (both-present preserved).
func TestRemoveArchiveHeadTombstoneFailureIsAtomic(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	base := archiveBase(l.Path())
	archivePath := base + ".2022.jsonl"
	headPath := archivePath + headSuffix

	orig := renameArchiveFunc
	renameArchiveFunc = func(oldp, newp string) error {
		if strings.HasSuffix(oldp, ".2022.jsonl.head") {
			return errors.New("head tombstone boom")
		}
		return orig(oldp, newp)
	}
	removed, err := l.PruneArchivesBefore(2025)
	if err == nil || !strings.Contains(err.Error(), "tombstone archive head") {
		t.Fatalf("want tombstone-archive-head error, got %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none (first step failed)", removed)
	}
	assertBothPresentNoTombstones(t, archivePath, headPath)
}

// TestRemoveArchiveArchiveTombstoneFailureRollsBack — scenario (c): the head is
// tombstoned, then the archive tombstone fails. The transaction rolls the head
// BACK to both-present. This is the exact round-3 hazard (a valid archive left
// without its head) and the round-2 hazard (an orphaned head) — neither may
// occur: after rollback both files are at their real paths and removed is empty.
func TestRemoveArchiveArchiveTombstoneFailureRollsBack(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	base := archiveBase(l.Path())
	archivePath := base + ".2022.jsonl"
	headPath := archivePath + headSuffix

	orig := renameArchiveFunc
	renameArchiveFunc = func(oldp, newp string) error {
		// Fail only the archive tombstone (old path ends in .jsonl, not
		// .pruning); let the head tombstone AND the rollback rename through.
		if strings.HasSuffix(oldp, ".2022.jsonl") {
			return errors.New("archive tombstone boom")
		}
		return orig(oldp, newp)
	}
	removed, err := l.PruneArchivesBefore(2025)
	if err == nil || !strings.Contains(err.Error(), "tombstone archive") {
		t.Fatalf("want tombstone-archive error, got %v", err)
	}
	if strings.Contains(err.Error(), "CRITICAL") {
		t.Fatalf("rollback should have succeeded (no CRITICAL): %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none (rolled back)", removed)
	}
	// Rolled back to both-present: NO archive-without-head, NO orphan head.
	assertBothPresentNoTombstones(t, archivePath, headPath)
}

// TestRemoveArchiveDoubleFailureIsLoud — scenario (d): the archive tombstone
// fails AND the head rollback also fails. This is the one unavoidable
// inconsistent state; it MUST be surfaced as a loud, explicit error naming both
// paths — never silently — and removed must stay empty.
func TestRemoveArchiveDoubleFailureIsLoud(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	base := archiveBase(l.Path())
	archivePath := base + ".2022.jsonl"
	headPath := archivePath + headSuffix

	orig := renameArchiveFunc
	renameArchiveFunc = func(oldp, newp string) error {
		if strings.HasSuffix(oldp, ".2022.jsonl") { // archive tombstone
			return errors.New("archive tombstone boom")
		}
		if strings.HasSuffix(oldp, ".2022.jsonl.head"+pruningSuffix) { // rollback
			return errors.New("rollback boom")
		}
		return orig(oldp, newp)
	}
	removed, err := l.PruneArchivesBefore(2025)
	if err == nil || !strings.Contains(err.Error(), "CRITICAL") {
		t.Fatalf("want loud CRITICAL double-failure error, got %v", err)
	}
	if !strings.Contains(err.Error(), archivePath) || !strings.Contains(err.Error(), headPath) {
		t.Fatalf("double-failure error must name both the archive and head paths: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none on double failure", removed)
	}
	// Acknowledged inconsistent-but-LOUD state: the head is stranded at its
	// tombstone path and the archive remains — surfaced explicitly above.
	if _, statErr := os.Stat(headPath + pruningSuffix); statErr != nil {
		t.Fatalf("head should be stranded at the tombstone path: %v", statErr)
	}
	if _, statErr := os.Stat(archivePath); statErr != nil {
		t.Fatalf("archive should still be present: %v", statErr)
	}
}

// TestRemoveArchiveVanishedHeadIsToleratedNotFatal exercises the ENOENT branch:
// if the head sidecar vanishes (a race or crash) after verify judged the
// archive prunable but before the transaction tombstones it, the missing head
// is tolerated and the archive is still pruned.
func TestRemoveArchiveVanishedHeadIsToleratedNotFatal(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	base := archiveBase(l.Path())
	archivePath := base + ".2022.jsonl"

	orig := renameArchiveFunc
	renameArchiveFunc = func(oldp, newp string) error {
		if strings.HasSuffix(oldp, ".2022.jsonl.head") {
			return os.ErrNotExist
		}
		return orig(oldp, newp)
	}
	removed, err := l.PruneArchivesBefore(2025)
	if err != nil {
		t.Fatalf("vanished head must be tolerated, got %v", err)
	}
	if len(removed) != 1 || removed[0] != archivePath {
		t.Fatalf("removed = %v, want the 2022 archive", removed)
	}
	if _, statErr := os.Stat(archivePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("archive should be pruned: stat err=%v", statErr)
	}
}

// TestRemoveArchiveCommitThenTombstoneCruftIsReportedNotFatal — post-commit
// path: both tombstone renames succeed (transaction COMMITTED — neither file
// remains at its attestable path), but unlinking a ".pruning" tombstone fails.
// That is inert cruft: the archive is still counted in removed and the error
// wraps errTombstoneCruft, never a corruption.
func TestRemoveArchiveCommitThenTombstoneCruftIsReportedNotFatal(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	base := archiveBase(l.Path())
	archivePath := base + ".2022.jsonl"
	headPath := archivePath + headSuffix

	origRemove := removeFunc
	removeFunc = func(p string) error {
		// Fail BOTH post-commit tombstone unlinks (head and archive) so both
		// inert-cruft branches are exercised; the archive is already off its
		// attestable path, so accounting must still count it.
		if strings.HasSuffix(p, pruningSuffix) {
			return errors.New("tombstone unlink boom")
		}
		return origRemove(p)
	}
	removed, err := l.PruneArchivesBefore(2025)
	if err == nil || !errors.Is(err, errTombstoneCruft) {
		t.Fatalf("want errTombstoneCruft, got %v", err)
	}
	// Accurate accounting: the archive WAS pruned, so it is still counted.
	if len(removed) != 1 || removed[0] != archivePath {
		t.Fatalf("removed = %v, want the 2022 archive counted despite cruft", removed)
	}
	// Both-or-neither: neither the archive nor its head remains at its real path.
	if _, statErr := os.Stat(archivePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("archive must be gone from its attestable path: %v", statErr)
	}
	if _, statErr := os.Stat(headPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("head must be gone from its attestable path: %v", statErr)
	}
	// Both inert tombstones remain (the reported cruft) — no attestation harm.
	for _, tomb := range []string{archivePath + pruningSuffix, headPath + pruningSuffix} {
		if _, statErr := os.Stat(tomb); statErr != nil {
			t.Fatalf("tombstone should remain as inert cruft (%s): %v", tomb, statErr)
		}
	}
}

// TestPruneArchivesBeforeSkipsTornAppendArchive pins the tamper-evidence guard:
// an archive whose head anchor is one behind reads as OK+TornAppend, which is
// byte-indistinguishable from a forged out-of-band append, so prune must SKIP it
// (leave it in place) and report it rather than destroy the evidence.
func TestPruneArchivesBeforeSkipsTornAppendArchive(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	base := archiveBase(l.Path())
	tornArchive := base + ".2022.jsonl"
	// Drop the head anchor so the single-record archive reads as a torn (first)
	// append: chain intact (OK=true) but the anchor is one behind (TornAppend).
	if err := os.Remove(tornArchive + ".head"); err != nil {
		t.Fatalf("drop head anchor: %v", err)
	}
	if res, err := Open(tornArchive).Verify(); err != nil || !res.OK || !res.TornAppend {
		t.Fatalf("precondition: want OK+TornAppend, got %+v err=%v", res, err)
	}

	removed, err := l.PruneArchivesBefore(2025)
	if !errors.Is(err, ErrUnprunableArchive) {
		t.Fatalf("want ErrUnprunableArchive, got %v", err)
	}
	if !strings.Contains(err.Error(), tornArchive) || !strings.Contains(err.Error(), "torn-append") {
		t.Fatalf("error should name the torn archive with a torn-append reason: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("torn-append archive must not be pruned; removed=%v", removed)
	}
	if _, err := os.Stat(tornArchive); err != nil {
		t.Fatalf("torn-append archive must be left in place: %v", err)
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

// TestPruneArchivesBeforeDecoyBackupSiblingNotPruned pins Bug 1: a sibling
// whose name matches the archive stem and ends in .jsonl but whose mid-segment
// is "<year>.<non-integer>" (e.g. "2024.backup") must NOT be treated as a
// rotation archive. Before the fix, leadingYear("2024.backup") stripped the
// ".backup" suffix and returned year 2024, so a healthy-looking decoy would be
// silently compacted. After the fix, the non-integer suffix is rejected and the
// file is left untouched.
func TestPruneArchivesBeforeDecoyBackupSiblingNotPruned(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	// Seed a real 2020 archive and keep 2026 active.
	seedYearArchives(t, l, 2020, 2026)
	base := archiveBase(l.Path())
	dir := filepath.Dir(l.Path())

	// Create a decoy at audit.log.2024.backup.jsonl with a fully-anchored chain
	// (one valid record + head sidecar) so archiveSkipReason would mark it safe
	// to compact if it were incorrectly classified as a year-2024 archive.
	decoyPath := filepath.Join(dir, "audit.log.2024.backup.jsonl")
	decoy := Open(decoyPath)
	if _, err := decoy.Append(Event{
		Actor:  "admin@example.com",
		Role:   "admin",
		Action: ActionUserCreate,
		Target: "user/decoy",
		Now:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("write decoy archive: %v", err)
	}
	// Precondition: the decoy verifies clean so the old bug would have pruned it.
	if res, err := Open(decoyPath).Verify(); err != nil || !res.OK || res.TornAppend {
		t.Fatalf("decoy precondition: want clean verify, got %+v err=%v", res, err)
	}

	removed, err := l.PruneArchivesBefore(2025)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	// Only the genuine 2020 archive should be compacted.
	if len(removed) != 1 || removed[0] != base+".2020.jsonl" {
		t.Fatalf("removed = %v, want only the 2020 archive; decoy must be untouched", removed)
	}
	if _, err := os.Stat(decoyPath); err != nil {
		t.Fatalf("decoy %s must survive prune (was wrongly classified as year-2024 archive): %v", decoyPath, err)
	}
}

// TestPruneArchivesBeforeMidFailureLeavesNoPartialState pins BOTH bounced
// delete-ordering hazards at once, now that removeArchive is transactional
// rather than order-dependent: a mid-transaction failure must leave NEITHER
//   - round-2: an orphaned .head with its archive already gone (misreported), NOR
//   - round-3: a valid archive left WITHOUT its .head attestation anchor.
//
// The archive tombstone fails after the head was tombstoned; the transaction
// rolls back to both-present. removed is empty (accurate) and both files are
// intact at their real paths (neither hazard occurred).
func TestPruneArchivesBeforeMidFailureLeavesNoPartialState(t *testing.T) {
	restoreSeams(t)
	restorePruneSeams(t)
	l := tempLog(t)
	seedYearArchives(t, l, 2022, 2026)
	base := archiveBase(l.Path())
	archivePath := base + ".2022.jsonl"
	headPath := archivePath + headSuffix

	// Confirm both files exist before the prune attempt.
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive precondition: %v", err)
	}
	if _, err := os.Stat(headPath); err != nil {
		t.Fatalf("head precondition: %v", err)
	}

	orig := renameArchiveFunc
	renameArchiveFunc = func(oldp, newp string) error {
		// Fail the archive tombstone; the head tombstone and the rollback both
		// go through, so the transaction rewinds to both-present.
		if strings.HasSuffix(oldp, ".2022.jsonl") {
			return errors.New("archive tombstone boom")
		}
		return orig(oldp, newp)
	}

	removed, err := l.PruneArchivesBefore(2025)

	// Accurate removed count: nothing was fully compacted.
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want 0 when the transaction rolls back", removed)
	}
	if err == nil || !strings.Contains(err.Error(), "tombstone archive") {
		t.Fatalf("expected tombstone-archive error, got %v", err)
	}

	// No round-3 archive-without-head and no round-2 orphan: both intact, no
	// tombstone leftovers.
	assertBothPresentNoTombstones(t, archivePath, headPath)
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
