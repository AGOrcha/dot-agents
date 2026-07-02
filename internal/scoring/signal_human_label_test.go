package scoring

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/review/labels"
)

// labelTime returns a fixed, strictly increasing timestamp for label
// fixtures: base plus n minutes. Deterministic times keep the
// latest-per-reviewer assertions unambiguous.
func labelTime(n int) time.Time {
	return time.Date(2026, 7, 1, 12, n, 0, 0, time.UTC)
}

// addLabel persists one label via the real labels package so the fixture
// matches exactly what the R5 collection endpoint writes.
func addLabel(t *testing.T, dir string, iter int, in labels.AddInput) labels.Label {
	t.Helper()
	l, err := labels.Add(dir, iter, in)
	if err != nil {
		t.Fatalf("labels.Add() = %v, want nil", err)
	}
	return l
}

// structured builds a Structured judgement fixture.
func structured(correctness int, scope labels.ScopeJudgement, halluc labels.Hallucination) labels.Structured {
	return labels.Structured{Correctness: correctness, ScopeJudgement: scope, Hallucination: halluc}
}

// --- absent cases: the signal must never vote without labels ---------------

func TestExtractHumanLabelSignalsEmptyDir(t *testing.T) {
	got := ExtractHumanLabelSignals("  ", 3)
	if got.Present {
		t.Fatalf("Present = true, want absent for empty iterLogDir: %+v", got)
	}
	if !strings.Contains(got.Detail, "no iteration-log directory") {
		t.Errorf("Detail = %q, want the empty-dir explanation", got.Detail)
	}
}

func TestExtractHumanLabelSignalsNoSidecar(t *testing.T) {
	got := ExtractHumanLabelSignals(t.TempDir(), 7)
	if got.Present {
		t.Fatalf("Present = true, want absent when no sidecar exists: %+v", got)
	}
	if !strings.Contains(got.Detail, "no human labels for iter-7") {
		t.Errorf("Detail = %q, want the no-labels explanation", got.Detail)
	}
}

func TestExtractHumanLabelSignalsCorruptSidecar(t *testing.T) {
	dir := t.TempDir()
	path := labels.IterationLabelsPath(dir, 2)
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ExtractHumanLabelSignals(dir, 2)
	if got.Present {
		t.Fatalf("Present = true, want absent for a corrupt sidecar: %+v", got)
	}
	if !strings.Contains(got.Detail, "cannot read labels sidecar for iter-2") {
		t.Errorf("Detail = %q, want the read-failure explanation", got.Detail)
	}
}

func TestExtractHumanLabelSignalsUnsupportedSidecarSchema(t *testing.T) {
	dir := t.TempDir()
	addLabel(t, dir, 4, labels.AddInput{
		Actor: "rev@example.com", Role: labels.RoleReviewer,
		Structured: structured(3, labels.ScopeOnTarget, labels.HallucinationNone),
		Now:        labelTime(0),
	})
	sc, err := labels.ReadSidecar(dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	sc.SchemaVersion = "2.0.0"
	if _, err := labels.WriteSidecar(dir, sc); err != nil {
		t.Fatal(err)
	}

	got := ExtractHumanLabelSignals(dir, 4)
	if got.Present {
		t.Fatalf("Present = true, want absent for schema major 2: %+v", got)
	}
	if !strings.Contains(got.Detail, `unsupported label schema "2.0.0"`) {
		t.Errorf("Detail = %q, want it to name the unsupported version", got.Detail)
	}
}

func TestExtractHumanLabelSignalsUnsupportedLabelSchema(t *testing.T) {
	dir := t.TempDir()
	addLabel(t, dir, 4, labels.AddInput{
		Actor: "rev@example.com", Role: labels.RoleReviewer,
		Structured: structured(3, labels.ScopeOnTarget, labels.HallucinationNone),
		Now:        labelTime(0),
	})
	sc, err := labels.ReadSidecar(dir, 4)
	if err != nil {
		t.Fatal(err)
	}
	sc.Labels[0].SchemaVersion = "3" // majorless form is its own major
	if _, err := labels.WriteSidecar(dir, sc); err != nil {
		t.Fatal(err)
	}

	got := ExtractHumanLabelSignals(dir, 4)
	if got.Present {
		t.Fatalf("Present = true, want absent for a label at schema major 3: %+v", got)
	}
	if !strings.Contains(got.Detail, `unsupported label schema "3"`) {
		t.Errorf("Detail = %q, want it to name the offending label version", got.Detail)
	}
}

// --- sub-score mapping (spec D5.7) ------------------------------------------

func TestHumanLabelSubScoreMapping(t *testing.T) {
	tests := []struct {
		name string
		in   labels.Structured
		want float64
	}{
		{"best on every dimension", structured(3, labels.ScopeOnTarget, labels.HallucinationNone), 1.0},
		{"worst on every dimension", structured(0, labels.ScopeBreach, labels.HallucinationMajor), 0.0},
		{"middle enums", structured(3, labels.ScopePartial, labels.HallucinationMinor), (1.0 + 0.5 + 0.5) / 3.0},
		{"correctness only partial", structured(1, labels.ScopeOnTarget, labels.HallucinationNone), (1.0/3.0 + 1.0 + 1.0) / 3.0},
		{"scope breach drags", structured(2, labels.ScopeBreach, labels.HallucinationNone), (2.0/3.0 + 0.0 + 1.0) / 3.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := humanLabelSubScore(tt.in); !approxEq(got, tt.want) {
				t.Errorf("humanLabelSubScore(%+v) = %g, want %g", tt.in, got, tt.want)
			}
		})
	}
}

