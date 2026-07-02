package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
	"github.com/AGOrcha/dot-agents/internal/service/state"
)

// fakeScorer records Score calls and returns canned results, so run()'s
// watermark/ordering/publish semantics are testable without the real
// (git-dependent) scoring pipeline.
type fakeScorer struct {
	mu         sync.Mutex
	scored     []int
	scoreErr   error
	sidecarErr error
}

func (f *fakeScorer) Score(_, _ string, n int) (scoring.Score, scoring.IterationRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scoreErr != nil {
		return scoring.Score{}, scoring.IterationRecord{}, f.scoreErr
	}
	f.scored = append(f.scored, n)
	s := scoring.Score{
		Iteration:     n,
		RubricVersion: scoring.RubricVersion,
		Value:         0.8,
		Scored:        true,
		Band:          "strong",
	}
	return s, scoring.IterationRecord{Iteration: n}, nil
}

func (f *fakeScorer) WriteSidecar(iterLogDir string, s scoring.Score, _ scoring.IterationRecord) (string, error) {
	if f.sidecarErr != nil {
		return "", f.sidecarErr
	}
	return filepath.Join(iterLogDir, fmt.Sprintf("iter-%d.score.yaml", s.Iteration)), nil
}

func (f *fakeScorer) calls() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.scored...)
}

// testEnv bundles the moving parts every run() test needs.
type testEnv struct {
	iterLogDir string
	repoDir    string
	bus        *events.InProcBus
	fake       *fakeScorer
	ing        *iterLogIngester
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		iterLogDir: t.TempDir(),
		repoDir:    t.TempDir(),
		bus:        events.NewInProcBus(),
		fake:       &fakeScorer{},
	}
	t.Cleanup(func() { _ = env.bus.Close() })
	env.ing = env.newIngester(t)
	return env
}

// newIngester builds a fresh ingester over the env's dirs — a second call
// simulates a service restart (fresh in-memory state, same watermark file).
func (env *testEnv) newIngester(t *testing.T) *iterLogIngester {
	t.Helper()
	cfg := IterLogIngesterConfig{IterLogDir: env.iterLogDir, RepoDir: env.repoDir, Bus: env.bus}
	ing, err := newIterLogIngester(cfg, env.fake)
	if err != nil {
		t.Fatalf("newIterLogIngester: %v", err)
	}
	return ing
}

