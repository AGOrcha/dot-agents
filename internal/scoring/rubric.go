// Package scoring defines and applies the dot-agents outcome-scoring rubric:
// an explainable quality score per agent-run iteration and session, computed
// from already-captured telemetry.
//
// This file is the versioned rubric data structure. It is the Go twin of
// docs/OUTCOME_SCORING_RUBRIC.md and must agree with it: the document is the
// canonical contract, this code is its machine-readable form. Changing the
// rubric means editing both in the same commit and bumping RubricVersion.
package scoring

import (
	"fmt"
	"math"
)

// RubricVersion is the semantic version of the active outcome-scoring rubric.
//
// Bumping it is a deliberate, reviewable act — see the versioning policy in
// docs/OUTCOME_SCORING_RUBRIC.md. major: the signal set or combination method
// changed. minor: a weight or sub-score mapping changed. patch: docs or band
// thresholds only. Every persisted score records the version it was computed
// under, so a rubric change never silently invalidates historical scores.
const RubricVersion = "1.0.0"

// weightEpsilon is the tolerance for the "weights sum to 1.0" invariant,
// allowing for floating-point representation error.
const weightEpsilon = 1e-9

// SignalID identifies one input signal in the rubric. The five IDs are fixed
// by the R1 requirement; adding or removing one is a major version bump.
type SignalID string

const (
	// SignalVerifier scores whether the iteration's verification gates passed.
	SignalVerifier SignalID = "verifier"
	// SignalTests scores whether the iteration's tests passed.
	SignalTests SignalID = "tests"
	// SignalMergeBack scores whether the iteration's work was accepted/merged.
	SignalMergeBack SignalID = "merge_back"
	// SignalScope scores whether the iteration stayed within its write-scope.
	SignalScope SignalID = "scope"
	// SignalTokenEfficiency scores model/cache usage efficiency.
	SignalTokenEfficiency SignalID = "token_efficiency"
)

// CombinationMethod names how per-signal sub-scores combine into the final
// score. Naming it (rather than hard-coding the formula in the scorer) keeps a
// change of method a reviewable diff against the rubric.
type CombinationMethod string

// CombineWeightedMeanRenormalized is the v1 combination method:
//
//	score = Σ(weightᵢ · sub_scoreᵢ) / Σ(weightᵢ)   over present signals i
//
// Absent signals drop out of both sums; the remaining weights renormalize, so
// a missing signal neither inflates nor deflates the score. If every signal is
// absent the iteration is unscored.
const CombineWeightedMeanRenormalized CombinationMethod = "weighted_mean_renormalized"

// SignalSpec is the rubric entry for one input signal: its identity, its
// weight in the combination, and a one-line description of what it measures.
type SignalSpec struct {
	ID          SignalID
	Label       string
	Weight      float64
	Description string
}

// ScoreBand is a human-readable label for a numeric-score range, identified by
// its inclusive lower bound. Bands are held sorted descending by Min.
type ScoreBand struct {
	Name string
	Min  float64
}

// BandUnscored is the band reported when no signal was present and the
// iteration therefore has no numeric score.
const BandUnscored = "unscored"

// Rubric is the versioned outcome-scoring rubric: the signal set, their
// weights, the combination method, and the score bands. It is data, not
// behaviour — the scorer task consumes it; it does not redefine it.
type Rubric struct {
	Version     string
	Combination CombinationMethod
	Signals     []SignalSpec
	Bands       []ScoreBand
}

