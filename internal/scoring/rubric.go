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
const RubricVersion = "2.0.0"

// weightEpsilon is the tolerance for the "weights sum to 1.0" invariant,
// allowing for floating-point representation error.
const weightEpsilon = 1e-9

// SignalID identifies one input signal in the rubric.
type SignalID string

const (
	// SignalLanded scores whether the iteration's work survived to master.
	SignalLanded SignalID = "landed"
	// SignalVerifier scores whether the iteration's verification gates passed.
	SignalVerifier SignalID = "verifier"
	// SignalTests scores whether the iteration's tests passed.
	SignalTests SignalID = "tests"
	// SignalCorrectionPressure scores how little the iteration had to be
	// corrected — retries, user corrections, and tool-call errors.
	SignalCorrectionPressure SignalID = "correction_pressure"
	// SignalScope scores whether the iteration stayed within its write-scope.
	SignalScope SignalID = "scope"
	// SignalTokenEfficiency scores model/cache usage efficiency.
	SignalTokenEfficiency SignalID = "token_efficiency"
)

// AgentRole identifies which role made a self-reported claim. The v2
// iteration-log blocks are role-owned, so the integrity track can attribute a
// claimed-vs-observed gap to the role responsible for it.
type AgentRole string

const (
	// RoleImpl is the implementation agent / loop worker.
	RoleImpl AgentRole = "impl"
	// RoleVerifier is the verification agent.
	RoleVerifier AgentRole = "verifier"
	// RoleReview is the review agent.
	RoleReview AgentRole = "review"
)

// CombinationMethod names how per-signal sub-scores combine into the final
// score. Naming it (rather than hard-coding the formula in the scorer) keeps a
// change of method a reviewable diff against the rubric.
type CombinationMethod string

// CombineWeightedMeanRenormalized is the combination method:
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
	// TwoWay marks a signal that has both an objective source — which scores
	// the run — and a paired self-reported source. For a two-way signal the
	// scorer also emits a claimed-vs-observed delta into the integrity track;
	// that delta never affects the numeric score. See the integrity-track
	// section of docs/OUTCOME_SCORING_RUBRIC.md.
	TwoWay bool
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
// Weights: correctness signals (landed 0.22, verifier 0.20, tests 0.18) total
// 0.60 and dominate; process signals (correction_pressure 0.15, scope 0.15)
// total 0.30; efficiency (token_efficiency 0.10) is the remainder. Rationale
// and per-signal sourcing live in docs/OUTCOME_SCORING_RUBRIC.md.
func DefaultRubric() Rubric {
	return Rubric{
		Version:     RubricVersion,
		Combination: CombineWeightedMeanRenormalized,
		Signals: []SignalSpec{
			{
				ID:          SignalLanded,
				Label:       "Landed on master",
				Weight:      0.22,
				Description: "Did the iteration's work survive to master.",
				TwoWay:      true,
			},
			{
				ID:          SignalVerifier,
				Label:       "Verifier results",
				Weight:      0.20,
				Description: "Did the iteration's verification gates pass.",
				TwoWay:      true,
			},
			{
				ID:          SignalTests,
				Label:       "Test outcomes",
				Weight:      0.18,
				Description: "Did the iteration's focused and total tests pass.",
				TwoWay:      true,
			},
			{
				ID:          SignalCorrectionPressure,
				Label:       "Correction pressure",
				Weight:      0.15,
				Description: "How little the iteration had to be corrected (retries, user corrections, tool errors).",
				TwoWay:      false,
			},
			{
				ID:          SignalScope,
				Label:       "Scope adherence",
				Weight:      0.15,
				Description: "Did the iteration stay within its declared write-scope.",
				TwoWay:      true,
			},
			{
				ID:          SignalTokenEfficiency,
				Label:       "Token & cache efficiency",
				Weight:      0.10,
				Description: "How efficiently the iteration used the model and prompt cache.",
				TwoWay:      false,
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

// TwoWaySignals returns the signals that carry both an objective and a
// self-reported source — the ones the scorer feeds into the integrity track.
func (r Rubric) TwoWaySignals() []SignalSpec {
	var out []SignalSpec
	for _, s := range r.Signals {
		if s.TwoWay {
			out = append(out, s)
		}
	}
	return out
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
