// Package store persists the store-owned sidecar files of a completed eval run
// into the canonical run directory <root>/.agents/eval/runs/<run-id>/. It is one
// participant in an INCREMENTALLY ASSEMBLED directory: the sandbox creates the
// run dir, the score stage writes iteration-log/{iter-1.yaml,iter-1.score.yaml}
// into it, and this package writes the two files it owns (taskspec.yaml,
// eval-run.yaml). The store therefore does NOT own the whole directory — it
// never publishes it atomically as a unit, never refuses an existing dir, and
// never deletes it. Crash-safety is per-file: each file it writes appears
// atomically (temp-then-rename via internal/fsops) or not at all.
package store

import (
	"crypto/sha256"
	"encoding/hex"
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

	// digestPrefix labels the hash algorithm of a digest field so a future
	// algorithm swap is self-describing on disk.
	digestPrefix = "sha256:"
)

// Filesystem seams. They default to the fsops primitives (fsguard: every
// mutation routes through internal/fsops) and are overridable in tests so the
// atomic-write / mkdir / stat error paths are coverable without
// platform-specific filesystem tricks.
var (
	mkdirAll        = fsops.MkdirAll
	writeFileAtomic = fsops.WriteFileAtomic
	statPath        = os.Stat
)

// Typed errors callers can match with errors.Is.
var (
	// ErrEmptyRunID is returned when the run carries no run ID.
	ErrEmptyRunID = errors.New("store: run id is required")
	// ErrUnsafeRunID is returned when the run ID contains a path separator,
	// a ".." segment, or otherwise does not name a single path element — any
	// of which could escape the <root>/.agents/eval/runs/ directory.
	ErrUnsafeRunID = errors.New("store: run id is not a safe path element")
	// ErrNilSpec is returned when the run carries no task spec.
	ErrNilSpec = errors.New("store: task spec is nil")
	// ErrEmptyRoot is returned when the root dir is empty.
	ErrEmptyRoot = errors.New("store: root dir is required")
	// ErrEmptyRecordPath is returned when the run score has no iteration-record
	// path. The score stage writes it (scoringbridge.Result.RecordPath); its
	// absence means the scoring stage did not complete.
	ErrEmptyRecordPath = errors.New("store: score record path is required")
	// ErrEmptyScorePath is returned when the run score has no score-sidecar
	// path (scoringbridge.Result.ScorePath) — the same "scoring did not
	// complete" signal for the score artifact.
	ErrEmptyScorePath = errors.New("store: score sidecar path is required")
)

// Result reports the persisted artifact paths produced by WriteEvalRun. Every
// path names the canonical location under the run dir.
type Result struct {
	// RunDir is the run's sidecar directory (<root>/.agents/eval/runs/<run-id>).
	RunDir string
	// TaskspecPath is the persisted task spec (<RunDir>/taskspec.yaml).
	TaskspecPath string
	// EvalRunPath is the persisted run aggregate (<RunDir>/eval-run.yaml).
	EvalRunPath string
	// IterLogDir is the iteration-log subdirectory (<RunDir>/iteration-log).
	IterLogDir string
	// RecordPath is the iteration record (<IterLogDir>/iter-1.yaml).
	RecordPath string
	// ScorePath is the score sidecar (<IterLogDir>/iter-1.score.yaml).
	ScorePath string
}

// PersistedEvalRun is the on-disk YAML shape of eval-run.yaml. It aggregates
// run identity, the agent-runner identity + reproducibility metadata (spec R9 /
// R10, plan D6), the verification result, and the score summary so the R2
// dashboard can render a run without reading all four sidecar files.
type PersistedEvalRun struct {
	RunID      string        `yaml:"run_id"`
	BaseCommit string        `yaml:"base_commit"`
	TaskID     string        `yaml:"task_id"`
	Language   string        `yaml:"language"`
	Difficulty string        `yaml:"difficulty"`
	Agent      agentIdentity `yaml:"agent"`
	Verify     verifyOutcome `yaml:"verify"`
	Score      scoreOutcome  `yaml:"score"`
}

