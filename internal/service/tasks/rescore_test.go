package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
	"github.com/AGOrcha/dot-agents/internal/service/state"
)

// oldRubricVersion is a rubric version guaranteed to differ from the active
// scoring.RubricVersion, used to seed a stale watermark.
const oldRubricVersion = "0.0.1-test"

// fakeBulkScorer records pipeline calls and returns canned results so run()'s
// version-delta/watermark/publish semantics are testable without the real
// (git-dependent) scoring pipeline.
type fakeBulkScorer struct {
	mu         sync.Mutex
	scoreCalls int
	writeCalls int
	result     bulkScores
	scoreErr   error
	writeErr   error
	// onScore, when set, runs inside ScoreAll — used to cancel the task
	// context mid-pass.
	onScore func()
}

func (f *fakeBulkScorer) ScoreAll(_, _ string) (bulkScores, error) {
	f.mu.Lock()
	f.scoreCalls++
	hook := f.onScore
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if f.scoreErr != nil {
		return bulkScores{}, f.scoreErr
	}
	return f.result, nil
}

func (f *fakeBulkScorer) WriteSidecars(_ string, _ bulkScores) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeCalls++
	return f.writeErr
}

func (f *fakeBulkScorer) calls() (score, write int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scoreCalls, f.writeCalls
}

// rescoreEnv bundles the moving parts every rescore run() test needs.
type rescoreEnv struct {
	iterLogDir string
	repoDir    string
	bus        *events.InProcBus
	fake       *fakeBulkScorer
	task       *rescoreTask
}

func newRescoreEnv(t *testing.T) *rescoreEnv {
	t.Helper()
	env := &rescoreEnv{
		iterLogDir: t.TempDir(),
		repoDir:    t.TempDir(),
		bus:        events.NewInProcBus(),
		fake: &fakeBulkScorer{result: bulkScores{
			scores: []scoring.Score{{Iteration: 1}, {Iteration: 2}},
		}},
	}
	t.Cleanup(func() { _ = env.bus.Close() })
	cfg := RescoreConfig{IterLogDir: env.iterLogDir, RepoDir: env.repoDir, Bus: env.bus}
	task, err := newRescoreTask(cfg, env.fake)
	if err != nil {
		t.Fatalf("newRescoreTask: %v", err)
	}
	env.task = task
	return env
}

// seedWatermark persists a rescore watermark at the canonical path.
func (env *rescoreEnv) seedWatermark(t *testing.T, version string) {
	t.Helper()
	wm := RescoreWatermark{RubricVersion: version}
	if err := state.Save(state.Path(env.repoDir, RescoreName), &wm); err != nil {
		t.Fatalf("seed watermark: %v", err)
	}
}

func (env *rescoreEnv) loadWatermark(t *testing.T) (RescoreWatermark, bool) {
	t.Helper()
	var wm RescoreWatermark
	found, err := state.Load(state.Path(env.repoDir, RescoreName), &wm)
	if err != nil {
		t.Fatalf("load watermark: %v", err)
	}
	return wm, found
}

func (env *rescoreEnv) subscribeDone(t *testing.T) <-chan events.Event {
	t.Helper()
	ch, unsub, err := env.bus.Subscribe(events.TopicRescoreDone)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(unsub)
	return ch
}

func recvRescoreDone(t *testing.T, ch <-chan events.Event) events.RescoreDone {
	t.Helper()
	select {
	case evt := <-ch:
		payload, ok := evt.Payload.(events.RescoreDone)
		if !ok {
			t.Fatalf("payload type = %T, want events.RescoreDone", evt.Payload)
		}
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rescore.done event")
		return events.RescoreDone{}
	}
}

func wantBulkCalls(t *testing.T, fake *fakeBulkScorer, score, write int) {
	t.Helper()
	gotScore, gotWrite := fake.calls()
	if gotScore != score || gotWrite != write {
		t.Fatalf("calls = (score %d, write %d), want (score %d, write %d)", gotScore, gotWrite, score, write)
	}
}

