// Package store is the R2 observability dashboard's read-through data layer.
//
// It reads directly from the on-disk iteration-log roots and the
// iter-N.score.yaml / session-<id>.score.yaml sidecars that R1 already writes
// (internal/scoring), and projects them into the stable presentation DTOs the
// dashboard HTTP handlers (t03) and frontend (t08) share. There is no new
// persistent store (spec D2.1): aggregates are computed on demand behind an
// in-process LRU (cache.go) keyed per iter-log root and invalidated when the
// root's newest file mtime changes.
//
// The DTO field shapes here are the Go twin of the JSON Schemas under
// schemas/dashboard-*.schema.json and the endpoint contract in
// .agents/workflow/plans/r2-observability-dashboard/design/API.md — those two
// artifacts are authority for payload shape and endpoint behaviour
// respectively. The Store interface below is the one API.md pins as
// authoritative for this task (t02).
//
// Anti-scope (pinned by the task): this layer never writes and never recomputes
// a score. Missing or stale sidecars degrade to scored:false / score:null — the
// synchronous recompute-on-miss path is t06's job. Consequently the detail-only
// integrity / objective / integrity_observation_count / transcript_turn_count
// fields, which are products of the scoring pipeline rather than the persisted
// sidecar, are left empty here; t06 populates them when it recomputes.
package store

