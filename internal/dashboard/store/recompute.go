// recompute.go implements the t06 score-recompute-on-miss half of plan task
// t06-recompute-on-miss-and-fswatch.
//
// RecomputeStore decorates DiskStore's GetIteration: when the requested
// iteration's iter-N.score.yaml sidecar is missing, corrupt, or older than its
// iter-N.yaml record, the store calls scoring.ScoreIterationWithSignals
// synchronously — the same single-iteration entry point close-task's
// scoring.ScoreIteration wraps, in the variant that also returns the scored
// SignalSet so its integrity / objective outputs survive into the DTO — and
// returns the freshly-computed score. The sidecar write is best-effort in a
// background goroutine, so the cold-cache dashboard never says "no score" when
// the scorer could just answer, and never blocks the response on a disk write.
//
// The recompute path is also what populates the DTO fields t02's raw read
// layer declares but leaves empty (integrity, objective,
// integrity_observation_count, transcript_turn_count): they are products of
// the scoring pipeline / raw iter-log entry, not of the persisted sidecar, so
// only this path can source them. A detail read served from a fresh sidecar
// still leaves them empty — that is the t02/t06 boundary, not a bug.
//
// Any pipeline failure degrades to the raw t02 read (spec R10 resilience):
// GetIteration never trades a valid raw answer for a recompute error.
package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

// RecomputeStore is the t06 decorator over DiskStore. It implements Store;
// every method except GetIteration is the embedded DiskStore's. Construct
// with NewRecompute; the zero value is not usable.
type RecomputeStore struct {
	*DiskStore
	repoDir        string
	transcriptDirs []string
	// wg tracks the best-effort background sidecar writes so Flush (t07's
	// shutdown hook, and tests) can wait for them to settle.
	wg sync.WaitGroup
}

// Compile-time proof the decorator still satisfies the t02 Store contract.
var _ Store = (*RecomputeStore)(nil)

// NewRecompute wraps s with the score-recompute-on-miss path. repoDir is the
// repository root the scoring pipeline runs git topology against (the same
// argument scoring.ScoreIteration takes); transcriptDirs are the optional
// agent session-log roots for token backfill and the transcript-derived
// objective checks.
func NewRecompute(s *DiskStore, repoDir string, transcriptDirs ...string) *RecomputeStore {
	return &RecomputeStore{
		DiskStore:      s,
		repoDir:        repoDir,
		transcriptDirs: append([]string(nil), transcriptDirs...),
	}
}

// Flush waits for every pending background sidecar write. t07's server calls
// it on graceful shutdown so a just-recomputed score is not lost with the
// process; tests use it to observe the write deterministically.
func (r *RecomputeStore) Flush() { r.wg.Wait() }

// GetIteration implements Store with the recompute-on-miss overlay: a fresh
// sidecar serves the plain t02 read; a missing / corrupt / stale sidecar
// triggers a synchronous pipeline recompute whose result both fills the
// response and is persisted back (best-effort, in the background).
func (r *RecomputeStore) GetIteration(_ context.Context, iterLogDir string, n int) (IterationDetail, error) {
	root, err := r.resolveRoot(iterLogDir)
	if err != nil {
		return IterationDetail{}, err
	}
	snap := r.snapshot(root)
	rec, ok := findRecord(snap, n)
	if !ok {
		return IterationDetail{}, ErrNotFound
	}
	base := buildIteration(rec, snap, true)
	if !needsRecompute(snap, n) {
		return base, nil
	}
	return r.recompute(root, rec, base), nil
}

// findRecord locates iteration n in the snapshot's parsed records.
func findRecord(snap rootSnapshot, n int) (scoring.IterationRecord, bool) {
	for _, rec := range snap.records {
		if rec.Iteration == n {
			return rec, true
		}
	}
	return scoring.IterationRecord{}, false
}

// needsRecompute reports whether iteration n's score sidecar fails to answer
// on its own: absent or corrupt (it never parsed into the snapshot), or older
// than the iter-N.yaml record it scores (a checkpoint rewrote the record after
// the score was computed).
func needsRecompute(snap rootSnapshot, n int) bool {
	if _, parsed := snap.iterScores[n]; !parsed {
		return true
	}
	scoreMt, ok := snap.fileMtime[iterScoreFileName(n)]
	if !ok {
		// The parsed sidecar came from a file whose NAME disagrees with its
		// iteration field (e.g. a mis-copied iter-3.score.yaml claiming
		// iteration 5). Trust the filename convention and recompute.
		return true
	}
	recMt, ok := snap.fileMtime[iterFileName(n)]
	return ok && scoreMt.Before(recMt)
}

