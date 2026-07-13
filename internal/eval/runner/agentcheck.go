package runner

import (
	"errors"
	"fmt"
	"os/exec"
)

// AgentStartReason classifies why an agent invocation did not yield a scorable
// run.
type AgentStartReason string

const (
	// ReasonUnavailable: the agent CLI could not be found on PATH.
	ReasonUnavailable AgentStartReason = "unavailable"
	// ReasonAuth: the agent CLI ran but exited with an authentication or
	// configuration failure — the expected outcome when the eval sandbox runs it
	// under an isolated HOME that carries no credentials.
	ReasonAuth AgentStartReason = "auth"
)

// AgentStartError signals that an agent invocation did not produce a scorable
// run: the CLI was absent (ReasonUnavailable) or it failed to
// authenticate/configure under the sandbox's isolated HOME (ReasonAuth). It is
// DISTINCT from a completed agent run that produced a wrong solution — a
// non-zero exit with no auth signature stays a scorable Result. The harness
// aborts the run on this error with a clear message, so an environment/credential
// problem is never scored as poor model quality (dogfood #10). Match it with
// errors.As(err, *AgentStartError).
type AgentStartError struct {
	// Agent is the adapter/harness identity (claude-code, codex, gh-copilot).
	Agent string
	// Binary is the CLI that was invoked or looked up.
	Binary string
	// Reason is why the agent did not run.
	Reason AgentStartReason
	// ExitCode is the CLI's exit code for the auth case; 0 when unavailable.
	ExitCode int
	// Detail is the auth signature that matched (auth case); empty otherwise.
	Detail string
	// Cause is the underlying exec error (unavailable case); nil otherwise.
	Cause error
}

// Error implements error with an actionable, operator-facing message that spells
// out the isolated-HOME cause for the auth case.
func (e *AgentStartError) Error() string {
	if e.Reason == ReasonAuth {
		return fmt.Sprintf(
			"runner: %s agent auth/config failure under isolated HOME (exit %d, matched %q) — the eval sandbox runs the agent under a credential-free HOME; authenticate the CLI or see docs/EVAL_HARNESS.md",
			e.Agent, e.ExitCode, e.Detail)
	}
	return fmt.Sprintf(
		"runner: %s agent CLI %q not found on PATH — install it or choose an installed --agent",
		e.Agent, e.Binary)
}

// Unwrap exposes the underlying exec error so errors.Is/As traversal reaches it
// (the unavailable case wraps the original exec failure).
func (e *AgentStartError) Unwrap() error { return e.Cause }

// classifyExecError maps an exec-seam failure to an *AgentStartError only when it
// is a genuine "binary not found" on PATH; every other exec error (a real launch
// fault) returns nil so the caller wraps it as-is. The returned error unwraps to
// cause, so existing errors.Is checks against the original error keep passing.
//
// The complementary auth/config classification (a CLI that launched but could
// not authenticate under the sandbox's isolated HOME) does NOT live here: it
// requires evidence that the agent produced no solution — a working-tree fact
// only the harness holds — so it is gated and decided in
// internal/eval/harness (see the harness's agent auth detection). Detecting a
// missing binary, by contrast, is a pure exec concern with no such gate.
func classifyExecError(agent, binary string, cause error) *AgentStartError {
	if cause == nil || !errors.Is(cause, exec.ErrNotFound) {
		return nil
	}
	return &AgentStartError{
		Agent:  agent,
		Binary: binary,
		Reason: ReasonUnavailable,
		Cause:  cause,
	}
}
