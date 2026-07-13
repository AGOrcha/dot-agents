package scoring

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// mustTime parses an RFC3339 timestamp for test setup, failing the test on a
// malformed literal.
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("mustTime(%q): %v", s, err)
	}
	return ts.UTC()
}

// approx reports whether two floats are within a small epsilon.
func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestScanTranscriptWindowClaude(t *testing.T) {
	// Window (03:30, 05:00] selects the 04:00 token turn and the 04:05 / 04:10
	// tool-result turns; the 03:00 and 09:00 turns fall outside it.
	start := mustTime(t, "2026-05-17T03:30:00Z")
	end := mustTime(t, "2026-05-17T05:00:00Z")

	got, err := scanTranscriptWindow(filepath.Join("testdata", "backfill_claude"), start, end)
	if err != nil {
		t.Fatalf("scanTranscriptWindow() = %v, want nil", err)
	}

	if got.InputTokens != 20 || got.OutputTokens != 8 {
		t.Errorf("tokens = in %d / out %d, want 20 / 8", got.InputTokens, got.OutputTokens)
	}
	if got.CacheReadTokens != 300 || got.CacheCreationTokens != 100 {
		t.Errorf("cache = read %d / creation %d, want 300 / 100",
			got.CacheReadTokens, got.CacheCreationTokens)
	}
	if got.Turns != 1 {
		t.Errorf("Turns = %d, want 1", got.Turns)
	}
	if got.ToolCalls != 3 || got.ToolErrors != 1 {
		t.Errorf("tool calls = %d / errors = %d, want 3 / 1", got.ToolCalls, got.ToolErrors)
	}
	if !approx(got.cacheHitRate(), 0.75) {
		t.Errorf("cacheHitRate = %g, want 0.75", got.cacheHitRate())
	}
	if !approx(got.toolErrorRate(), 1.0/3.0) {
		t.Errorf("toolErrorRate = %g, want 1/3", got.toolErrorRate())
	}
}

func TestScanTranscriptWindowClaudeOpenLeft(t *testing.T) {
	// A zero start opens the window on the left, so the 03:00 turn is included.
	end := mustTime(t, "2026-05-17T05:00:00Z")
	got, err := scanTranscriptWindow(filepath.Join("testdata", "backfill_claude"), time.Time{}, end)
	if err != nil {
		t.Fatalf("scanTranscriptWindow() = %v, want nil", err)
	}
	// 03:00 turn (creation 100) + 04:00 turn (read 300, creation 100).
	if got.Turns != 2 {
		t.Errorf("Turns = %d, want 2 with an open-left window", got.Turns)
	}
	if got.CacheCreationTokens != 200 || got.CacheReadTokens != 300 {
		t.Errorf("cache = creation %d / read %d, want 200 / 300",
			got.CacheCreationTokens, got.CacheReadTokens)
	}
}

func TestScanTranscriptWindowCodex(t *testing.T) {
	// Window (03:30, 05:00] selects the two real token_count turns; the
	// null-info line, the non-token payload, and the 09:00 turn are skipped.
	start := mustTime(t, "2026-05-17T03:30:00Z")
	end := mustTime(t, "2026-05-17T05:00:00Z")

	got, err := scanTranscriptWindow(filepath.Join("testdata", "backfill_codex"), start, end)
	if err != nil {
		t.Fatalf("scanTranscriptWindow() = %v, want nil", err)
	}
	if got.Turns != 2 {
		t.Fatalf("Turns = %d, want 2 (null-info line must be skipped)", got.Turns)
	}
	// Turn 1: input 500, cached 400 -> read 400, creation 100.
	// Turn 2: input 200, cached 200 -> read 200, creation 0.
	if got.InputTokens != 700 || got.OutputTokens != 60 {
		t.Errorf("tokens = in %d / out %d, want 700 / 60", got.InputTokens, got.OutputTokens)
	}
	if got.CacheReadTokens != 600 || got.CacheCreationTokens != 100 {
		t.Errorf("cache = read %d / creation %d, want 600 / 100",
			got.CacheReadTokens, got.CacheCreationTokens)
	}
	if !approx(got.cacheHitRate(), 600.0/700.0) {
		t.Errorf("cacheHitRate = %g, want 600/700", got.cacheHitRate())
	}
	// Codex lines carry no tool-error flag.
	if got.ToolCalls != 0 {
		t.Errorf("ToolCalls = %d, want 0 for a Codex-only directory", got.ToolCalls)
	}
}