// NewRescore rejects incomplete configuration with the shared sentinel errors.
func TestNewRescoreValidation(t *testing.T) {
	bus := events.NewInProcBus()
	defer func() { _ = bus.Close() }()
	cases := []struct {
		name string
		cfg  RescoreConfig
		want error
	}{
		{"missing iter-log dir", RescoreConfig{RepoDir: "r", Bus: bus}, ErrNoIterLogDir},
		{"missing repo dir", RescoreConfig{IterLogDir: "d", Bus: bus}, ErrNoRepoDir},
		{"missing bus", RescoreConfig{IterLogDir: "d", RepoDir: "r"}, ErrNoBus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRescore(tc.cfg); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// The built task carries the contract shape: name, interval trigger at the
// 60s default, the default timeout, and a run function.
func TestNewRescoreTaskShape(t *testing.T) {
	bus := events.NewInProcBus()
	defer func() { _ = bus.Close() }()
	task, err := NewRescore(RescoreConfig{IterLogDir: t.TempDir(), RepoDir: t.TempDir(), Bus: bus})
	if err != nil {
		t.Fatalf("NewRescore: %v", err)
	}
	if task.Name != RescoreName {
		t.Errorf("Name = %q, want %q", task.Name, RescoreName)
	}
	trig, ok := task.Trigger.(*scheduler.IntervalTrigger)
	if !ok {
		t.Fatalf("Trigger type = %T, want *scheduler.IntervalTrigger", task.Trigger)
	}
	if trig.Every != defaultRescoreInterval {
		t.Errorf("interval = %v, want default %v", trig.Every, defaultRescoreInterval)
	}
	if task.Timeout != defaultRescoreTimeout {
		t.Errorf("Timeout = %v, want default %v", task.Timeout, defaultRescoreTimeout)
	}
	if task.RunFn == nil {
		t.Error("RunFn is nil")
	}
}

// Explicit interval and timeout override the defaults.
func TestNewRescoreExplicitIntervalAndTimeout(t *testing.T) {
	bus := events.NewInProcBus()
	defer func() { _ = bus.Close() }()
	cfg := RescoreConfig{
		IterLogDir: t.TempDir(),
		RepoDir:    t.TempDir(),
		Bus:        bus,
		Interval:   5 * time.Second,
		Timeout:    time.Minute,
	}
	task, err := NewRescore(cfg)
	if err != nil {
		t.Fatalf("NewRescore: %v", err)
	}
	if trig := task.Trigger.(*scheduler.IntervalTrigger); trig.Every != 5*time.Second {
		t.Errorf("interval = %v, want 5s", trig.Every)
	}
	if task.Timeout != time.Minute {
		t.Errorf("Timeout = %v, want 1m", task.Timeout)
	}
}

// An absent watermark records the current rubric version as the baseline
// without rescoring (anti-scope: the ingester already scores on ingest) and
// without publishing.
func TestRunAbsentWatermarkBaselines(t *testing.T) {
	env := newRescoreEnv(t)
	ch := env.subscribeDone(t)

	if err := env.task.run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}
	wantBulkCalls(t, env.fake, 0, 0)
	wm, found := env.loadWatermark(t)
	if !found || wm.RubricVersion != scoring.RubricVersion {
		t.Errorf("watermark = (%+v, %v), want current version persisted", wm, found)
	}
	expectNoEvent(t, ch)
}

// The steady state — watermark matches the active rubric — is a pure no-op.
func TestRunNoopWhenVersionCurrent(t *testing.T) {
	env := newRescoreEnv(t)
	env.seedWatermark(t, scoring.RubricVersion)
	ch := env.subscribeDone(t)

	if err := env.task.run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}
	wantBulkCalls(t, env.fake, 0, 0)
	expectNoEvent(t, ch)
}

// A version delta triggers exactly one full pass: score, write sidecars,
// persist the watermark, publish RescoreDone with the version transition and
// iteration count. The following tick is a no-op again.
func TestRunRescoresOncePerVersionBump(t *testing.T) {
	env := newRescoreEnv(t)
	env.seedWatermark(t, oldRubricVersion)
	ch := env.subscribeDone(t)

	if err := env.task.run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}
	wantBulkCalls(t, env.fake, 1, 1)

	done := recvRescoreDone(t, ch)
	if done.FromVersion != oldRubricVersion || done.ToVersion != scoring.RubricVersion {
		t.Errorf("versions = %s -> %s, want %s -> %s", done.FromVersion, done.ToVersion, oldRubricVersion, scoring.RubricVersion)
	}
	if done.IterCount != 2 {
		t.Errorf("IterCount = %d, want 2", done.IterCount)
	}
	wm, found := env.loadWatermark(t)
	if !found || wm.RubricVersion != scoring.RubricVersion {
		t.Errorf("watermark = (%+v, %v), want advanced to current version", wm, found)
	}

	// Next tick sees the advanced watermark: no second rescore.
	if err := env.task.run(t.Context()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	wantBulkCalls(t, env.fake, 1, 1)
	expectNoEvent(t, ch)
}

// A corrupt watermark surfaces the load error instead of silently
// baselining or rescoring behind the operator's back.
func TestRunRescoreWatermarkLoadError(t *testing.T) {
	env := newRescoreEnv(t)
	wmPath := state.Path(env.repoDir, RescoreName)
	if err := os.MkdirAll(filepath.Dir(wmPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wmPath, []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := env.task.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "parse watermark") {
		t.Errorf("run = %v, want watermark parse error", err)
	}
	wantBulkCalls(t, env.fake, 0, 0)
}

// A baseline-save failure is surfaced so the scheduler records it.
func TestRunBaselineSaveError(t *testing.T) {
	env := newRescoreEnv(t)
	// Block .agents so the state dir cannot be created.
	if err := os.WriteFile(filepath.Join(env.repoDir, ".agents"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := env.task.run(t.Context()); err == nil || !strings.Contains(err.Error(), "create state dir") {
		t.Errorf("run = %v, want state-dir error", err)
	}
}

// A scoring failure aborts the pass without advancing the watermark, so the
// next tick retries the whole idempotent pass.
func TestRunScoreAllErrorLeavesWatermark(t *testing.T) {
	env := newRescoreEnv(t)
	env.seedWatermark(t, oldRubricVersion)
	env.fake.scoreErr = errors.New("boom")
	ch := env.subscribeDone(t)

	err := env.task.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("rescore %s -> %s", oldRubricVersion, scoring.RubricVersion)) {
		t.Errorf("run = %v, want wrapped rescore error", err)
	}
	wantBulkCalls(t, env.fake, 1, 0)
	if wm, _ := env.loadWatermark(t); wm.RubricVersion != oldRubricVersion {
		t.Errorf("watermark advanced to %q despite score failure", wm.RubricVersion)
	}
	expectNoEvent(t, ch)
}

// A sidecar-write failure aborts the pass without advancing the watermark.
func TestRunWriteSidecarsErrorLeavesWatermark(t *testing.T) {
	env := newRescoreEnv(t)
	env.seedWatermark(t, oldRubricVersion)
	env.fake.writeErr = errors.New("disk full")

	err := env.task.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("run = %v, want wrapped write error", err)
	}
	if wm, _ := env.loadWatermark(t); wm.RubricVersion != oldRubricVersion {
		t.Errorf("watermark advanced to %q despite write failure", wm.RubricVersion)
	}
}

// A watermark-save failure after the sidecar writes aborts before the event
// is published: the bus must stay eventually consistent with disk, never
// ahead of it.
func TestRunRescoreSaveErrorSuppressesEvent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permissions are not enforced this way on Windows")
	}
	env := newRescoreEnv(t)
	env.seedWatermark(t, oldRubricVersion)
	stateDir := filepath.Dir(state.Path(env.repoDir, RescoreName))
	if err := os.Chmod(stateDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o755) })
	ch := env.subscribeDone(t)

	if err := env.task.run(t.Context()); err == nil || !strings.Contains(err.Error(), "write watermark") {
		t.Errorf("run = %v, want watermark write error", err)
	}
	wantBulkCalls(t, env.fake, 1, 1)
	expectNoEvent(t, ch)
}