// recompute runs the mandated single-iteration scoring entry point for rec's
// iteration and overlays the fresh result onto the raw detail. On any
// pipeline failure the raw detail is returned unchanged (never worse than t02
// alone). freshRec — the record as the pipeline just re-read it — feeds the
// sidecar write and the overlay so both halves see one consistent view even
// if the snapshot raced a checkpoint rewrite.
func (r *RecomputeStore) recompute(root string, rec scoring.IterationRecord, base Iteration) Iteration {
	score, set, freshRec, err := scoring.ScoreIterationWithSignals(root, r.repoDir, rec.Iteration, r.transcriptDirs...)
	if err != nil {
		r.logger.Warn("dashboard/store: recompute-on-miss pipeline failed, serving raw read",
			"root", root, "iteration", rec.Iteration, "error", err)
		return base
	}
	r.persistAsync(root, score, freshRec)
	return overlayRecompute(base, score, set, freshRec, root)
}

// persistAsync writes the freshly-computed sidecar in the background. The
// write is best-effort: failure is logged, never surfaced to the reader.
func (r *RecomputeStore) persistAsync(root string, score scoring.Score, rec scoring.IterationRecord) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if _, err := scoring.WriteIterationScoreWithRecord(root, score, rec); err != nil {
			r.logger.Warn("dashboard/store: best-effort sidecar write failed",
				"root", root, "iteration", score.Iteration, "error", err)
		}
	}()
}

// overlayRecompute replaces base's score-sourced fields with the fresh
// pipeline result and fills the recompute-only DTO fields.
func overlayRecompute(base Iteration, score scoring.Score, set scoring.SignalSet, rec scoring.IterationRecord, root string) Iteration {
	ps := scoring.BuildPersistedScore(score, rec)
	base.RubricVersion = ps.RubricVersion
	base.Scored = ps.Scored
	base.Band = ps.Band
	base.Score = nil
	if ps.Scored {
		base.Score = floatPtr(ps.Value)
	}
	base.Breakdown = mapBreakdown(ps.Breakdown)
	base.Integrity = mapIntegrity(set.Integrity)
	base.IntegrityObservationCount = comparableCount(set.Integrity)
	base.Objective = mapObjective(set.Objective)
	base.TranscriptTurnCount = readMessageCount(filepath.Join(root, iterFileName(rec.Iteration)))
	return base
}

// mapIntegrity projects the pipeline's claimed-vs-observed observations into
// the DTO rows. Delta is populated only for comparable pairs (both sides
// present), matching IntegrityObservation.Delta's semantics.
func mapIntegrity(obs []scoring.IntegrityObservation) []IntegrityRow {
	if len(obs) == 0 {
		return nil
	}
	rows := make([]IntegrityRow, len(obs))
	for i, o := range obs {
		row := IntegrityRow{
			Signal:     string(o.Signal),
			Role:       string(o.Role),
			Claimed:    mapSignalSide(o.Claimed),
			Observed:   mapSignalSide(o.Observed),
			Comparable: o.Comparable(),
		}
		if row.Comparable {
			row.Delta = floatPtr(o.Delta())
		}
		rows[i] = row
	}
	return rows
}

// comparableCount is the integrity_observation_count field: observations with
// BOTH sides present (schemas/dashboard-iteration.schema.json).
func comparableCount(obs []scoring.IntegrityObservation) int {
	n := 0
	for _, o := range obs {
		if o.Comparable() {
			n++
		}
	}
	return n
}

func mapSignalSide(v scoring.SignalValue) SignalSide {
	return SignalSide{Present: v.Present, SubScore: v.SubScore, Detail: v.Detail}
}

func mapObjective(obj scoring.IterationObjectives) *Objective {
	return &Objective{
		RanCliCommand:       mapSignalSide(obj.RanCliCommand),
		CommittedAfterTests: mapSignalSide(obj.CommittedAfterTests),
		ReadLoopState:       mapSignalSide(obj.ReadLoopState),
	}
}

// readMessageCount extracts session_tokens.message_count from the raw
// iter-N.yaml entry. internal/scoring's normalized IterationRecord does not
// carry the field, so the recompute layer re-reads the one file it needs.
// Nil (JSON null) when the file, block, or field is absent — the schema's
// "null when unavailable" contract.
func readMessageCount(path string) *int {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var probe struct {
		SessionTokens *struct {
			MessageCount *int `yaml:"message_count"`
		} `yaml:"session_tokens"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil || probe.SessionTokens == nil {
		return nil
	}
	return probe.SessionTokens.MessageCount
}