func TestScanTranscriptWindowMixedHarness(t *testing.T) {
	// A directory mixing a Claude and a Codex transcript: both fold together.
	start := mustTime(t, "2026-05-17T03:30:00Z")
	end := mustTime(t, "2026-05-17T05:00:00Z")

	got, err := scanTranscriptWindow(filepath.Join("testdata", "backfill_mixed"), start, end)
	if err != nil {
		t.Fatalf("scanTranscriptWindow() = %v, want nil", err)
	}
	// Claude turn: read 150, creation 50; Codex turn: read 50, creation 50.
	if got.Turns != 2 {
		t.Errorf("Turns = %d, want 2 (one Claude, one Codex)", got.Turns)
	}
	if got.CacheReadTokens != 200 || got.CacheCreationTokens != 100 {
		t.Errorf("cache = read %d / creation %d, want 200 / 100",
			got.CacheReadTokens, got.CacheCreationTokens)
	}
	if got.ToolCalls != 1 || got.ToolErrors != 1 {
		t.Errorf("tool calls = %d / errors = %d, want 1 / 1", got.ToolCalls, got.ToolErrors)
	}
}

func TestScanTranscriptWindowNoOverlap(t *testing.T) {
	// A window that no turn falls into yields a zero-Turns total.
	start := mustTime(t, "2026-05-18T00:00:00Z")
	end := mustTime(t, "2026-05-18T01:00:00Z")
	got, err := scanTranscriptWindow(filepath.Join("testdata", "backfill_claude"), start, end)
	if err != nil {
		t.Fatalf("scanTranscriptWindow() = %v, want nil", err)
	}
	if got.Turns != 0 || got.ToolCalls != 0 {
		t.Errorf("got %+v, want all-zero totals", got)
	}
}

func TestScanTranscriptWindowEmptyDir(t *testing.T) {
	got, err := scanTranscriptWindow(filepath.Join("testdata", "backfill_empty"),
		time.Time{}, mustTime(t, "2026-05-17T05:00:00Z"))
	if err != nil {
		t.Fatalf("scanTranscriptWindow(empty) = %v, want nil", err)
	}
	if got.Turns != 0 {
		t.Errorf("Turns = %d, want 0 for a dir with no .jsonl files", got.Turns)
	}
}

func TestScanTranscriptWindowMissingDir(t *testing.T) {
	_, err := scanTranscriptWindow(filepath.Join("testdata", "no_such_dir"),
		time.Time{}, time.Now())
	if err == nil {
		t.Fatal("scanTranscriptWindow(missing dir) = nil, want error")
	}
}

func TestZeroDenominators(t *testing.T) {
	var empty transcriptTotals
	if empty.cacheHitRate() != 0 {
		t.Errorf("cacheHitRate of zero totals = %g, want 0", empty.cacheHitRate())
	}
	if empty.toolErrorRate() != 0 {
		t.Errorf("toolErrorRate of zero totals = %g, want 0", empty.toolErrorRate())
	}
}

func TestParseTranscriptTime(t *testing.T) {
	if _, ok := parseTranscriptTime(""); ok {
		t.Error("parseTranscriptTime(\"\") ok = true, want false")
	}
	if _, ok := parseTranscriptTime("not-a-time"); ok {
		t.Error("parseTranscriptTime(garbage) ok = true, want false")
	}
	ts, ok := parseTranscriptTime("2026-05-17T03:50:55.399Z")
	if !ok {
		t.Fatal("parseTranscriptTime(valid) ok = false, want true")
	}
	if ts.Location() != time.UTC {
		t.Errorf("parsed time location = %v, want UTC", ts.Location())
	}
}

func TestInWindow(t *testing.T) {
	start := mustTime(t, "2026-05-17T03:00:00Z")
	end := mustTime(t, "2026-05-17T05:00:00Z")
	cases := []struct {
		name string
		ts   string
		want bool
	}{
		{"before start", "2026-05-17T02:00:00Z", false},
		{"exactly start (exclusive)", "2026-05-17T03:00:00Z", false},
		{"inside", "2026-05-17T04:00:00Z", true},
		{"exactly end (inclusive)", "2026-05-17T05:00:00Z", true},
		{"after end", "2026-05-17T06:00:00Z", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inWindow(mustTime(t, c.ts), start, end); got != c.want {
				t.Errorf("inWindow(%s) = %v, want %v", c.ts, got, c.want)
			}
		})
	}
	// A zero start opens the window on the left.
	if !inWindow(mustTime(t, "2000-01-01T00:00:00Z"), time.Time{}, end) {
		t.Error("inWindow with zero start should accept an early timestamp")
	}
}

