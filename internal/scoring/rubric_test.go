package scoring

import (
	"math"
	"strings"
	"testing"
)

func TestRubricVersionPinned(t *testing.T) {
	if RubricVersion != "2.0.2" {
		t.Errorf("RubricVersion = %q, want 2.0.2 — a version change must be deliberate", RubricVersion)
	}
	if got := DefaultRubric().Version; got != RubricVersion {
		t.Errorf("DefaultRubric().Version = %q, want %q", got, RubricVersion)
	}
}

func TestDefaultRubricValidates(t *testing.T) {
	if err := DefaultRubric().Validate(); err != nil {
		t.Fatalf("DefaultRubric().Validate() = %v, want nil", err)
	}
}

func TestDefaultRubricWeightsSumToOne(t *testing.T) {
	var sum float64
	for _, s := range DefaultRubric().Signals {
		sum += s.Weight
	}
	if math.Abs(sum-1.0) > weightEpsilon {
		t.Errorf("signal weights sum to %g, want 1.0", sum)
	}
}

func TestDefaultRubricSignalSet(t *testing.T) {
	want := []SignalID{
		SignalLanded, SignalVerifier, SignalTests,
		SignalCorrectionPressure, SignalScope, SignalTokenEfficiency,
	}
	got := DefaultRubric().Signals
	if len(got) != len(want) {
		t.Fatalf("rubric has %d signals, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("signal[%d].ID = %q, want %q", i, got[i].ID, id)
		}
		if got[i].Label == "" || got[i].Description == "" {
			t.Errorf("signal %q is missing a label or description", id)
		}
	}
}

func TestDefaultRubricCombinationMethod(t *testing.T) {
	if got := DefaultRubric().Combination; got != CombineWeightedMeanRenormalized {
		t.Errorf("Combination = %q, want %q", got, CombineWeightedMeanRenormalized)
	}
}

func TestSignalLookup(t *testing.T) {
	r := DefaultRubric()

	spec, ok := r.Signal(SignalLanded)
	if !ok {
		t.Fatal("Signal(landed) ok = false, want true")
	}
	if spec.Weight != 0.22 {
		t.Errorf("landed weight = %g, want 0.22", spec.Weight)
	}

	if _, ok := r.Signal(SignalID("does_not_exist")); ok {
		t.Error("Signal(does_not_exist) ok = true, want false")
	}
}

func TestTwoWaySignals(t *testing.T) {
	twoWay := make(map[SignalID]bool)
	for _, s := range DefaultRubric().TwoWaySignals() {
		twoWay[s.ID] = true
	}

	wantTwoWay := []SignalID{SignalLanded, SignalVerifier, SignalTests, SignalScope}
	for _, id := range wantTwoWay {
		if !twoWay[id] {
			t.Errorf("signal %q should be two-way (objective + self-reported source)", id)
		}
	}
	wantOneWay := []SignalID{SignalCorrectionPressure, SignalTokenEfficiency}
	for _, id := range wantOneWay {
		if twoWay[id] {
			t.Errorf("signal %q should not be two-way", id)
		}
	}
	if len(twoWay) != len(wantTwoWay) {
		t.Errorf("TwoWaySignals returned %d signals, want %d", len(twoWay), len(wantTwoWay))
	}
}

func TestBand(t *testing.T) {
	r := DefaultRubric()
	tests := []struct {
		score float64
		want  string
	}{
		{1.00, "excellent"},
		{0.85, "excellent"}, // inclusive lower bound
		{0.849, "good"},
		{0.70, "good"},
		{0.69, "fair"},
		{0.50, "fair"},
		{0.49, "poor"},
		{0.00, "poor"},
		{1.50, "excellent"}, // clamps above
		{-0.5, "poor"},      // clamps below
	}
	for _, tt := range tests {
		if got := r.Band(tt.score); got != tt.want {
			t.Errorf("Band(%.3f) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestBandEmptyLadder(t *testing.T) {
	if got := (Rubric{}).Band(0.5); got != "" {
		t.Errorf("Band on empty ladder = %q, want empty string", got)
	}
}

func TestValidateRejectsBadRubrics(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Rubric)
		errSubstr string
	}{
		{
			name:      "empty version",
			mutate:    func(r *Rubric) { r.Version = "" },
			errSubstr: "version is empty",
		},
		{
			name:      "empty combination",
			mutate:    func(r *Rubric) { r.Combination = "" },
			errSubstr: "combination method is empty",
		},
		{
			name:      "no signals",
			mutate:    func(r *Rubric) { r.Signals = nil },
			errSubstr: "no signals",
		},
		{
			name: "duplicate signal ID",
			mutate: func(r *Rubric) {
				r.Signals[1].ID = r.Signals[0].ID
			},
			errSubstr: "duplicate signal ID",
		},
		{
			name: "non-positive weight",
			mutate: func(r *Rubric) {
				r.Signals[0].Weight = 0
			},
			errSubstr: "non-positive weight",
		},
		{
			name: "weights do not sum to 1.0",
			mutate: func(r *Rubric) {
				r.Signals[0].Weight += 0.5
			},
			errSubstr: "weights sum to",
		},
		{
			name:      "no bands",
			mutate:    func(r *Rubric) { r.Bands = nil },
			errSubstr: "no score bands",
		},
		{
			name: "band without name",
			mutate: func(r *Rubric) {
				r.Bands[0].Name = ""
			},
			errSubstr: "has no name",
		},
		{
			name: "bands not sorted descending",
			mutate: func(r *Rubric) {
				r.Bands[0].Min = 0.1 // below the next band's Min
			},
			errSubstr: "not sorted descending",
		},
		{
			name: "lowest band not anchored at zero",
			mutate: func(r *Rubric) {
				r.Bands[len(r.Bands)-1].Min = 0.2
			},
			errSubstr: "want 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := DefaultRubric()
			tt.mutate(&r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.errSubstr)
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("Validate() = %q, want it to contain %q", err, tt.errSubstr)
			}
		})
	}
}