// A publish failure after Close is surfaced as ErrClosed; the sidecars and
// watermark are already on disk (G1: disk is canonical, the bus is not).
func TestRunRescorePublishError(t *testing.T) {
	env := newRescoreEnv(t)
	env.seedWatermark(t, oldRubricVersion)
	if err := env.bus.Close(); err != nil {
		t.Fatal(err)
	}
	err := env.task.run(t.Context())
	if !errors.Is(err, events.ErrClosed) {
		t.Errorf("run = %v, want ErrClosed", err)
	}
	if wm, _ := env.loadWatermark(t); wm.RubricVersion != scoring.RubricVersion {
		t.Error("watermark should be persisted before the failed publish")
	}
}

// A context cancelled before the tick starts stops the pass before any work.
func TestRunRescoreCtxCancelled(t *testing.T) {
	env := newRescoreEnv(t)
	env.seedWatermark(t, oldRubricVersion)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := env.task.run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want context.Canceled", err)
	}
	wantBulkCalls(t, env.fake, 0, 0)
}

// A context cancelled mid-pass (after scoring, before the writes) stops the
// pass without touching the sidecars or the watermark.
func TestRunRescoreCtxCancelledMidPass(t *testing.T) {
	env := newRescoreEnv(t)
	env.seedWatermark(t, oldRubricVersion)
	ctx, cancel := context.WithCancel(t.Context())
	env.fake.onScore = cancel
	if err := env.task.run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want context.Canceled", err)
	}
	wantBulkCalls(t, env.fake, 1, 0)
	if wm, _ := env.loadWatermark(t); wm.RubricVersion != oldRubricVersion {
		t.Errorf("watermark advanced to %q despite cancelled pass", wm.RubricVersion)
	}
}

