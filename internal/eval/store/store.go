// Package store persists the four sidecar artifacts of a completed eval run to
// the canonical layout under <root>/.agents/eval/runs/<run-id>/. It is the
// single write path the R2 dashboard reads; the layout it produces is the
// stable contract every downstream consumer depends on.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/eval/harness"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

// Sidecar file and directory name constants. Every path the store writes is
// derived from these so a rename is a one-line change.
const (
	iterLogDirName = "iteration-log"
	taskspecYAML   = "taskspec.yaml"
	evalRunYAML    = "eval-run.yaml"
	iterRecordYAML = "iter-1.yaml"
)

// Typed errors callers can match with errors.Is.
var (
	// ErrEmptyRunID is returned when the run carries no run ID.
	ErrEmptyRunID = errors.New("store: run id is required")
	// ErrNilSpec is returned when the run carries no task spec.
	ErrNilSpec = errors.New("store: task spec is nil")
	// ErrEmptyRoot is returned when the root dir is empty.
	ErrEmptyRoot = errors.New("store: root dir is required")
	// ErrEmptyRecordPath is returned when the run score has no record path.
	// The score record path is written by scoringbridge.ScoreRun; its absence
	// means the scoring stage did not complete.
	ErrEmptyRecordPath = errors.New("store: score record path is required")
)

// Result reports the persisted artifact paths produced by WriteEvalRun.
type Result struct {
	// RunDir is the run's sidecar directory (<root>/.agents/eval/runs/<run-id>).
	RunDir string
	// TaskspecPath is the persisted task spec (<RunDir>/taskspec.yaml).
	TaskspecPath string
	// EvalRunPath is the persisted run aggregate (<RunDir>/eval-run.yaml).
	EvalRunPath string
	// IterLogDir is the iteration-log subdirectory (<RunDir>/iteration-log).
	IterLogDir string
	// RecordPath is the persisted iteration record (<IterLogDir>/iter-1.yaml).
	RecordPath string
	// ScorePath is the persisted score sidecar (<IterLogDir>/iter-1.score.yaml).
	ScorePath string
}

// PersistedEvalRun is the on-disk YAML shape of eval-run.yaml. It aggregates
// run identity, agent outcome, verification result, and score summary so the
// R2 dashboard can render a run summary without reading all four sidecar files.
type PersistedEvalRun struct {
	RunID      string        `yaml:"run_id"`
	BaseCommit string        `yaml:"base_commit"`
	TaskID     string        `yaml:"task_id"`
	Language   string        `yaml:"language"`
	Difficulty string        `yaml:"difficulty"`
	Agent      agentOutcome  `yaml:"agent"`
	Verify     verifyOutcome `yaml:"verify"`
	Score      scoreOutcome  `yaml:"score"`
}

// agentOutcome is the agent-runner summary block in eval-run.yaml.
type agentOutcome struct {
	ExitCode int    `yaml:"exit_code"`
	Duration string `yaml:"duration"`
}

// verifyOutcome is the verification-result summary block in eval-run.yaml.
type verifyOutcome struct {
	Passed   bool   `yaml:"passed"`
	Phase    string `yaml:"phase,omitempty"`
	ExitCode int    `yaml:"exit_code"`
	Duration string `yaml:"duration"`
}

// scoreOutcome is the scoring-result summary block in eval-run.yaml.
type scoreOutcome struct {
	Value  float64 `yaml:"value"`
	Band   string  `yaml:"band"`
	Scored bool    `yaml:"scored"`
}

// RunDir returns the canonical sidecar directory for root and runID:
//
//	<root>/.agents/eval/runs/<runID>
//
// This path matches the default sandbox.Config.RunsRoot layout so the path the
// sandbox provisions and the path the store writes are always the same.
func RunDir(root, runID string) string {
	return filepath.Join(root, ".agents", "eval", "runs", runID)
}

