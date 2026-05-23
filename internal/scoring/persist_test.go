package scoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Per-iteration write produces a sidecar named iter-N.score.yaml next to the
// iteration log, the file round-trips through yaml, and the breakdown survives
// row-for-row in rubric order.
func TestWriteIterationScoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := DefaultRubric()
	set := SignalSet{
		Iteration: 7,
		Verifier:  PresentSignal(0.5, "1/2 verifiers pass"),
		Scope:     PresentSignal(1.0, ""),
	}
	score := r.Score(set)

	path, err := WriteIterationScore(dir, score)
	if err != nil {
		t.Fatalf("WriteIterationScore: %v", err)
	}
	wantPath := filepath.Join(dir, "iter-7.score.yaml")
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got PersistedScore
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Iteration != 7 {
		t.Errorf("Iteration = %d, want 7", got.Iteration)
	}
	if got.RubricVersion != r.Version {
		t.Errorf("RubricVersion = %q, want %q", got.RubricVersion, r.Version)
	}
	if !got.Scored {
		t.Error("Scored = false, want true")
	}
	if !approxEq(got.Value, score.Value) {
		t.Errorf("Value = %g, want %g", got.Value, score.Value)
	}
	if got.Band != score.Band {
		t.Errorf("Band = %q, want %q", got.Band, score.Band)
	}
	if len(got.Breakdown) != len(r.Signals) {
		t.Fatalf("Breakdown len = %d, want %d", len(got.Breakdown), len(r.Signals))
	}
	for i, row := range got.Breakdown {
		if row.Signal != r.Signals[i].ID {
			t.Errorf("Breakdown[%d].Signal = %q, want %q", i, row.Signal, r.Signals[i].ID)
		}
		if row.NominalWeight == 0 {
			t.Errorf("Breakdown[%d] %q lost its nominal_weight", i, row.Signal)
		}
	}
}

