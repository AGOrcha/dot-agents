package eval

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/AGOrcha/dot-agents/internal/eval/harness"
	"github.com/AGOrcha/dot-agents/internal/eval/store"
)

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
