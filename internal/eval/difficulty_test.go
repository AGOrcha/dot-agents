package eval

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// minimal returns inputs at the floor of every signal, so per-signal tests
// can vary exactly one axis while the others stay pinned to easy.
func minimal() DifficultyInputs {
	return DifficultyInputs{NodeCount: 1, EdgeCount: 0, Cyclomatic: 1}
}

// TestDeriveDifficultyBoundaries pins every rubric v1 threshold, one signal
// at a time. A future rubric change must show up here as a deliberate diff.
func TestDeriveDifficultyBoundaries(t *testing.T) {
	tests := []struct {
		name string
		in   DifficultyInputs
		want Difficulty
	}{
		// Floor: everything at its minimum.
		{"all minimal", minimal(), DifficultyEasy},

		// Cyclomatic axis (nodes=1, edges=0).
		{"cyclomatic at easy max", DifficultyInputs{NodeCount: 1, EdgeCount: 0, Cyclomatic: 3}, DifficultyEasy},
		{"cyclomatic just above easy", DifficultyInputs{NodeCount: 1, EdgeCount: 0, Cyclomatic: 4}, DifficultyMedium},
		{"cyclomatic at medium max", DifficultyInputs{NodeCount: 1, EdgeCount: 0, Cyclomatic: 7}, DifficultyMedium},
		{"cyclomatic just above medium", DifficultyInputs{NodeCount: 1, EdgeCount: 0, Cyclomatic: 8}, DifficultyHard},

		// Edge-count axis (nodes=1, cyclomatic=1).
		{"edges at easy max", DifficultyInputs{NodeCount: 1, EdgeCount: 4, Cyclomatic: 1}, DifficultyEasy},
		{"edges just above easy", DifficultyInputs{NodeCount: 1, EdgeCount: 5, Cyclomatic: 1}, DifficultyMedium},
		{"edges at medium max", DifficultyInputs{NodeCount: 1, EdgeCount: 12, Cyclomatic: 1}, DifficultyMedium},
		{"edges just above medium", DifficultyInputs{NodeCount: 1, EdgeCount: 13, Cyclomatic: 1}, DifficultyHard},

		// Node-count axis (edges=0, cyclomatic=1).
		{"nodes at easy max", DifficultyInputs{NodeCount: 3, EdgeCount: 0, Cyclomatic: 1}, DifficultyEasy},
		{"nodes just above easy", DifficultyInputs{NodeCount: 4, EdgeCount: 0, Cyclomatic: 1}, DifficultyMedium},
		{"nodes at medium max", DifficultyInputs{NodeCount: 10, EdgeCount: 0, Cyclomatic: 1}, DifficultyMedium},
		{"nodes just above medium", DifficultyInputs{NodeCount: 11, EdgeCount: 0, Cyclomatic: 1}, DifficultyHard},

		// Hardest-wins composition.
		{"one hard signal dominates", DifficultyInputs{NodeCount: 1, EdgeCount: 0, Cyclomatic: 20}, DifficultyHard},
		{"one medium signal lifts easy", DifficultyInputs{NodeCount: 4, EdgeCount: 1, Cyclomatic: 2}, DifficultyMedium},
		{"all hard stays hard", DifficultyInputs{NodeCount: 50, EdgeCount: 80, Cyclomatic: 30}, DifficultyHard},

		// The plan-level TaskSpec example (design.md): {4 symbols, 12
		// edges, cyclomatic 7} is published as medium.
		{"plan taskspec example", DifficultyInputs{NodeCount: 4, EdgeCount: 12, Cyclomatic: 7}, DifficultyMedium},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := DeriveDifficulty(tc.in)
			if err != nil {
				t.Fatalf("DeriveDifficulty(%+v) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("DeriveDifficulty(%+v) = %q, want %q", tc.in, got, tc.want)
			}
			if !got.Valid() {
				t.Errorf("DeriveDifficulty(%+v) returned invalid band %q", tc.in, got)
			}
		})
	}
}

