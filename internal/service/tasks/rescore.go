package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
	"github.com/AGOrcha/dot-agents/internal/service/state"
)

// RescoreName is the scheduler task name and the watermark key of the
// rubric-bump rescorer (sidecar: rescore.watermark.yaml).
const RescoreName = "rescore"

// defaultRescoreInterval is how often the rescorer checks for a rubric
// version bump when the config does not set an explicit interval. A check
// tick is one watermark read plus a string compare, so a short interval
// stays cheap; the expensive full-log pass only runs on a version delta.
const defaultRescoreInterval = 60 * time.Second

// defaultRescoreTimeout bounds one rescore pass when the config does not
// set an explicit timeout. A pass re-scores the entire iteration log, so it
// gets more headroom than the per-iteration ingest default.
const defaultRescoreTimeout = 5 * time.Minute

// RescoreWatermark is the rescorer's D3 restart watermark: the rubric
// version the on-disk score sidecars were last written under. A delta
// between it and scoring.RubricVersion is the sole rescore trigger — never
// the mere absence of sidecars (the ingester scores those on ingest).
type RescoreWatermark struct {
	RubricVersion string `yaml:"rubric_version"`
}

// RescoreConfig configures NewRescore.
type RescoreConfig struct {
	// IterLogDir is the iteration-log directory whose score sidecars are
	// refreshed, conventionally .agents/active/iteration-log under RepoDir.
	IterLogDir string
	// RepoDir is the repository root scoring resolves commits against; it
	// also anchors the watermark sidecar under .agents/active/service-state.
	RepoDir string
	// Bus receives RescoreDone notifications. This is the D4.1 interface
	// seam; any conformant backend works.
	Bus events.EventBus
	// Interval is the version-check cadence. Zero applies
	// defaultRescoreInterval.
	Interval time.Duration
	// Timeout bounds one rescore pass. Zero applies defaultRescoreTimeout.
	Timeout time.Duration
}

// bulkScores bundles the aligned outputs of one full-log scoring pass:
// records[i] is the iteration scores[i] was computed from (BuildSignalSets
// and Rubric.ScoreAll both preserve iteration order).
type bulkScores struct {
	records  []scoring.IterationRecord
	scores   []scoring.Score
	sessions []scoring.SessionScore
}

// bulkScorer is the seam to internal/scoring for full-log rescoring,
// injected so tests can fake the (expensive, git-dependent) pipeline. The
// default implementation is a thin passthrough — the task adds no scoring
// logic of its own.
type bulkScorer interface {
	ScoreAll(iterLogDir, repoDir string) (bulkScores, error)
	WriteSidecars(iterLogDir string, b bulkScores) error
}

// packageBulkScorer delegates to internal/scoring: the same BuildSignalSets
// → Rubric.ScoreAll → AggregateSessions pipeline `da score run` executes,
// followed by the same sidecar writers.
type packageBulkScorer struct{}

// ScoreAll runs the full scoring pipeline over every iteration record. An
// empty (or absent) iteration log yields an empty result, not an error.
func (packageBulkScorer) ScoreAll(iterLogDir, repoDir string) (bulkScores, error) {
	records, err := scoring.LoadIterationLog(iterLogDir)
	if err != nil {
		return bulkScores{}, err
	}
	if len(records) == 0 {
		return bulkScores{}, nil
	}
	sets, err := scoring.BuildSignalSets(iterLogDir, repoDir)
	if err != nil {
		return bulkScores{}, err
	}
	rubric := scoring.DefaultRubric()
	scores := rubric.ScoreAll(sets)
	return bulkScores{
		records:  records,
		scores:   scores,
		sessions: scoring.AggregateSessions(rubric, records, scores),
	}, nil
}

// WriteSidecars persists every per-iteration sidecar via the record-aware
// writer — the augmented shape the ingester writes (#262) survives a
// rescore instead of being stripped back to the bare shape — then the
// per-session aggregates via WriteSessionScores.
func (packageBulkScorer) WriteSidecars(iterLogDir string, b bulkScores) error {
	// scores[i]↔records[i] pairing is a construction invariant of Score
	// (BuildSignalSets order); guard it so a misaligned bulkScores from a
	// future scorer fails loudly instead of panicking or pairing a score
	// with the wrong iteration's record.
	if len(b.scores) != len(b.records) {
		return fmt.Errorf("tasks: bulkScores misaligned: %d scores vs %d records", len(b.scores), len(b.records))
	}
	for i, s := range b.scores {
		if _, err := scoring.WriteIterationScoreWithRecord(iterLogDir, s, b.records[i]); err != nil {
			return fmt.Errorf("iter-%d sidecar: %w", s.Iteration, err)
		}
	}
	if _, err := scoring.WriteSessionScores(iterLogDir, b.sessions); err != nil {
		return err
	}
	return nil
}