func TestBackfillWindowNativeTokens(t *testing.T) {
	// A record with native session_tokens takes its cache_hit_rate directly;
	// the transcript is still consulted for the tool-error rate.
	rec := IterationRecord{
		Iteration:     7,
		SessionTokens: &TokenUsage{CacheHitRate: 0.42},
	}
	win := IterationWindow{
		Iteration: 7,
		Start:     mustTime(t, "2026-05-17T03:30:00Z"),
		End:       mustTime(t, "2026-05-17T05:00:00Z"),
	}
	got, err := backfillWindow(rec, win, filepath.Join("testdata", "backfill_claude"))
	if err != nil {
		t.Fatalf("backfillWindow() = %v, want nil", err)
	}
	if !got.TokenEfficiency.Present || !approx(got.TokenEfficiency.SubScore, 0.42) {
		t.Errorf("TokenEfficiency = %+v, want present sub-score 0.42", got.TokenEfficiency)
	}
	if !got.ToolErrorRatePresent || !approx(got.ToolErrorRate, 1.0/3.0) {
		t.Errorf("ToolErrorRate = %g present=%v, want 1/3 present",
			got.ToolErrorRate, got.ToolErrorRatePresent)
	}
}

func TestBackfillWindowFromTranscript(t *testing.T) {
	// No native session_tokens: cache_hit_rate is backfilled from the window.
	rec := IterationRecord{Iteration: 8}
	win := IterationWindow{
		Iteration: 8,
		Start:     mustTime(t, "2026-05-17T03:30:00Z"),
		End:       mustTime(t, "2026-05-17T05:00:00Z"),
	}
	got, err := backfillWindow(rec, win, filepath.Join("testdata", "backfill_claude"))
	if err != nil {
		t.Fatalf("backfillWindow() = %v, want nil", err)
	}
	if !got.TokenEfficiency.Present || !approx(got.TokenEfficiency.SubScore, 0.75) {
		t.Errorf("TokenEfficiency = %+v, want present sub-score 0.75", got.TokenEfficiency)
	}
}

func TestBackfillWindowAbsent(t *testing.T) {
	// No native tokens and no transcript coverage: token_efficiency is absent
	// and the tool-error rate is reported not-present.
	rec := IterationRecord{Iteration: 9}
	win := IterationWindow{
		Iteration: 9,
		Start:     mustTime(t, "2026-05-18T00:00:00Z"),
		End:       mustTime(t, "2026-05-18T01:00:00Z"),
	}
	got, err := backfillWindow(rec, win, filepath.Join("testdata", "backfill_claude"))
	if err != nil {
		t.Fatalf("backfillWindow() = %v, want nil", err)
	}
	if got.TokenEfficiency.Present {
		t.Errorf("TokenEfficiency = %+v, want absent", got.TokenEfficiency)
	}
	if got.ToolErrorRatePresent {
		t.Errorf("ToolErrorRatePresent = true, want false with no tool calls")
	}
}

func TestBackfillWindowScanError(t *testing.T) {
	rec := IterationRecord{Iteration: 1}
	win := IterationWindow{Iteration: 1, End: time.Now()}
	if _, err := backfillWindow(rec, win, filepath.Join("testdata", "no_such_dir")); err == nil {
		t.Fatal("backfillWindow(missing dir) = nil, want error")
	}
}

func TestResolveWindows(t *testing.T) {
	// A throwaway git repo with two commits; resolveWindows must chain their
	// timestamps into adjacent (Start, End] windows.
	repo := bfNewGitRepo(t)
	sha1 := commitInRepo(t, repo, "first")
	sha2 := commitInRepo(t, repo, "second")

	records := []IterationRecord{
		{Iteration: 2, Commit: sha2},
		{Iteration: 1, Commit: sha1}, // deliberately out of order
	}
	windows := resolveWindows(records, repo)
	if len(windows) != 2 {
		t.Fatalf("resolved %d windows, want 2", len(windows))
	}
	w1, w2 := windows[1], windows[2]
	if !w1.Start.IsZero() {
		t.Errorf("iteration 1 Start = %v, want zero (open-left)", w1.Start)
	}
	if !w1.End.Equal(w2.Start) {
		t.Errorf("iteration 2 Start (%v) must equal iteration 1 End (%v)", w2.Start, w1.End)
	}
	if !w2.End.After(w2.Start) {
		t.Errorf("iteration 2 window is not forward: %v .. %v", w2.Start, w2.End)
	}
}

