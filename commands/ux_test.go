package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRenderCommandError_CLIError_PrintsHints(t *testing.T) {
	root := &cobra.Command{Use: "da"}
	var buf bytes.Buffer

	cliErr := &CLIError{
		Message: "something went wrong",
		Hints:   []string{"Try running da init first.", "Check your config."},
	}

	RenderCommandError(&buf, root, nil, cliErr)
	out := buf.String()

	if !strings.Contains(out, "something went wrong") {
		t.Errorf("expected error message in output, got %q", out)
	}
	if !strings.Contains(out, "Try running da init first.") {
		t.Errorf("expected first hint in output, got %q", out)
	}
	if !strings.Contains(out, "Check your config.") {
		t.Errorf("expected second hint in output, got %q", out)
	}
}

func TestRenderCommandError_PlainError(t *testing.T) {
	root := &cobra.Command{Use: "da"}
	var buf bytes.Buffer

	RenderCommandError(&buf, root, nil, errors.New("plain error"))
	out := buf.String()

	if !strings.Contains(out, "plain error") {
		t.Errorf("expected error message in output, got %q", out)
	}
}

func TestErrorWithHints_ReturnsCLIError(t *testing.T) {
	err := ErrorWithHints("bad input", "hint one", "hint two")
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.Message != "bad input" {
		t.Errorf("Message = %q, want %q", cliErr.Message, "bad input")
	}
	if len(cliErr.Hints) != 2 {
		t.Errorf("Hints = %v, want 2 hints", cliErr.Hints)
	}
}

func TestErrorWithHints_TrimsWhitespace(t *testing.T) {
	err := ErrorWithHints("  spaced  ", "  hint  ")
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if cliErr.Message != "spaced" {
		t.Errorf("Message = %q, want %q", cliErr.Message, "spaced")
	}
}

func TestUsageError_SetsShowUsage(t *testing.T) {
	err := UsageError("wrong args")
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if !cliErr.ShowUsage {
		t.Error("expected ShowUsage = true")
	}
}

