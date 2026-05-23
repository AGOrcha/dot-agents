package scoring

import (
	"os"
	"testing"

	"go.yaml.in/yaml/v3"
)

// An iteration with no verifier entries has no linked traces by definition.
func TestDeriveLinkedTracesToOutcomesNoVerifier(t *testing.T) {
	if DeriveLinkedTracesToOutcomes(IterationRecord{}) {
		t.Error("got true, want false (no verifiers means no pairs)")
	}
}

// An iteration with verifiers but no named pairs returns false — the boolean
// must reflect the structured replacement, not the absence of the structured
// replacement.
func TestDeriveLinkedTracesToOutcomesEmptyPairs(t *testing.T) {
	rec := IterationRecord{
		Verifiers: []VerifierRecord{
			{Type: "test", Status: "pass"},
			{Type: "api", Status: "pass"},
		},
	}
	if DeriveLinkedTracesToOutcomes(rec) {
		t.Error("got true, want false (no named pairs on any verifier)")
	}
}

// One verifier carrying at least one pair is enough — the marker is OR across
// verifiers, not AND.
func TestDeriveLinkedTracesToOutcomesOneVerifierHasPair(t *testing.T) {
	rec := IterationRecord{
		Verifiers: []VerifierRecord{
			{Type: "test", Status: "pass"},
			{Type: "api", Status: "pass", LinkedTraces: []LinkedTrace{
				{TraceRef: ".agents/active/verification/X.yaml", OutcomeRef: "commit:abc123"},
			}},
		},
	}
	if !DeriveLinkedTracesToOutcomes(rec) {
		t.Error("got false, want true (api verifier names one pair)")
	}
}

// Multiple pairs across multiple verifiers still resolve to true (and only
// true once — the marker is boolean).
func TestDeriveLinkedTracesToOutcomesMultiple(t *testing.T) {
	rec := IterationRecord{
		Verifiers: []VerifierRecord{
			{LinkedTraces: []LinkedTrace{{TraceRef: "a", OutcomeRef: "1"}}},
			{LinkedTraces: []LinkedTrace{{TraceRef: "b", OutcomeRef: "2"}, {TraceRef: "c", OutcomeRef: "3"}}},
		},
	}
	if !DeriveLinkedTracesToOutcomes(rec) {
		t.Error("got false, want true")
	}
}

// BuildPersistedScore combines a scored Score with the derived marker. The
// resulting PersistedScore carries both, and the YAML round-trip preserves the
// marker field.
func TestBuildPersistedScoreCarriesMarker(t *testing.T) {
	r := DefaultRubric()
	score := r.Score(SignalSet{Iteration: 12, Verifier: PresentSignal(1.0, "")})
	rec := IterationRecord{
		Iteration: 12,
		Verifiers: []VerifierRecord{
			{LinkedTraces: []LinkedTrace{{TraceRef: "trace/a", OutcomeRef: "commit:def"}}},
		},
	}
	ps := BuildPersistedScore(score, rec)
	if !ps.LinkedTracesToOutcomes {
		t.Error("LinkedTracesToOutcomes = false, want true (rec has a pair)")
	}
	if ps.Iteration != 12 || ps.RubricVersion != r.Version {
		t.Errorf("score header lost: %+v", ps)
	}
	if len(ps.Breakdown) != len(r.Signals) {
		t.Errorf("Breakdown len = %d, want %d", len(ps.Breakdown), len(r.Signals))
	}
}

// When the iteration recorded no pairs, the marker is false and the field is
// omitted from the YAML so legacy consumers see no extra surface.
func TestBuildPersistedScoreFalseMarkerOmitted(t *testing.T) {
	r := DefaultRubric()
	score := r.Score(SignalSet{Iteration: 3, Verifier: PresentSignal(1.0, "")})
	ps := BuildPersistedScore(score, IterationRecord{Iteration: 3})
	if ps.LinkedTracesToOutcomes {
		t.Error("LinkedTracesToOutcomes = true, want false")
	}
	data, err := yaml.Marshal(ps)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if contains(string(data), "linked_traces_to_outcomes") {
		t.Errorf("emitted YAML carries the marker even though it is false:\n%s", data)
	}
}

// WriteIterationScoreWithRecord persists the augmented shape: marker is true
// when the record has a pair, and the round-trip preserves it.
func TestWriteIterationScoreWithRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := DefaultRubric()
	score := r.Score(SignalSet{Iteration: 19, Verifier: PresentSignal(0.5, "")})
	rec := IterationRecord{
		Iteration: 19,
		Verifiers: []VerifierRecord{
			{LinkedTraces: []LinkedTrace{{TraceRef: "t", OutcomeRef: "o"}}},
		},
	}
	path, err := WriteIterationScoreWithRecord(dir, score, rec)
	if err != nil {
		t.Fatalf("WriteIterationScoreWithRecord: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got PersistedScore
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.LinkedTracesToOutcomes {
		t.Error("LinkedTracesToOutcomes did not round-trip true")
	}
	if got.Iteration != 19 {
		t.Errorf("Iteration = %d, want 19", got.Iteration)
	}
}

// The augmented writer surfaces the underlying write error path: missing
// destination dir → os.CreateTemp fails inside writeYAMLAtomic.
func TestWriteIterationScoreWithRecordErrorOnMissingDir(t *testing.T) {
	r := DefaultRubric()
	score := r.Score(SignalSet{Iteration: 5, Verifier: PresentSignal(1.0, "")})
	_, err := WriteIterationScoreWithRecord(
		"/this/path/should/never/exist/under/normal/circumstances",
		score, IterationRecord{Iteration: 5},
	)
	if err == nil {
		t.Fatal("expected error for missing dir, got nil")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
