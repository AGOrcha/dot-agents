package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestInstallRejectsUnexpectedPositionalArgsWithUsageHint(t *testing.T) {
	cmd := NewInstallCmd()
	cmd.SetArgs([]string{"extra"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}

	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T", err)
	}
	if !cliErr.ShowUsage {
		t.Fatal("expected usage to be shown")
	}
	if !strings.Contains(cliErr.Error(), "does not accept positional arguments") {
		t.Fatalf("unexpected error: %v", cliErr)
	}
	if got := strings.Join(cliErr.Hints, "\n"); !strings.Contains(got, "dot-agents install --help") {
		t.Fatalf("expected help hint, got %q", got)
	}
}

func TestRenderCommandErrorAddsWorkflowRecoveryHints(t *testing.T) {
	root := &cobra.Command{Use: "dot-agents"}
	root.AddCommand(NewWorkflowCmd())

	var buf bytes.Buffer
	RenderCommandError(&buf, root, []string{"workflow", "status"}, errNoWorkflowProject)

	out := buf.String()
	for _, want := range []string{
		"workflow commands must run inside a project directory",
		"dot-agents add .",
		"dot-agents workflow status --help",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered error missing %q:\n%s", want, out)
		}
	}
}

func TestWorkflowHelpIncludesExamples(t *testing.T) {
	cmd := NewWorkflowCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	if err := cmd.Help(); err != nil {
		t.Fatalf("Help: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"Examples:",
		"dot-agents workflow orient",
		"dot-agents workflow checkpoint",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workflow help missing %q:\n%s", want, out)
		}
	}
}
