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

// iterLogFileRE matches canonical iteration-log file names (iter-<digits>.yaml)
// strictly. Anything else in the iter-log dir is ignored — including the R1
// per-iteration score sidecars (iter-N.score.yaml) which would otherwise look
// like high-N entries to a naive max-scan.
var iterLogFileRE = regexp.MustCompile(`^iter-(\d+)\.yaml$`)

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
	entries, err := os.ReadDir(iterLogDir)
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
	return filepath.Join(projectPath, ".agents", "active", "iteration-log")
}
