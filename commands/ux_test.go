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
