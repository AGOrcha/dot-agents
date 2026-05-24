// score_iteration.go provides the single-iteration scoring entry point
// workflow-client-commands close-task uses to score the just-closed
// iteration without rerunning the whole log.
//
// Old scores are computed from immutable inputs (iter-N.yaml + commit SHA +
// frozen transcript window) so they stay valid forever once written — only
// a RubricVersion bump or a signals-shape change warrants backfill. The
// default close-task flow therefore wants "score iteration N", not "score
// every iteration the log has ever recorded."
package scoring

import "fmt"

// ScoreIteration computes the Score and returns the source IterationRecord
// for iteration n alone. iterLogDir + repoDir + transcriptDirs match
// BuildSignalSets's contract so callers reuse the same path conventions.
//
// The implementation runs BuildSignalSets across the full log (needed so
// the per-iteration commit-window resolver sees its neighbours) and then
// returns the single matching SignalSet's Score. The wider pipeline is
// pure-ish — the heavy work is git topology + transcript scan, both of
// which are bounded by the iter-log size — so the optimisation target is
// not "skip the pipeline" but "skip the per-iter sidecar writes," which
// the close-task --recompute=current default already achieves.
//
// Returns an error when iter-N.yaml does not exist or BuildSignalSets
// itself errors.
func ScoreIteration(iterLogDir, repoDir string, n int, transcriptDirs ...string) (Score, IterationRecord, error) {
	rubric := DefaultRubric()
	records, err := LoadIterationLog(iterLogDir)
	if err != nil {
		return Score{}, IterationRecord{}, fmt.Errorf("scoring: load iteration log: %w", err)
	}
	targetIdx := -1
	for i, r := range records {
		if r.Iteration == n {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		return Score{}, IterationRecord{}, fmt.Errorf("scoring: iter-%d.yaml not found in %s", n, iterLogDir)
	}
	sets, err := BuildSignalSets(iterLogDir, repoDir, transcriptDirs...)
	if err != nil {
		return Score{}, IterationRecord{}, fmt.Errorf("scoring: build signal sets: %w", err)
	}
	// BuildSignalSets's contract is one SignalSet per IterationRecord in
	// the same order, so the same index resolves both. Indexing rather
	// than re-scanning eliminates the dead invariant-breach branch a
	// second scan would have to guard against (and that the coverage
	// gate would have to allowlist).
	return rubric.Score(sets[targetIdx]), records[targetIdx], nil
}