// agentIdentity is the agent-runner identity + reproducibility block of
// eval-run.yaml (spec R9: platform, model, prompt overlay captured so the
// platform-tuner persona can diff platforms; R10: a run is reproducible from
// its inputs). PromptDigest hashes the prompt the agent actually ran against —
// in v1 the runner delivers TaskSpec.Prompt verbatim, so that hash is the
// prompt-overlay identity. OutputDigest hashes the agent's stdout so a re-run's
// output can be compared without persisting the full transcript here.
type agentIdentity struct {
	Harness      string `yaml:"harness,omitempty"`
	Model        string `yaml:"model,omitempty"`
	SessionID    string `yaml:"session_id,omitempty"`
	Retries      int    `yaml:"retries"`
	ExitCode     int    `yaml:"exit_code"`
	Duration     string `yaml:"duration"`
	PromptDigest string `yaml:"prompt_digest"`
	OutputDigest string `yaml:"output_digest"`
}

// verifyOutcome is the verification-result summary block in eval-run.yaml.
type verifyOutcome struct {
	Passed   bool   `yaml:"passed"`
	Phase    string `yaml:"phase,omitempty"`
	ExitCode int    `yaml:"exit_code"`
	Duration string `yaml:"duration"`
}

// scoreOutcome is the scoring-result summary block in eval-run.yaml. RubricVersion
// pins the rubric the value was produced under (spec R10 reproducibility).
type scoreOutcome struct {
	Value         float64 `yaml:"value"`
	Band          string  `yaml:"band"`
	Scored        bool    `yaml:"scored"`
	RubricVersion string  `yaml:"rubric_version,omitempty"`
}

// runsRoot returns the eval runs root for a repo root: <root>/.agents/eval/runs.
func runsRoot(root string) string {
	return filepath.Join(root, ".agents", "eval", "runs")
}

// RunDir returns the canonical sidecar directory for root and runID:
//
//	<root>/.agents/eval/runs/<runID>
//
// This path matches the default sandbox.Config.RunsRoot layout so the path the
// sandbox provisions and the path the store writes are always the same.
func RunDir(root, runID string) string {
	return filepath.Join(runsRoot(root), runID)
}

