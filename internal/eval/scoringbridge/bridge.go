package scoringbridge

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

// evalIteration is the fixed iteration number of every eval run. Per R4 spec
// OQ2 (recommendation applied) v1 scoring is 1-shot — a run is a single
// sample, so its iteration log holds exactly iter-1.
const evalIteration = 1

// iterationLogDirName is the iteration-log directory under a run dir — the
// eval-namespaced iter-log root of spec invariant D4.6.
const iterationLogDirName = "iteration-log"

// Typed errors callers can match with errors.Is.
var (
	// ErrEmptyRunID is returned by ScoreRun when the run carries no run ID.
	ErrEmptyRunID = errors.New("scoringbridge: run id is required")

	// ErrEmptyRunDir is returned by ScoreRun when the run names no run
	// directory — without it there is no eval-namespaced iter-log root to
	// write into.
	ErrEmptyRunDir = errors.New("scoringbridge: run dir is required")

	// ErrNilTaskSpec is returned by ScoreRun when the run carries no task
	// spec.
	ErrNilTaskSpec = errors.New("scoringbridge: task spec is nil")
)

// IterationLogDir returns the eval-namespaced iteration-log directory for a
// run dir (<runDir>/iteration-log). It is the only iter-log location this
// package ever writes to: deriving it from the run dir — rather than
// accepting an arbitrary log dir — is how the bridge structurally upholds
// spec invariant D4.6 (eval runs never write into the active orchestration
// log).
func IterationLogDir(runDir string) string {
	return filepath.Join(runDir, iterationLogDirName)
}

// AgentTelemetry is the agent-runner identity and usage telemetry for one
// eval run (R4 spec requirement R9: platform, model, and session identity are
// captured so the platform-tuner persona can diff platforms). All fields are
// optional; what is present flows into the iteration record's agent /
// session_tokens blocks and from there into the rubric's token-efficiency
// signal.
type AgentTelemetry struct {
	// SessionID is the platform session UUID, when the runner captured one.
	SessionID string
	// Harness is the agent platform harness (claude-code, codex, ...).
	Harness string
	// Model is the model ID the run used.
	Model string
	// Retries is how many times the runner had to re-invoke the agent; it
	// feeds the rubric's correction-pressure signal.
	Retries int
	// Tokens is the run's token usage; nil when the runner captured none,
	// which leaves the token-efficiency signal absent (absent is
	// first-class — the rubric renormalizes).
	Tokens *scoring.TokenUsage
}

// VerifyResult is the bridge's view of the sandbox verification outcome: did
// the TaskSpec's build and test commands run, and did they pass. It is the
// translation input for the record's verifiers block; the full stdout/stderr
// capture stays with the verifier packages and the eval-run sidecar — the
// rubric only consumes pass/fail.
type VerifyResult struct {
	// BuildRan reports whether the TaskSpec's build_cmd was executed; false
	// when the spec has no build_cmd or the harness never reached it.
	BuildRan bool
	// BuildPassed reports the build_cmd outcome; meaningful only when
	// BuildRan.
	BuildPassed bool
	// TestRan reports whether the TaskSpec's test_cmd was executed. When
	// false the test verifier is recorded with status "unknown", which the
	// rubric excludes from the verifier mean — "never checked" is not the
	// same as "failed".
	TestRan bool
	// TestPassed reports the test_cmd outcome; meaningful only when TestRan.
	TestPassed bool
}

// EvalRun is the bridge's input: everything one completed eval run produced.
// The harness driver assembles it from the sandbox instance (RunID, RunDir,
// BaseCommit), the generated TaskSpec, the agent-runner telemetry, and the
// verifier outcome.
type EvalRun struct {
	// RunID uniquely identifies the run (sandbox.Instance.RunID).
	RunID string
	// RunDir is the run's sidecar directory (sandbox.Instance.RunDir,
	// .agents/eval/runs/<run-id>). The iteration record and score sidecar
	// are written under <RunDir>/iteration-log/ per spec invariant D4.6.
	RunDir string
	// BaseCommit is the source-repo commit the sandbox was provisioned at,
	// recorded on the iteration entry for reproducibility (spec R10).
	BaseCommit string
	// Spec is the task the run executed. Required and must validate.
	Spec *eval.TaskSpec
	// Agent is the runner identity + usage telemetry (spec R9).
	Agent AgentTelemetry
	// Verify is the sandbox verification outcome.
	Verify VerifyResult
	// FinishedAt timestamps the run for the record's date / checkpoint_at
	// fields; the zero value falls back to the current time.
	FinishedAt time.Time
}

// validate checks the structural invariants ScoreRun depends on.
func (r EvalRun) validate() error {
	if strings.TrimSpace(r.RunID) == "" {
		return ErrEmptyRunID
	}
	if strings.TrimSpace(r.RunDir) == "" {
		return ErrEmptyRunDir
	}
	if r.Spec == nil {
		return ErrNilTaskSpec
	}
	if err := r.Spec.Validate(); err != nil {
		return fmt.Errorf("scoringbridge: invalid task spec: %w", err)
	}
	return nil
}