func TestResolveWindowsUnresolvableCommit(t *testing.T) {
	repo := bfNewGitRepo(t)
	sha1 := commitInRepo(t, repo, "first")

	records := []IterationRecord{
		{Iteration: 1, Commit: sha1},
		{Iteration: 2, Commit: ""},                 // empty SHA
		{Iteration: 3, Commit: "deadbeefdeadbeef"}, // never existed
	}
	windows := resolveWindows(records, repo)
	if _, ok := windows[1]; !ok {
		t.Error("iteration 1 should resolve")
	}
	if _, ok := windows[2]; ok {
		t.Error("iteration 2 (empty SHA) should not resolve")
	}
	if _, ok := windows[3]; ok {
		t.Error("iteration 3 (bogus SHA) should not resolve")
	}
}

func TestCommitTime(t *testing.T) {
	if _, ok := commitTime("/tmp", ""); ok {
		t.Error("commitTime with empty SHA ok = true, want false")
	}
	repo := bfNewGitRepo(t)
	sha := commitInRepo(t, repo, "only")
	if _, ok := commitTime(repo, sha); !ok {
		t.Error("commitTime of a real commit ok = false, want true")
	}
	if _, ok := commitTime(repo, "deadbeef"); ok {
		t.Error("commitTime of a bogus SHA ok = true, want false")
	}
}