// rescoreTask is the task state shared across ticks.
type rescoreTask struct {
	cfg           RescoreConfig
	scorer        bulkScorer
	watermarkPath string
}

// newRescoreTask validates cfg and builds the rescorer with an explicit
// scorer (the constructor seam tests use to fake the scoring pipeline).
func newRescoreTask(cfg RescoreConfig, sc bulkScorer) (*rescoreTask, error) {
	if cfg.IterLogDir == "" {
		return nil, ErrNoIterLogDir
	}
	if cfg.RepoDir == "" {
		return nil, ErrNoRepoDir
	}
	if cfg.Bus == nil {
		return nil, ErrNoBus
	}
	return &rescoreTask{
		cfg:           cfg,
		scorer:        sc,
		watermarkPath: state.Path(cfg.RepoDir, RescoreName),
	}, nil
}

// NewRescore builds the scheduler task that ticks on an interval (default
// 60s) and rescores the whole iteration log when scoring.RubricVersion has
// moved past the version recorded in the rescore watermark: score every
// record, rewrite the per-iteration + per-session sidecars, persist the
// watermark, then publish RescoreDone on the bus. The watermark is
// persisted before the publish so the bus stays eventually consistent with
// disk (G1: a missed event is recovered from the sidecars, never the bus).
func NewRescore(cfg RescoreConfig) (scheduler.Task, error) {
	rt, err := newRescoreTask(cfg, packageBulkScorer{})
	if err != nil {
		return scheduler.Task{}, err
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = defaultRescoreInterval
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultRescoreTimeout
	}
	return scheduler.Task{
		Name:    RescoreName,
		Trigger: scheduler.Interval(interval),
		RunFn:   rt.run,
		Timeout: timeout,
	}, nil
}

// run is one tick: load the watermark and act only on a rubric-version
// delta. An absent watermark records the current version as the baseline
// WITHOUT rescoring — the per-iteration ingester already scores on ingest,
// so first-tick backfill would be redundant quadratic work (task
// anti-scope) — and every later tick compares against that baseline.
func (r *rescoreTask) run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var wm RescoreWatermark
	found, err := state.Load(r.watermarkPath, &wm)
	if err != nil {
		return err
	}
	switch {
	case !found:
		return state.Save(r.watermarkPath, &RescoreWatermark{RubricVersion: scoring.RubricVersion})
	case wm.RubricVersion == scoring.RubricVersion:
		// Steady state: the sidecars already match the active rubric.
		return nil
	default:
		return r.rescore(ctx, wm.RubricVersion)
	}
}

// rescore performs one full pass for a version bump: score everything,
// rewrite the sidecars, advance the watermark, and only then publish — so a
// subscriber woken by RescoreDone always finds the rescored sidecars and
// the new watermark on disk. A failure before the watermark save leaves the
// old version in place and the next tick retries the whole idempotent pass.
func (r *rescoreTask) rescore(ctx context.Context, fromVersion string) error {
	b, err := r.scorer.ScoreAll(r.cfg.IterLogDir, r.cfg.RepoDir)
	if err != nil {
		return fmt.Errorf("tasks: rescore %s -> %s: %w", fromVersion, scoring.RubricVersion, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.scorer.WriteSidecars(r.cfg.IterLogDir, b); err != nil {
		return fmt.Errorf("tasks: rescore %s -> %s: %w", fromVersion, scoring.RubricVersion, err)
	}
	if err := state.Save(r.watermarkPath, &RescoreWatermark{RubricVersion: scoring.RubricVersion}); err != nil {
		return err
	}
	payload := events.RescoreDone{
		FromVersion: fromVersion,
		ToVersion:   scoring.RubricVersion,
		IterCount:   len(b.scores),
	}
	if err := r.cfg.Bus.Publish(events.TopicRescoreDone, payload); err != nil {
		return fmt.Errorf("tasks: publish %s: %w", events.TopicRescoreDone, err)
	}
	return nil
}