// Result reports everything ScoreRun persisted and computed for one run.
type Result struct {
	// IterLogDir is the eval-namespaced iteration-log dir the artifacts
	// were written into.
	IterLogDir string
	// RecordPath is the persisted iteration record (iter-1.yaml).
	RecordPath string
	// ScorePath is the persisted score sidecar (iter-1.score.yaml).
	ScorePath string
	// Record is the scored iteration record, exactly as a re-load of
	// RecordPath through scoring.LoadIterationLog would yield it.
	Record scoring.IterationRecord
	// Signals is the assembled signal set the rubric scored, including the
	// parallel integrity / objective outputs that never reach the numeric
	// score.
	Signals scoring.SignalSet
	// Score is the rubric-applied outcome, already persisted at ScorePath.
	Score scoring.Score
}

// ScoreRun bridges one completed eval run into R1: it emits the R1-shaped
// iteration record into <RunDir>/iteration-log/iter-1.yaml, assembles the
// production signal set, scores it with scoring.DefaultRubric(), and persists
// the score sidecar via scoring.WriteIterationScore.
//
// A failed verification still produces a score sidecar — the failure is
// encoded as a 0-valued verifier/tests signal, not a missing file (spec done
// criterion 8). The rubric and its version are the production ones (spec
// invariant D4.4): nothing here forks the scoring path.
func ScoreRun(run EvalRun) (Result, error) {
	if err := run.validate(); err != nil {
		return Result{}, err
	}

	emit := buildEmitRecord(run)
	data, err := yaml.Marshal(emit)
	if err != nil {
		return Result{}, fmt.Errorf("scoringbridge: marshal iteration record: %w", err)
	}

	iterDir := IterationLogDir(run.RunDir)
	if err := fsops.MkdirAll(iterDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("scoringbridge: create eval iter-log dir: %w", err)
	}
	recordPath := filepath.Join(iterDir, fmt.Sprintf("iter-%d.yaml", evalIteration))
	if err := fsops.WriteFileAtomic(recordPath, data); err != nil {
		return Result{}, fmt.Errorf("scoringbridge: write iteration record: %w", err)
	}

	rec := emit.toIterationRecord()
	set := assembleSignals(rec, run.RunDir)
	score := scoring.DefaultRubric().Score(set)
	scorePath, err := scoring.WriteIterationScore(iterDir, score)
	if err != nil {
		return Result{}, fmt.Errorf("scoringbridge: persist score: %w", err)
	}

	return Result{
		IterLogDir: iterDir,
		RecordPath: recordPath,
		ScorePath:  scorePath,
		Record:     rec,
		Signals:    set,
		Score:      score,
	}, nil
}

// assembleSignals builds the production SignalSet for the emitted record.
// The extractor lineup mirrors scoring.BuildSignalSets with the eval-run
// substitutions: iter-log signals read the record itself (artifact paths
// resolve under the run dir); git topology and transcript objectives have no
// eval counterpart and are explicitly absent; token efficiency comes from the
// record's native session_tokens; hook-outcome and human-label sidecars are
// read from the EVAL iter-log dir, so a labeled eval run scores its label
// under the same rubric as production (and an unlabeled run stays absent).
func assembleSignals(rec scoring.IterationRecord, runDir string) scoring.SignalSet {
	iterDir := IterationLogDir(runDir)
	return scoring.AssembleSignalSet(
		rec,
		scoring.ExtractIterlogSignals(rec, runDir),
		evalGitSignals(),
		tokenSignals(rec.SessionTokens),
		evalObjectives(),
		scoring.ExtractHookOutcomesSignal(iterDir, evalIteration),
		scoring.ExtractHumanLabelSignals(iterDir, evalIteration),
	)
}

// evalGitSignals returns the git-topology partial for an eval run. Both
// objective git signals are absent by construction: an eval run's work lives
// on an ephemeral sandbox worktree that never lands on trunk, and eval tasks
// declare no TASKS.yaml write_scope. Absent is first-class — the rubric
// renormalizes over the present signals, which is exactly the D4.4 "same
// rubric, no eval-special semantics" contract.
func evalGitSignals() scoring.GitSignals {
	return scoring.GitSignals{
		LandedObserved: scoring.AbsentSignal("eval sandbox run: trunk landing not applicable"),
		ScopeObserved:  scoring.AbsentSignal("eval sandbox run: no declared write_scope"),
	}
}

// evalObjectives returns the transcript-derived objective checks for an eval
// run. v1 captures no transcript window for sandboxed agent sessions, so all
// three checks are absent; they are parallel observations that never enter
// the numeric score, and a future runner that exposes its session transcript
// can populate them without touching the rubric.
func evalObjectives() scoring.IterationObjectives {
	absent := scoring.AbsentSignal("eval run captured no transcript window")
	return scoring.IterationObjectives{
		RanCliCommand:       absent,
		ReadLoopState:       absent,
		CommittedAfterTests: absent,
	}
}

// tokenSignals derives the backfill partial from the record's native
// session_tokens — the same "native telemetry is authoritative" shortcut
// scoring's transcript backfill applies. With no telemetry the
// token-efficiency signal is absent and renormalizes away.
func tokenSignals(tokens *scoring.TokenUsage) scoring.BackfillSignals {
	bf := scoring.BackfillSignals{Iteration: evalIteration}
	if tokens == nil {
		bf.TokenEfficiency = scoring.AbsentSignal("eval run captured no token telemetry")
		return bf
	}
	bf.TokenEfficiency = scoring.PresentSignal(
		tokens.CacheHitRate,
		fmt.Sprintf("native session_tokens: cache_hit_rate %.3f", tokens.CacheHitRate),
	)
	return bf
}
