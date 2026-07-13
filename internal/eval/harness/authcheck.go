package harness

import (
	"strings"

	"github.com/go-git/go-git/v6"

	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
)

// authStartupSignatures are lowercased substrings that are unambiguously an agent
// CLI's OWN auth/config startup failure — the CLI could not authenticate under
// the sandbox's isolated, credential-free HOME. They are deliberately NOT generic
// auth vocabulary: bare "unauthorized" / "authentication failed" / "401" are
// excluded because they legitimately appear in a real solution's diff or test
// output for an auth-related task. Matching one of these is still only a
// necessary condition — the decision is additionally gated on the agent having
// produced NO solution (see detectAuthFailure), so a run that actually wrote code
// is never auth-aborted regardless of its output text.
var authStartupSignatures = []string{
	"not logged in",
	"please log in",
	"please run /login",
	"run /login",
	"claude login",
	"codex login",
	"gh auth login",
	"invalid api key",
	"no api key",
	"missing api key",
	"api key not set",
	"api key is not set",
}

// authStartupSignature reports whether the CLI's own stderr carries an
// auth-startup signature, returning the matched phrase. Only stderr is scanned:
// an agent's SOLUTION and task output flow to stdout (and to the working tree),
// so scanning stderr keeps solution content — a common source of auth vocabulary
// — out of the match entirely.
func authStartupSignature(stderr []byte) (string, bool) {
	haystack := strings.ToLower(string(stderr))
	for _, sig := range authStartupSignatures {
		if strings.Contains(haystack, sig) {
			return sig, true
		}
	}
	return "", false
}

// detectWorktreeChanges reports whether the sandbox working tree has any change —
// a tracked edit or an untracked file — i.e. the agent produced a solution. On
// ANY uncertainty (the path is not a resolvable git worktree, or Status errors)
// it returns true: the fail-safe direction, so a run is scored rather than
// wrongly aborted as an auth failure. It is the default for Harness.producedSolution.
func detectWorktreeChanges(workdir string) bool {
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return true
	}
	wt, err := repo.Worktree()
	if err != nil {
		return true
	}
	st, err := wt.Status()
	if err != nil {
		return true
	}
	return !st.IsClean()
}

// detectAuthFailure classifies a COMPLETED agent run (the runner returned no
// error) as an auth/config failure ONLY when all three hold: the CLI exited
// non-zero, its stderr carries an auth-startup signature, AND the agent produced
// no solution in the working tree. If the agent produced a solution, it ran —
// the run is scored as an ordinary (possibly failing) outcome, never auth-aborted,
// so a legitimate wrong solution about an auth topic is never misread as the CLI
// failing to authenticate. Returns nil when the run should be scored normally.
func (h *Harness) detectAuthFailure(instance *sandbox.Instance, result runner.Result) *runner.AgentStartError {
	if result.ExitCode == 0 {
		return nil
	}
	sig, ok := authStartupSignature(result.Stderr)
	if !ok {
		return nil
	}
	if h.producedSolution(instance.Workdir) {
		return nil
	}
	return &runner.AgentStartError{
		Agent:    result.Telemetry.Harness,
		Reason:   runner.ReasonAuth,
		ExitCode: result.ExitCode,
		Detail:   sig,
	}
}
