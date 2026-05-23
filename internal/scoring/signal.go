package scoring

// SignalValue is one signal's contribution for one iteration: either a
// sub-score in [0,1], or absent when the telemetry to compute it was not
// available.
//
// Absent is first-class. The scorer renormalizes weights over the signals that
// are present, so an absent signal neither inflates nor deflates the score —
// see docs/OUTCOME_SCORING_RUBRIC.md. The signal extractors return SignalValue;
// the scorer consumes it.
type SignalValue struct {
	Present bool
	// SubScore is meaningful only when Present; PresentSignal keeps it in [0,1].
	SubScore float64
	// Detail is a short human-readable note on what produced the value, for the
	// explainable score breakdown — e.g. "2/2 verifiers passed" or, when
	// absent, "no verifier records".
	Detail string
}

// PresentSignal builds a present SignalValue, clamping the sub-score to [0,1].
func PresentSignal(subScore float64, detail string) SignalValue {
	switch {
	case subScore < 0:
		subScore = 0
	case subScore > 1:
		subScore = 1
	}
	return SignalValue{Present: true, SubScore: subScore, Detail: detail}
}

// AbsentSignal builds an absent SignalValue; detail explains why it is absent.
func AbsentSignal(detail string) SignalValue {
	return SignalValue{Present: false, Detail: detail}
}