// --- aggregation (spec D5.8 + OQ2) ------------------------------------------

func TestExtractHumanLabelSignalsSingleReviewer(t *testing.T) {
	dir := t.TempDir()
	addLabel(t, dir, 5, labels.AddInput{
		Actor: "rev@example.com", Role: labels.RoleReviewer,
		Structured: structured(3, labels.ScopeOnTarget, labels.HallucinationNone),
		Now:        labelTime(0),
	})

	got := ExtractHumanLabelSignals(dir, 5)
	if !got.Present {
		t.Fatalf("Present = false, want true: %+v", got)
	}
	if !approxEq(got.SubScore, 1.0) {
		t.Errorf("SubScore = %g, want 1.0", got.SubScore)
	}
	if !strings.Contains(got.Detail, "rev@example.com") {
		t.Errorf("Detail = %q, want it to name the reviewer", got.Detail)
	}
}

func TestExtractHumanLabelSignalsMeanAcrossReviewers(t *testing.T) {
	dir := t.TempDir()
	addLabel(t, dir, 5, labels.AddInput{
		Actor: "alice@example.com", Role: labels.RoleReviewer,
		Structured: structured(3, labels.ScopeOnTarget, labels.HallucinationNone), // 1.0
		Now:        labelTime(0),
	})
	addLabel(t, dir, 5, labels.AddInput{
		Actor: "bob@example.com", Role: labels.RoleReviewer,
		Structured: structured(0, labels.ScopeBreach, labels.HallucinationMajor), // 0.0
		Now:        labelTime(1),
	})

	got := ExtractHumanLabelSignals(dir, 5)
	if !got.Present || !approxEq(got.SubScore, 0.5) {
		t.Fatalf("SubScore = %+v, want present 0.5 (mean of 1.0 and 0.0)", got)
	}
	// Actors render sorted so the breakdown detail is reproducible.
	if !strings.Contains(got.Detail, "alice@example.com, bob@example.com") {
		t.Errorf("Detail = %q, want sorted actor list", got.Detail)
	}
}

func TestExtractHumanLabelSignalsLatestPerReviewerWins(t *testing.T) {
	dir := t.TempDir()
	// Same reviewer labels twice: only the later label may vote (D5.8).
	addLabel(t, dir, 6, labels.AddInput{
		Actor: "rev@example.com", Role: labels.RoleReviewer,
		Structured: structured(0, labels.ScopeBreach, labels.HallucinationMajor), // 0.0
		Now:        labelTime(0),
	})
	addLabel(t, dir, 6, labels.AddInput{
		Actor: "rev@example.com", Role: labels.RoleReviewer,
		Structured: structured(3, labels.ScopeOnTarget, labels.HallucinationNone), // 1.0
		Now:        labelTime(5),
	})

	got := ExtractHumanLabelSignals(dir, 6)
	if !got.Present || !approxEq(got.SubScore, 1.0) {
		t.Fatalf("SubScore = %+v, want present 1.0 (latest label only)", got)
	}
}

func TestExtractHumanLabelSignalsEditHistoryUsesLatestEdit(t *testing.T) {
	dir := t.TempDir()
	l := addLabel(t, dir, 8, labels.AddInput{
		Actor: "rev@example.com", Role: labels.RoleReviewer,
		Structured: structured(0, labels.ScopeBreach, labels.HallucinationMajor), // 0.0
		Now:        labelTime(0),
	})
	if _, err := labels.EditLabel(dir, 8, l.ID, labels.EditInput{
		Actor: "rev@example.com", Role: labels.RoleReviewer,
		Structured: structured(3, labels.ScopeOnTarget, labels.HallucinationNone), // 1.0
		Now:        labelTime(2),
	}); err != nil {
		t.Fatalf("labels.EditLabel() = %v, want nil", err)
	}

	got := ExtractHumanLabelSignals(dir, 8)
	if !got.Present || !approxEq(got.SubScore, 1.0) {
		t.Fatalf("SubScore = %+v, want present 1.0 (effective = latest edit)", got)
	}
}

