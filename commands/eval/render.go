package eval

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/harness"
	"github.com/AGOrcha/dot-agents/internal/eval/store"
)

// sandboxTypeWorktree names the sandbox a live run would provision: an isolated
// git worktree under the eval runs root (see internal/eval/sandbox).
const sandboxTypeWorktree = "git-worktree"

// runPreview is the `da eval run --dry-run` plan: the resolved task, the agent
// that would run, the sandbox that would be provisioned, and the verification
// that would execute — the shape of what a live run WOULD do, with nothing done.
// It is also the --json preview envelope (dry_run is always true here so a
// consumer can tell a preview object from a completed-run object).
type runPreview struct {
	DryRun       bool               `json:"dry_run"`
	Task         previewTaskJSON    `json:"task"`
	Agent        string             `json:"agent"`
	Sandbox      previewSandboxJSON `json:"sandbox"`
	Verification previewVerifyJSON  `json:"verification"`
}

type previewTaskJSON struct {
	TaskID     string `json:"task_id"`
	Language   string `json:"language"`
	Difficulty string `json:"difficulty"`
	TemplateID string `json:"template_id,omitempty"`
	Kind       string `json:"kind,omitempty"`
}

type previewSandboxJSON struct {
	Type     string `json:"type"`
	RunsRoot string `json:"runs_root"`
}

