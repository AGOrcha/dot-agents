// Package tasks contains the concrete background tasks hosted by the
// `da service` scheduler (R3 design D2). Each task binds a trigger to a unit
// of work, keeps its restart watermark via internal/service/state (D3), and
// publishes notifications through the events.EventBus interface seam (D4.1)
// — never a concrete bus — assuming only the D4.2 G1–G4 delivery floor: a
// missed event is recovered from the on-disk sidecars, never from the bus.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"github.com/AGOrcha/dot-agents/internal/service/events"
	"github.com/AGOrcha/dot-agents/internal/service/scheduler"
	"github.com/AGOrcha/dot-agents/internal/service/state"
)

// IterLogIngesterName is the scheduler task name and the watermark key of
// the iter-log ingester (sidecar: iterlog-ingester.watermark.yaml).
const IterLogIngesterName = "iterlog-ingester"

// defaultIngestTimeout bounds one ingest pass when the config does not set
// an explicit timeout. Scoring is git-topology + transcript bounded, so a
// pass over a handful of new iterations comfortably fits.
const defaultIngestTimeout = 2 * time.Minute

// iterFileRE matches iteration log entries (iter-N.yaml). Score sidecars
// (iter-N.score.yaml) and other files in the directory do not match.
var iterFileRE = regexp.MustCompile(`^iter-(\d+)\.yaml$`)

// Configuration errors returned by NewIterLogIngester.
var (
	ErrNoIterLogDir = errors.New("tasks: iter-log dir is required")
	ErrNoRepoDir    = errors.New("tasks: repo dir is required")
	ErrNoBus        = errors.New("tasks: event bus is required")
)

// IterationScored is the payload published on events.TopicIterationScored
// after an iteration has been scored and its sidecar written. Subscribers
// treat it as a wake-up: the sidecar at SidecarPath is the canonical state
// (G1 — a dropped event is recovered from disk, not from the bus).
type IterationScored struct {
	Iteration   int     `json:"iter"`
	Score       float64 `json:"score"`
	Band        string  `json:"band"`
	SidecarPath string  `json:"sidecar_path"`
}

// IterLogWatermark is the ingester's D3 restart watermark: the highest
// iteration already ingested, the newest file mtime seen when it was
// processed, and the rubric version the sidecars were written under.
type IterLogWatermark struct {
	LastIterProcessed int       `yaml:"last_iter_processed"`
	LastMTime         time.Time `yaml:"last_mtime"`
	RubricVersion     string    `yaml:"rubric_version"`
}

// IterLogIngesterConfig configures NewIterLogIngester.
type IterLogIngesterConfig struct {
	// IterLogDir is the iteration-log directory to watch and ingest,
	// conventionally .agents/active/iteration-log under RepoDir.
	IterLogDir string
	// RepoDir is the repository root scoring resolves commits against; it
	// also anchors the watermark sidecar under .agents/active/service-state.
	RepoDir string
	// Bus receives IterationScored notifications. This is the D4.1
	// interface seam; any conformant backend works.
	Bus events.EventBus
	// Timeout bounds one ingest pass. Zero applies defaultIngestTimeout.
	Timeout time.Duration
}

// iterScorer is the seam to internal/scoring, injected so tests can fake the
// (expensive, git-dependent) scoring pipeline. The default implementation is
// a thin passthrough — the task adds no scoring logic of its own.
type iterScorer interface {
	Score(iterLogDir, repoDir string, n int) (scoring.Score, scoring.IterationRecord, error)
	WriteSidecar(iterLogDir string, s scoring.Score, rec scoring.IterationRecord) (string, error)
}

// packageScorer delegates to internal/scoring: ScoreIteration is the same
// entrypoint the workflow close-task command uses, and
// WriteIterationScoreWithRecord persists the augmented sidecar shape.
type packageScorer struct{}

func (packageScorer) Score(iterLogDir, repoDir string, n int) (scoring.Score, scoring.IterationRecord, error) {
	return scoring.ScoreIteration(iterLogDir, repoDir, n)
}

func (packageScorer) WriteSidecar(iterLogDir string, s scoring.Score, rec scoring.IterationRecord) (string, error) {
	return scoring.WriteIterationScoreWithRecord(iterLogDir, s, rec)
}

// iterLogIngester is the task state shared across runs.
type iterLogIngester struct {
	cfg           IterLogIngesterConfig
	scorer        iterScorer
	watermarkPath string
}

