// Package store persists the four sidecar artifacts of a completed eval run to
// the canonical layout under <root>/.agents/eval/runs/<run-id>/. It is the
// single write path the R2 dashboard reads; the layout it produces is the
// stable contract every downstream consumer depends on.
//
// WriteEvalRun is transactional: the whole run directory is assembled in a
// hidden staging sibling and moved into place with a single atomic rename, so a
// crash or a mid-write failure never leaves a partial run directory visible at
// the canonical path.
package store

import (
	"crypto/rand"
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
// atomic-write and rename error paths are coverable without platform-specific
// filesystem tricks.
var (
	mkdirAll            = fsops.MkdirAll
	writeFileAtomic     = fsops.WriteFileAtomic
	renameDir           = fsops.Rename
	removeAll           = fsops.RemoveAll
	statPath            = os.Stat
	writeIterationScore = scoring.WriteIterationScore
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
	// ErrEmptyRecordPath is returned when the run score has no record path.
	// The score record path is written by scoringbridge.ScoreRun; its absence
	// means the scoring stage did not complete.
	ErrEmptyRecordPath = errors.New("store: score record path is required")
	// ErrRunExists is returned when the canonical run dir already exists. Eval
	// runs are write-once — a run id names exactly one run (the sandbox reserves
	// ids with an O_EXCL claim, and R10 reproducibility means a re-run gets a
	// NEW id) — so WriteEvalRun refuses to overwrite a persisted run rather than
	// risk erasing it.
	ErrRunExists = errors.New("store: run already persisted")
)

// Result reports the persisted artifact paths produced by WriteEvalRun. Every
// path names the committed canonical location, not the staging location.
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

// WriteEvalRun transactionally persists the four sidecar artifacts of a
// completed eval run to:
//
//	<root>/.agents/eval/runs/<run.RunID>/
//	  taskspec.yaml                    — the eval.TaskSpec
//	  eval-run.yaml                    — the run aggregate (identity + R9/R10 metadata)
//	  iteration-log/iter-1.yaml        — R1-shaped iteration record
//	  iteration-log/iter-1.score.yaml  — score sidecar
//
// The whole directory is built in a hidden staging sibling under the runs root
// and published with a single atomic rename, so a mid-write failure leaves NO
// partial directory at the canonical path (the staging dir is removed on any
// error). Eval runs are WRITE-ONCE: if the canonical run dir already exists,
// WriteEvalRun returns ErrRunExists and leaves the persisted run untouched
// rather than deleting it — a run id names exactly one run, so there is no
// legitimate overwrite, and refusing eliminates any erase hazard. Every file
// write is itself atomic (temp-then-rename via fsops.WriteFileAtomic). The
// iter-1.yaml bytes are copied byte-for-byte from run.Score.RecordPath — an
// input produced by the scoring stage in its own working location, distinct
// from this canonical dir — preserving the emitRecord YAML schema that
// scoring.LoadIterationLog expects; the score sidecar is re-persisted via the
// production scoring.WriteIterationScore.
func WriteEvalRun(run harness.EvalRun, root string) (res Result, err error) {
	if err := validateRun(run, root); err != nil {
		return Result{}, err
	}

	runs := runsRoot(root)
	if err := mkdirAll(runs, 0o755); err != nil {
		return Result{}, fmt.Errorf("store: create runs root: %w", err)
	}

	staging, err := makeStagingDir(runs, run.RunID)
	if err != nil {
		return Result{}, err
	}
	// Remove the staging dir unless it is successfully committed into place, so
	// a failed write never leaks a hidden partial directory.
	committed := false
	defer func() {
		if !committed {
			_ = removeAll(staging)
		}
	}()

	if err := writeSidecars(staging, run); err != nil {
		return Result{}, err
	}

	finalDir := RunDir(root, run.RunID)
	if err := commitStaging(staging, finalDir); err != nil {
		return Result{}, err
	}
	committed = true

	return newResult(finalDir, run.Score.Score.Iteration), nil
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

// makeStagingDir creates a hidden, uniquely-named staging directory as a
// sibling of the eventual run dir (same filesystem, so the commit rename is
// atomic). The leading dot keeps it out of the dashboard's run globs, and the
// random suffix keeps concurrent writers of the same run ID from colliding.
func makeStagingDir(runs, runID string) (string, error) {
	tok, err := randToken()
	if err != nil {
		return "", fmt.Errorf("store: generate staging token: %w", err)
	}
	staging := filepath.Join(runs, fmt.Sprintf(".%s.staging-%s", runID, tok))
	if err := mkdirAll(staging, 0o755); err != nil {
		return "", fmt.Errorf("store: create staging dir: %w", err)
	}
	return staging, nil
}

// writeSidecars lays down all four artifacts inside dir (a staging directory).
// It performs no rename into the canonical location — the caller commits.
func writeSidecars(dir string, run harness.EvalRun) error {
	iterDir := filepath.Join(dir, iterLogDirName)
	if err := mkdirAll(iterDir, 0o755); err != nil {
		return fmt.Errorf("store: create iteration-log dir: %w", err)
	}
	if err := writeYAML(filepath.Join(dir, taskspecYAML), run.Spec); err != nil {
		return fmt.Errorf("store: taskspec: %w", err)
	}
	if err := writeYAML(filepath.Join(dir, evalRunYAML), buildPersistedEvalRun(run)); err != nil {
		return fmt.Errorf("store: eval-run: %w", err)
	}
	if err := copyFileAtomic(filepath.Join(iterDir, iterRecordYAML), run.Score.RecordPath); err != nil {
		return fmt.Errorf("store: iter record: %w", err)
	}
	if _, err := writeIterationScore(iterDir, run.Score.Score); err != nil {
		return fmt.Errorf("store: persist score: %w", err)
	}
	return nil
}

// commitStaging publishes the fully-built staging directory at final with a
// single atomic rename. Eval runs are WRITE-ONCE, so an existing canonical dir
// is never deleted or overwritten: commitStaging refuses with ErrRunExists,
// which eliminates the delete-then-rename erase hazard (a mid-commit failure can
// never leave a persisted run erased with nothing to restore). When the target
// is absent the lone rename is atomic — the run appears all-at-once or not at
// all. os.Rename also refuses a non-empty existing target at the syscall level,
// so the stat pre-check is the friendly typed error, not the only guard.
func commitStaging(staging, final string) error {
	switch _, err := statPath(final); {
	case err == nil:
		return fmt.Errorf("store: run %q: %w", filepath.Base(final), ErrRunExists)
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("store: stat run dir: %w", err)
	}
	if err := renameDir(staging, final); err != nil {
		return fmt.Errorf("store: commit run dir: %w", err)
	}
	return nil
}

// writeYAML marshals v and atomically writes it to path.
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

// newResult derives the committed artifact paths from the canonical run dir.
func newResult(runDir string, iteration int) Result {
	iterDir := filepath.Join(runDir, iterLogDirName)
	return Result{
		RunDir:       runDir,
		TaskspecPath: filepath.Join(runDir, taskspecYAML),
		EvalRunPath:  filepath.Join(runDir, evalRunYAML),
		IterLogDir:   iterDir,
		RecordPath:   filepath.Join(iterDir, iterRecordYAML),
		ScorePath:    scoring.IterationScorePath(iterDir, iteration),
	}
}

// randToken returns 16 hex characters of cryptographic randomness for the
// staging directory suffix.
func randToken() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