func TestDeriveDifficultySignalsMap(t *testing.T) {
	in := DifficultyInputs{NodeCount: 4, EdgeCount: 12, Cyclomatic: 7}
	_, signals, err := DeriveDifficulty(in)
	if err != nil {
		t.Fatalf("DeriveDifficulty error: %v", err)
	}
	want := map[string]int{
		SignalInvolvedSymbols:      4,
		SignalEdgeCount:            12,
		SignalCyclomaticComplexity: 7,
	}
	if !reflect.DeepEqual(signals, want) {
		t.Errorf("signals = %v, want %v", signals, want)
	}
}

// TestDeriveDifficultyMatchesTaskSpecFixture ties the canonical signal keys
// to the TaskSpec contract: deriving from the fixture's inputs reproduces
// the fixture's published difficulty and difficulty_signals exactly.
func TestDeriveDifficultyMatchesTaskSpecFixture(t *testing.T) {
	spec := validSpec()
	in := DifficultyInputs{
		NodeCount:  spec.DifficultySignals[SignalInvolvedSymbols],
		EdgeCount:  spec.DifficultySignals[SignalEdgeCount],
		Cyclomatic: spec.DifficultySignals[SignalCyclomaticComplexity],
	}
	band, signals, err := DeriveDifficulty(in)
	if err != nil {
		t.Fatalf("DeriveDifficulty error: %v", err)
	}
	if band != spec.Difficulty {
		t.Errorf("band = %q, want fixture difficulty %q", band, spec.Difficulty)
	}
	if !reflect.DeepEqual(signals, spec.DifficultySignals) {
		t.Errorf("signals = %v, want fixture signals %v", signals, spec.DifficultySignals)
	}
}

func TestDeriveDifficultyInvalidInputs(t *testing.T) {
	tests := []struct {
		name    string
		in      DifficultyInputs
		wantSub string
	}{
		{"zero node count", DifficultyInputs{NodeCount: 0, EdgeCount: 0, Cyclomatic: 1}, "node count"},
		{"negative node count", DifficultyInputs{NodeCount: -1, EdgeCount: 0, Cyclomatic: 1}, "node count"},
		{"negative edge count", DifficultyInputs{NodeCount: 1, EdgeCount: -1, Cyclomatic: 1}, "edge count"},
		{"zero cyclomatic", DifficultyInputs{NodeCount: 1, EdgeCount: 0, Cyclomatic: 0}, "cyclomatic"},
		{"negative cyclomatic", DifficultyInputs{NodeCount: 1, EdgeCount: 0, Cyclomatic: -3}, "cyclomatic"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			band, signals, err := DeriveDifficulty(tc.in)
			if err == nil {
				t.Fatalf("DeriveDifficulty(%+v) = (%q, %v), want error", tc.in, band, signals)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention %q", err, tc.wantSub)
			}
			if band != "" || signals != nil {
				t.Errorf("on error want zero results, got (%q, %v)", band, signals)
			}
		})
	}
}

// TestDeriveDifficultyReproducible pins the R2 contract end to end: the
// same inputs yield the same band, an equal signals map, and — once the
// signals flow through TaskSpec's sorted-key marshaling — byte-identical
// YAML.
func TestDeriveDifficultyReproducible(t *testing.T) {
	in := DifficultyInputs{NodeCount: 6, EdgeCount: 9, Cyclomatic: 5}

	band1, signals1, err1 := DeriveDifficulty(in)
	band2, signals2, err2 := DeriveDifficulty(in)
	if err1 != nil || err2 != nil {
		t.Fatalf("DeriveDifficulty errors: %v, %v", err1, err2)
	}
	if band1 != band2 || !reflect.DeepEqual(signals1, signals2) {
		t.Fatalf("non-reproducible: (%q, %v) vs (%q, %v)", band1, signals1, band2, signals2)
	}

	specA, specB := validSpec(), validSpec()
	specA.Difficulty, specA.DifficultySignals = band1, signals1
	specB.Difficulty, specB.DifficultySignals = band2, signals2
	yamlA, errA := specA.MarshalYAML()
	yamlB, errB := specB.MarshalYAML()
	if errA != nil || errB != nil {
		t.Fatalf("MarshalYAML errors: %v, %v", errA, errB)
	}
	if !bytes.Equal(yamlA, yamlB) {
		t.Errorf("YAML not byte-identical:\n%s\nvs\n%s", yamlA, yamlB)
	}
}