func TestExtractHumanLabelSignalsAdminOverrideSupersedes(t *testing.T) {
	dir := t.TempDir()
	// Two reviewers agree on 1.0; the admin's own override label says 0.0 —
	// OQ2: the override supersedes the reviewer mean entirely.
	for _, actor := range []string{"alice@example.com", "bob@example.com"} {
		addLabel(t, dir, 9, labels.AddInput{
			Actor: actor, Role: labels.RoleReviewer,
			Structured: structured(3, labels.ScopeOnTarget, labels.HallucinationNone),
			Now:        labelTime(0),
		})
	}
	addLabel(t, dir, 9, labels.AddInput{
		Actor: "admin@example.com", Role: labels.RoleAdmin, AdminOverride: true,
		Structured: structured(0, labels.ScopeBreach, labels.HallucinationMajor),
		Now:        labelTime(1),
	})

	got := ExtractHumanLabelSignals(dir, 9)
	if !got.Present || !approxEq(got.SubScore, 0.0) {
		t.Fatalf("SubScore = %+v, want present 0.0 (admin override wins)", got)
	}
	if !strings.Contains(got.Detail, "admin override by admin@example.com") {
		t.Errorf("Detail = %q, want the admin-override explanation", got.Detail)
	}
}

func TestExtractHumanLabelSignalsAdminEditStaysReviewerLabel(t *testing.T) {
	dir := t.TempDir()
	// OQ2's second half: an admin *editing* a reviewer's label keeps the
	// label attributed to the reviewer — it participates as the reviewer's
	// latest state, not as an override.
	l := addLabel(t, dir, 10, labels.AddInput{
		Actor: "rev@example.com", Role: labels.RoleReviewer,
		Structured: structured(3, labels.ScopeOnTarget, labels.HallucinationNone), // 1.0
		Now:        labelTime(0),
	})
	if _, err := labels.EditLabel(dir, 10, l.ID, labels.EditInput{
		Actor: "admin@example.com", Role: labels.RoleAdmin,
		Structured: structured(1, labels.ScopePartial, labels.HallucinationMinor), // (1/3+.5+.5)/3
		Now:        labelTime(3),
	}); err != nil {
		t.Fatalf("labels.EditLabel() = %v, want nil", err)
	}

	got := ExtractHumanLabelSignals(dir, 10)
	want := (1.0/3.0 + 0.5 + 0.5) / 3.0
	if !got.Present || !approxEq(got.SubScore, want) {
		t.Fatalf("SubScore = %+v, want present %g (admin edit becomes reviewer state)", got, want)
	}
	if strings.Contains(got.Detail, "admin override") {
		t.Errorf("Detail = %q, an admin edit must not report as an override", got.Detail)
	}
	if !strings.Contains(got.Detail, "rev@example.com") {
		t.Errorf("Detail = %q, want the label still attributed to the reviewer", got.Detail)
	}
}

func TestLatestLabelPerActorTieKeepsLaterEntry(t *testing.T) {
	ts := labelTime(0)
	pool := []labels.Label{
		{ID: "a", Actor: "rev", UpdatedAt: ts},
		{ID: "b", Actor: "rev", UpdatedAt: ts},
	}
	latest := latestLabelPerActor(pool)
	if got := latest["rev"].ID; got != "b" {
		t.Errorf("tie kept label %q, want the later sidecar entry \"b\"", got)
	}
}

// --- absent-safety pin: existing iterations without labels are untouched ---

// An iteration with every telemetry signal present but no human label scores
// exactly the renormalized mean over the telemetry signals — human_label
// contributes nothing and its breakdown row stays absent. This pins spec
// R4/R5: shipping the signal does not retroactively change label-less scores
// computed under this rubric version.
func TestHumanLabelAbsentDoesNotVote(t *testing.T) {
	r := DefaultRubric()
	set := SignalSet{
		Iteration:          11,
		Landed:             PresentSignal(1.0, ""),
		Verifier:           PresentSignal(1.0, ""),
		Tests:              PresentSignal(0.5, ""),
		HumanLabel:         AbsentSignal("no human labels for iter-11"),
		CorrectionPressure: PresentSignal(1.0, ""),
		Scope:              PresentSignal(1.0, ""),
		HookOutcomes:       PresentSignal(1.0, ""),
		TokenEfficiency:    PresentSignal(1.0, ""),
	}

	got := r.Score(set)
	if !got.Scored {
		t.Fatal("Scored = false, want true")
	}
	// Present weights: 1.0 - 0.15 (human_label) = 0.85; only tests deviates.
	presentSum := 0.0
	weighted := 0.0
	for _, s := range r.Signals {
		if s.ID == SignalHumanLabel {
			continue
		}
		presentSum += s.Weight
		weighted += s.Weight * set.Value(s.ID).SubScore
	}
	if want := weighted / presentSum; !approxEq(got.Value, want) {
		t.Errorf("Value = %g, want %g (renormalized over the present signals)", got.Value, want)
	}
	for _, row := range got.Breakdown {
		if row.Signal != SignalHumanLabel {
			continue
		}
		if row.Present || row.EffectiveWeight != 0 || row.Contribution != 0 {
			t.Errorf("human_label row = %+v, want absent with zero weight and contribution", row)
		}
	}
}
