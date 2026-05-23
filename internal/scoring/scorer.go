package scoring

// Score is the rubric-applied outcome for one iteration: the numeric value,
// the human-readable band, and the per-signal breakdown that explains how the
// value was assembled.
//
// The score is explainable by construction: every signal in the rubric
// contributes one row to Breakdown, and the contributions of the present
// signals sum exactly to Value. A signal that was absent contributes a row
// with Present=false, EffectiveWeight=0, Contribution=0 — present in the
// breakdown so the explanation is complete, but voting nothing.
type Score struct {
	Iteration     int
	RubricVersion string
	// Value is the final score in [0, 1]; 0 when Scored is false.
	Value float64
	// Scored is false when every rubric signal was absent on the input set —
	// the rubric refuses to invent a score from nothing.
	Scored bool
	// Band is the human-readable label for Value, or BandUnscored when Scored
	// is false.
	Band string
	// Breakdown carries one row per rubric signal, in the rubric's declared
	// order, for stable rendering.
	Breakdown []SignalContribution
}

// SignalContribution is one row of the explainable per-signal breakdown.
type SignalContribution struct {
	Signal          SignalID
	Label           string
	Present         bool
	SubScore        float64
	Detail          string
	NominalWeight   float64
	EffectiveWeight float64
	Contribution    float64
}

// Score applies the rubric to one SignalSet. It is pure — no I/O, no git, no
// transcripts. The combination method is weighted_mean_renormalized: absent
// signals drop out of both the numerator and the denominator, the remaining
// weights renormalize, and the present signals' contributions sum exactly to
// Value.
//
// When every rubric signal is absent on set, Scored is false, Value is 0,
// and Band is BandUnscored. The Breakdown is still populated — every rubric
// signal gets a row marked Present=false — so the "unscored" explanation is
// complete.
func (r Rubric) Score(set SignalSet) Score {
	rows := make([]SignalContribution, len(r.Signals))
	var sumPresentWeights float64
	for i, spec := range r.Signals {
		sv := set.Value(spec.ID)
		rows[i] = SignalContribution{
			Signal:        spec.ID,
			Label:         spec.Label,
			Present:       sv.Present,
			SubScore:      sv.SubScore,
			Detail:        sv.Detail,
			NominalWeight: spec.Weight,
		}
		if sv.Present {
			sumPresentWeights += spec.Weight
		}
	}

	score := Score{
		Iteration:     set.Iteration,
		RubricVersion: r.Version,
		Breakdown:     rows,
	}

	if sumPresentWeights == 0 {
		score.Band = BandUnscored
		return score
	}

	var value float64
	for i := range rows {
		if !rows[i].Present {
			continue
		}
		rows[i].EffectiveWeight = rows[i].NominalWeight / sumPresentWeights
		rows[i].Contribution = rows[i].EffectiveWeight * rows[i].SubScore
		value += rows[i].Contribution
	}
	score.Value = value
	score.Scored = true
	score.Band = r.Band(value)
	return score
}

// ScoreAll applies the rubric to every SignalSet in sets, returning one Score
// per set in input order. A typical caller pairs ScoreAll with BuildSignalSets
// to produce the full per-iteration score series for an iteration log.
func (r Rubric) ScoreAll(sets []SignalSet) []Score {
	out := make([]Score, len(sets))
	for i, s := range sets {
		out[i] = r.Score(s)
	}
	return out
}
