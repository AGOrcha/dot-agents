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

// ---- classifyAuthFailure ------------------------------------------------------

func TestClassifyAuthFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		code   int
		stdout string
		stderr string
		want   bool
	}{
		{"clean exit", 0, "Please log in", "", false},
		{"non-auth failure", 1, "compile error: undefined Foo", "", false},
		{"login on stderr", 1, "", "Error: Not logged in. Please run /login", true},
		{"api key on stdout", 2, "set ANTHROPIC_API_KEY to continue", "", true},
	}
	for _, tc := range cases {
		got := classifyAuthFailure("claude-code", "claude", tc.code, []byte(tc.stdout), []byte(tc.stderr))
		if (got != nil) != tc.want {
			t.Errorf("%s: got %v, want non-nil=%v", tc.name, got, tc.want)
		}
		if got != nil && got.Reason != ReasonAuth {
			t.Errorf("%s: reason = %q, want %q", tc.name, got.Reason, ReasonAuth)
		}
	}
}

// ---- adapter integration ------------------------------------------------------

// notFoundErr is an exec failure that unwraps to exec.ErrNotFound, the shape a
// real missing-CLI launch produces.
func notFoundErr() error { return fmt.Errorf("exec: %w", exec.ErrNotFound) }

// assertStartReason runs r and asserts Run aborted with an *AgentStartError of
// the given reason (the adapter classified a missing CLI or an auth failure).
func assertStartReason(t *testing.T, r Runner, want AgentStartReason) {
	t.Helper()
	_, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	var se *AgentStartError
	if !errors.As(err, &se) || se.Reason != want {
		t.Fatalf("want %s AgentStartError, got %T: %v", want, err, err)
	}
}

func TestClaudeRunner_AuthFailureClassified(t *testing.T) {
	t.Parallel()
	assertStartReason(t, &claudeRunner{run: fixedCmdFn([]byte("Error: Not logged in"), nil, 1, nil), scan: emptyScan}, ReasonAuth)
}

func TestClaudeRunner_MissingCLIClassified(t *testing.T) {
	t.Parallel()
	assertStartReason(t, &claudeRunner{run: fixedCmdFn(nil, nil, 0, notFoundErr()), scan: emptyScan}, ReasonUnavailable)
}

func TestCodexRunner_AuthFailureClassified(t *testing.T) {
	t.Parallel()
	assertStartReason(t, &codexRunner{run: fixedCmdFn(nil, []byte("unauthorized"), 1, nil)}, ReasonAuth)
}

func TestCodexRunner_MissingCLIClassified(t *testing.T) {
	t.Parallel()
	assertStartReason(t, &codexRunner{run: fixedCmdFn(nil, nil, 0, notFoundErr())}, ReasonUnavailable)
}

func TestCopilotRunner_AuthFailureClassified(t *testing.T) {
	t.Parallel()
	assertStartReason(t, &copilotRunner{run: fixedCmdFn([]byte("Please run /login"), nil, 1, nil), scan: emptyScan}, ReasonAuth)
}

func TestCopilotRunner_MissingCLIClassified(t *testing.T) {
	t.Parallel()
	assertStartReason(t, &copilotRunner{run: fixedCmdFn(nil, nil, 0, notFoundErr()), scan: emptyScan}, ReasonUnavailable)
}