// WriteEvalRun persists the four sidecar artifacts of a completed eval run to:
//
//	<root>/.agents/eval/runs/<run.RunID>/
//	  taskspec.yaml                    — the eval.TaskSpec
//	  eval-run.yaml                    — the run aggregate
//	  iteration-log/iter-1.yaml        — R1-shaped iteration record
//	  iteration-log/iter-1.score.yaml  — score sidecar
//
// All four writes are atomic (temp-then-rename via fsops.WriteFileAtomic) so a
// crash never leaves a partial sidecar visible to the dashboard. The
// iter-1.yaml bytes are copied byte-for-byte from run.Score.RecordPath,
// preserving the emitRecord YAML schema that scoring.LoadIterationLog expects.
// The score sidecar is re-persisted from run.Score.Score via the production
// scoring.WriteIterationScore so the same PersistedScore shape is always used.
func WriteEvalRun(run harness.EvalRun, root string) (Result, error) {
	if err := validateRun(run, root); err != nil {
		return Result{}, err
	}

	runDir := RunDir(root, run.RunID)
	iterDir := filepath.Join(runDir, iterLogDirName)
	if err := fsops.MkdirAll(iterDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("store: create sidecar dirs: %w", err)
	}

	taskspecPath := filepath.Join(runDir, taskspecYAML)
	if err := writeTaskspec(taskspecPath, run); err != nil {
		return Result{}, err
	}

	evalRunPath := filepath.Join(runDir, evalRunYAML)
	if err := writeEvalRunFile(evalRunPath, run); err != nil {
		return Result{}, err
	}

	recordPath := filepath.Join(iterDir, iterRecordYAML)
	if err := copyFileAtomic(recordPath, run.Score.RecordPath); err != nil {
		return Result{}, fmt.Errorf("store: write iter record: %w", err)
	}

	scorePath, err := scoring.WriteIterationScore(iterDir, run.Score.Score)
	if err != nil {
		return Result{}, fmt.Errorf("store: persist score: %w", err)
	}

	return Result{
		RunDir:       runDir,
		TaskspecPath: taskspecPath,
		EvalRunPath:  evalRunPath,
		IterLogDir:   iterDir,
		RecordPath:   recordPath,
		ScorePath:    scorePath,
	}, nil
}

// validateRun checks the structural invariants WriteEvalRun depends on.
func validateRun(run harness.EvalRun, root string) error {
	if strings.TrimSpace(run.RunID) == "" {
		return ErrEmptyRunID
	}
	if run.Spec == nil {
		return ErrNilSpec
	}
	if strings.TrimSpace(root) == "" {
		return ErrEmptyRoot
	}
	if strings.TrimSpace(run.Score.RecordPath) == "" {
		return ErrEmptyRecordPath
	}
	return nil
}

// writeTaskspec marshals run.Spec to YAML and atomically writes it to path.
func writeTaskspec(path string, run harness.EvalRun) error {
	data, err := yaml.Marshal(run.Spec)
	if err != nil {
		return fmt.Errorf("store: marshal taskspec: %w", err)
	}
	if err := fsops.WriteFileAtomic(path, data); err != nil {
		return fmt.Errorf("store: write taskspec: %w", err)
	}
	return nil
}

// writeEvalRunFile builds a PersistedEvalRun from run, marshals it, and
// atomically writes it to path.
func writeEvalRunFile(path string, run harness.EvalRun) error {
	persisted := buildPersistedEvalRun(run)
	data, err := yaml.Marshal(persisted)
	if err != nil {
		return fmt.Errorf("store: marshal eval-run: %w", err)
	}
	if err := fsops.WriteFileAtomic(path, data); err != nil {
		return fmt.Errorf("store: write eval-run: %w", err)
	}
	return nil
}

// buildPersistedEvalRun translates a completed harness.EvalRun into the
// eval-run.yaml shape. A nil Verify leaves the verify block zero-valued —
// present in the YAML with zero fields — rather than omitting the key
// entirely, so the schema stays stable.
func buildPersistedEvalRun(run harness.EvalRun) PersistedEvalRun {
	p := PersistedEvalRun{
		RunID:      run.RunID,
		BaseCommit: run.BaseCommit,
		TaskID:     run.Spec.TaskID,
		Language:   string(run.Spec.Language),
		Difficulty: string(run.Spec.Difficulty),
		Agent: agentOutcome{
			ExitCode: run.Run.ExitCode,
			Duration: run.Run.Duration.String(),
		},
		Score: scoreOutcome{
			Value:  run.Score.Score.Value,
			Band:   run.Score.Score.Band,
			Scored: run.Score.Score.Scored,
		},
	}
	if run.Verify != nil {
		p.Verify = verifyOutcome{
			Passed:   run.Verify.Passed,
			Phase:    string(run.Verify.Phase),
			ExitCode: run.Verify.ExitCode,
			Duration: run.Verify.Duration.String(),
		}
	}
	return p
}

// copyFileAtomic reads src and atomically writes its bytes to dst, so a crash
// mid-copy never leaves dst in a partial state. When dst == src the operation
// is an atomic idempotent overwrite: correct and harmless.
func copyFileAtomic(dst, src string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read source %s: %w", src, err)
	}
	return fsops.WriteFileAtomic(dst, data)
}