// WriteEvalRun persists the store-owned files of a completed eval run into the
// (incrementally assembled) canonical run directory:
//
//	<root>/.agents/eval/runs/<run.RunID>/
//	  taskspec.yaml                    — the eval.TaskSpec        (store-owned)
//	  eval-run.yaml                    — run aggregate + R9/R10    (store-owned)
//	  iteration-log/iter-1.yaml        — R1-shaped iteration record (score stage)
//	  iteration-log/iter-1.score.yaml  — score sidecar             (score stage)
//
// It ensures the run dir (and iteration-log) exist, then writes taskspec.yaml
// and eval-run.yaml with per-file atomic writes (temp-then-rename via
// fsops.WriteFileAtomic) — the crash-safety unit is the individual file, so a
// mid-write failure of one leaves no torn file and never touches the others.
//
// The iteration-log artifacts belong to the score stage. WriteEvalRun ADAPTS to
// both wirings: when run.Score.RecordPath / ScorePath already resolve to their
// canonical location inside this run dir (the merged harness, where the score
// stage wrote them in place), it adopts them — validating they exist, never
// copying a file onto itself. When they resolve to a scratch location outside
// the run dir, it copies them in atomically. It never deletes the run dir and
// never refuses an existing one.
func WriteEvalRun(run harness.EvalRun, root string) (Result, error) {
	if err := validateRun(run, root); err != nil {
		return Result{}, err
	}

	runDir := RunDir(root, run.RunID)
	iterDir := filepath.Join(runDir, iterLogDirName)
	if err := mkdirAll(iterDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("store: ensure run dir: %w", err)
	}

	taskspecPath := filepath.Join(runDir, taskspecYAML)
	if err := writeYAML(taskspecPath, run.Spec); err != nil {
		return Result{}, fmt.Errorf("store: taskspec: %w", err)
	}
	evalRunPath := filepath.Join(runDir, evalRunYAML)
	if err := writeYAML(evalRunPath, buildPersistedEvalRun(run)); err != nil {
		return Result{}, fmt.Errorf("store: eval-run: %w", err)
	}

	recordPath := filepath.Join(iterDir, iterRecordYAML)
	if err := placeArtifact(recordPath, run.Score.RecordPath); err != nil {
		return Result{}, fmt.Errorf("store: iter record: %w", err)
	}
	scorePath := scoring.IterationScorePath(iterDir, run.Score.Score.Iteration)
	if err := placeArtifact(scorePath, run.Score.ScorePath); err != nil {
		return Result{}, fmt.Errorf("store: iter score: %w", err)
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
	if err := validateRunID(run.RunID); err != nil {
		return err
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
	if strings.TrimSpace(run.Score.ScorePath) == "" {
		return ErrEmptyScorePath
	}
	return nil
}

// validateRunID rejects a run ID that is empty or that does not name a single,
// separator-free path element — the guard that keeps a hostile or malformed run
// ID from escaping the runs directory (e.g. "../../etc" or "a/b").
func validateRunID(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return ErrEmptyRunID
	}
	if runID == "." || strings.Contains(runID, "..") ||
		strings.ContainsAny(runID, `/\`) || filepath.Base(runID) != runID {
		return ErrUnsafeRunID
	}
	return nil
}

// placeArtifact ensures the score-stage artifact at src is present at its
// canonical path dst. When src already IS dst (the score stage wrote it into the
// canonical run dir — the merged-harness wiring), it is ADOPTED in place:
// validated to exist, never copied onto itself. Otherwise src is a scratch
// location and its bytes are copied to dst atomically.
func placeArtifact(dst, src string) error {
	if samePath(dst, src) {
		if _, err := statPath(src); err != nil {
			return fmt.Errorf("adopt in place: %w", err)
		}
		return nil
	}
	return copyFileAtomic(dst, src)
}

// samePath reports whether a and b denote the same filesystem path. Both the
// canonical dst (derived from root) and the score-stage src (derived from the
// sandbox run dir) are absolute in the real pipeline, so a cleaned string
// comparison is sufficient to distinguish "already in place" from "in a scratch
// dir" without resolving symlinks or touching the working directory.
func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

// writeYAML marshals v and atomically writes it to path (temp-then-rename).
func writeYAML(path string, v any) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// copyFileAtomic reads src and atomically writes its bytes to dst, so a crash
// mid-copy never leaves dst in a partial state.
func copyFileAtomic(dst, src string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read source %s: %w", src, err)
	}
	return writeFileAtomic(dst, data)
}

// buildPersistedEvalRun translates a completed harness.EvalRun into the
// eval-run.yaml shape. A nil Verify leaves the verify block zero-valued —
// present in the YAML with zero fields — rather than omitting the key, so the
// schema stays stable.
func buildPersistedEvalRun(run harness.EvalRun) PersistedEvalRun {
	p := PersistedEvalRun{
		RunID:      run.RunID,
		BaseCommit: run.BaseCommit,
		TaskID:     run.Spec.TaskID,
		Language:   string(run.Spec.Language),
		Difficulty: string(run.Spec.Difficulty),
		Agent:      buildAgentIdentity(run),
		Score: scoreOutcome{
			Value:         run.Score.Score.Value,
			Band:          run.Score.Score.Band,
			Scored:        run.Score.Score.Scored,
			RubricVersion: run.Score.Score.RubricVersion,
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

// buildAgentIdentity assembles the R9/R10 agent block: runner identity from the
// telemetry, run outcome from the runner result, and the two reproducibility
// digests (prompt delivered to the agent, and the agent's stdout).
func buildAgentIdentity(run harness.EvalRun) agentIdentity {
	t := run.Run.Telemetry
	return agentIdentity{
		Harness:      t.Harness,
		Model:        t.Model,
		SessionID:    t.SessionID,
		Retries:      t.Retries,
		ExitCode:     run.Run.ExitCode,
		Duration:     run.Run.Duration.String(),
		PromptDigest: digest([]byte(run.Spec.Prompt)),
		OutputDigest: digest(run.Run.Stdout),
	}
}

// digest returns the algorithm-prefixed hex SHA-256 of data. A nil slice hashes
// to the well-known empty digest — a stable, non-empty marker.
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return digestPrefix + hex.EncodeToString(sum[:])
}