// An unscored iteration persists with scored=false, band=unscored, value=0,
// and a full breakdown — the explanation is complete even when no signal was
// present.
func TestWriteIterationScoreUnscored(t *testing.T) {
	dir := t.TempDir()
	r := DefaultRubric()
	score := r.Score(SignalSet{Iteration: 4})

	path, err := WriteIterationScore(dir, score)
	if err != nil {
		t.Fatalf("WriteIterationScore: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got PersistedScore
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Scored {
		t.Error("Scored = true, want false")
	}
	if got.Value != 0 {
		t.Errorf("Value = %g, want 0", got.Value)
	}
	if got.Band != BandUnscored {
		t.Errorf("Band = %q, want %q", got.Band, BandUnscored)
	}
	if len(got.Breakdown) != len(r.Signals) {
		t.Errorf("Breakdown len = %d, want %d", len(got.Breakdown), len(r.Signals))
	}
}

// WriteIterationScores writes one sidecar per input score and returns paths in
// input order.
func TestWriteIterationScoresMany(t *testing.T) {
	dir := t.TempDir()
	r := DefaultRubric()
	scores := r.ScoreAll([]SignalSet{
		{Iteration: 1, Verifier: PresentSignal(1.0, "")},
		{Iteration: 2, Verifier: PresentSignal(0.0, "")},
		{Iteration: 3},
	})

	paths, err := WriteIterationScores(dir, scores)
	if err != nil {
		t.Fatalf("WriteIterationScores: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("paths len = %d, want 3", len(paths))
	}
	for i, iter := range []int{1, 2, 3} {
		want := filepath.Join(dir, "iter-"+itoa(iter)+".score.yaml")
		if paths[i] != want {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want)
		}
		if _, err := os.Stat(paths[i]); err != nil {
			t.Errorf("expected sidecar at %q: %v", paths[i], err)
		}
	}
}

// An empty session_id is not aggregable — those iterations drop out silently.
// Sessions with at least one scored iteration aggregate to mean(scored); a
// session whose iterations are all unscored aggregates to BandUnscored.
func TestAggregateSessionsBuckets(t *testing.T) {
	r := DefaultRubric()
	records := []IterationRecord{
		{Iteration: 1, Agent: AgentInfo{SessionID: "sess-A"}},
		{Iteration: 2, Agent: AgentInfo{SessionID: "sess-A"}},
		{Iteration: 3, Agent: AgentInfo{SessionID: "sess-A"}},
		{Iteration: 4, Agent: AgentInfo{SessionID: ""}}, // skipped
		{Iteration: 5, Agent: AgentInfo{SessionID: "sess-B"}},
	}
	scores := []Score{
		{Iteration: 1, Scored: true, Value: 1.0, Band: "excellent", RubricVersion: r.Version},
		{Iteration: 2, Scored: true, Value: 0.5, Band: "fair", RubricVersion: r.Version},
		{Iteration: 3, Scored: false, Value: 0, Band: BandUnscored, RubricVersion: r.Version},
		{Iteration: 4, Scored: true, Value: 0.9, Band: "excellent", RubricVersion: r.Version},
		{Iteration: 5, Scored: false, Value: 0, Band: BandUnscored, RubricVersion: r.Version},
	}

	sessions := AggregateSessions(r, records, scores)
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d, want 2 (empty-id iteration skipped)", len(sessions))
	}

	if sessions[0].SessionID != "sess-A" {
		t.Errorf("sessions[0].SessionID = %q, want sess-A", sessions[0].SessionID)
	}
	if !sessions[0].Scored {
		t.Error("sess-A scored = false, want true (two iters scored)")
	}
	wantA := (1.0 + 0.5) / 2 // iteration 3 was unscored → drops out
	if !approxEq(sessions[0].Value, wantA) {
		t.Errorf("sess-A Value = %g, want %g", sessions[0].Value, wantA)
	}
	if sessions[0].Band != r.Band(wantA) {
		t.Errorf("sess-A Band = %q, want %q", sessions[0].Band, r.Band(wantA))
	}
	if len(sessions[0].Iterations) != 3 {
		t.Errorf("sess-A Iterations = %v, want 3 entries", sessions[0].Iterations)
	}
	if len(sessions[0].PerIteration) != 3 {
		t.Errorf("sess-A PerIteration len = %d, want 3", len(sessions[0].PerIteration))
	}

	if sessions[1].SessionID != "sess-B" {
		t.Errorf("sessions[1].SessionID = %q, want sess-B", sessions[1].SessionID)
	}
	if sessions[1].Scored {
		t.Error("sess-B scored = true, want false (single iter is unscored)")
	}
	if sessions[1].Band != BandUnscored {
		t.Errorf("sess-B Band = %q, want %q", sessions[1].Band, BandUnscored)
	}
	if sessions[1].Value != 0 {
		t.Errorf("sess-B Value = %g, want 0", sessions[1].Value)
	}
}

// Sessions are sorted by session_id so the output is deterministic.
func TestAggregateSessionsOrderDeterministic(t *testing.T) {
	r := DefaultRubric()
	records := []IterationRecord{
		{Iteration: 1, Agent: AgentInfo{SessionID: "zeta"}},
		{Iteration: 2, Agent: AgentInfo{SessionID: "alpha"}},
		{Iteration: 3, Agent: AgentInfo{SessionID: "mu"}},
	}
	scores := []Score{
		{Iteration: 1, Scored: true, Value: 0.1, RubricVersion: r.Version},
		{Iteration: 2, Scored: true, Value: 0.2, RubricVersion: r.Version},
		{Iteration: 3, Scored: true, Value: 0.3, RubricVersion: r.Version},
	}
	got := AggregateSessions(r, records, scores)
	wantIDs := []string{"alpha", "mu", "zeta"}
	for i, ss := range got {
		if ss.SessionID != wantIDs[i] {
			t.Errorf("sessions[%d].SessionID = %q, want %q", i, ss.SessionID, wantIDs[i])
		}
	}
}

// AggregateSessions returns nil when the inputs do not align by index — the
// contract requires records[i] and scores[i] to describe the same iteration.
func TestAggregateSessionsMisalignedReturnsNil(t *testing.T) {
	got := AggregateSessions(DefaultRubric(),
		[]IterationRecord{{Iteration: 1}, {Iteration: 2}},
		[]Score{{Iteration: 1}},
	)
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

// WriteSessionScore writes session-<id>.score.yaml; the file round-trips and
// the per-iteration refs survive.
func TestWriteSessionScoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ss := SessionScore{
		SessionID:     "abc-123",
		RubricVersion: "9.9.9",
		Iterations:    []int{1, 2},
		Scored:        true,
		Value:         0.6,
		Band:          "fair",
		PerIteration: []SessionIterRef{
			{Iteration: 1, Scored: true, Value: 0.7, Band: "good"},
			{Iteration: 2, Scored: true, Value: 0.5, Band: "fair"},
		},
	}
	path, err := WriteSessionScore(dir, ss)
	if err != nil {
		t.Fatalf("WriteSessionScore: %v", err)
	}
	wantPath := filepath.Join(dir, "session-abc-123.score.yaml")
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got SessionScore
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.SessionID != ss.SessionID || got.RubricVersion != ss.RubricVersion {
		t.Errorf("header lost in roundtrip: got %+v", got)
	}
	if len(got.PerIteration) != 2 {
		t.Errorf("PerIteration len = %d, want 2", len(got.PerIteration))
	}
}

// Writing a session with empty id is a programmer error — surfaces explicitly
// rather than producing an unaddressable file.
func TestSessionScorePathEmptyIDError(t *testing.T) {
	_, err := SessionScorePath("/anywhere", "")
	if err == nil {
		t.Fatal("expected error for empty session_id, got nil")
	}
}

func TestWriteSessionScoreEmptyIDError(t *testing.T) {
	_, err := WriteSessionScore(t.TempDir(), SessionScore{})
	if err == nil {
		t.Fatal("expected error for empty session_id, got nil")
	}
}

// Atomic write: a successful WriteIterationScore leaves no .tmp files behind.
func TestWriteIterationScoreNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	r := DefaultRubric()
	if _, err := WriteIterationScore(dir, r.Score(SignalSet{Iteration: 1, Verifier: PresentSignal(1.0, "")})); err != nil {
		t.Fatalf("WriteIterationScore: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".score-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

// A nonexistent target directory makes os.CreateTemp fail, which surfaces as
// an error through the public WriteIterationScore wrapper. The error message
// must mention the iteration number so log diagnosis is straightforward.
func TestWriteIterationScoreErrorOnMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")
	_, err := WriteIterationScore(missing, DefaultRubric().Score(SignalSet{Iteration: 1, Verifier: PresentSignal(1.0, "")}))
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
	if !strings.Contains(err.Error(), "iter-1") {
		t.Errorf("error should name the iteration: %v", err)
	}
}

// WriteIterationScores returns immediately on the first failure and reports
// only the paths it managed to write — the caller decides whether to retry
// or roll back, so we must not swallow partial progress.
func TestWriteIterationScoresStopsOnFirstError(t *testing.T) {
	good := t.TempDir()
	scores := []Score{
		DefaultRubric().Score(SignalSet{Iteration: 1, Verifier: PresentSignal(1.0, "")}),
	}
	// First write into a healthy dir succeeds, then we corrupt the dir name
	// for the second call.
	paths, err := WriteIterationScores(good, scores)
	if err != nil {
		t.Fatalf("seed write: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("seed paths len = %d, want 1", len(paths))
	}

	// Now request another write into a path that does not exist as a dir.
	missing := filepath.Join(t.TempDir(), "not-a-dir")
	scores2 := []Score{
		DefaultRubric().Score(SignalSet{Iteration: 2, Verifier: PresentSignal(1.0, "")}),
		DefaultRubric().Score(SignalSet{Iteration: 3, Verifier: PresentSignal(1.0, "")}),
	}
	paths2, err := WriteIterationScores(missing, scores2)
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
	if len(paths2) != 0 {
		t.Errorf("got %d paths back, want 0 (nothing should have been written)", len(paths2))
	}
}

func TestWriteSessionScoreErrorOnMissingDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	_, err := WriteSessionScore(missing, SessionScore{SessionID: "sess-err", RubricVersion: "x"})
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
	if !strings.Contains(err.Error(), "sess-err") {
		t.Errorf("error should name the session: %v", err)
	}
}

// WriteSessionScores is the bulk session writer. Cover the success path
// (multiple sessions written in order) and the stop-on-first-error path.
func TestWriteSessionScoresMany(t *testing.T) {
	dir := t.TempDir()
	sessions := []SessionScore{
		{SessionID: "a", RubricVersion: "1.0.0", Scored: true, Value: 0.6, Band: "fair"},
		{SessionID: "b", RubricVersion: "1.0.0", Scored: true, Value: 0.8, Band: "good"},
	}
	paths, err := WriteSessionScores(dir, sessions)
	if err != nil {
		t.Fatalf("WriteSessionScores: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %d, want 2", len(paths))
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing sidecar %q: %v", p, err)
		}
	}
}

func TestWriteSessionScoresStopsOnFirstError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such")
	sessions := []SessionScore{
		{SessionID: "a", RubricVersion: "1.0.0"},
		{SessionID: "b", RubricVersion: "1.0.0"},
	}
	paths, err := WriteSessionScores(missing, sessions)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(paths) != 0 {
		t.Errorf("got %d paths, want 0", len(paths))
	}
}