func (env *testEnv) writeIter(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(env.iterLogDir, fmt.Sprintf("iter-%d.yaml", n))
	if err := os.WriteFile(path, []byte(fmt.Sprintf("iteration: %d\n", n)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (env *testEnv) loadWatermark(t *testing.T) (IterLogWatermark, bool) {
	t.Helper()
	var wm IterLogWatermark
	found, err := state.Load(state.Path(env.repoDir, IterLogIngesterName), &wm)
	if err != nil {
		t.Fatalf("load watermark: %v", err)
	}
	return wm, found
}

func recvScored(t *testing.T, ch <-chan events.Event) IterationScored {
	t.Helper()
	select {
	case evt := <-ch:
		payload, ok := evt.Payload.(IterationScored)
		if !ok {
			t.Fatalf("payload type = %T, want IterationScored", evt.Payload)
		}
		return payload
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for iteration.scored event")
		return IterationScored{}
	}
}

func expectNoEvent(t *testing.T, ch <-chan events.Event) {
	t.Helper()
	select {
	case evt := <-ch:
		t.Fatalf("unexpected event published: %+v", evt)
	case <-time.After(100 * time.Millisecond):
	}
}

func wantCalls(t *testing.T, fake *fakeScorer, want ...int) {
	t.Helper()
	got := fake.calls()
	if len(got) != len(want) {
		t.Fatalf("scored iterations = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scored iterations = %v, want %v", got, want)
		}
	}
}

// NewIterLogIngester rejects incomplete configuration with sentinel errors.
func TestNewIterLogIngesterValidation(t *testing.T) {
	bus := events.NewInProcBus()
	defer func() { _ = bus.Close() }()
	cases := []struct {
		name string
		cfg  IterLogIngesterConfig
		want error
	}{
		{"missing iter-log dir", IterLogIngesterConfig{RepoDir: "r", Bus: bus}, ErrNoIterLogDir},
		{"missing repo dir", IterLogIngesterConfig{IterLogDir: "d", Bus: bus}, ErrNoRepoDir},
		{"missing bus", IterLogIngesterConfig{IterLogDir: "d", RepoDir: "r"}, ErrNoBus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewIterLogIngester(tc.cfg); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// The built task carries the contract shape: name, fsnotify trigger on the
// iter-log dir, and the default timeout when none is configured.
func TestNewIterLogIngesterTaskShape(t *testing.T) {
	bus := events.NewInProcBus()
	defer func() { _ = bus.Close() }()
	dir := t.TempDir()
	task, err := NewIterLogIngester(IterLogIngesterConfig{IterLogDir: dir, RepoDir: t.TempDir(), Bus: bus})
	if err != nil {
		t.Fatalf("NewIterLogIngester: %v", err)
	}
	if task.Name != IterLogIngesterName {
		t.Errorf("Name = %q, want %q", task.Name, IterLogIngesterName)
	}
	trig, ok := task.Trigger.(*scheduler.FSNotifyTrigger)
	if !ok {
		t.Fatalf("Trigger type = %T, want *scheduler.FSNotifyTrigger", task.Trigger)
	}
	if len(trig.Paths) != 1 || trig.Paths[0] != dir {
		t.Errorf("Trigger paths = %v, want [%s]", trig.Paths, dir)
	}
	if task.Timeout != defaultIngestTimeout {
		t.Errorf("Timeout = %v, want default %v", task.Timeout, defaultIngestTimeout)
	}
	if task.RunFn == nil {
		t.Error("RunFn is nil")
	}
}

// An explicit timeout overrides the default.
func TestNewIterLogIngesterExplicitTimeout(t *testing.T) {
	bus := events.NewInProcBus()
	defer func() { _ = bus.Close() }()
	cfg := IterLogIngesterConfig{IterLogDir: t.TempDir(), RepoDir: t.TempDir(), Bus: bus, Timeout: 5 * time.Second}
	task, err := NewIterLogIngester(cfg)
	if err != nil {
		t.Fatalf("NewIterLogIngester: %v", err)
	}
	if task.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", task.Timeout)
	}
}

// First run with no watermark ingests every iteration in ascending order,
// persists the watermark, and publishes one event per iteration whose
// payload points at the sidecar.
func TestRunFirstRunProcessesAllPending(t *testing.T) {
	env := newTestEnv(t)
	env.writeIter(t, 2)
	env.writeIter(t, 1)
	ch, unsub, err := env.bus.Subscribe(events.TopicIterationScored)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsub()

	if err := env.ing.run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}
	wantCalls(t, env.fake, 1, 2)

	first := recvScored(t, ch)
	if first.Iteration != 1 || first.Band != "strong" || first.Score != 0.8 {
		t.Errorf("first payload = %+v", first)
	}
	wantSidecar := filepath.Join(env.iterLogDir, "iter-1.score.yaml")
	if first.SidecarPath != wantSidecar {
		t.Errorf("SidecarPath = %q, want %q", first.SidecarPath, wantSidecar)
	}
	if second := recvScored(t, ch); second.Iteration != 2 {
		t.Errorf("second payload iteration = %d, want 2", second.Iteration)
	}

	wm, found := env.loadWatermark(t)
	if !found {
		t.Fatal("watermark not persisted")
	}
	if wm.LastIterProcessed != 2 || wm.RubricVersion != scoring.RubricVersion {
		t.Errorf("watermark = %+v", wm)
	}
	if wm.LastMTime.IsZero() {
		t.Error("watermark LastMTime is zero")
	}
}

// Restart safety (D3): after processing iter-1 and restarting, only iter-2
// is ingested — the watermark, not in-memory state, carries the progress.
func TestRunRestartProcessesOnlyNew(t *testing.T) {
	env := newTestEnv(t)
	env.writeIter(t, 1)
	if err := env.ing.run(t.Context()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	wantCalls(t, env.fake, 1)

	env.writeIter(t, 2)
	restarted := env.newIngester(t) // fresh instance = restarted service
	if err := restarted.run(t.Context()); err != nil {
		t.Fatalf("run after restart: %v", err)
	}
	wantCalls(t, env.fake, 1, 2)
}

// A run with nothing new is a no-op: the sidecars are already current per
// the watermark.
func TestRunIdempotentWhenCurrent(t *testing.T) {
	env := newTestEnv(t)
	env.writeIter(t, 1)
	if err := env.ing.run(t.Context()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := env.ing.run(t.Context()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	wantCalls(t, env.fake, 1)
}

// A modified iter file (mtime beyond the watermark) is re-ingested even
// though its iteration number is already processed.
func TestRunReprocessesModified(t *testing.T) {
	env := newTestEnv(t)
	path := env.writeIter(t, 1)
	if err := env.ing.run(t.Context()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if err := env.ing.run(t.Context()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	wantCalls(t, env.fake, 1, 1)
}

// A missing iter-log directory means nothing to ingest, not an error — the
// directory may not exist yet in a fresh repo.
func TestRunMissingIterLogDir(t *testing.T) {
	env := newTestEnv(t)
	env.ing.cfg.IterLogDir = filepath.Join(env.iterLogDir, "does-not-exist")
	if err := env.ing.run(t.Context()); err != nil {
		t.Fatalf("run: %v", err)
	}
	wantCalls(t, env.fake)
}

// A scan failure (iter-log path is a file) is surfaced with context.
func TestRunScanError(t *testing.T) {
	env := newTestEnv(t)
	blocker := filepath.Join(env.repoDir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.ing.cfg.IterLogDir = blocker
	err := env.ing.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "scan iter-log dir") {
		t.Errorf("run = %v, want scan error", err)
	}
}

// A scoring failure aborts the pass without advancing the watermark, so the
// next fire retries the same iteration.
func TestRunScoreErrorLeavesWatermark(t *testing.T) {
	env := newTestEnv(t)
	env.writeIter(t, 1)
	env.fake.scoreErr = errors.New("boom")
	err := env.ing.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "score iter-1") {
		t.Errorf("run = %v, want score error", err)
	}
	if _, found := env.loadWatermark(t); found {
		t.Error("watermark persisted despite score failure")
	}
}

// A sidecar-write failure aborts the pass without advancing the watermark.
func TestRunSidecarErrorLeavesWatermark(t *testing.T) {
	env := newTestEnv(t)
	env.writeIter(t, 1)
	env.fake.sidecarErr = errors.New("disk full")
	err := env.ing.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "write sidecar for iter-1") {
		t.Errorf("run = %v, want sidecar error", err)
	}
	if _, found := env.loadWatermark(t); found {
		t.Error("watermark persisted despite sidecar failure")
	}
}

// A corrupt watermark surfaces the load error instead of reprocessing from
// zero behind the operator's back.
func TestRunWatermarkLoadError(t *testing.T) {
	env := newTestEnv(t)
	wmPath := state.Path(env.repoDir, IterLogIngesterName)
	if err := os.MkdirAll(filepath.Dir(wmPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wmPath, []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := env.ing.run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "parse watermark") {
		t.Errorf("run = %v, want watermark parse error", err)
	}
	wantCalls(t, env.fake)
}

// A watermark-save failure aborts before the event is published: the bus
// must stay eventually consistent with disk, never ahead of it.
func TestRunWatermarkSaveErrorSuppressesEvent(t *testing.T) {
	env := newTestEnv(t)
	env.writeIter(t, 1)
	// Block .agents so the state dir cannot be created.
	if err := os.WriteFile(filepath.Join(env.repoDir, ".agents"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ch, unsub, err := env.bus.Subscribe(events.TopicIterationScored)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsub()
	if err := env.ing.run(t.Context()); err == nil || !strings.Contains(err.Error(), "create state dir") {
		t.Errorf("run = %v, want state-dir error", err)
	}
	expectNoEvent(t, ch)
}

// A publish failure after Close is surfaced as ErrClosed; sidecar and
// watermark are already on disk (G1: disk is canonical, the bus is not).
func TestRunPublishError(t *testing.T) {
	env := newTestEnv(t)
	env.writeIter(t, 1)
	if err := env.bus.Close(); err != nil {
		t.Fatal(err)
	}
	err := env.ing.run(t.Context())
	if !errors.Is(err, events.ErrClosed) {
		t.Errorf("run = %v, want ErrClosed", err)
	}
	if _, found := env.loadWatermark(t); !found {
		t.Error("watermark should be persisted before the failed publish")
	}
}

// A cancelled context stops the pass before any scoring work.
func TestRunCtxCancelled(t *testing.T) {
	env := newTestEnv(t)
	env.writeIter(t, 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := env.ing.run(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("run = %v, want context.Canceled", err)
	}
	wantCalls(t, env.fake)
}

// iterNumber accepts only iter-N.yaml log entries.
func TestIterNumber(t *testing.T) {
	cases := []struct {
		name   string
		wantN  int
		wantOK bool
	}{
		{"iter-3.yaml", 3, true},
		{"iter-3.score.yaml", 0, false},
		{"historical.yaml", 0, false},
		{"iter-.yaml", 0, false},
		{"iter-99999999999999999999.yaml", 0, false}, // overflows int
	}
	for _, tc := range cases {
		n, ok := iterNumber(tc.name)
		if ok != tc.wantOK || (ok && n != tc.wantN) {
			t.Errorf("iterNumber(%q) = (%d, %v), want (%d, %v)", tc.name, n, ok, tc.wantN, tc.wantOK)
		}
	}
}

// packageScorer is a passthrough to internal/scoring: the same entrypoint
// close-task uses, plus the augmented sidecar writer. Uses the scoring
// fixture read-only and writes the sidecar to a temp dir.
func TestPackageScorerPassthrough(t *testing.T) {
	fixture := filepath.Join("..", "..", "scoring", "testdata", "iterlog")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("iter-log fixture not present: %v", err)
	}
	repoRoot := filepath.Join("..", "..", "..")
	sc := packageScorer{}
	score, rec, err := sc.Score(fixture, repoRoot, 1)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if score.Iteration != 1 || rec.Iteration != 1 {
		t.Errorf("Score iteration = %d/%d, want 1/1", score.Iteration, rec.Iteration)
	}
	dir := t.TempDir()
	sidecar, err := sc.WriteSidecar(dir, score, rec)
	if err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	if _, err := os.Stat(sidecar); err != nil {
		t.Errorf("sidecar not written: %v", err)
	}
}

// End-to-end through the real scheduler + fsnotify trigger: a rapid burst of
// writes to one iter file coalesces into one ingest pass (at most two if a
// straggler event lands after the first scan), never one pass per raw write.
func TestIngesterCoalescesRapidEvents(t *testing.T) {
	env := newTestEnv(t)
	sched := scheduler.New()
	task := scheduler.Task{
		Name:    IterLogIngesterName,
		Trigger: scheduler.FSNotify(env.iterLogDir),
		RunFn:   env.ing.run,
		Timeout: time.Minute,
	}
	if err := sched.Register(task); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := sched.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sched.Stop(2 * time.Second) }()

	for range 5 {
		env.writeIter(t, 1)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(env.fake.calls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(env.fake.calls()) == 0 {
		t.Fatal("ingester never ran after fsnotify burst")
	}
	// Let any trailing debounce window fire, then assert coalescing.
	time.Sleep(300 * time.Millisecond)
	if got := len(env.fake.calls()); got > 2 {
		t.Errorf("burst of 5 writes produced %d ingest passes, want <= 2 (coalesced)", got)
	}
}
