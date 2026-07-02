package scoring

import (
	"testing"
)

// All-signals-present, all sub-scores 1.0: the score is 1.0, every row carries
// its nominal weight as the effective weight (weights sum to 1.0 → no
// renormalization), and contributions sum exactly to the value.
func TestScoreAllPresentMax(t *testing.T) {
	r := DefaultRubric()
	set := SignalSet{
		Iteration:          5,
		Landed:             PresentSignal(1.0, ""),
		Verifier:           PresentSignal(1.0, ""),
		Tests:              PresentSignal(1.0, ""),
		HumanLabel:         PresentSignal(1.0, ""),
		CorrectionPressure: PresentSignal(1.0, ""),
		Scope:              PresentSignal(1.0, ""),
		HookOutcomes:       PresentSignal(1.0, ""),
		TokenEfficiency:    PresentSignal(1.0, ""),
	}

	got := r.Score(set)
	if !got.Scored {
		t.Fatal("Scored = false, want true")
	}
	if !approxEq(got.Value, 1.0) {
		t.Errorf("Value = %g, want 1.0", got.Value)
	}
	if got.Band != "excellent" {
		t.Errorf("Band = %q, want excellent", got.Band)
	}
	if got.Iteration != 5 {
		t.Errorf("Iteration = %d, want 5", got.Iteration)
	}
	if got.RubricVersion != r.Version {
		t.Errorf("RubricVersion = %q, want %q", got.RubricVersion, r.Version)
	}

	var sum float64
	for _, row := range got.Breakdown {
		sum += row.Contribution
		if !approxEq(row.EffectiveWeight, row.NominalWeight) {
			t.Errorf("all-present should not renormalize, but %q has eff=%g nom=%g",
				row.Signal, row.EffectiveWeight, row.NominalWeight)
		}
	}
	if !approxEq(sum, got.Value) {
		t.Errorf("contributions sum to %g, want exactly %g", sum, got.Value)
	}
}

// Some signals absent: the weights renormalize over the present ones, and the
// contributions still sum exactly to Value.
func TestScoreSomeAbsentRenormalizes(t *testing.T) {
	r := DefaultRubric()
	// Only verifier (.15) and scope (.11) present — present sum = .26.
	set := SignalSet{
		Verifier: PresentSignal(1.0, ""),
		Scope:    PresentSignal(0.5, ""),
	}
	got := r.Score(set)
	if !got.Scored {
		t.Fatal("Scored = false, want true")
	}
	const presentSum = 0.15 + 0.11
	want := (0.15*1.0 + 0.11*0.5) / presentSum
	if !approxEq(got.Value, want) {
		t.Errorf("Value = %g, want %g", got.Value, want)
	}

	var sum float64
	for _, row := range got.Breakdown {
		sum += row.Contribution
		if row.Present {
			wantEff := row.NominalWeight / presentSum
			if !approxEq(row.EffectiveWeight, wantEff) {
				t.Errorf("%q eff = %g, want %g", row.Signal, row.EffectiveWeight, wantEff)
			}
		} else {
			if row.EffectiveWeight != 0 || row.Contribution != 0 {
				t.Errorf("absent %q got eff=%g contrib=%g, want both 0",
					row.Signal, row.EffectiveWeight, row.Contribution)
			}
		}
	}
	if !approxEq(sum, got.Value) {
		t.Errorf("contributions sum to %g, want exactly %g", sum, got.Value)
	}
}

// Every signal absent: Scored=false, Value=0, Band=unscored. The breakdown
// still carries one row per rubric signal so the unscored explanation is
// complete.
func TestScoreEveryAbsentUnscored(t *testing.T) {
	r := DefaultRubric()
	got := r.Score(SignalSet{Iteration: 9})
	if got.Scored {
		t.Error("Scored = true, want false")
	}
	if got.Value != 0 {
		t.Errorf("Value = %g, want 0 when unscored", got.Value)
	}
	if got.Band != BandUnscored {
		t.Errorf("Band = %q, want %q", got.Band, BandUnscored)
	}
	if len(got.Breakdown) != len(r.Signals) {
		t.Fatalf("Breakdown len = %d, want %d", len(got.Breakdown), len(r.Signals))
	}
	for _, row := range got.Breakdown {
		if row.Present {
			t.Errorf("signal %q should be absent", row.Signal)
		}
		if row.EffectiveWeight != 0 || row.Contribution != 0 {
			t.Errorf("absent %q has nonzero weight/contribution", row.Signal)
		}
		if row.NominalWeight == 0 {
			t.Errorf("breakdown row %q lost its nominal weight", row.Signal)
		}
	}
}