func TestConfigureRootCommandUX_AppliesFlagErrorFunc(t *testing.T) {
	root := &cobra.Command{Use: "da"}
	ConfigureRootCommandUX(root)

	// Trigger the flag error func by passing an unknown flag.
	root.SetArgs([]string{"--nonexistent"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error from unknown flag")
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError from flag error func, got %T: %v", err, err)
	}
}

func TestExactArgsWithHints_TooFewArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := ExactArgsWithHints(2, "Pass two args.")
	err := validator(cmd, []string{"one"})
	if err == nil {
		t.Fatal("expected error for wrong arg count")
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if !cliErr.ShowUsage {
		t.Error("expected ShowUsage = true")
	}
}

func TestExactArgsWithHints_CorrectCount(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := ExactArgsWithHints(1)
	if err := validator(cmd, []string{"one"}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestNoArgsWithHints_RejectsArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := NoArgsWithHints("No args allowed.")
	err := validator(cmd, []string{"extra"})
	if err == nil {
		t.Fatal("expected error")
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
}

func TestNoArgsWithHints_AcceptsNoArgs(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := NoArgsWithHints()
	if err := validator(cmd, nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestMaximumNArgsWithHints_RejectsExcess(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := MaximumNArgsWithHints(1, "At most one arg.")
	err := validator(cmd, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaximumNArgsWithHints_AcceptsWithinLimit(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := MaximumNArgsWithHints(2)
	if err := validator(cmd, []string{"a"}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestCLIError_Error_NilReceiver(t *testing.T) {
	var cliErr *CLIError
	if cliErr.Error() != "" {
		t.Errorf("expected empty string from nil CLIError, got %q", cliErr.Error())
	}
}

func TestCLIError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	cliErr := &CLIError{Message: "wrapper", Cause: cause}
	if !errors.Is(cliErr, cause) {
		t.Error("expected Unwrap to return cause")
	}
}

func TestCLIError_Unwrap_NilReceiver(t *testing.T) {
	var cliErr *CLIError
	if cliErr.Unwrap() != nil {
		t.Error("expected nil from Unwrap on nil receiver")
	}
}

func TestRangeArgsWithHints(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	validator := RangeArgsWithHints(1, 2, "Pass 1 or 2 args.")

	// Out of range -> error.
	if err := validator(cmd, nil); err == nil {
		t.Error("expected error for zero args")
	}
	if err := validator(cmd, []string{"a", "b", "c"}); err == nil {
		t.Error("expected error for three args")
	}
	// In range -> no error.
	if err := validator(cmd, []string{"a"}); err != nil {
		t.Errorf("expected no error for one arg, got %v", err)
	}
	if err := validator(cmd, []string{"a", "b"}); err != nil {
		t.Errorf("expected no error for two args, got %v", err)
	}
}

func TestExampleBlock(t *testing.T) {
	got := ExampleBlock("a", "b")
	if got != "a\nb" {
		t.Errorf("ExampleBlock = %q", got)
	}
}

func TestCompactHints_DedupesAndTrims(t *testing.T) {
	hints := []string{"  a  ", "a", "", "  ", "b", "b"}
	got := compactHints(hints)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("compactHints = %v", got)
	}
}

func TestHelpHintForCommand_NilCmd(t *testing.T) {
	got := helpHintForCommand(nil)
	if !strings.Contains(got, "da --help") {
		t.Errorf("expected fallback hint, got %q", got)
	}
}

func TestHelpHintForCommand_PrefixesDaForRoot(t *testing.T) {
	root := &cobra.Command{Use: "status"}
	got := helpHintForCommand(root)
	if !strings.Contains(got, "da status") {
		t.Errorf("expected 'da status' prefix, got %q", got)
	}
}

func TestFindChildCommand(t *testing.T) {
	parent := &cobra.Command{Use: "parent"}
	child := &cobra.Command{Use: "child", Aliases: []string{"c"}}
	parent.AddCommand(child)

	if got := findChildCommand(parent, "child"); got != child {
		t.Errorf("findChildCommand(name) = %v", got)
	}
	if got := findChildCommand(parent, "c"); got != child {
		t.Errorf("findChildCommand(alias) = %v", got)
	}
	if got := findChildCommand(parent, "missing"); got != nil {
		t.Errorf("expected nil for missing, got %v", got)
	}
}

func TestResolveRequestedCommand(t *testing.T) {
	root := &cobra.Command{Use: "da"}
	sub := &cobra.Command{Use: "review"}
	leaf := &cobra.Command{Use: "approve"}
	sub.AddCommand(leaf)
	root.AddCommand(sub)

	// No argv -> root.
	if got := resolveRequestedCommand(root, nil); got != root {
		t.Errorf("expected root, got %v", got)
	}
	// Nil root short-circuits.
	if got := resolveRequestedCommand(nil, []string{"x"}); got != nil {
		t.Errorf("expected nil for nil root, got %v", got)
	}
	// Walks two levels.
	if got := resolveRequestedCommand(root, []string{"review", "approve"}); got != leaf {
		t.Errorf("expected leaf, got %v", got)
	}
	// Stops at first unknown.
	if got := resolveRequestedCommand(root, []string{"review", "bogus"}); got != sub {
		t.Errorf("expected sub for unknown leaf, got %v", got)
	}
	// Stops at flag.
	if got := resolveRequestedCommand(root, []string{"--flag"}); got != root {
		t.Errorf("expected root when arg starts with -, got %v", got)
	}
}

func TestEnrichCLIError_AddsWorkflowProjectHints(t *testing.T) {
	out := classifyCLIError(errNoWorkflowProject, &cobra.Command{Use: "test"})
	joined := strings.Join(out.Hints, " ")
	if !strings.Contains(joined, ".agentsrc.json") {
		t.Errorf("expected workflow project hint, got %v", out.Hints)
	}
}

func TestEnrichCLIError_MessageBasedBranches(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantHintSub string
	}{
		{"manifest_not_found", errors.New("manifest not found"), "da install --generate"},
		{"agents_not_initialized", errors.New("~/.agents/ not initialized"), "da init"},
		{"project_not_found", errors.New("project not found: foo"), "da status"},
		{"invalid_scope", errors.New("invalid scope: nope"), "Supported scopes"},
		{"unknown_pref", errors.New("unknown preference key bar"), "documented configuration keys"},
		{"invalid_task_status", errors.New("invalid task status"), "pending"},
		{"invalid_plan_status", errors.New("invalid plan status"), "draft"},
		{"invalid_verify_status", errors.New("invalid verification status"), "partial"},
		{"not_found_in_source", errors.New("not found in any source"), ".agentsrc.json"},
		{"code_graph_not_built", errors.New("eval: code graph not built at /x/ops/graphstore.db"), "da kg build"},
		{"unknown_command", errors.New("unknown command"), "da --help"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := classifyCLIError(tc.err, nil)
			if !strings.Contains(strings.Join(out.Hints, " "), tc.wantHintSub) {
				t.Errorf("missing %q hint in %v", tc.wantHintSub, out.Hints)
			}
		})
	}
}

func TestClassifyCLIError_UnknownCommand_SetsUsage(t *testing.T) {
	out := classifyCLIError(errors.New("unknown command \"foo\""), nil)
	if !out.ShowUsage {
		t.Error("expected ShowUsage = true for unknown command")
	}
}

func TestEnrichCLIError_NilInput(t *testing.T) {
	got := enrichCLIError(nil, nil)
	if got == nil || got.Message != "command failed" {
		t.Errorf("expected fallback CLIError, got %+v", got)
	}
}

func TestRenderCommandError_ShowsUsageBlock(t *testing.T) {
	root := &cobra.Command{Use: "da"}
	sub := &cobra.Command{Use: "sub", Short: "s"}
	root.AddCommand(sub)

	var buf bytes.Buffer
	cliErr := &CLIError{Message: "oops", ShowUsage: true}
	RenderCommandError(&buf, root, []string{"sub"}, cliErr)
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected usage block in output: %s", buf.String())
	}
}

func TestRenderCommandError_RendersHintLabel(t *testing.T) {
	root := &cobra.Command{Use: "da"}
	var buf bytes.Buffer
	cliErr := &CLIError{Message: "oops", Hints: []string{"only hint"}}
	RenderCommandError(&buf, root, nil, cliErr)
	out := buf.String()
	if !strings.Contains(out, "only hint") {
		t.Errorf("expected hint text in output, got %s", out)
	}
	// Either label is acceptable depending on how many hints survive enrichCLIError.
	if !strings.Contains(out, "Hint") {
		t.Errorf("expected Hint(s) label, got %s", out)
	}
}

func TestErrNoWorkflowProject_Sentinel(t *testing.T) {
	// errors.Is must thread through the sentinel even after wrapping.
	wrapper := &CLIError{Message: "x", Cause: errNoWorkflowProject}
	if !errors.Is(wrapper, errNoWorkflowProject) {
		t.Error("expected errors.Is to identify the sentinel through Unwrap")
	}
}