// packageBulkScorer is a passthrough to internal/scoring's full pipeline.
// Uses the scoring fixture read-only and writes the sidecars to a temp dir.
func TestPackageBulkScorerPassthrough(t *testing.T) {
	fixture := filepath.Join("..", "..", "scoring", "testdata", "iterlog")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("iter-log fixture not present: %v", err)
	}
	repoRoot := filepath.Join("..", "..", "..")
	sc := packageBulkScorer{}
	b, err := sc.ScoreAll(fixture, repoRoot)
	if err != nil {
		t.Fatalf("ScoreAll: %v", err)
	}
	if len(b.scores) == 0 || len(b.scores) != len(b.records) {
		t.Fatalf("scores/records = %d/%d, want equal and non-empty", len(b.scores), len(b.records))
	}
	dir := t.TempDir()
	if err := sc.WriteSidecars(dir, b); err != nil {
		t.Fatalf("WriteSidecars: %v", err)
	}
	for _, s := range b.scores {
		if _, err := os.Stat(scoring.IterationScorePath(dir, s.Iteration)); err != nil {
			t.Errorf("iter-%d sidecar not written: %v", s.Iteration, err)
		}
	}
}

// An empty (or brand-new) iteration log yields an empty result, not an
// error — there is simply nothing to rescore yet.
func TestPackageBulkScorerEmptyLog(t *testing.T) {
	b, err := packageBulkScorer{}.ScoreAll(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("ScoreAll: %v", err)
	}
	if len(b.records) != 0 || len(b.scores) != 0 || len(b.sessions) != 0 {
		t.Errorf("result = %+v, want empty", b)
	}
}

// A corrupt iteration log surfaces the load error.
func TestPackageBulkScorerLoadError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "iter-1.yaml"), []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (packageBulkScorer{}).ScoreAll(dir, t.TempDir()); err == nil {
		t.Error("ScoreAll = nil error, want load failure")
	}
}

// A signal-build failure (repoDir is not a git repository) surfaces the
// pipeline error.
func TestPackageBulkScorerBuildError(t *testing.T) {
	dir := t.TempDir()
	rec := "schema_version: 1\niteration: 1\ndate: \"2026-01-01\"\ncommit: \"abc123\"\n"
	if err := os.WriteFile(filepath.Join(dir, "iter-1.yaml"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (packageBulkScorer{}).ScoreAll(dir, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("ScoreAll = %v, want non-git-repo error", err)
	}
}

// A failing per-iteration sidecar write is wrapped with the iteration.
func TestPackageBulkScorerWriteIterationError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	b := bulkScores{
		records: []scoring.IterationRecord{{Iteration: 1}},
		scores:  []scoring.Score{{Iteration: 1}},
	}
	err := packageBulkScorer{}.WriteSidecars(missing, b)
	if err == nil || !strings.Contains(err.Error(), "iter-1 sidecar") {
		t.Errorf("WriteSidecars = %v, want wrapped iter-1 error", err)
	}
}

// A failing session write is surfaced (an empty session id has no
// addressable sidecar path).
func TestPackageBulkScorerWriteSessionError(t *testing.T) {
	b := bulkScores{sessions: []scoring.SessionScore{{SessionID: ""}}}
	err := packageBulkScorer{}.WriteSidecars(t.TempDir(), b)
	if err == nil || !strings.Contains(err.Error(), "session_id") {
		t.Errorf("WriteSidecars = %v, want empty-session_id error", err)
	}
}