// newIterLogIngester validates cfg and builds the ingester with an explicit
// scorer (the constructor seam tests use to fake scoring).
func newIterLogIngester(cfg IterLogIngesterConfig, sc iterScorer) (*iterLogIngester, error) {
	if cfg.IterLogDir == "" {
		return nil, ErrNoIterLogDir
	}
	if cfg.RepoDir == "" {
		return nil, ErrNoRepoDir
	}
	if cfg.Bus == nil {
		return nil, ErrNoBus
	}
	return &iterLogIngester{
		cfg:           cfg,
		scorer:        sc,
		watermarkPath: state.Path(cfg.RepoDir, IterLogIngesterName),
	}, nil
}

// NewIterLogIngester builds the scheduler task that watches IterLogDir via
// fsnotify (bursts are coalesced by the trigger's debounce) and, on each
// fire, ingests every new or modified iter-N.yaml: score via internal/
// scoring, write the score sidecar, persist the watermark, then publish
// IterationScored on the bus. The watermark is persisted before the publish
// so the bus stays eventually consistent with disk.
func NewIterLogIngester(cfg IterLogIngesterConfig) (scheduler.Task, error) {
	ing, err := newIterLogIngester(cfg, packageScorer{})
	if err != nil {
		return scheduler.Task{}, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultIngestTimeout
	}
	return scheduler.Task{
		Name:    IterLogIngesterName,
		Trigger: scheduler.FSNotify(cfg.IterLogDir),
		RunFn:   ing.run,
		Timeout: timeout,
	}, nil
}

// run is one ingest pass: load the watermark (absent means start from
// scratch, per D3), collect pending iterations, and ingest them in ascending
// order. The first failure aborts the pass; the next trigger fire retries
// from the persisted watermark, which makes the pass idempotent.
func (g *iterLogIngester) run(ctx context.Context) error {
	var wm IterLogWatermark
	if _, err := state.Load(g.watermarkPath, &wm); err != nil {
		return err
	}
	pending, err := pendingIterations(g.cfg.IterLogDir, wm)
	if err != nil {
		return err
	}
	for _, p := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := g.ingest(p, &wm); err != nil {
			return err
		}
	}
	return nil
}

// pendingIter is one iter-N.yaml file due for ingestion.
type pendingIter struct {
	n     int
	mtime time.Time
}

// pendingIterations scans dir for iter-N.yaml entries that are new
// (n beyond the watermark) or modified (mtime after the watermark's newest
// processed mtime), sorted ascending by iteration. A missing directory means
// nothing to ingest — the log dir may not exist yet in a fresh repo.
func pendingIterations(dir string, wm IterLogWatermark) ([]pendingIter, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tasks: scan iter-log dir %s: %w", dir, err)
	}
	var out []pendingIter
	for _, e := range entries {
		n, ok := iterNumber(e.Name())
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			// Entry vanished between ReadDir and stat; the next
			// fire re-scans.
			continue
		}
		if n > wm.LastIterProcessed || info.ModTime().After(wm.LastMTime) {
			out = append(out, pendingIter{n: n, mtime: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].n < out[j].n })
	return out, nil
}

// iterNumber extracts N from an iter-N.yaml filename; ok is false for any
// other name (score sidecars, historical.yaml, temp files).
func iterNumber(name string) (int, bool) {
	m := iterFileRE.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// ingest scores one iteration, writes its sidecar, advances and persists the
// watermark, and only then publishes the notification — so a subscriber
// woken by the event always finds the sidecar and watermark on disk.
func (g *iterLogIngester) ingest(p pendingIter, wm *IterLogWatermark) error {
	score, rec, err := g.scorer.Score(g.cfg.IterLogDir, g.cfg.RepoDir, p.n)
	if err != nil {
		return fmt.Errorf("tasks: score iter-%d: %w", p.n, err)
	}
	sidecar, err := g.scorer.WriteSidecar(g.cfg.IterLogDir, score, rec)
	if err != nil {
		return fmt.Errorf("tasks: write sidecar for iter-%d: %w", p.n, err)
	}
	wm.advance(p)
	if err := state.Save(g.watermarkPath, wm); err != nil {
		return err
	}
	payload := IterationScored{
		Iteration:   p.n,
		Score:       score.Value,
		Band:        score.Band,
		SidecarPath: sidecar,
	}
	if err := g.cfg.Bus.Publish(events.TopicIterationScored, payload); err != nil {
		return fmt.Errorf("tasks: publish %s for iter-%d: %w", events.TopicIterationScored, p.n, err)
	}
	return nil
}

// advance folds one ingested iteration into the watermark: highest iteration
// wins, newest mtime wins, and the rubric version tracks the current rubric.
func (wm *IterLogWatermark) advance(p pendingIter) {
	if p.n > wm.LastIterProcessed {
		wm.LastIterProcessed = p.n
	}
	if p.mtime.After(wm.LastMTime) {
		wm.LastMTime = p.mtime
	}
	wm.RubricVersion = scoring.RubricVersion
}