type previewVerifyJSON struct {
	BuildCmd       []string `json:"build_cmd,omitempty"`
	TestCmd        []string `json:"test_cmd,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// buildRunPreview projects the resolved spec + run options into the preview
// envelope. The sandbox runs root is the same <root>/.agents/eval/runs a live
// run would provision under (via evalRunsRoot), so the preview names the exact
// location it would otherwise create.
func buildRunPreview(spec *evalcore.TaskSpec, opts runOptions, root string) runPreview {
	return runPreview{
		DryRun: true,
		Task: previewTaskJSON{
			TaskID:     spec.TaskID,
			Language:   string(spec.Language),
			Difficulty: string(spec.Difficulty),
			TemplateID: spec.GeneratedFrom.TemplateID,
			Kind:       string(spec.GeneratedFrom.Kind),
		},
		Agent: opts.adapter,
		Sandbox: previewSandboxJSON{
			Type:     sandboxTypeWorktree,
			RunsRoot: evalRunsRoot(root),
		},
		Verification: previewVerifyJSON{
			BuildCmd:       spec.Verification.BuildCmd,
			TestCmd:        spec.Verification.TestCmd,
			TimeoutSeconds: spec.Verification.TimeoutSeconds,
		},
	}
}

// renderRunPreview emits the dry-run plan as JSON (when asJSON) or a compact
// text summary that spells out, at every stage, that nothing was actually run.
func renderRunPreview(out io.Writer, p runPreview, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(p)
	}
	renderPreviewText(out, p)
	return nil
}

// renderPreviewText prints the human-readable dry-run plan.
func renderPreviewText(out io.Writer, p runPreview) {
	fmt.Fprintln(out, "Eval run PLAN (dry-run: nothing was executed or persisted)")
	fmt.Fprintf(out, "  task:     %s  (%s/%s)\n", p.Task.TaskID, p.Task.Language, p.Task.Difficulty)
	fmt.Fprintf(out, "  template: %s  (%s)\n", orDash(p.Task.TemplateID), orDash(p.Task.Kind))
	fmt.Fprintf(out, "  agent:    %s  [would run]\n", p.Agent)
	fmt.Fprintf(out, "  sandbox:  %s under %s  [would be provisioned]\n", p.Sandbox.Type, p.Sandbox.RunsRoot)
	fmt.Fprintln(out, "  verify:   [would run after the agent]")
	fmt.Fprintf(out, "    build:  %s\n", cmdLabel(p.Verification.BuildCmd))
	fmt.Fprintf(out, "    test:   %s\n", cmdLabel(p.Verification.TestCmd))
	fmt.Fprintf(out, "    timeout: %ds\n", p.Verification.TimeoutSeconds)
	fmt.Fprintln(out, "Preview only: no agent ran, no sandbox was provisioned, no run dir was written.")
}

// orDash renders an empty optional field as an em dash.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// cmdLabel renders an argv command as a space-joined line, or "(none)" when the
// spec declares no command for that phase (build_cmd is optional).
func cmdLabel(cmd []string) string {
	if len(cmd) == 0 {
		return "(none)"
	}
	return strings.Join(cmd, " ")
}

// runJSON is the `da eval run --json` envelope: run identity, the scored
// outcome, the verify summary, and the persisted sidecar paths — everything an
// R2 consumer needs without re-reading the sidecars.
type runJSON struct {
	RunID      string       `json:"run_id"`
	TaskID     string       `json:"task_id"`
	Language   string       `json:"language"`
	Difficulty string       `json:"difficulty"`
	BaseCommit string       `json:"base_commit"`
	Agent      agentJSON    `json:"agent"`
	Verify     verifyJSON   `json:"verify"`
	Score      scoreJSON    `json:"score"`
	Sidecars   sidecarsJSON `json:"sidecars"`
}

type agentJSON struct {
	Harness  string `json:"harness,omitempty"`
	Model    string `json:"model,omitempty"`
	ExitCode int    `json:"exit_code"`
}

type verifyJSON struct {
	Passed   bool   `json:"passed"`
	Phase    string `json:"phase,omitempty"`
	ExitCode int    `json:"exit_code"`
}

type scoreJSON struct {
	Value         float64 `json:"value"`
	Band          string  `json:"band"`
	Scored        bool    `json:"scored"`
	RubricVersion string  `json:"rubric_version"`
}

type sidecarsJSON struct {
	RunDir   string `json:"run_dir"`
	EvalRun  string `json:"eval_run"`
	TaskSpec string `json:"taskspec"`
	Record   string `json:"record"`
	Score    string `json:"score"`
}

// renderRun emits the completed run as JSON (when asJSON) or a compact text
// summary: run id, the scored outcome, the verify result, and the sidecar paths.
func renderRun(out io.Writer, run harness.EvalRun, res store.Result, asJSON bool) error {
	if asJSON {
		return emitRunJSON(out, run, res)
	}
	renderRunText(out, run, res)
	return nil
}

// emitRunJSON writes the indented run envelope.
func emitRunJSON(out io.Writer, run harness.EvalRun, res store.Result) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(buildRunJSON(run, res))
}

// buildRunJSON projects the harness result + store paths into the JSON envelope.
func buildRunJSON(run harness.EvalRun, res store.Result) runJSON {
	return runJSON{
		RunID:      run.RunID,
		TaskID:     run.Spec.TaskID,
		Language:   string(run.Spec.Language),
		Difficulty: string(run.Spec.Difficulty),
		BaseCommit: run.BaseCommit,
		Agent: agentJSON{
			Harness:  run.Run.Telemetry.Harness,
			Model:    run.Run.Telemetry.Model,
			ExitCode: run.Run.ExitCode,
		},
		Verify: buildVerifyJSON(run),
		Score: scoreJSON{
			Value:         run.Score.Score.Value,
			Band:          run.Score.Score.Band,
			Scored:        run.Score.Score.Scored,
			RubricVersion: run.Score.Score.RubricVersion,
		},
		Sidecars: sidecarsJSON{
			RunDir:   res.RunDir,
			EvalRun:  res.EvalRunPath,
			TaskSpec: res.TaskspecPath,
			Record:   res.RecordPath,
			Score:    res.ScorePath,
		},
	}
}

// buildVerifyJSON summarises the verify stage, tolerating a nil result.
func buildVerifyJSON(run harness.EvalRun) verifyJSON {
	if run.Verify == nil {
		return verifyJSON{}
	}
	return verifyJSON{
		Passed:   run.Verify.Passed,
		Phase:    string(run.Verify.Phase),
		ExitCode: run.Verify.ExitCode,
	}
}

// renderRunText prints the human-readable run summary.
func renderRunText(out io.Writer, run harness.EvalRun, res store.Result) {
	fmt.Fprintf(out, "Eval run %s\n", run.RunID)
	fmt.Fprintf(out, "  task:     %s  (%s/%s)\n",
		run.Spec.TaskID, run.Spec.Language, run.Spec.Difficulty)
	fmt.Fprintf(out, "  agent:    %s  exit %d\n", agentLabel(run), run.Run.ExitCode)
	fmt.Fprintf(out, "  verify:   %s\n", verifyLabel(run))
	fmt.Fprintf(out, "  score:    %s  band %s  rubric %s\n",
		scoreValue(run), run.Score.Score.Band, run.Score.Score.RubricVersion)
	fmt.Fprintln(out, "  sidecars:")
	fmt.Fprintf(out, "    eval-run:  %s\n", res.EvalRunPath)
	fmt.Fprintf(out, "    taskspec:  %s\n", res.TaskspecPath)
	fmt.Fprintf(out, "    record:    %s\n", res.RecordPath)
	fmt.Fprintf(out, "    score:     %s\n", res.ScorePath)
}

// agentLabel renders the agent identity as harness/model, collapsing an unknown
// harness to a placeholder so the row never reads as a bare slash.
func agentLabel(run harness.EvalRun) string {
	h := run.Run.Telemetry.Harness
	if h == "" {
		h = "(unknown)"
	}
	if m := run.Run.Telemetry.Model; m != "" {
		return h + "/" + m
	}
	return h
}

// verifyLabel renders the verify outcome as pass/fail plus the terminal phase.
func verifyLabel(run harness.EvalRun) string {
	if run.Verify == nil {
		return "(not run)"
	}
	status := "fail"
	if run.Verify.Passed {
		status = "pass"
	}
	return fmt.Sprintf("%s  (phase %s, exit %d)", status, run.Verify.Phase, run.Verify.ExitCode)
}

// scoreValue formats the numeric score, showing a dash for an unscored run.
func scoreValue(run harness.EvalRun) string {
	if !run.Score.Score.Scored {
		return "-"
	}
	return fmt.Sprintf("%.3f", run.Score.Score.Value)
}
