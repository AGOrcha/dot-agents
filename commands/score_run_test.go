package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// newTestRepo creates a tiny throwaway git repo + iter-log dir, returning the
// repo root (also used as repoDir for BuildSignalSets) and the iter-log dir.
// One iteration file is written and committed so BuildSignalSets has work to
// do without the test having to mock out every signal extractor.
func newScoreTestRepo(t *testing.T) (repo, iterLogDir string) {
	t.Helper()
	repo = t.TempDir()
	gitInit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-04-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-04-01T00:00:00Z")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitInit("init", "-q")
	gitInit("config", "user.email", "test@example.com")
	gitInit("config", "user.name", "Test")
	gitInit("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "first.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	gitInit("add", ".")
	gitInit("commit", "-q", "-m", "initial")
	sha, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	shaStr := strings.TrimSpace(string(sha))

	iterLogDir = filepath.Join(repo, ".agents", "active", "iteration-log")
	if err := os.MkdirAll(iterLogDir, 0o755); err != nil {
		t.Fatalf("mkdir iter-log: %v", err)
	}
	iter1 := "schema_version: 2\niteration: 1\ndate: \"2026-04-01\"\ntask_id: t1\ncommit: " + shaStr + "\nagent:\n  session_id: test-session-A\nimpl:\n  item: smoke\n  scope_note: on-target\n"
	if err := os.WriteFile(filepath.Join(iterLogDir, "iter-1.yaml"), []byte(iter1), 0o644); err != nil {
		t.Fatalf("write iter-1: %v", err)
	}
	return repo, iterLogDir
}

// runScoreRun against a real (tiny) iter-log + git repo: success path
// exercises BuildSignalSets, ScoreAll, sidecar persistence, and renderRunSummary.
func TestRunScoreRunWritesSidecarsAndRenders(t *testing.T) {
	repo, iterLogDir := newScoreTestRepo(t)
	var buf bytes.Buffer
	err := runScoreRun(&buf, scoreRunOpts{iterLogDir: iterLogDir, repoDir: repo})
	if err != nil {
		t.Fatalf("runScoreRun: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Outcome scoring", "rubric 3.0.0", "Iterations: 1", "Wrote 1 iter sidecars"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(iterLogDir, "iter-1.score.yaml")); err != nil {
		t.Errorf("iter sidecar not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(iterLogDir, "session-test-session-A.score.yaml")); err != nil {
		t.Errorf("session sidecar not written: %v", err)
	}
}

// --no-write must skip persistence (no sidecars on disk afterwards) but still
// print the summary.
func TestRunScoreRunNoWriteSkipsSidecars(t *testing.T) {
	repo, iterLogDir := newScoreTestRepo(t)
	var buf bytes.Buffer
	if err := runScoreRun(&buf, scoreRunOpts{iterLogDir: iterLogDir, repoDir: repo, noWrite: true}); err != nil {
		t.Fatalf("runScoreRun: %v", err)
	}
	if _, err := os.Stat(filepath.Join(iterLogDir, "iter-1.score.yaml")); !os.IsNotExist(err) {
		t.Errorf("sidecar written despite --no-write: err=%v", err)
	}
	if !strings.Contains(buf.String(), "Iterations: 1") {
		t.Errorf("summary missing iter count:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "Wrote ") {
		t.Errorf("--no-write should not log a Wrote line:\n%s", buf.String())
	}
}

// --json flips runScoreRun to emit a structured payload through emitScoreRunJSON.
// Flags.JSON is package-level mutable; restore it via t.Cleanup so other tests
// see the default.
func TestRunScoreRunJSONOutput(t *testing.T) {
	prior := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prior })

	repo, iterLogDir := newScoreTestRepo(t)
	var buf bytes.Buffer
	if err := runScoreRun(&buf, scoreRunOpts{iterLogDir: iterLogDir, repoDir: repo, noWrite: true}); err != nil {
		t.Fatalf("runScoreRun: %v", err)
	}

	var payload struct {
		RubricVersion string                   `json:"rubric_version"`
		Iterations    []scoring.PersistedScore `json:"iterations"`
		Sessions      []scoring.SessionScore   `json:"sessions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v\nraw:\n%s", err, buf.String())
	}
	if payload.RubricVersion == "" {
		t.Error("payload.RubricVersion empty")
	}
	if len(payload.Iterations) != 1 {
		t.Errorf("iterations = %d, want 1", len(payload.Iterations))
	}
	if len(payload.Sessions) != 1 {
		t.Errorf("sessions = %d, want 1", len(payload.Sessions))
	}
}

// Empty iter-log: friendly notice, no error.
func TestRunScoreRunEmptyShortCircuits(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := runScoreRun(&buf, scoreRunOpts{iterLogDir: dir, repoDir: dir, noWrite: true}); err != nil {
		t.Fatalf("runScoreRun: %v", err)
	}
	if !strings.Contains(buf.String(), "no iterations found") {
		t.Errorf("missing notice in:\n%s", buf.String())
	}
}

// LoadIterationLog errors when an iter file is malformed; the run command
// surfaces that as a wrapped error rather than swallowing it.
func TestRunScoreRunSurfacesLoadError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "iter-1.yaml"), []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runScoreRun(&bytes.Buffer{}, scoreRunOpts{iterLogDir: dir, repoDir: dir})
	if err == nil {
		t.Fatal("expected load error, got nil")
	}
	if !strings.Contains(err.Error(), "load iteration log") {
		t.Errorf("error should mention load step: %v", err)
	}
}

// Non-git repoDir surfaces a BuildSignalSets error through the wrapper.
func TestRunScoreRunSurfacesBuildSignalSetsError(t *testing.T) {
	repo, iterLogDir := newScoreTestRepo(t)
	_ = repo // we ignore the git repo and pass a non-git dir as repoDir
	nonGit := t.TempDir()
	err := runScoreRun(&bytes.Buffer{}, scoreRunOpts{iterLogDir: iterLogDir, repoDir: nonGit})
	if err == nil {
		t.Fatal("expected BuildSignalSets error, got nil")
	}
	if !strings.Contains(err.Error(), "build signals") {
		t.Errorf("error should mention build signals: %v", err)
	}
}

// runScoreIteration in JSON mode emits the PersistedScore directly.
func TestRunScoreIterationJSONOutput(t *testing.T) {
	prior := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prior })

	dir := t.TempDir()
	r := scoring.DefaultRubric()
	if _, err := scoring.WriteIterationScore(dir, r.Score(scoring.SignalSet{Iteration: 4, Verifier: scoring.PresentSignal(1.0, "")})); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runScoreIteration(&buf, dir, 4); err != nil {
		t.Fatalf("runScoreIteration: %v", err)
	}
	var got scoring.PersistedScore
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got.Iteration != 4 {
		t.Errorf("Iteration = %d, want 4", got.Iteration)
	}
}

// runScoreSession in JSON mode emits the SessionScore directly.
func TestRunScoreSessionJSONOutput(t *testing.T) {
	prior := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prior })

	dir := t.TempDir()
	if _, err := scoring.WriteSessionScore(dir, scoring.SessionScore{
		SessionID:     "sess-X",
		RubricVersion: "9.9.9",
		Scored:        true,
		Value:         0.4,
		Band:          "poor",
		Iterations:    []int{1},
		PerIteration:  []scoring.SessionIterRef{{Iteration: 1, Scored: true, Value: 0.4, Band: "poor"}},
	}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runScoreSession(&buf, dir, "sess-X"); err != nil {
		t.Fatalf("runScoreSession: %v", err)
	}
	var got scoring.SessionScore
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got.SessionID != "sess-X" || got.Value != 0.4 {
		t.Errorf("session round-trip lost data: %+v", got)
	}
}

// Empty session-id is unaddressable on disk; runScoreSession surfaces the
// error from SessionScorePath rather than producing a misleading "no sidecar"
// message.
func TestRunScoreSessionEmptyIDError(t *testing.T) {
	err := runScoreSession(&bytes.Buffer{}, t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "empty session_id") {
		t.Errorf("error should call out the cause: %v", err)
	}
}

// Cobra wiring check: the command tree exposes the three documented
// subcommands with their advertised Use lines. A typo in command construction
// would silently change the CLI surface — this pins it.
func TestNewScoreCmdWiresSubcommands(t *testing.T) {
	root := NewScoreCmd()
	if root.Use != "score" {
		t.Errorf("Use = %q, want %q", root.Use, "score")
	}
	want := map[string]bool{"run": false, "iteration <N>": false, "session <session-id>": false}
	for _, sub := range root.Commands() {
		if _, ok := want[sub.Use]; ok {
			want[sub.Use] = true
		} else {
			t.Errorf("unexpected subcommand %q", sub.Use)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

// newScoreRunCmd registers the four documented flags. A regression that
// renames or drops one would silently break scripts; pin the surface.
func TestNewScoreRunCmdHasFlags(t *testing.T) {
	cmd := newScoreRunCmd()
	for _, name := range []string{"iter-log-dir", "repo-dir", "transcript-dir", "no-write"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

// Drive the iteration subcommand through cobra Execute so the RunE closure
// (the body of newScoreIterationCmd) is covered end-to-end, including the
// strconv parse, the iter-log-dir flag plumbing, and the args validation.
func TestScoreIterationSubcommandExecute(t *testing.T) {
	dir := t.TempDir()
	r := scoring.DefaultRubric()
	if _, err := scoring.WriteIterationScore(dir, r.Score(scoring.SignalSet{Iteration: 11, Verifier: scoring.PresentSignal(1.0, "")})); err != nil {
		t.Fatal(err)
	}
	cmd := newScoreIterationCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--iter-log-dir", dir, "11"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Iteration 11") {
		t.Errorf("missing iteration header in:\n%s", buf.String())
	}
}

// A non-integer argument to the iteration subcommand surfaces a clear error
// from the RunE closure's strconv path.
func TestScoreIterationSubcommandRejectsNonInt(t *testing.T) {
	cmd := newScoreIterationCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"not-a-number"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "integer") {
		t.Errorf("error should mention integer parsing: %v", err)
	}
}

// Drive the session subcommand through cobra Execute for its RunE coverage.
func TestScoreSessionSubcommandExecute(t *testing.T) {
	dir := t.TempDir()
	if _, err := scoring.WriteSessionScore(dir, scoring.SessionScore{
		SessionID: "exec-sess", RubricVersion: "1.0.0", Scored: true, Value: 0.5, Band: "fair",
		PerIteration: []scoring.SessionIterRef{{Iteration: 1, Scored: true, Value: 0.5, Band: "fair"}},
	}); err != nil {
		t.Fatal(err)
	}
	cmd := newScoreSessionCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--iter-log-dir", dir, "exec-sess"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Session exec-sess") {
		t.Errorf("missing session header in:\n%s", buf.String())
	}
}

// Drive the run subcommand through cobra Execute. We exercise the empty-log
// short-circuit so the test does not need a fixture git repo + iter-log.
func TestScoreRunSubcommandExecute(t *testing.T) {
	cmd := newScoreRunCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--iter-log-dir", t.TempDir(), "--repo-dir", t.TempDir(), "--no-write"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "no iterations found") {
		t.Errorf("expected empty-log notice in:\n%s", buf.String())
	}
}

// runScoreRun falls back to os.Getwd when opts.repoDir is empty. Drive that
// branch by setting cwd (via t.Chdir, which restores the prior cwd at
// cleanup time and unblocks Windows TempDir teardown) to a non-git tempdir;
// we expect the build-signals error to surface (no git repo at cwd) instead
// of a Getwd error.
func TestRunScoreRunUsesCwdWhenRepoDirEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Need an iter-log entry so we get past the empty short-circuit and into
	// BuildSignalSets, which is what exercises the cwd-as-repoDir branch.
	if err := os.WriteFile(filepath.Join(dir, "iter-1.yaml"),
		[]byte("schema_version: 2\niteration: 1\ndate: \"2026-04-01\"\ntask_id: t1\n"), 0o644); err != nil {
		t.Fatalf("seed iter: %v", err)
	}
	err := runScoreRun(&bytes.Buffer{}, scoreRunOpts{iterLogDir: dir})
	if err == nil {
		t.Fatal("expected build-signals error from non-git cwd, got nil")
	}
	if !strings.Contains(err.Error(), "build signals") {
		t.Errorf("error should surface as build-signals: %v", err)
	}
}

// --recompute on the iteration subcommand drives the recompute path: it
// scores iteration N fresh from the canonical inputs, writes the iter-N.
// score.yaml sidecar, and renders the breakdown. close-task uses this as
// its --score-recompute=current implementation.
func TestRunScoreIterationRecomputeWritesSidecar(t *testing.T) {
	repo, iterLogDir := newScoreTestRepo(t)
	var buf bytes.Buffer
	if err := runScoreIterationRecompute(&buf, iterLogDir, repo, 1, nil); err != nil {
		t.Fatalf("runScoreIterationRecompute: %v\n%s", err, buf.String())
	}
	if _, err := os.Stat(filepath.Join(iterLogDir, "iter-1.score.yaml")); err != nil {
		t.Errorf("sidecar not written: %v", err)
	}
	if !strings.Contains(buf.String(), "Iteration 1") {
		t.Errorf("missing iteration header: %s", buf.String())
	}
}

// --recompute with --json emits the structured PersistedScore (same shape
// as the read path) so callers can parse it without a write-vs-read branch.
func TestRunScoreIterationRecomputeJSONOutput(t *testing.T) {
	prior := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prior })
	repo, iterLogDir := newScoreTestRepo(t)
	var buf bytes.Buffer
	if err := runScoreIterationRecompute(&buf, iterLogDir, repo, 1, nil); err != nil {
		t.Fatalf("runScoreIterationRecompute: %v\n%s", err, buf.String())
	}
	var got scoring.PersistedScore
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got.Iteration != 1 {
		t.Errorf("got iteration %d, want 1", got.Iteration)
	}
}

// --recompute with empty repoDir falls back to cwd — headless invocations
// from the repo root do not need to pass --repo-dir.
func TestRunScoreIterationRecomputeUsesCwdWhenRepoDirEmpty(t *testing.T) {
	repo, iterLogDir := newScoreTestRepo(t)
	t.Chdir(repo)
	var buf bytes.Buffer
	if err := runScoreIterationRecompute(&buf, iterLogDir, "", 1, nil); err != nil {
		t.Fatalf("runScoreIterationRecompute: %v\n%s", err, buf.String())
	}
}

// A missing iteration surfaces a wrapped error rather than crashing.
func TestRunScoreIterationRecomputeMissingIter(t *testing.T) {
	repo, iterLogDir := newScoreTestRepo(t)
	err := runScoreIterationRecompute(&bytes.Buffer{}, iterLogDir, repo, 99, nil)
	if err == nil {
		t.Fatal("expected error for missing iter, got nil")
	}
	if !strings.Contains(err.Error(), "score iteration") {
		t.Errorf("error should carry the subcommand identity: %v", err)
	}
}

// Drive the cobra subcommand through Execute with --recompute so the RunE
// closure's recompute branch is covered. Asserts the sidecar is written
// as a side effect.
func TestScoreIterationSubcommandRecomputeExecute(t *testing.T) {
	repo, iterLogDir := newScoreTestRepo(t)
	cmd := newScoreIterationCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--iter-log-dir", iterLogDir, "--repo-dir", repo, "--recompute", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\n%s", err, buf.String())
	}
	if _, err := os.Stat(filepath.Join(iterLogDir, "iter-1.score.yaml")); err != nil {
		t.Errorf("sidecar not written via cobra Execute: %v", err)
	}
}

// errWriter returns an error on every Write. Used to trigger the json.
// Encoder error branches in the score subcommands without standing up
// a broken pipe or full-disk fixture.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write boom") }

// runScoreRun's --json branch propagates json.Encoder.Encode errors.
func TestRunScoreRunJSONEncodeError(t *testing.T) {
	prior := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prior })

	repo, iterLogDir := newScoreTestRepo(t)
	err := runScoreRun(errWriter{}, scoreRunOpts{iterLogDir: iterLogDir, repoDir: repo, noWrite: true})
	if err == nil {
		t.Fatal("expected JSON encode error, got nil")
	}
}

// runScoreIterationRecompute's --json branch propagates encoder errors.
func TestRunScoreIterationRecomputeJSONEncodeError(t *testing.T) {
	prior := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prior })

	repo, iterLogDir := newScoreTestRepo(t)
	err := runScoreIterationRecompute(errWriter{}, iterLogDir, repo, 1, nil)
	if err == nil {
		t.Fatal("expected JSON encode error, got nil")
	}
}

// runScoreIteration's --json branch propagates encoder errors. Writes
// a real sidecar first so the read path succeeds; the error is in the
// encode step.
func TestRunScoreIterationJSONEncodeError(t *testing.T) {
	prior := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prior })

	dir := t.TempDir()
	r := scoring.DefaultRubric()
	if _, err := scoring.WriteIterationScore(dir, r.Score(scoring.SignalSet{Iteration: 4, Verifier: scoring.PresentSignal(1.0, "")})); err != nil {
		t.Fatal(err)
	}
	if err := runScoreIteration(errWriter{}, dir, 4); err == nil {
		t.Fatal("expected JSON encode error, got nil")
	}
}

// runScoreSession's --json branch propagates encoder errors.
func TestRunScoreSessionJSONEncodeError(t *testing.T) {
	prior := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prior })

	dir := t.TempDir()
	if _, err := scoring.WriteSessionScore(dir, scoring.SessionScore{SessionID: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := runScoreSession(errWriter{}, dir, "s"); err == nil {
		t.Fatal("expected JSON encode error, got nil")
	}
}

// runScoreRun surfaces persist errors (WriteIterationScoreWithRecord /
// WriteSessionScores). Trigger by replacing the iter-log dir with a
// file BEFORE the write step — checkpoint + score succeed via existing
// fixtures since runScoreRun reads from there first, but writing the
// sidecar fails. Sequencing: the write happens AFTER LoadIterationLog
// reads — we replace the file mid-test by stubbing the writer... no,
// easier path: pass a dir that doesn't exist yet for iterLogDir but is
// the same as the existing iter-1.yaml — already in the existing
// happy-path fixture, the write succeeds. Skipping this branch — the
// reachable persistence errors are out of test reach without a writer
// seam. Documented via the persist.go [defensive-guards] entry already.
// (Placeholder to keep test surface explicit.)
var _ = errors.New

// renderRunSummary covers the `len(sessions) == 0` branch when
// AggregateSessions returns nothing (every iteration has empty
// session_id). Confirms the per-iter table still renders.
func TestRenderRunSummaryNoSessions(t *testing.T) {
	r := scoring.DefaultRubric()
	records := []scoring.IterationRecord{{Iteration: 1}}
	scores := []scoring.Score{{Iteration: 1, Scored: true, Value: 0.5, Band: "fair"}}
	var buf bytes.Buffer
	renderRunSummary(&buf, r, records, scores, nil, true, "/tmp/x")
	if !strings.Contains(buf.String(), "Iterations: 1") {
		t.Errorf("missing iter row: %s", buf.String())
	}
	if strings.Contains(buf.String(), "SESSION") {
		t.Errorf("session header rendered for empty sessions: %s", buf.String())
	}
}

// newScoreIterationCmd / newScoreSessionCmd require exactly one positional arg
// and expose the iter-log-dir flag.
func TestNewScoreLookupCmdsArgsAndFlags(t *testing.T) {
	for _, c := range []struct {
		name string
		cmd  interface{ Args() }
	}{} {
		_ = c
	}
	iter := newScoreIterationCmd()
	if iter.Flags().Lookup("iter-log-dir") == nil {
		t.Error("iteration: missing --iter-log-dir")
	}
	if err := iter.Args(iter, nil); err == nil {
		t.Error("iteration: expected ExactArgs(1) to reject 0 args")
	}
	sess := newScoreSessionCmd()
	if sess.Flags().Lookup("iter-log-dir") == nil {
		t.Error("session: missing --iter-log-dir")
	}
	if err := sess.Args(sess, []string{"a", "b"}); err == nil {
		t.Error("session: expected ExactArgs(1) to reject 2 args")
	}
}