import (
	"context"
	"errors"

	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// ErrNotFound is returned by GetRun / GetIteration when the requested session or
// iteration is absent from every resolved iter-log root. Handlers (t03) map it
// to a 404 not_found envelope.
var ErrNotFound = errors.New("dashboard/store: not found")

// bandUnscored is the band reported for a session or iteration that has no
// numeric score (no sidecar, or a sidecar whose Scored flag is false).
const bandUnscored = scoring.BandUnscored

// Store is the read-through surface the dashboard handlers depend on. It is the
// interface API.md pins as authoritative for t02; a future denormalized backend
// (spec D2.1 forward door) can satisfy the same contract.
type Store interface {
	// ListRuns returns one RunSummary per session discovered across the
	// resolved roots, filtered / sorted / paginated per f. Never errors on a
	// corrupt file — the bad file is skipped and the remainder returned.
	ListRuns(ctx context.Context, f RunFilter) ([]RunSummary, error)
	// GetRun returns the full RunDetail (including per_iteration) for one
	// session, or ErrNotFound.
	GetRun(ctx context.Context, sessionID string) (RunDetail, error)
	// ListIterations returns the iteration list for one session ascending by
	// iteration, or ErrNotFound if the session is unknown. A listed iteration
	// whose score sidecar is absent returns scored:false / score:null.
	ListIterations(ctx context.Context, sessionID string) ([]IterationSummary, error)
	// GetIteration returns the full IterationDetail for iteration n in
	// iterLogDir (defaulting to the active root when empty), or ErrNotFound.
	GetIteration(ctx context.Context, iterLogDir string, n int) (IterationDetail, error)
	// Rubric returns the active rubric projection (spec R4).
	Rubric(ctx context.Context) (RubricDoc, error)
	// Health returns liveness counts for cheap operator triage (spec R9).
	Health(ctx context.Context) (Health, error)
}

// RunFilter is the query shape for ListRuns. Zero values are normalized to the
// contract defaults (limit 50, offset 0, sort last_update, order desc) — param
// validation / 400s are the handler's job (t03), so the store is forgiving.
type RunFilter struct {
	Limit   int
	Offset  int
	Sort    string // last_update | score | iteration_count | session_id
	Order   string // asc | desc
	Band    string // optional exact-match band filter
	Harness string // optional exact-match harness filter
}

// Run is the presentation projection of one agent-run session (dashboard-run
// schema). RunSummary omits per_iteration; RunDetail includes it — they are the
// same shape, distinguished only by which fields the store populates.
type Run struct {
	SessionID        string         `json:"session_id"`
	Harness          string         `json:"harness"`
	Model            string         `json:"model"`
	Wave             string         `json:"wave"`
	RubricVersion    string         `json:"rubric_version"`
	IterationCount   int            `json:"iteration_count"`
	Scored           bool           `json:"scored"`
	Score            *float64       `json:"score"`
	Band             string         `json:"band"`
	FirstIteration   *int           `json:"first_iteration"`
	LastIteration    *int           `json:"last_iteration"`
	LastUpdate       *string        `json:"last_update"`
	IterLogDir       string         `json:"iter_log_dir"`
	PerIteration     []IterScoreRef `json:"per_iteration,omitempty"`
	MeanCacheHitRate *float64       `json:"mean_cache_hit_rate"`
}

// RunSummary is the row shape returned by list runs (per_iteration omitted).
type RunSummary = Run

// RunDetail is the full object returned by get one run (per_iteration present).
type RunDetail = Run

// IterScoreRef is a lightweight per-iteration score ref inside RunDetail.
type IterScoreRef struct {
	Iteration int      `json:"iteration"`
	Scored    bool     `json:"scored"`
	Score     *float64 `json:"score"`
	Band      string   `json:"band"`
}

// Iteration is the presentation projection of one agent-run iteration
// (dashboard-iteration schema). IterationSummary omits the heavy detail-only
// fields (breakdown / integrity / objective / verifiers); IterationDetail
// includes those the store can source from disk.
type Iteration struct {
	Iteration                 int            `json:"iteration"`
	SessionID                 string         `json:"session_id"`
	SchemaVersion             int            `json:"schema_version"`
	Date                      string         `json:"date"`
	Wave                      string         `json:"wave"`
	TaskID                    string         `json:"task_id"`
	Commit                    string         `json:"commit"`
	RubricVersion             string         `json:"rubric_version"`
	Scored                    bool           `json:"scored"`
	Score                     *float64       `json:"score"`
	Band                      string         `json:"band"`
	FilesChanged              int            `json:"files_changed"`
	LinesAdded                int            `json:"lines_added"`
	LinesRemoved              int            `json:"lines_removed"`
	Retries                   int            `json:"retries"`
	IntegrityObservationCount int            `json:"integrity_observation_count"`
	TranscriptTurnCount       *int           `json:"transcript_turn_count"`
	TokenUsage                *TokenUsage    `json:"token_usage"`
	Verifiers                 []Verifier     `json:"verifiers,omitempty"`
	Breakdown                 []BreakdownRow `json:"breakdown,omitempty"`
	Integrity                 []IntegrityRow `json:"integrity,omitempty"`
	Objective                 *Objective     `json:"objective,omitempty"`
}

// IterationSummary is the row shape inside the iteration list.
type IterationSummary = Iteration

// IterationDetail is the full object returned by get one iteration.
type IterationDetail = Iteration

// TokenUsage mirrors internal/scoring.TokenUsage for the DTO.
type TokenUsage struct {
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	CacheHitRate        float64 `json:"cache_hit_rate"`
}

// Verifier is one per-verifier outcome row (detail only).
type Verifier struct {
	Type       string `json:"type"`
	Status     string `json:"status"`
	GatePassed bool   `json:"gate_passed"`
	TestsAdded int    `json:"tests_added"`
	Retries    int    `json:"retries"`
}

// BreakdownRow is one per-signal score-breakdown row (detail only), sourced from
// the persisted sidecar's breakdown in rubric order.
type BreakdownRow struct {
	Signal          string  `json:"signal"`
	Label           string  `json:"label"`
	Present         bool    `json:"present"`
	SubScore        float64 `json:"sub_score"`
	Detail          string  `json:"detail,omitempty"`
	NominalWeight   float64 `json:"nominal_weight"`
	EffectiveWeight float64 `json:"effective_weight"`
	Contribution    float64 `json:"contribution"`
}

// SignalSide mirrors internal/scoring.SignalValue for the integrity DTO.
type SignalSide struct {
	Present  bool    `json:"present"`
	SubScore float64 `json:"sub_score,omitempty"`
	Detail   string  `json:"detail,omitempty"`
}

// IntegrityRow is one claimed-vs-observed integrity observation (detail only).
// It is a product of the scoring pipeline, not the persisted sidecar, so the
// read-through store leaves it empty — t06's recompute path populates it.
type IntegrityRow struct {
	Signal     string     `json:"signal"`
	Role       string     `json:"role"`
	Claimed    SignalSide `json:"claimed"`
	Observed   SignalSide `json:"observed"`
	Comparable bool       `json:"comparable"`
	Delta      *float64   `json:"delta"`
}

// Objective carries transcript-derived process checks (detail only). Like
// IntegrityRow it is recompute-sourced, so the store leaves it nil.
type Objective struct {
	RanCliCommand       SignalSide `json:"ran_cli_command"`
	CommittedAfterTests SignalSide `json:"committed_after_tests"`
	ReadLoopState       SignalSide `json:"read_loop_state"`
}

// RubricDoc is the active-rubric projection (dashboard-rubric schema).
type RubricDoc struct {
	Version     string         `json:"version"`
	Combination string         `json:"combination"`
	Signals     []RubricSignal `json:"signals"`
	Bands       []RubricBand   `json:"bands"`
}

// RubricSignal is one signal row of the rubric doc.
type RubricSignal struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Weight      float64 `json:"weight"`
	Description string  `json:"description"`
	TwoWay      bool    `json:"two_way"`
}

// RubricBand is one band row of the rubric doc.
type RubricBand struct {
	Name string  `json:"name"`
	Min  float64 `json:"min"`
}

// Health is the inline liveness payload (API.md §3.6, spec R9).
type Health struct {
	Status           string   `json:"status"`
	RunCount         int      `json:"run_count"`
	IterationCount   int      `json:"iteration_count"`
	LastIterLogMtime *string  `json:"last_iter_log_mtime"`
	SubscriberCount  int      `json:"subscriber_count"`
	RubricVersion    string   `json:"rubric_version"`
	Roots            []string `json:"roots"`
}
