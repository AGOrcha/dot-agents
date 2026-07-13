package eval

import "fmt"

// This file is the canonical difficulty contract of the R4 eval harness
// (R4 spec requirement R2): the ONE place a task's difficulty band is
// derived from knowledge-graph signals. Generators must not invent their
// own bucketing; they gather the three inputs from the KG query layer and
// call [DeriveDifficulty].
//
// The derivation is a pure function of three integers, so re-running a
// generator on the same KG state yields the same band and the same
// difficulty_signals map — byte-identical once serialized through
// [TaskSpec.MarshalYAML], which emits map keys in sorted order.
//
// The inputs come from internal/eval/kgquery (which imports this package,
// hence the primitive-int seam rather than kgquery types):
//
//   - NodeCount = len(Neighborhood.Nodes) — symbols reachable from the seed
//     across ALL edge kinds (per the recorded kg-query-layer edge-kind
//     ruling), including the seed itself.
//   - EdgeCount = len(Neighborhood.Edges) — distinct edges among them,
//     again across all edge kinds.
//   - Cyclomatic = Complexity.Cyclomatic — the seed's McCabe-style proxy
//     (1 + distinct CALLS fan-out).

// Canonical difficulty-signal keys. These are the exact keys generators
// publish in [TaskSpec.DifficultySignals]; the dashboard and verifier join
// on them, so they are part of the TaskSpec v1 contract.
const (
	// SignalInvolvedSymbols is the neighborhood node count (seed included).
	SignalInvolvedSymbols = "involved_symbols"
	// SignalEdgeCount is the neighborhood edge count (all edge kinds).
	SignalEdgeCount = "edge_count"
	// SignalCyclomaticComplexity is the seed's cyclomatic-complexity proxy.
	SignalCyclomaticComplexity = "cyclomatic_complexity"
)

// Rubric v1 thresholds. Each signal buckets independently — a value at or
// below the easy max is easy, at or below the medium max is medium,
// anything above is hard — and the task's band is the hardest per-signal
// band. Editing any threshold is a deliberate rubric change: the
// table-driven tests in difficulty_test.go pin every boundary, so the
// change surfaces as a reviewable test diff.
const (
	easyMaxCyclomatic   = 3
	mediumMaxCyclomatic = 7
	easyMaxEdges        = 4
	mediumMaxEdges      = 12
	easyMaxNodes        = 3
	mediumMaxNodes      = 10
)

// difficultyRank orders bands for the hardest-signal-wins composition.
var difficultyRank = map[Difficulty]int{
	DifficultyEasy:   0,
	DifficultyMedium: 1,
	DifficultyHard:   2,
}

// DifficultyInputs holds the reproducible KG signals a task's difficulty
// derives from. All three are structural counts read from stored graph
// state — no re-parse, no wall-clock, no randomness — which is what makes
// the derived band reproducible per R2.
type DifficultyInputs struct {
	// NodeCount is the number of symbols in the seed's KG neighborhood,
	// including the seed itself, so it is always at least 1.
	NodeCount int
	// EdgeCount is the number of distinct edges among those symbols.
	EdgeCount int
	// Cyclomatic is the seed symbol's cyclomatic-complexity proxy. The KG
	// query layer computes it as 1 + distinct CALLS fan-out, so it is
	// always at least 1.
	Cyclomatic int
}

// validate checks the structural invariants the KG query layer guarantees,
// so a mis-wired caller fails loudly instead of producing a bogus band.
func (in DifficultyInputs) validate() error {
	if in.NodeCount < 1 {
		return fmt.Errorf("eval: difficulty node count must be at least 1 (the seed itself), got %d", in.NodeCount)
	}
	if in.EdgeCount < 0 {
		return fmt.Errorf("eval: difficulty edge count must be non-negative, got %d", in.EdgeCount)
	}
	if in.Cyclomatic < 1 {
		return fmt.Errorf("eval: difficulty cyclomatic proxy must be at least 1, got %d", in.Cyclomatic)
	}
	return nil
}

// DeriveDifficulty is the canonical difficulty derivation (R4 requirement
// R2). It buckets each signal against the rubric v1 thresholds, composes
// the per-signal bands by hardest-wins, and returns the band together with
// the difficulty_signals map generators publish on the TaskSpec.
//
// The function is pure and deterministic: the same inputs always yield the
// same band and an equal signals map, and the map serializes
// byte-identically through the TaskSpec's sorted-key YAML marshaling.
func DeriveDifficulty(in DifficultyInputs) (Difficulty, map[string]int, error) {
	if err := in.validate(); err != nil {
		return "", nil, err
	}
	band := hardestBand(
		bandFor(in.Cyclomatic, easyMaxCyclomatic, mediumMaxCyclomatic),
		bandFor(in.EdgeCount, easyMaxEdges, mediumMaxEdges),
		bandFor(in.NodeCount, easyMaxNodes, mediumMaxNodes),
	)
	signals := map[string]int{
		SignalInvolvedSymbols:      in.NodeCount,
		SignalEdgeCount:            in.EdgeCount,
		SignalCyclomaticComplexity: in.Cyclomatic,
	}
	return band, signals, nil
}

// bandFor buckets a single signal value against its easy/medium maxima.
func bandFor(value, easyMax, mediumMax int) Difficulty {
	switch {
	case value <= easyMax:
		return DifficultyEasy
	case value <= mediumMax:
		return DifficultyMedium
	default:
		return DifficultyHard
	}
}

// hardestBand returns the highest-ranked of the given bands, so one hard
// signal makes the task hard regardless of the others.
func hardestBand(bands ...Difficulty) Difficulty {
	top := DifficultyEasy
	for _, b := range bands {
		if difficultyRank[b] > difficultyRank[top] {
			top = b
		}
	}
	return top
}
