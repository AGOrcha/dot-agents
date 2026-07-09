package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// ---- AgentStartError ----------------------------------------------------------

func TestAgentStartError_ErrorAuth(t *testing.T) {
	t.Parallel()
	e := &AgentStartError{Agent: "claude-code", Binary: "claude", Reason: ReasonAuth, ExitCode: 1, Detail: "not logged in"}
	msg := e.Error()
	for _, want := range []string{"auth/config failure", "isolated HOME", "not logged in", "docs/EVAL_HARNESS.md"} {
		if !strings.Contains(msg, want) {
			t.Errorf("auth message %q missing %q", msg, want)
		}
	}
}

func TestAgentStartError_ErrorUnavailable(t *testing.T) {
	t.Parallel()
	e := &AgentStartError{Agent: "codex", Binary: "codex", Reason: ReasonUnavailable}
	if msg := e.Error(); !strings.Contains(msg, "not found on PATH") || !strings.Contains(msg, "codex") {
		t.Errorf("unavailable message %q missing expected text", msg)
	}
}

func TestAgentStartError_Unwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	e := &AgentStartError{Reason: ReasonUnavailable, Cause: cause}
	if !errors.Is(e, cause) {
		t.Error("errors.Is should reach the wrapped cause")
	}
}

// ---- classifyExecError --------------------------------------------------------

func TestClassifyExecError(t *testing.T) {
	t.Parallel()
	notFound := fmt.Errorf("exec claude: %w", exec.ErrNotFound)
	cases := []struct {
		name  string
		cause error
		want  bool
	}{
		{"nil cause", nil, false},
		{"other error", errors.New("some launch fault"), false},
		{"not found", notFound, true},
	}
	for _, tc := range cases {
		got := classifyExecError("claude-code", "claude", tc.cause)
		if (got != nil) != tc.want {
			t.Errorf("%s: got %v, want non-nil=%v", tc.name, got, tc.want)
		}
	}
	// The not-found classification must preserve the cause chain.
	if se := classifyExecError("claude-code", "claude", notFound); se == nil || se.Reason != ReasonUnavailable || !errors.Is(se, exec.ErrNotFound) {
		t.Fatalf("not-found: want ReasonUnavailable unwrapping to ErrNotFound, got %+v", se)
	}
}

// ---- adapter integration: missing-CLI classification --------------------------
//
// The auth/config classification is gated on "no solution produced" and lives in
// internal/eval/harness (a completed run whose working tree changed is scored,
// never auth-aborted); these adapter tests cover only the missing-CLI launch
// fault, which is a pure exec concern the runner owns.

// notFoundErr is an exec failure that unwraps to exec.ErrNotFound, the shape a
// real missing-CLI launch produces.
func notFoundErr() error { return fmt.Errorf("exec: %w", exec.ErrNotFound) }

// assertUnavailable runs r and asserts Run aborted with an unavailable
// *AgentStartError (the adapter classified a missing CLI).
func assertUnavailable(t *testing.T, r Runner) {
	t.Helper()
	_, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	var se *AgentStartError
	if !errors.As(err, &se) || se.Reason != ReasonUnavailable {
		t.Fatalf("want unavailable AgentStartError, got %T: %v", err, err)
	}
}

func TestClaudeRunner_MissingCLIClassified(t *testing.T) {
	t.Parallel()
	assertUnavailable(t, &claudeRunner{run: fixedCmdFn(nil, nil, 0, notFoundErr()), scan: emptyScan})
}

func TestCodexRunner_MissingCLIClassified(t *testing.T) {
	t.Parallel()
	assertUnavailable(t, &codexRunner{run: fixedCmdFn(nil, nil, 0, notFoundErr())})
}

func TestCopilotRunner_MissingCLIClassified(t *testing.T) {
	t.Parallel()
	assertUnavailable(t, &copilotRunner{run: fixedCmdFn(nil, nil, 0, notFoundErr()), scan: emptyScan})
}
