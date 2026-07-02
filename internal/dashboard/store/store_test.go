package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// --- fixture helpers -------------------------------------------------------

func testStore(t *testing.T, roots ...string) *DiskStore {
	t.Helper()
	return New(roots, WithLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// iterOpts describes a v2 iteration-record fixture. withTokens adds a
// session_tokens block carrying cacheHitRate; withVerifier adds one passing
// verifier. Bundled into a struct so the writer stays under the argument limit.
type iterOpts struct {
	n            int
	sid          string
	harness      string
	cacheHitRate float64
	withTokens   bool
	withVerifier bool
}

// writeV2Iter writes a v2 iteration record described by o.
func writeV2Iter(t *testing.T, dir string, o iterOpts) {
	t.Helper()
	var b bytes.Buffer
	fmt.Fprintf(&b, "schema_version: 2\niteration: %d\ndate: \"2026-05-01\"\nwave: \"w1\"\ntask_id: \"task-%d\"\ncommit: \"commit%d\"\nfiles_changed: %d\nlines_added: %d\nlines_removed: %d\n", o.n, o.n, o.n, o.n, o.n*10, o.n*2)
	fmt.Fprintf(&b, "agent:\n  session_id: \"%s\"\n  harness: \"%s\"\n  model: \"m-%s\"\n", o.sid, o.harness, o.harness)
	fmt.Fprintf(&b, "impl:\n  summary: \"impl %d\"\n  retries: %d\n", o.n, o.n)
	if o.withTokens {
		fmt.Fprintf(&b, "session_tokens:\n  input_tokens: 100\n  output_tokens: 200\n  cache_read_tokens: 900\n  cache_creation_tokens: 100\n  cache_hit_rate: %g\n", o.cacheHitRate)
	}
	if o.withVerifier {
		b.WriteString("verifiers:\n  - type: test\n    status: pass\n    gate_passed: true\n    tests_added: 3\n    retries: 0\n")
	}
	writeFile(t, dir, fmt.Sprintf("iter-%d.yaml", o.n), b.String())
}

// plainIter writes a minimal v2 record (no tokens, no verifier) — the common
// fixture shape across the tests.
func plainIter(t *testing.T, dir string, n int, sid, harness string) {
	t.Helper()
	writeV2Iter(t, dir, iterOpts{n: n, sid: sid, harness: harness})
}

func writeIterScore(t *testing.T, dir string, n int, value float64, band string) {
	t.Helper()
	sc := scoring.Score{
		Iteration:     n,
		RubricVersion: scoring.RubricVersion,
		Value:         value,
		Scored:        true,
		Band:          band,
		Breakdown: []scoring.SignalContribution{{
			Signal:          scoring.SignalLanded,
			Label:           "Landed on master",
			Present:         true,
			SubScore:        1,
			NominalWeight:   0.2,
			EffectiveWeight: 1,
			Contribution:    value,
		}},
	}
	if _, err := scoring.WriteIterationScore(dir, sc); err != nil {
		t.Fatalf("write iter score: %v", err)
	}
}

func writeSessionScore(t *testing.T, dir, sid string, iters []int, value float64, band string, refs []scoring.SessionIterRef) {
	t.Helper()
	ss := scoring.SessionScore{
		SessionID:     sid,
		RubricVersion: scoring.RubricVersion,
		Iterations:    iters,
		Scored:        true,
		Value:         value,
		Band:          band,
		PerIteration:  refs,
	}
	if _, err := scoring.WriteSessionScore(dir, ss); err != nil {
		t.Fatalf("write session score: %v", err)
	}
}

// standardRoot builds a root with two scored sessions and one unaddressable
// (empty session_id) v1 record:
//   - sess-a: iters 1,2,3 all scored, session sidecar present, tokens on 2 & 3.
//   - sess-b: iter 5, NO score sidecar (staleness window), harness codex.
//   - a v1 flat iter-9 with no session id (skipped from runs).
func standardRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	plainIter(t, dir, 1, "sess-a", "claude-code")
	writeV2Iter(t, dir, iterOpts{n: 2, sid: "sess-a", harness: "claude-code", cacheHitRate: 0.8, withTokens: true, withVerifier: true})
	writeV2Iter(t, dir, iterOpts{n: 3, sid: "sess-a", harness: "claude-code", cacheHitRate: 0.6, withTokens: true})
	writeIterScore(t, dir, 1, 0.9, "excellent")
	writeIterScore(t, dir, 2, 0.8, "good")
	writeIterScore(t, dir, 3, 0.7, "good")
	writeSessionScore(t, dir, "sess-a", []int{1, 2, 3}, 0.8, "good", []scoring.SessionIterRef{
		{Iteration: 1, Scored: true, Value: 0.9, Band: "excellent"},
		{Iteration: 2, Scored: true, Value: 0.8, Band: "good"},
		{Iteration: 3, Scored: true, Value: 0.7, Band: "good"},
	})
	plainIter(t, dir, 5, "sess-b", "codex")
	writeFile(t, dir, "iter-9.yaml", "schema_version: 1\niteration: 9\ndate: \"2026-05-09\"\ncommit: \"flat9\"\ntests_total_pass: true\n")
	return dir
}

// --- ListRuns --------------------------------------------------------------

// runsByID indexes runs by session id and asserts each summary omits the
// detail-only per_iteration array.
func runsByID(t *testing.T, runs []Run) map[string]Run {
	t.Helper()
	byID := make(map[string]Run, len(runs))
	for _, r := range runs {
		byID[r.SessionID] = r
		if r.PerIteration != nil {
			t.Errorf("summary must omit per_iteration for %s", r.SessionID)
		}
	}
	return byID
}

// assertScoredRunA checks the fully-scored sess-a projection.
func assertScoredRunA(t *testing.T, a Run) {
	t.Helper()
	if !a.Scored || a.Score == nil || *a.Score != 0.8 || a.Band != "good" {
		t.Errorf("sess-a score projection wrong: %+v", a)
	}
	if a.IterationCount != 3 || a.Harness != "claude-code" || a.Wave != "w1" {
		t.Errorf("sess-a meta wrong: %+v", a)
	}
	if a.FirstIteration == nil || *a.FirstIteration != 1 || a.LastIteration == nil || *a.LastIteration != 3 {
		t.Errorf("sess-a iteration bounds wrong: %+v", a)
	}
	if a.MeanCacheHitRate == nil || *a.MeanCacheHitRate != 0.7 { // mean(0.8,0.6)
		t.Errorf("sess-a mean cache hit rate wrong: %v", a.MeanCacheHitRate)
	}
	if a.LastUpdate == nil {
		t.Error("sess-a last_update should be set")
	}
}

// assertUnscoredRunB checks the staleness-window sess-b projection.
func assertUnscoredRunB(t *testing.T, b Run) {
	t.Helper()
	if b.Scored || b.Score != nil || b.Band != bandUnscored || b.RubricVersion != "" {
		t.Errorf("sess-b should be unscored: %+v", b)
	}
	if b.MeanCacheHitRate != nil {
		t.Errorf("sess-b has no token telemetry, want nil cache rate: %v", b.MeanCacheHitRate)
	}
}

func TestListRunsBasic(t *testing.T) {
	s := testStore(t, standardRoot(t))
	runs, err := s.ListRuns(context.Background(), RunFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 addressable runs, got %d", len(runs))
	}
	byID := runsByID(t, runs)
	assertScoredRunA(t, byID["sess-a"])
	assertUnscoredRunB(t, byID["sess-b"])
}

func TestListRunsFilterBandAndHarness(t *testing.T) {
	s := testStore(t, standardRoot(t))
	good, _ := s.ListRuns(context.Background(), RunFilter{Band: "good"})
	if len(good) != 1 || good[0].SessionID != "sess-a" {
		t.Fatalf("band filter want [sess-a], got %+v", good)
	}
	codex, _ := s.ListRuns(context.Background(), RunFilter{Harness: "codex"})
	if len(codex) != 1 || codex[0].SessionID != "sess-b" {
		t.Fatalf("harness filter want [sess-b], got %+v", codex)
	}
	none, _ := s.ListRuns(context.Background(), RunFilter{Harness: "nope"})
	if len(none) != 0 {
		t.Fatalf("unknown harness want empty, got %+v", none)
	}
}

func TestListRunsSort(t *testing.T) {
	s := testStore(t, standardRoot(t))
	ctx := context.Background()
	// score desc: scored sess-a (0.8) before unscored sess-b (-1).
	byScore, _ := s.ListRuns(ctx, RunFilter{Sort: "score", Order: "desc"})
	if byScore[0].SessionID != "sess-a" || byScore[1].SessionID != "sess-b" {
		t.Errorf("score desc order wrong: %s,%s", byScore[0].SessionID, byScore[1].SessionID)
	}
	// score asc: unscored first.
	byScoreAsc, _ := s.ListRuns(ctx, RunFilter{Sort: "score", Order: "asc"})
	if byScoreAsc[0].SessionID != "sess-b" {
		t.Errorf("score asc should put unscored first, got %s", byScoreAsc[0].SessionID)
	}
	// iteration_count desc: sess-a(3) before sess-b(1).
	byCount, _ := s.ListRuns(ctx, RunFilter{Sort: "iteration_count", Order: "desc"})
	if byCount[0].SessionID != "sess-a" {
		t.Errorf("iteration_count desc wrong: %s", byCount[0].SessionID)
	}
	// session_id asc.
	bySid, _ := s.ListRuns(ctx, RunFilter{Sort: "session_id", Order: "asc"})
	if bySid[0].SessionID != "sess-a" || bySid[1].SessionID != "sess-b" {
		t.Errorf("session_id asc wrong: %+v", bySid)
	}
}

func TestListRunsSortTieBreak(t *testing.T) {
	// Two sessions with identical iteration_count -> tie broken by session_id asc
	// regardless of order direction.
	dir := t.TempDir()
	plainIter(t, dir, 1, "zeta", "h")
	plainIter(t, dir, 2, "alpha", "h")
	s := testStore(t, dir)
	runs, _ := s.ListRuns(context.Background(), RunFilter{Sort: "iteration_count", Order: "desc"})
	if runs[0].SessionID != "alpha" || runs[1].SessionID != "zeta" {
		t.Fatalf("tie-break must be session_id asc, got %s,%s", runs[0].SessionID, runs[1].SessionID)
	}
}

func TestListRunsPagination(t *testing.T) {
	s := testStore(t, standardRoot(t))
	ctx := context.Background()
	page, _ := s.ListRuns(ctx, RunFilter{Limit: 1, Sort: "session_id", Order: "asc"})
	if len(page) != 1 || page[0].SessionID != "sess-a" {
		t.Fatalf("limit=1 page wrong: %+v", page)
	}
	page2, _ := s.ListRuns(ctx, RunFilter{Limit: 1, Offset: 1, Sort: "session_id", Order: "asc"})
	if len(page2) != 1 || page2[0].SessionID != "sess-b" {
		t.Fatalf("offset=1 page wrong: %+v", page2)
	}
	empty, _ := s.ListRuns(ctx, RunFilter{Offset: 99})
	if len(empty) != 0 {
		t.Fatalf("offset past end must be empty, got %+v", empty)
	}
}

func TestListRunsLimitCap(t *testing.T) {
	f := normalizeFilter(RunFilter{Limit: 100000})
	if f.Limit != maxLimit {
		t.Fatalf("limit should cap at %d, got %d", maxLimit, f.Limit)
	}
	f2 := normalizeFilter(RunFilter{Offset: -5})
	if f2.Offset != 0 || f2.Limit != defaultLimit || f2.Sort != "last_update" || f2.Order != "desc" {
		t.Fatalf("normalize defaults wrong: %+v", f2)
	}
}

func TestListRunsEmptyRoot(t *testing.T) {
	// Cold start: nonexistent + empty roots -> 200-equivalent empty slice.
	s := testStore(t, filepath.Join(t.TempDir(), "does-not-exist"), t.TempDir())
	runs, err := s.ListRuns(context.Background(), RunFilter{})
	if err != nil || len(runs) != 0 {
		t.Fatalf("empty roots want ([],nil), got %v,%v", runs, err)
	}
}

// --- GetRun ----------------------------------------------------------------

func TestGetRunDetail(t *testing.T) {
	s := testStore(t, standardRoot(t))
	run, err := s.GetRun(context.Background(), "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(run.PerIteration) != 3 {
		t.Fatalf("detail must include 3 per_iteration refs, got %d", len(run.PerIteration))
	}
	if run.PerIteration[0].Iteration != 1 || run.PerIteration[0].Score == nil || *run.PerIteration[0].Score != 0.9 {
		t.Errorf("per_iteration[0] wrong: %+v", run.PerIteration[0])
	}
}

func TestGetRunNotFound(t *testing.T) {
	s := testStore(t, standardRoot(t))
	if _, err := s.GetRun(context.Background(), "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestGetRunPerIterationDerivedWhenNoSessionSidecar(t *testing.T) {
	// sess-c has per-iteration sidecars but NO session sidecar -> refs derived
	// from the iter score sidecars (and unscored where absent).
	dir := t.TempDir()
	plainIter(t, dir, 1, "sess-c", "h")
	plainIter(t, dir, 2, "sess-c", "h")
	writeIterScore(t, dir, 1, 0.75, "good")
	// iter-2 has no score sidecar -> unscored ref.
	s := testStore(t, dir)
	run, err := s.GetRun(context.Background(), "sess-c")
	if err != nil {
		t.Fatal(err)
	}
	if run.Scored { // no session sidecar -> unscored aggregate
		t.Errorf("no session sidecar should leave run unscored: %+v", run)
	}
	if len(run.PerIteration) != 2 {
		t.Fatalf("want 2 derived refs, got %d", len(run.PerIteration))
	}
	if !run.PerIteration[0].Scored || run.PerIteration[0].Score == nil {
		t.Errorf("ref[0] should be scored: %+v", run.PerIteration[0])
	}
	if run.PerIteration[1].Scored || run.PerIteration[1].Band != bandUnscored {
		t.Errorf("ref[1] should be unscored: %+v", run.PerIteration[1])
	}
}

// --- ListIterations --------------------------------------------------------

func TestListIterations(t *testing.T) {
	s := testStore(t, standardRoot(t))
	its, err := s.ListIterations(context.Background(), "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(its) != 3 {
		t.Fatalf("want 3 iterations, got %d", len(its))
	}
	// ascending
	if its[0].Iteration != 1 || its[2].Iteration != 3 {
		t.Errorf("iterations not ascending: %+v", its)
	}
	// summary omits detail-only fields
	if its[0].Breakdown != nil || its[0].Verifiers != nil {
		t.Errorf("summary must omit breakdown/verifiers: %+v", its[0])
	}
	// token usage present on iter-2
	if its[1].TokenUsage == nil || its[1].TokenUsage.CacheHitRate != 0.8 {
		t.Errorf("iter-2 token usage wrong: %+v", its[1].TokenUsage)
	}
	if its[0].TokenUsage != nil {
		t.Errorf("iter-1 has no tokens: %+v", its[0].TokenUsage)
	}
}

func TestListIterationsStalenessWindow(t *testing.T) {
	s := testStore(t, standardRoot(t))
	its, err := s.ListIterations(context.Background(), "sess-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(its) != 1 {
		t.Fatalf("want 1 iteration, got %d", len(its))
	}
	it := its[0]
	if it.Scored || it.Score != nil || it.Band != bandUnscored || it.RubricVersion != "" {
		t.Errorf("missing sidecar must degrade to unscored/null: %+v", it)
	}
}

func TestListIterationsUnknownSession(t *testing.T) {
	s := testStore(t, standardRoot(t))
	if _, err := s.ListIterations(context.Background(), "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// --- GetIteration ----------------------------------------------------------

func TestGetIterationDetailDefaultRoot(t *testing.T) {
	root := standardRoot(t)
	s := testStore(t, root)
	it, err := s.GetIteration(context.Background(), "", 2) // empty -> active root
	if err != nil {
		t.Fatal(err)
	}
	if !it.Scored || it.Score == nil || *it.Score != 0.8 {
		t.Errorf("iter-2 score wrong: %+v", it)
	}
	if len(it.Breakdown) != 1 || it.Breakdown[0].Signal != "landed" {
		t.Errorf("iter-2 breakdown wrong: %+v", it.Breakdown)
	}
	if len(it.Verifiers) != 1 || it.Verifiers[0].Type != "test" || !it.Verifiers[0].GatePassed {
		t.Errorf("iter-2 verifiers wrong: %+v", it.Verifiers)
	}
	if it.Retries != 2 { // impl.retries == iteration number in fixture
		t.Errorf("retries projection wrong: %d", it.Retries)
	}
}

// TestIterationDetailRecomputeFieldsPendingT06 pins the t02 read-layer boundary:
// IterationDetail DECLARES the recompute-derived fields (integrity, objective,
// integrity_observation_count, transcript_turn_count) so the payload is
// shape-complete per API.md, but the raw read layer leaves them empty/null. They
// are populated by t06's recompute-on-miss path — this asserts the t02 boundary,
// NOT that the fields are permanently empty or non-conformant.
func TestIterationDetailRecomputeFieldsPendingT06(t *testing.T) {
	s := testStore(t, standardRoot(t))
	it, err := s.GetIteration(context.Background(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if it.Integrity != nil || it.Objective != nil || it.IntegrityObservationCount != 0 || it.TranscriptTurnCount != nil {
		t.Errorf("t02 read layer must leave recompute-derived fields empty pending t06, got: %+v", it)
	}
}

// TestGetIterationRejectsUnlistedRoot proves iter_log_dir cannot widen the
// store beyond its configured roots: a well-formed but unlisted directory
// (even one that is itself a valid iter-log) is rejected, and a traversal that
// escapes the configured root is rejected too.
func TestGetIterationRejectsUnlistedRoot(t *testing.T) {
	configured := standardRoot(t)
	// A separate directory that IS a valid iter-log but was not configured.
	outside := t.TempDir()
	plainIter(t, outside, 1, "sneaky", "h")

	s := testStore(t, configured)
	if _, err := s.GetIteration(context.Background(), outside, 1); !errors.Is(err, ErrRootNotAllowed) {
		t.Fatalf("unlisted root must be rejected with ErrRootNotAllowed, got %v", err)
	}
	// A traversal that normalizes outside the configured root is also rejected.
	escape := filepath.Join(configured, "..", filepath.Base(outside))
	if _, err := s.GetIteration(context.Background(), escape, 1); !errors.Is(err, ErrRootNotAllowed) {
		t.Fatalf("traversal escaping the root must be rejected, got %v", err)
	}
	// A ".."-containing path that normalizes back TO the configured root is allowed.
	viaDotDot := filepath.Join(configured, "..", filepath.Base(configured))
	if _, err := s.GetIteration(context.Background(), viaDotDot, 2); err != nil {
		t.Fatalf("normalized-to-root path should resolve, got %v", err)
	}
}

func TestGetIterationExplicitRoot(t *testing.T) {
	root := standardRoot(t)
	s := testStore(t, root)
	it, err := s.GetIteration(context.Background(), root, 3)
	if err != nil {
		t.Fatal(err)
	}
	if it.Iteration != 3 {
		t.Fatalf("want iter 3, got %d", it.Iteration)
	}
}

func TestGetIterationNotFound(t *testing.T) {
	s := testStore(t, standardRoot(t))
	if _, err := s.GetIteration(context.Background(), "", 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestGetIterationUnscoredDetail(t *testing.T) {
	// Detail for an iteration with no sidecar: non-score fields populated,
	// breakdown omitted, scored false.
	s := testStore(t, standardRoot(t))
	it, err := s.GetIteration(context.Background(), "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if it.Scored || it.Breakdown != nil || it.Commit != "commit5" {
		t.Errorf("unscored detail wrong: %+v", it)
	}
}

// --- Rubric / Health -------------------------------------------------------

func TestRubric(t *testing.T) {
	s := testStore(t, standardRoot(t))
	doc, err := s.Rubric(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != scoring.RubricVersion || doc.Combination != string(scoring.CombineWeightedMeanRenormalized) {
		t.Errorf("rubric header wrong: %+v", doc)
	}
	if len(doc.Signals) != len(scoring.DefaultRubric().Signals) || doc.Signals[0].ID != "landed" {
		t.Errorf("rubric signals wrong: %+v", doc.Signals)
	}
	if len(doc.Bands) != 4 || doc.Bands[0].Name != "excellent" {
		t.Errorf("rubric bands wrong: %+v", doc.Bands)
	}
}

func TestHealth(t *testing.T) {
	s := testStore(t, standardRoot(t))
	s.subscriberCount = func() int { return 4 }
	h, err := s.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" || h.RunCount != 2 || h.RubricVersion != scoring.RubricVersion {
		t.Errorf("health header wrong: %+v", h)
	}
	if h.IterationCount != 5 { // iters 1,2,3,5,9 (all records incl. flat)
		t.Errorf("iteration_count wrong: %d", h.IterationCount)
	}
	if h.SubscriberCount != 4 {
		t.Errorf("subscriber count injection failed: %d", h.SubscriberCount)
	}
	if h.LastIterLogMtime == nil {
		t.Error("last_iter_log_mtime should be set")
	}
	if len(h.Roots) != 1 {
		t.Errorf("roots wrong: %+v", h.Roots)
	}
}

func TestHealthEmpty(t *testing.T) {
	s := testStore(t, t.TempDir())
	h, err := s.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.RunCount != 0 || h.IterationCount != 0 || h.LastIterLogMtime != nil || h.SubscriberCount != 0 {
		t.Errorf("empty health wrong: %+v", h)
	}
}

// --- Resilience (spec R10) -------------------------------------------------

func TestResilientCorruptIterFile(t *testing.T) {
	dir := t.TempDir()
	plainIter(t, dir, 1, "sess-a", "h")
	writeFile(t, dir, "iter-2.yaml", "this: [is not: valid yaml\n") // breaks LoadIterationLog
	plainIter(t, dir, 3, "sess-a", "h")
	s := testStore(t, dir)
	its, err := s.ListIterations(context.Background(), "sess-a")
	if err != nil {
		t.Fatalf("corrupt iter file must not fail the list: %v", err)
	}
	if len(its) != 2 { // 1 and 3 survive, 2 skipped
		t.Fatalf("want 2 surviving iterations, got %d", len(its))
	}
}

func TestResilientCorruptSidecar(t *testing.T) {
	dir := t.TempDir()
	plainIter(t, dir, 1, "sess-a", "h")
	writeFile(t, dir, "iter-1.score.yaml", "{{{ not yaml") // corrupt sidecar -> skipped
	s := testStore(t, dir)
	its, err := s.ListIterations(context.Background(), "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(its) != 1 || its[0].Scored {
		t.Fatalf("corrupt sidecar should degrade to unscored: %+v", its)
	}
}

func TestResilientCorruptSessionSidecar(t *testing.T) {
	dir := t.TempDir()
	plainIter(t, dir, 1, "sess-a", "h")
	writeFile(t, dir, "session-sess-a.score.yaml", ":\n  bad") // corrupt session sidecar
	s := testStore(t, dir)
	run, err := s.GetRun(context.Background(), "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if run.Scored {
		t.Errorf("corrupt session sidecar should leave run unscored: %+v", run)
	}
}

// --- Multi-root / discovery edge cases -------------------------------------

func TestDuplicateSessionAcrossRoots(t *testing.T) {
	r1 := t.TempDir()
	r2 := t.TempDir()
	plainIter(t, r1, 1, "dup", "h")
	plainIter(t, r2, 2, "dup", "h")
	s := testStore(t, r1, r2)
	runs, _ := s.ListRuns(context.Background(), RunFilter{})
	if len(runs) != 1 || runs[0].IterLogDir != r1 {
		t.Fatalf("duplicate session should keep first root: %+v", runs)
	}
}

func TestEmptySessionIDSkipped(t *testing.T) {
	dir := t.TempDir()
	// only a flat v1 record with no session id
	writeFile(t, dir, "iter-1.yaml", "schema_version: 1\niteration: 1\ncommit: \"x\"\ntests_total_pass: true\n")
	s := testStore(t, dir)
	runs, _ := s.ListRuns(context.Background(), RunFilter{})
	if len(runs) != 0 {
		t.Fatalf("record with empty session id is unaddressable, want 0 runs, got %d", len(runs))
	}
}

func TestNormSchemaVersion(t *testing.T) {
	cases := map[int]int{0: 2, 1: 1, 2: 2, 7: 2}
	for in, want := range cases {
		if got := normSchemaVersion(in); got != want {
			t.Errorf("normSchemaVersion(%d)=%d want %d", in, got, want)
		}
	}
}

// --- Cache behaviour at the store surface ----------------------------------

func TestStoreCacheHitThenMtimeInvalidation(t *testing.T) {
	root := standardRoot(t)
	s := testStore(t, root)
	ctx := context.Background()
	_, _ = s.ListRuns(ctx, RunFilter{})
	_, _ = s.ListRuns(ctx, RunFilter{}) // second call should hit cache
	if m := s.CacheMetrics(); m.Hits == 0 {
		t.Fatalf("expected cache hits on repeat read, metrics=%+v", m)
	}
	// Add a brand-new session with a distinctly newer mtime -> snapshot invalidates.
	plainIter(t, root, 20, "sess-new", "h")
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "iter-20.yaml"), future, future); err != nil {
		t.Fatal(err)
	}
	runs, _ := s.ListRuns(ctx, RunFilter{})
	found := false
	for _, r := range runs {
		if r.SessionID == "sess-new" {
			found = true
		}
	}
	if !found {
		t.Fatal("mtime change should invalidate cache and surface the new session")
	}
}

// TestStoreInvalidatesOnOldFileChange is the regression for the fingerprint
// invalidation: a score-sidecar backfill on an OLD iteration whose mtime is
// BACKDATED strictly below the root's newest file must still invalidate the
// cached snapshot. A max-mtime cache key would miss this (the directory's
// newest mtime never changes); the per-file (name, mtime) fingerprint catches it.
func TestStoreInvalidatesOnOldFileChange(t *testing.T) {
	root := standardRoot(t)
	// Pin iter-5's record file far in the future so it is decisively the newest
	// file before AND after the backfill below.
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "iter-5.yaml"), future, future); err != nil {
		t.Fatal(err)
	}
	s := testStore(t, root)
	ctx := context.Background()

	// Warm the cache: iter-5 (sess-b) is unscored.
	before, err := s.ListIterations(ctx, "sess-b")
	if err != nil {
		t.Fatal(err)
	}
	if before[0].Scored {
		t.Fatalf("precondition: iter-5 should start unscored, got %+v", before[0])
	}
	_, _ = s.ListIterations(ctx, "sess-b") // ensure a cached read happened
	if m := s.CacheMetrics(); m.Hits == 0 {
		t.Fatalf("precondition: expected a cache hit, metrics=%+v", m)
	}

	// Backfill iter-5's score sidecar, then BACKDATE its mtime below the newest
	// file so the directory's max mtime is unchanged.
	writeIterScore(t, root, 5, 0.9, "excellent")
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "iter-5.score.yaml"), past, past); err != nil {
		t.Fatal(err)
	}

	after, err := s.ListIterations(ctx, "sess-b")
	if err != nil {
		t.Fatal(err)
	}
	if !after[0].Scored || after[0].Score == nil || *after[0].Score != 0.9 {
		t.Fatalf("backdated sidecar backfill must invalidate the cache and surface the score, got %+v", after[0])
	}
}

// TestStoreInvalidatesOnFileDeletion: deleting a non-newest file must also
// invalidate (the max mtime is unchanged; the fingerprint is not).
func TestStoreInvalidatesOnFileDeletion(t *testing.T) {
	root := standardRoot(t)
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "iter-5.yaml"), future, future); err != nil {
		t.Fatal(err)
	}
	s := testStore(t, root)
	ctx := context.Background()
	before, err := s.ListIterations(ctx, "sess-a")
	if err != nil || len(before) != 3 || !before[0].Scored {
		t.Fatalf("precondition: sess-a has 3 iters with iter-1 scored, got %v err=%v", before, err)
	}
	// Delete iter-1's score sidecar (not the newest file).
	if err := os.Remove(filepath.Join(root, "iter-1.score.yaml")); err != nil {
		t.Fatal(err)
	}
	after, err := s.ListIterations(ctx, "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Scored {
		t.Fatalf("deleting a non-newest sidecar must invalidate the cache, got %+v", after[0])
	}
}

