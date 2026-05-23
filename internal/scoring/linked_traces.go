package scoring

// DeriveLinkedTracesToOutcomes computes the legacy iteration-log marker from
// the structured replacement that the structured-claims task introduced.
//
// The proposal calls the boolean "uncomputed -- the clearest marker of the
// gap" between captured evidence and outcome assessment. The structured-claims
// schema deprecated the self-reported boolean and replaced it with a named
// list of trace ↔ outcome pairs: verifier.linked_traces. The boolean is now
// fully derivable — true exactly when the iteration recorded at least one
// pair — so persisting it is the close-out act for the proposal's named gap.
//
// Returning a derived value (instead of trusting the legacy self-report) is
// the point: the rubber-stamp era of the boolean is over.
//
// An iteration may carry more than one verifier entry (unit + api + ui-e2e,
// say); the marker is true when at least one of them recorded a pair.
func DeriveLinkedTracesToOutcomes(rec IterationRecord) bool {
	for _, v := range rec.Verifiers {
		if len(v.LinkedTraces) > 0 {
			return true
		}
	}
	return false
}

// BuildPersistedScore augments a Score with the derived per-iteration markers
// before it is written. It is the caller-side constructor for the on-disk
// shape: anything that comes from outside the pure scorer (the linked-traces
// marker today, possibly more markers later) is folded in here rather than
// leaking into the Rubric.Score signature.
//
// Callers that only have the Score (no IterationRecord) can keep using the
// raw WriteIterationScore path; the marker field on PersistedScore is omitted
// from the YAML when unset, so legacy callers do not regress.
func BuildPersistedScore(s Score, rec IterationRecord) PersistedScore {
	ps := toPersistedScore(s)
	ps.LinkedTracesToOutcomes = DeriveLinkedTracesToOutcomes(rec)
	return ps
}

// WriteIterationScoreWithRecord is the augmented per-iteration write: it
// derives the linked_traces_to_outcomes marker from rec and persists it
// alongside the score.
func WriteIterationScoreWithRecord(iterLogDir string, s Score, rec IterationRecord) (string, error) {
	ps := BuildPersistedScore(s, rec)
	path := IterationScorePath(iterLogDir, s.Iteration)
	if err := writeYAMLAtomic(path, ps); err != nil {
		return "", err
	}
	return path, nil
}