// Breakdown order matches the rubric's declared signal order, for stable
// rendering downstream.
func TestScoreBreakdownInRubricOrder(t *testing.T) {
	r := DefaultRubric()
	got := r.Score(SignalSet{})
	for i, row := range got.Breakdown {
		if row.Signal != r.Signals[i].ID {
			t.Errorf("Breakdown[%d].Signal = %q, want %q", i, row.Signal, r.Signals[i].ID)
		}
		if row.Label != r.Signals[i].Label {
			t.Errorf("Breakdown[%d].Label = %q, want %q", i, row.Label, r.Signals[i].Label)
		}
	}
}

// SubScore details survive into the breakdown — the explanation strings the
// extractors produced are what cli will render.
func TestScoreCarriesDetail(t *testing.T) {
	r := DefaultRubric()
	set := SignalSet{Verifier: PresentSignal(0.7, "2/3 verifiers passed")}
	got := r.Score(set)

	var found bool
	for _, row := range got.Breakdown {
		if row.Signal != SignalVerifier {
			continue
		}
		found = true
		if row.Detail != "2/3 verifiers passed" {
			t.Errorf("Detail = %q, want %q", row.Detail, "2/3 verifiers passed")
		}
		if row.SubScore != 0.7 {
			t.Errorf("SubScore = %g, want 0.7", row.SubScore)
		}
	}
	if !found {
		t.Error("verifier row missing from breakdown")
	}
}

// Worst-case present score: a single signal present with sub-score 0 yields
// Value 0, Band "poor", but Scored stays true — distinct from BandUnscored.
func TestScorePresentZeroIsNotUnscored(t *testing.T) {
	got := DefaultRubric().Score(SignalSet{Landed: PresentSignal(0.0, "")})
	if !got.Scored {
		t.Error("a present-but-zero signal should still be Scored")
	}
	if got.Value != 0.0 {
		t.Errorf("Value = %g, want 0.0", got.Value)
	}
	if got.Band != "poor" {
		t.Errorf("Band = %q, want poor", got.Band)
	}
}

// Band lookup is delegated to the rubric — pin a few thresholds end-to-end.
func TestScoreBandThresholds(t *testing.T) {
	r := DefaultRubric()
	cases := []struct {
		landed float64
		want   string
	}{
		{0.20, "poor"},
		{0.50, "fair"},
		{0.70, "good"},
		{0.85, "excellent"},
	}
	for _, tt := range cases {
		got := r.Score(SignalSet{Landed: PresentSignal(tt.landed, "")})
		if got.Band != tt.want {
			t.Errorf("landed=%g → Band=%q, want %q", tt.landed, got.Band, tt.want)
		}
	}
}

func TestScoreAll(t *testing.T) {
	r := DefaultRubric()
	sets := []SignalSet{
		{Iteration: 1, Verifier: PresentSignal(1.0, "")},
		{Iteration: 2, Verifier: PresentSignal(0.0, "")},
		{Iteration: 3}, // unscored
	}
	got := r.ScoreAll(sets)
	if len(got) != 3 {
		t.Fatalf("ScoreAll len = %d, want 3", len(got))
	}
	for i, want := range []int{1, 2, 3} {
		if got[i].Iteration != want {
			t.Errorf("ScoreAll[%d].Iteration = %d, want %d", i, got[i].Iteration, want)
		}
	}
	if !got[0].Scored || got[0].Value != 1.0 {
		t.Errorf("ScoreAll[0] = %+v, want scored 1.0", got[0])
	}
	if !got[1].Scored || got[1].Value != 0.0 {
		t.Errorf("ScoreAll[1] = %+v, want scored 0.0", got[1])
	}
	if got[2].Scored {
		t.Errorf("ScoreAll[2] should be unscored (no signals present)")
	}
}

func TestScoreAllEmpty(t *testing.T) {
	if got := DefaultRubric().ScoreAll(nil); len(got) != 0 {
		t.Errorf("ScoreAll(nil) = %d entries, want 0", len(got))
	}
}