func TestStoreEvictHooks(t *testing.T) {
	root := standardRoot(t)
	s := testStore(t, root)
	ctx := context.Background()
	_, _ = s.ListRuns(ctx, RunFilter{})
	s.Evict(root)
	if m := s.CacheMetrics(); m.Size != 0 {
		t.Fatalf("Evict(root) should drop the snapshot, size=%d", m.Size)
	}
	_, _ = s.ListRuns(ctx, RunFilter{})
	s.EvictAll()
	if m := s.CacheMetrics(); m.Size != 0 {
		t.Fatalf("EvictAll should clear the cache, size=%d", m.Size)
	}
}

func TestWithCacheOption(t *testing.T) {
	s := New([]string{standardRoot(t)}, WithCache(2, time.Second), WithLogger(nil))
	if s.cache.capacity != 2 {
		t.Fatalf("WithCache should set capacity, got %d", s.cache.capacity)
	}
	// WithLogger(nil) must not overwrite the default logger.
	if s.logger == nil {
		t.Fatal("nil logger option must be ignored")
	}
}

// --- Schema conformance (proves the projection matches the shipped contract) -

func schemaPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "schemas", name)
}

func compileSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	path := schemaPath(t, name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open schema %s: %v", name, err)
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("parse schema %s: %v", name, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		t.Fatalf("add schema %s: %v", name, err)
	}
	sch, err := c.Compile(name)
	if err != nil {
		t.Fatalf("compile schema %s: %v", name, err)
	}
	return sch
}