func TestBackfillIterations(t *testing.T) {
	repo := bfNewGitRepo(t)
	sha1 := commitInRepo(t, repo, "first")
	sha2 := commitInRepo(t, repo, "second")

	// Place transcript turns so each lands in its iteration's window. The first
	// iteration has an open-left window; the second is (commitTime1, commitTime2].
	t1, _ := commitTime(repo, sha1)
	t2, _ := commitTime(repo, sha2)
	dir := t.TempDir()
	// Early turn: cacheRead 0 / creation 100 -> hit rate 0, lands open-left.
	writeClaudeTurn(t, dir, "early.jsonl", t1.Add(-time.Minute), 0, 100, false)
	// Late turn: cacheRead 70 / creation 30 -> hit rate 0.7, lands in window 2.
	writeClaudeTurn(t, dir, "late.jsonl", t1.Add(time.Minute), 70, 30, true)

	records := []IterationRecord{
		{Iteration: 1, Commit: sha1},
		{Iteration: 2, Commit: sha2},
	}
	_ = t2
	got, err := BackfillIterations(records, repo, dir)
	if err != nil {
		t.Fatalf("BackfillIterations() = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	// Iteration 1: open-left window catches the early turn (creation 100).
	if !got[0].TokenEfficiency.Present || !approx(got[0].TokenEfficiency.SubScore, 0) {
		t.Errorf("iter 1 TokenEfficiency = %+v, want present sub-score 0", got[0].TokenEfficiency)
	}
	// Iteration 2: window catches the late turn (read 70, creation 30) and its
	// erroring tool result.
	if !got[1].TokenEfficiency.Present || !approx(got[1].TokenEfficiency.SubScore, 0.7) {
		t.Errorf("iter 2 TokenEfficiency = %+v, want present sub-score 0.7", got[1].TokenEfficiency)
	}
	if !got[1].ToolErrorRatePresent || !approx(got[1].ToolErrorRate, 1.0) {
		t.Errorf("iter 2 ToolErrorRate = %g present=%v, want 1.0 present",
			got[1].ToolErrorRate, got[1].ToolErrorRatePresent)
	}
}

func TestBackfillIterationsNativeWins(t *testing.T) {
	repo := bfNewGitRepo(t)
	sha := commitInRepo(t, repo, "only")
	records := []IterationRecord{
		{Iteration: 1, Commit: sha, SessionTokens: &TokenUsage{CacheHitRate: 0.9}},
	}
	got, err := BackfillIterations(records, repo, t.TempDir())
	if err != nil {
		t.Fatalf("BackfillIterations() = %v, want nil", err)
	}
	if !approx(got[0].TokenEfficiency.SubScore, 0.9) {
		t.Errorf("native session_tokens ignored: sub-score = %g, want 0.9",
			got[0].TokenEfficiency.SubScore)
	}
}

func TestBackfillIterationsUnresolvableCommit(t *testing.T) {
	repo := bfNewGitRepo(t)
	records := []IterationRecord{{Iteration: 1, Commit: "deadbeef"}}
	got, err := BackfillIterations(records, repo, t.TempDir())
	if err != nil {
		t.Fatalf("BackfillIterations() = %v, want nil", err)
	}
	if got[0].TokenEfficiency.Present {
		t.Errorf("unresolvable commit should yield an absent signal, got %+v",
			got[0].TokenEfficiency)
	}
}

func TestBackfillIterationsMissingTranscriptDir(t *testing.T) {
	// A missing or empty transcript root is skipped, not fatal.
	repo := bfNewGitRepo(t)
	sha := commitInRepo(t, repo, "only")
	records := []IterationRecord{{Iteration: 1, Commit: sha}}
	got, err := BackfillIterations(records, repo,
		filepath.Join("testdata", "no_such_dir"), "")
	if err != nil {
		t.Fatalf("BackfillIterations() = %v, want nil", err)
	}
	if got[0].TokenEfficiency.Present {
		t.Errorf("no transcript should yield an absent signal, got %+v",
			got[0].TokenEfficiency)
	}
}

func TestBackfillIterationsUnreadableTranscriptRoot(t *testing.T) {
	// A permission-denied transcript root degrades the same as a missing one
	// (not fatal, absent signal) but must now be logged so the swallow is
	// distinguishable from legitimate absence. Deny traversal on the PARENT
	// dir: os.Stat(child) needs execute on the parent, not on the child
	// itself, so chmod-ing the child would not reproduce the failure.
	parent := t.TempDir()
	dir := filepath.Join(parent, "transcripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, parent)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	repo := bfNewGitRepo(t)
	sha := commitInRepo(t, repo, "only")
	records := []IterationRecord{{Iteration: 1, Commit: sha}}
	got, err := BackfillIterations(records, repo, dir)
	if err != nil {
		t.Fatalf("BackfillIterations() = %v, want nil (permission-denied root degrades, not fatal)", err)
	}
	if got[0].TokenEfficiency.Present {
		t.Errorf("unreadable transcript root should yield an absent signal, got %+v",
			got[0].TokenEfficiency)
	}
	if !strings.Contains(buf.String(), "transcript root") {
		t.Errorf("expected a warning log naming the unreadable transcript root, got %q", buf.String())
	}
}

func TestBackfillIterationsMultipleDirs(t *testing.T) {
	// Token counts from two transcript roots merge into one window total.
	repo := bfNewGitRepo(t)
	sha := commitInRepo(t, repo, "only")
	ct, _ := commitTime(repo, sha)

	dirA, dirB := t.TempDir(), t.TempDir()
	writeClaudeTurn(t, dirA, "a.jsonl", ct.Add(-time.Minute), 60, 40, false)
	writeClaudeTurn(t, dirB, "b.jsonl", ct.Add(-2*time.Minute), 40, 60, true)

	records := []IterationRecord{{Iteration: 1, Commit: sha}}
	got, err := BackfillIterations(records, repo, dirA, dirB)
	if err != nil {
		t.Fatalf("BackfillIterations() = %v, want nil", err)
	}
	// Merged cache: read 40+60=100, creation 60+40=100 -> hit rate 0.5.
	if !approx(got[0].TokenEfficiency.SubScore, 0.5) {
		t.Errorf("merged sub-score = %g, want 0.5", got[0].TokenEfficiency.SubScore)
	}
	if !got[0].ToolErrorRatePresent || !approx(got[0].ToolErrorRate, 0.5) {
		t.Errorf("merged ToolErrorRate = %g, want 0.5 (one of two tool calls errored)",
			got[0].ToolErrorRate)
	}
}

func TestBackfillIterationsScanError(t *testing.T) {
	// An unreadable .jsonl file inside a real transcript dir surfaces an error.
	// testutil.MakeFileUnreadable handles the platform difference (POSIX
	// chmod 0 vs Windows exclusive-share handle); see its godoc for the
	// rationale.
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked.jsonl")
	if err := os.WriteFile(locked, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	testutil.MakeFileUnreadable(t, locked)

	repo := bfNewGitRepo(t)
	sha := commitInRepo(t, repo, "only")
	records := []IterationRecord{{Iteration: 1, Commit: sha}}
	if _, err := BackfillIterations(records, repo, dir); err == nil {
		t.Fatal("BackfillIterations(unreadable file) = nil, want error")
	}
}

// TestBackfillRealData is the slice's integration assertion: when the real
// ~/.claude / ~/.codex transcripts and the dot-agents git history are present,
// the whole iteration log backfills without error. It skips otherwise, mirroring
// TestLoadIterationLogRealData.
func TestBackfillRealData(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	claudeDir := filepath.Join(home, ".claude", "projects",
		"-Users-nikashp-Documents-dot-agents")
	repoDir := filepath.Join("..", "..")
	logDir := filepath.Join(repoDir, ".agents", "active", "iteration-log")

	for _, p := range []string{claudeDir, logDir, filepath.Join(repoDir, ".git")} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("real-data path not present: %s", p)
		}
	}

	records, err := LoadIterationLog(logDir)
	if err != nil {
		t.Fatalf("LoadIterationLog(real data) = %v", err)
	}
	got, err := BackfillIterations(records, repoDir, claudeDir)
	if err != nil {
		t.Fatalf("BackfillIterations(real data) = %v, want nil", err)
	}
	var present int
	for _, b := range got {
		if b.TokenEfficiency.Present {
			present++
		}
	}
	t.Logf("backfilled %d/%d iterations with a token-efficiency signal",
		present, len(got))
}

// --- git fixture helpers ---------------------------------------------------

// bfNewGitRepo creates a throwaway git repository in a temp dir and returns its
// path. The repo is removed when the test ends.
func bfNewGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bfRunGit(t, dir, "init", "-q")
	bfRunGit(t, dir, "config", "user.email", "test@example.com")
	bfRunGit(t, dir, "config", "user.name", "Test")
	bfRunGit(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

// commitSeq gives each test's commits a strictly increasing, second-resolution
// timestamp so windows never collapse — git's %cI is second-precision, and two
// real-time commits in the same test would otherwise share a second.
var commitSeq int

// commitInRepo writes a unique file and commits it at a synthetic, strictly
// increasing timestamp, returning the new SHA.
func commitInRepo(t *testing.T, dir, label string) string {
	t.Helper()
	file := filepath.Join(dir, label+".txt")
	if err := os.WriteFile(file, []byte(label), 0o644); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
	commitSeq++
	// One-hour spacing leaves room to place transcript turns inside a window.
	stamp := time.Date(2026, 1, 1, commitSeq, 0, 0, 0, time.UTC).Format(time.RFC3339)
	bfRunGit(t, dir, "add", ".")
	bfRunGitEnv(t, dir,
		[]string{"GIT_AUTHOR_DATE=" + stamp, "GIT_COMMITTER_DATE=" + stamp},
		"commit", "-q", "-m", label)
	out := bfRunGit(t, dir, "rev-parse", "HEAD")
	return string(trimNewline(out))
}

// bfRunGit runs a git command in dir, failing the test on error.
func bfRunGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	return bfRunGitEnv(t, dir, nil, args...)
}

// bfRunGitEnv runs a git command in dir with extra environment variables.
func bfRunGitEnv(t *testing.T, dir string, extraEnv []string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

// trimNewline drops a single trailing newline from git output.
func trimNewline(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		return b[:n-1]
	}
	return b
}

// writeClaudeTurn writes a one-line Claude transcript file with one assistant
// token turn and (optionally) one tool result; cacheRead/cacheCreation set the
// turn's cache split.
func writeClaudeTurn(t *testing.T, dir, name string, ts time.Time, cacheRead, cacheCreation int, toolError bool) {
	t.Helper()
	type usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	}
	assistant := map[string]any{
		"type":      "assistant",
		"timestamp": ts.UTC().Format(time.RFC3339Nano),
		"message": map[string]any{
			"role":  "assistant",
			"usage": usage{InputTokens: 1, OutputTokens: 1, CacheReadInputTokens: cacheRead, CacheCreationInputTokens: cacheCreation},
		},
	}
	user := map[string]any{
		"type":      "user",
		"timestamp": ts.UTC().Add(time.Second).Format(time.RFC3339Nano),
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "x", "is_error": toolError},
			},
		},
	}
	var buf []byte
	for _, obj := range []map[string]any{assistant, user} {
		b, err := json.Marshal(obj)
		if err != nil {
			t.Fatalf("marshal transcript line: %v", err)
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, name), buf, 0o644); err != nil {
		t.Fatalf("write transcript %s: %v", name, err)
	}
}
