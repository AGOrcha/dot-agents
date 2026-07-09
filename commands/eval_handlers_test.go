package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// evalSub returns the named `da eval` subcommand from the wired eval group, so
// the handler tests drive a command whose flags are defined exactly as the CLI
// defines them.
func evalSub(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, c := range rootEvalCmd().Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("eval subcommand %q not found", name)
	return nil
}

// runEvalGen delegates to eval.RunGen, which validates the language before it
// touches the knowledge graph — so a bare invocation surfaces the missing
// --language error without any I/O, covering the handler line.
func TestRunEvalGenSurfacesDelegateError(t *testing.T) {
	cmd := evalSub(t, "gen")
	cmd.SetOut(&bytes.Buffer{})
	if err := runEvalGen(cmd, nil); err == nil {
		t.Fatal("runEvalGen with no --language should return the delegate's error")
	}
}

// runEvalRun delegates to eval.RunEval, which validates the language before it
// provisions the sandbox or opens the graph — so a bare invocation surfaces the
// missing --language error, covering the handler line and its Flags.JSON read.
func TestRunEvalRunSurfacesDelegateError(t *testing.T) {
	cmd := evalSub(t, "run")
	cmd.SetOut(&bytes.Buffer{})
	if err := runEvalRun(cmd, nil); err == nil {
		t.Fatal("runEvalRun with no --language should return the delegate's error")
	}
}

// runEvalLs threads the global --json flag into eval.RunLs: against an empty
// eval root the listing succeeds, and with Flags.JSON set it must render the
// JSON envelope ("[]"), proving the handler wires the flag through.
func TestRunEvalLsThreadsJSONFlag(t *testing.T) {
	cmd := evalSub(t, "ls")
	if err := cmd.Flags().Set("repo-dir", t.TempDir()); err != nil {
		t.Fatalf("set repo-dir: %v", err)
	}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	prev := Flags.JSON
	Flags.JSON = true
	t.Cleanup(func() { Flags.JSON = prev })

	if err := runEvalLs(cmd, nil); err != nil {
		t.Fatalf("runEvalLs on an empty root: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("runEvalLs did not thread --json (want empty JSON array), got %q", buf.String())
	}
}

// The text path of the ls handler (Flags.JSON unset) renders the friendly
// empty-state notice.
func TestRunEvalLsTextEmptyRoot(t *testing.T) {
	cmd := evalSub(t, "ls")
	if err := cmd.Flags().Set("repo-dir", t.TempDir()); err != nil {
		t.Fatalf("set repo-dir: %v", err)
	}
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	prev := Flags.JSON
	Flags.JSON = false
	t.Cleanup(func() { Flags.JSON = prev })

	if err := runEvalLs(cmd, nil); err != nil {
		t.Fatalf("runEvalLs on an empty root: %v", err)
	}
	if !strings.Contains(buf.String(), "no runs found") {
		t.Errorf("runEvalLs text output missing empty-state notice: %q", buf.String())
	}
}
