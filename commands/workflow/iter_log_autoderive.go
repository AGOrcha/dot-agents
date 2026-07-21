// iter_log_autoderive.go provides the thin orchestration helpers that the
// workflow-client-commands close-task uses to invoke `da workflow checkpoint
// --log-to-iter N --log-to-iter-role <role>` without operator-supplied N.
//
// The actual iter-log writer (loadOrInitIterLogEntry → applyIterLogRole →
// writeIterLogEntry, plus the agent-block / git-diff / token-telemetry
// derivers) already exists in iter_log.go — this file does not duplicate
// any of that. It only picks (N, role) so callers do not have to.
package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// iterationLogRelDir is the repo-relative (forward-slash) iteration-log
// directory. IterationLogDir joins it beneath a project root; the commit
// include-path helper needs the relative form because DerivePathSet matches
// --include entries against git-status paths, which are repo-relative and
// forward-slashed.
const iterationLogRelDir = ".agents/active/iteration-log"

// iterLogFileRE matches canonical iteration-log file names (iter-<digits>.yaml)
// strictly. Anything else in the iter-log dir is ignored — including the R1
// per-iteration score sidecars (iter-N.score.yaml) which would otherwise look
// like high-N entries to a naive max-scan.
var iterLogFileRE = regexp.MustCompile(`^iter-(\d+)\.yaml$`)

// nextIterReadDir is the seam tests use to assert the read-error branch.
// Default is os.ReadDir; tests rebind to a stub returning a synthetic
// error (Windows's os.ReadDir does not reliably error on a non-directory
// path, so a portable test cannot trigger the error branch by fixturing
// the filesystem alone).
var nextIterReadDir = os.ReadDir

// NextIterationNumber returns the iteration number close-task should pass to
// `workflow checkpoint --log-to-iter`. If the iter-log directory has no
// existing iter-N.yaml entries (or does not exist yet), the next number is 1
// — the schema's enforced minimum, matching the existing --log-to-iter
// validation that requires N >= 1. Otherwise it returns max(existing) + 1
// so each close opens a fresh iteration entry.
//
// A directory that exists but is empty is not an error; that is the
// expected state for a brand-new project's first close. Only filesystem
// errors (permission denied, parent missing, etc.) surface as errors.
func NextIterationNumber(iterLogDir string) (int, error) {
	entries, err := nextIterReadDir(iterLogDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, fmt.Errorf("scoring next iteration: read %s: %w", iterLogDir, err)
	}
	max := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := iterLogFileRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			// Regex matched \d+ so Atoi can only fail on integer overflow —
			// treat as a corrupt name and skip, do not fail the whole scan.
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

// DefaultIterationRole returns the role close-task uses by default when no
// stronger context (a recent verifier record, a review-gate handoff) tells
// us to write a verifier or review entry instead.
//
// Indirected through a function rather than a const so a future close-task
// enhancement (read the verify log; if the last record is a verifier with
// matching scope, write the verifier block) can land here without touching
// every caller.
func DefaultIterationRole() string {
	return "impl"
}

// IterationLogDir returns the canonical iter-log directory for a project.
// One place to compute it so close-task, the auto-derivers, and the existing
// runWorkflowCheckpointLogToIter all agree on the path.
func IterationLogDir(projectPath string) string {
	return filepath.Join(projectPath, iterationLogRelDir)
}

// iterRecordPath returns the canonical iter-N.yaml record path for a project.
// Centralized (with hookOutcomeSidecarPath / scoreSidecarPath) so the
// "iter-%d.yaml" name lives in exactly one place.
func iterRecordPath(projectPath string, n int) string {
	return filepath.Join(IterationLogDir(projectPath), fmt.Sprintf("iter-%d.yaml", n))
}

// scoreSidecarPath returns the canonical per-iteration score sidecar path
// (iter-N.score.yaml) for a project — the R1 sibling of hookOutcomeSidecarPath.
func scoreSidecarPath(projectPath string, n int) string {
	return filepath.Join(IterationLogDir(projectPath), fmt.Sprintf("iter-%d.score.yaml", n))
}

// currentIterationIncludePaths returns the repo-relative (forward-slash),
// project-relative paths of the CURRENT iteration's iter-log artifacts that
// exist on disk: iter-N.yaml plus its optional hook-outcomes and score
// sidecars. N is resolved exactly as the iter-log readers do — the highest
// existing iter-N.yaml under the iteration-log dir (resolveActiveIterationN).
//
// These artifacts live OUTSIDE the auto-managed commit roots (they sit under
// .agents/active/iteration-log/, not .agents/workflow|history/), so
// DerivePathSet only stages them when a caller names them explicitly. Threading
// the returned paths through iterationCloseCommitWithIncludes is what folds the
// iteration record into the same workflow-state commit as close / advance /
// start-task.
//
// Best-effort: a resolution failure or "no active iteration" yields nil, so the
// caller behaves exactly as it did before this fix. Absent sidecars are skipped.
func currentIterationIncludePaths(projectPath string) []string {
	n, active, err := resolveActiveIterationN(stdHookOutcomeDeps{}, projectPath)
	if err != nil || !active {
		return nil
	}
	candidates := []string{
		iterRecordPath(projectPath, n),
		hookOutcomeSidecarPath(projectPath, n),
		scoreSidecarPath(projectPath, n),
	}
	var includes []string
	for _, abs := range candidates {
		if _, statErr := os.Stat(abs); statErr != nil {
			continue
		}
		includes = append(includes, filepath.ToSlash(filepath.Join(iterationLogRelDir, filepath.Base(abs))))
	}
	return includes
}