func validate(t *testing.T, sch *jsonschema.Schema, dto any) {
	t.Helper()
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := sch.Validate(inst); err != nil {
		t.Fatalf("DTO %s violates schema: %v\njson: %s", fmt.Sprintf("%T", dto), err, raw)
	}
}

func TestDTOsConformToShippedSchemas(t *testing.T) {
	s := testStore(t, standardRoot(t))
	ctx := context.Background()

	runSchema := compileSchema(t, "dashboard-run.schema.json")
	runs, _ := s.ListRuns(ctx, RunFilter{})
	for _, r := range runs {
		validate(t, runSchema, r) // summaries (both scored + unscored)
	}
	detail, _ := s.GetRun(ctx, "sess-a")
	validate(t, runSchema, detail) // detail with per_iteration

	iterSchema := compileSchema(t, "dashboard-iteration.schema.json")
	for _, sid := range []string{"sess-a", "sess-b"} {
		its, _ := s.ListIterations(ctx, sid)
		for _, it := range its {
			validate(t, iterSchema, it) // scored + unscored summaries
		}
	}
	full, _ := s.GetIteration(ctx, "", 2)
	validate(t, iterSchema, full) // detail w/ breakdown + verifiers + tokens
	unscored, _ := s.GetIteration(ctx, "", 5)
	validate(t, iterSchema, unscored)

	rubricSchema := compileSchema(t, "dashboard-rubric.schema.json")
	doc, _ := s.Rubric(ctx)
	validate(t, rubricSchema, doc)
}

// yamlSanity guards the fixture helper: the sidecars we write must round-trip
// through the real scoring decoder the store uses.
func TestFixtureSidecarsDecodeWithScoring(t *testing.T) {
	dir := t.TempDir()
	writeIterScore(t, dir, 1, 0.9, "excellent")
	data, err := os.ReadFile(filepath.Join(dir, "iter-1.score.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var ps scoring.PersistedScore
	if err := yaml.Unmarshal(data, &ps); err != nil || ps.Iteration != 1 || !ps.Scored {
		t.Fatalf("fixture iter score sidecar malformed: %+v err=%v", ps, err)
	}
}
