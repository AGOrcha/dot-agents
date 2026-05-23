package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/NikashPrakash/dot-agents/internal/scoring"
)

// An empty iter-log dir produces a friendly "no iterations" message rather
// than an error — the most common state when the command is first wired in.
func TestScoreRunEmptyIterLog(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := runScoreRun(&buf, scoreRunOpts{iterLogDir: dir, repoDir: dir, noWrite: true}); err != nil {
		t.Fatalf("runScoreRun: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "no iterations found") {
		t.Errorf("expected empty-log notice, got:\n%s", got)
	}
}

// CLI smoke: write a fixture iter-N.score.yaml, then drive `da score iteration N`
// against it and assert the breakdown rendering names every rubric signal.
// This is the smoke the persist task wired toward: a known sidecar, the
// command, an explainable rendering.
func TestScoreIterationRendersBreakdown(t *testing.T) {
	dir := t.TempDir()
	r := scoring.DefaultRubric()
	set := scoring.SignalSet{
		Iteration: 42,
		Verifier:  scoring.PresentSignal(1.0, "all gates green"),
		Scope:     scoring.PresentSignal(0.5, "partial scope"),
	}
	rec := scoring.IterationRecord{
		Iteration: 42,
		Verifiers: []scoring.VerifierRecord{
			{LinkedTraces: []scoring.LinkedTrace{{TraceRef: "t/a", OutcomeRef: "commit:beef"}}},
		},
	}
	if _, err := scoring.WriteIterationScoreWithRecord(dir, r.Score(set), rec); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var buf bytes.Buffer
	if err := runScoreIteration(&buf, dir, 42); err != nil {
		t.Fatalf("runScoreIteration: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Iteration 42") {
		t.Errorf("missing iteration header in:\n%s", got)
	}
	if !strings.Contains(got, "rubric "+r.Version) {
		t.Errorf("missing rubric version in:\n%s", got)
	}
	if !strings.Contains(got, "linked_traces_to_outcomes: yes") {
		t.Errorf("derived marker not rendered:\n%s", got)
	}
	for _, sig := range r.Signals {
		if !strings.Contains(got, string(sig.ID)) {
			t.Errorf("breakdown missing signal %q in:\n%s", sig.ID, got)
		}
	}
	// The verifier signal was present at sub-score 1.0; its detail string must
	// reach the rendered breakdown — that is the explainability promise.
	if !strings.Contains(got, "all gates green") {
		t.Errorf("verifier detail dropped:\n%s", got)
	}
}

// Iteration without a sidecar errors with a hint to run `da score run`.
func TestScoreIterationMissingSidecar(t *testing.T) {
	dir := t.TempDir()
	err := runScoreIteration(&bytes.Buffer{}, dir, 99)
	if err == nil {
		t.Fatal("expected error for missing sidecar, got nil")
	}
	if !strings.Contains(err.Error(), "no score sidecar") || !strings.Contains(err.Error(), "da score run") {
		t.Errorf("error message should point at the fix: %v", err)
	}
}

// Session smoke: write a session-<id>.score.yaml, drive `da score session <id>`,
// assert the session view renders every per-iteration ref row.
func TestScoreSessionRendersPerIteration(t *testing.T) {
	dir := t.TempDir()
	ss := scoring.SessionScore{
		SessionID:     "smoke-session-1",
		RubricVersion: "9.9.9",
		Iterations:    []int{1, 2, 3},
		Scored:        true,
		Value:         0.75,
		Band:          "good",
		PerIteration: []scoring.SessionIterRef{
			{Iteration: 1, Scored: true, Value: 1.0, Band: "excellent"},
			{Iteration: 2, Scored: true, Value: 0.5, Band: "fair"},
			{Iteration: 3, Scored: false, Value: 0, Band: scoring.BandUnscored},
		},
	}
	if _, err := scoring.WriteSessionScore(dir, ss); err != nil {
		t.Fatalf("WriteSessionScore: %v", err)
	}

	var buf bytes.Buffer
	if err := runScoreSession(&buf, dir, "smoke-session-1"); err != nil {
		t.Fatalf("runScoreSession: %v", err)
	}
	got := buf.String()

	if !strings.Contains(got, "Session smoke-session-1") {
		t.Errorf("missing session header in:\n%s", got)
	}
	if !strings.Contains(got, "rubric 9.9.9") {
		t.Errorf("missing rubric version in:\n%s", got)
	}
	if !strings.Contains(got, "0.750") || !strings.Contains(got, "good") {
		t.Errorf("missing session value/band in:\n%s", got)
	}
	for _, want := range []string{"1.000", "0.500", "excellent", "fair", scoring.BandUnscored} {
		if !strings.Contains(got, want) {
			t.Errorf("per-iter row missing %q in:\n%s", want, got)
		}
	}
}

// `da score session` errors when the sidecar is missing, again with a hint.
func TestScoreSessionMissingSidecar(t *testing.T) {
	dir := t.TempDir()
	err := runScoreSession(&bytes.Buffer{}, dir, "no-such-session")
	if err == nil {
		t.Fatal("expected error for missing sidecar, got nil")
	}
	if !strings.Contains(err.Error(), "no session sidecar") || !strings.Contains(err.Error(), "da score run") {
		t.Errorf("error message should point at the fix: %v", err)
	}
}

// resolveIterLogDir prefers explicit values and falls back to the default,
// matching how every score subcommand wires its --iter-log-dir flag.
func TestResolveIterLogDirDefault(t *testing.T) {
	if got := resolveIterLogDir(""); got != defaultIterLogDir {
		t.Errorf("default = %q, want %q", got, defaultIterLogDir)
	}
	if got := resolveIterLogDir("/custom/path"); got != "/custom/path" {
		t.Errorf("explicit override = %q, want /custom/path", got)
	}
}

// truncStr shortens strings to a width with an ellipsis; ≤3 widths fall back to
// a hard crop because three dots have no room.
func TestTruncStr(t *testing.T) {
	tests := []struct {
		s     string
		width int
		want  string
	}{
		{"abc", 10, "abc"},
		{"abcdef", 6, "abcdef"},
		{"abcdef", 5, "ab..."},
		{"abcdef", 3, "abc"},
		{"abcdef", 0, "abcdef"},
	}
	for _, tt := range tests {
		got := truncStr(tt.s, tt.width)
		if got != tt.want {
			t.Errorf("truncStr(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
		}
	}
}

// End-to-end mini-smoke for the iteration sidecar's YAML shape: the renderer
// must read what the persister writes without an in-band conversion table.
func TestScoreIterationSidecarRoundTripsThroughCommand(t *testing.T) {
	dir := t.TempDir()
	src := scoring.PersistedScore{
		Iteration:              7,
		RubricVersion:          "test-rubric",
		Scored:                 true,
		Value:                  0.42,
		Band:                   "poor",
		LinkedTracesToOutcomes: true,
		Breakdown: []scoring.PersistedContribution{
			{Signal: "landed", Label: "Landed", Present: true, SubScore: 0.42, NominalWeight: 1.0, EffectiveWeight: 1.0, Contribution: 0.42, Detail: "marker text"},
		},
	}
	data, err := yaml.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "iter-7.score.yaml"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var buf bytes.Buffer
	if err := runScoreIteration(&buf, dir, 7); err != nil {
		t.Fatalf("runScoreIteration: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Iteration 7", "test-rubric", "0.420", "poor", "marker text", "linked_traces_to_outcomes: yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