// DefaultRubric returns the active, versioned rubric (RubricVersion).
//
// Weights: correctness signals (verifier 0.30, tests 0.25, merge_back 0.20)
// total 0.75 and dominate; process (scope 0.15) and efficiency
// (token_efficiency 0.10) total 0.25. Rationale lives in
// docs/OUTCOME_SCORING_RUBRIC.md.
func DefaultRubric() Rubric {
	return Rubric{
		Version:     RubricVersion,
		Combination: CombineWeightedMeanRenormalized,
		Signals: []SignalSpec{
			{
				ID:          SignalVerifier,
				Label:       "Verifier results",
				Weight:      0.30,
				Description: "Did the iteration's verification gates pass.",
			},
			{
				ID:          SignalTests,
				Label:       "Test outcomes",
				Weight:      0.25,
				Description: "Did the iteration's focused and total tests pass.",
			},
			{
				ID:          SignalMergeBack,
				Label:       "Merge-back status",
				Weight:      0.20,
				Description: "Was the iteration's work accepted into the trunk.",
			},
			{
				ID:          SignalScope,
				Label:       "Scope adherence",
				Weight:      0.15,
				Description: "Did the iteration stay within its declared write-scope.",
			},
			{
				ID:          SignalTokenEfficiency,
				Label:       "Token & cache efficiency",
				Weight:      0.10,
				Description: "How efficiently the iteration used the model and prompt cache.",
			},
		},
		Bands: []ScoreBand{
			{Name: "excellent", Min: 0.85},
			{Name: "good", Min: 0.70},
			{Name: "fair", Min: 0.50},
			{Name: "poor", Min: 0.0},
		},
	}
}

// Signal returns the spec for id and true, or a zero spec and false if id is
// not part of the rubric.
func (r Rubric) Signal(id SignalID) (SignalSpec, bool) {
	for _, s := range r.Signals {
		if s.ID == id {
			return s, true
		}
	}
	return SignalSpec{}, false
}

// Band returns the band name for a numeric score in [0, 1]. Scores outside
// [0, 1] clamp to the nearest band. Callers report BandUnscored themselves
// for the no-signal case; this method always returns a numeric-range band.
func (r Rubric) Band(score float64) string {
	for _, b := range r.Bands { // sorted descending by Min
		if score >= b.Min {
			return b.Name
		}
	}
	if len(r.Bands) == 0 {
		return ""
	}
	return r.Bands[len(r.Bands)-1].Name
}

// Validate checks the rubric's internal invariants: a pinned version and
// combination method, a non-empty signal set with unique IDs and positive
// weights summing to 1.0, and a non-empty band ladder sorted descending by Min
// with a band anchored at 0. It is the guard that keeps a rubric edit
// internally consistent.
func (r Rubric) Validate() error {
	if r.Version == "" {
		return fmt.Errorf("scoring: rubric version is empty")
	}
	if r.Combination == "" {
		return fmt.Errorf("scoring: rubric combination method is empty")
	}
	if len(r.Signals) == 0 {
		return fmt.Errorf("scoring: rubric has no signals")
	}

	seen := make(map[SignalID]bool, len(r.Signals))
	var sum float64
	for _, s := range r.Signals {
		if seen[s.ID] {
			return fmt.Errorf("scoring: duplicate signal ID %q", s.ID)
		}
		seen[s.ID] = true
		if s.Weight <= 0 {
			return fmt.Errorf("scoring: signal %q has non-positive weight %g", s.ID, s.Weight)
		}
		sum += s.Weight
	}
	if math.Abs(sum-1.0) > weightEpsilon {
		return fmt.Errorf("scoring: signal weights sum to %g, want 1.0", sum)
	}

	return r.validateBands()
}

// validateBands checks the band ladder: non-empty, sorted strictly descending
// by Min, with the lowest band anchored at 0 so every score in [0, 1] lands.
func (r Rubric) validateBands() error {
	if len(r.Bands) == 0 {
		return fmt.Errorf("scoring: rubric has no score bands")
	}
	for i, b := range r.Bands {
		if b.Name == "" {
			return fmt.Errorf("scoring: band at index %d has no name", i)
		}
		if i > 0 && b.Min >= r.Bands[i-1].Min {
			return fmt.Errorf("scoring: bands not sorted descending by Min at index %d", i)
		}
	}
	if last := r.Bands[len(r.Bands)-1]; last.Min != 0 {
		return fmt.Errorf("scoring: lowest band %q has Min %g, want 0", last.Name, last.Min)
	}
	return nil
}
