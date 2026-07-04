package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// wantTestCmd is validSpec's test command, joined as the preview renders it.
const wantTestCmd = "go test ./..."

// ---- dry-run helpers --------------------------------------------------------

// runDryRun drives runEvalCommand in dry-run mode against a fresh repo root and
// returns the captured output plus that root. A dry-run must never error on a
// resolvable task, so a failure fails the test. The caller asserts the root
// stayed empty (assertNoRunDirs) — the whole point of the preview.
func runDryRun(t *testing.T, opts runOptions, asJSON bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	opts.repoDir = root
	var buf bytes.Buffer
	if err := runEvalCommand(context.Background(), &buf, opts, asJSON, true); err != nil {
		t.Fatalf("dry-run should not error: %v", err)
	}
	return buf.String(), root
}

// assertNoRunDirs fails if the eval runs root under root holds any entry. A
// dry-run provisions no sandbox and persists no run dir/sidecar, so the runs
// root must be absent or empty. It also asserts the store's canonical run root
// is untouched — the footgun this fix closes.
func assertNoRunDirs(t *testing.T, root string) {
	t.Helper()
	runsRoot := evalRunsRoot(root)
	entries, err := os.ReadDir(runsRoot)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat runs root %s: %v", runsRoot, err)
	}
	if len(entries) > 0 {
		t.Fatalf("dry-run wrote %d run dir(s) under %s, want 0", len(entries), runsRoot)
	}
}

// assertContainsAll fails for each substring absent from out.
func assertContainsAll(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

// ---- dry-run: preview + strictly side-effect-free ---------------------------

// A --task dry-run previews the resolved task, agent, sandbox, and verification
// commands, and writes nothing — no sandbox, no run dir, no sidecars.
func TestRunDryRunTaskModePreviewsAndWritesNothing(t *testing.T) {
	path := writeSpecFile(t, validSpec())
	out, root := runDryRun(t, runOptions{task: path, adapter: defaultAdapter}, false)

	assertNoRunDirs(t, root)
	assertContainsAll(t, out,
		"dry-run",
		validSpec().TaskID,
		"go/easy",
		"go build ./...",
		wantTestCmd,
		"would be provisioned",
		"no run dir was written",
	)
}

// The global -n/--dry-run flag (threaded by the root as RunEval's dryRun arg,
// modelled here through the entry point) drives the same preview-and-stop path
// end-to-end through the assembled cobra command.
func TestRunEvalDryRunViaEntryPreviewsAndWritesNothing(t *testing.T) {
	path := writeSpecFile(t, validSpec())
	root := t.TempDir()
	cmd := newRunCmd(func(c *cobra.Command, _ []string) error { return RunEval(c, false, true) })
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--task", path, "--repo-dir", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dry-run Execute: %v", err)
	}
	assertNoRunDirs(t, root)
	assertContainsAll(t, buf.String(), "dry-run", "Preview only")
}

// A --language dry-run generates the TaskSpec from the (fixture) KG and previews
// it, surfacing the chosen --agent adapter, while still writing no run dir.
func TestRunDryRunLanguageModeGeneratesPreview(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	out, root := runDryRun(t, runOptions{language: "go", adapter: "codex"}, false)

	assertNoRunDirs(t, root)
	assertContainsAll(t, out, "codex", "go/")
}

// The --json preview emits the structured runPreview envelope: dry_run=true, the
// resolved task, the agent, the sandbox that would be provisioned (its runs root
// under the repo root), and the verification commands.
func TestRunDryRunJSONPreviewShape(t *testing.T) {
	path := writeSpecFile(t, validSpec())
	out, root := runDryRun(t, runOptions{task: path, adapter: defaultAdapter}, true)

	assertNoRunDirs(t, root)
	p := decodePreview(t, out)
	if !p.DryRun {
		t.Error("preview JSON must set dry_run=true")
	}
	if p.Task.Language != "go" || p.Task.TaskID != validSpec().TaskID {
		t.Errorf("preview task = %+v", p.Task)
	}
	if p.Agent != defaultAdapter {
		t.Errorf("preview agent = %q, want %q", p.Agent, defaultAdapter)
	}
	if p.Sandbox.Type != sandboxTypeWorktree || !strings.Contains(p.Sandbox.RunsRoot, root) {
		t.Errorf("preview sandbox = %+v (root %s)", p.Sandbox, root)
	}
	if strings.Join(p.Verification.TestCmd, " ") != wantTestCmd {
		t.Errorf("preview verification test_cmd = %v", p.Verification.TestCmd)
	}
}

// build_cmd is optional in a TaskSpec; when the resolved task declares none the
// preview renders the build phase as "(none)" rather than a bare line.
func TestRunDryRunNoBuildCmdRendersNone(t *testing.T) {
	spec := validSpec()
	spec.Verification.BuildCmd = nil
	path := writeSpecFile(t, spec)
	out, root := runDryRun(t, runOptions{task: path, adapter: defaultAdapter}, false)

	assertNoRunDirs(t, root)
	assertContainsAll(t, out, "build:  (none)", wantTestCmd)
}

// decodePreview unmarshals the --json preview envelope.
func decodePreview(t *testing.T, out string) runPreview {
	t.Helper()
	var p runPreview
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("preview JSON did not parse: %v\n%s", err, out)
	}
	return p
}

// ---- dry-run: resolveTaskSpec error branches --------------------------------

// A dry-run with neither --language nor --task fails in generator resolution,
// before any preview is rendered (and, as ever, before any write).
func TestRunDryRunInvalidLanguageErrors(t *testing.T) {
	err := runEvalCommand(context.Background(), &bytes.Buffer{}, runOptions{}, false, true)
	if err == nil {
		t.Fatal("dry-run with no --language/--task should error before rendering")
	}
}

// With no generator profiles registered the language lookup misses, so
// resolveTaskSpec surfaces a missing-generator error rather than panicking.
func TestResolveTaskSpecNoGenerator(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	swapLanguageProfiles(t, nil)
	if _, err := resolveTaskSpec(context.Background(), runOptions{language: "go"}); err == nil {
		t.Fatal("empty profiles should surface a missing-generator error")
	}
}

// An unknown --template makes the generator fail; resolveTaskSpec wraps it as a
// generate error.
func TestResolveTaskSpecGenerateError(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	_, err := resolveTaskSpec(context.Background(), runOptions{language: "go", template: "no-such-template"})
	if err == nil {
		t.Fatal("an unknown --template should surface a generate error")
	}
}
