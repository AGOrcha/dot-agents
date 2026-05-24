// start_task.go wires `da workflow start-task`, the T1-molecule client
// command that mirrors close-task on the start side of the iteration:
//
//	plan update --status active → plan update --focus T → plan derive-scope T
//	→ workflow commit
//
// Each step is a T0 atom already in this package; this file orchestrates
// them and surfaces wrapped errors naming which step blew so the
// operator does not have to bisect by hand. fanout is intentionally NOT
// wired in this slice — the orchestrator typically wants to decide
// direct-vs-delegated explicitly, and `da workflow fanout` remains a
// separate primitive call.
//
// tier: molecule
// calls:
//   - workflow-plan-update
//   - workflow-plan-derive-scope
//   - workflow-commit
//
// Per the skill-tiering-contract (T0 atom → T1 molecule → T2 compound →
// T3 cell), runtime agent judgment is bounded to picking among the
// declared atoms above. A future lint pass will read this block.
package workflow

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type startTaskResult struct {
	PlanID         string `json:"plan_id"`
	TaskID         string `json:"task_id"`
	ActivatedPlan  bool   `json:"activated_plan"`
	FocusedTask    bool   `json:"focused_task"`
	DerivedScope   bool   `json:"derived_scope"`
	WorkflowCommit bool   `json:"workflow_commit"`
}

func newWorkflowStartTaskCmd() *cobra.Command {
	var (
		task          string
		noDeriveScope bool
		noCommit      bool
		seedSymbols   []string
		seedPaths     []string
	)
	cmd := &cobra.Command{
		Use:   "start-task <plan-id>",
		Short: "Start-of-iteration client command: activate plan → focus task → derive scope → commit",
		Long: "Composes the start-of-iteration primitive chain into one invocation.\n\n" +
			"Default sequence: flip the plan to status=active, set current_focus_task to\n" +
			"--task, run `plan derive-scope` to write the evidence sidecar the worker\n" +
			"stage will consume, then `workflow commit` the state mutation. --no-derive-\n" +
			"scope is the escape hatch when the operator wants to hand-author the\n" +
			"sidecar; --no-commit batches the state commit elsewhere.\n\n" +
			"Fanout is intentionally not wired here — the orchestrator typically\n" +
			"decides direct-vs-delegated explicitly via `da workflow fanout` as a\n" +
			"separate step.",
		Example: deps.ExampleBlock(
			"  da workflow start-task my-plan --task t1",
			"  da workflow start-task my-plan --task t1 --no-derive-scope",
			"  da workflow start-task my-plan --task t1 --seed-symbol RunWorkflowFanout",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the canonical plan ID that owns the task."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowStartTask(cmd.OutOrStdout(), startTaskOpts{
				planID:        args[0],
				taskID:        task,
				noDeriveScope: noDeriveScope,
				noCommit:      noCommit,
				seedSymbols:   seedSymbols,
				seedPaths:     seedPaths,
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "Task ID to start (required)")
	cmd.Flags().BoolVar(&noDeriveScope, "no-derive-scope", false, "Skip the plan derive-scope step (operator will hand-author the sidecar)")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Skip the workflow-state commit step")
	cmd.Flags().StringSliceVar(&seedSymbols, "seed-symbol", nil, "Symbol seed for derive-scope (repeatable; same as `plan derive-scope`)")
	cmd.Flags().StringSliceVar(&seedPaths, "seed-path", nil, "Path seed for derive-scope (repeatable; same as `plan derive-scope`)")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

type startTaskOpts struct {
	planID        string
	taskID        string
	noDeriveScope bool
	noCommit      bool
	seedSymbols   []string
	seedPaths     []string
}

// runWorkflowStartTask drives the chain. Each step's failure surfaces as
// "start-task: <step>: ..." so log triage maps to the chain position.
func runWorkflowStartTask(out io.Writer, opts startTaskOpts) error {
	if err := runWorkflowPlanUpdate(opts.planID, "active", "", "", "", "", ""); err != nil {
		return fmt.Errorf("start-task: plan update --status active: %w", err)
	}
	if err := runWorkflowPlanUpdate(opts.planID, "", "", "", opts.taskID, "", ""); err != nil {
		return fmt.Errorf("start-task: plan update --focus %s: %w", opts.taskID, err)
	}

	derived := false
	if !opts.noDeriveScope {
		if err := runWorkflowPlanDeriveScope(opts.planID, opts.taskID, opts.seedSymbols, opts.seedPaths); err != nil {
			return fmt.Errorf("start-task: plan derive-scope: %w", err)
		}
		derived = true
	}

	committed := false
	if !opts.noCommit {
		if err := iterationCloseCommit(out); err != nil {
			return fmt.Errorf("start-task: workflow commit: %w", err)
		}
		committed = true
	}

	result := startTaskResult{
		PlanID:         opts.planID,
		TaskID:         opts.taskID,
		ActivatedPlan:  true,
		FocusedTask:    true,
		DerivedScope:   derived,
		WorkflowCommit: committed,
	}
	if deps.Flags.JSON != nil && deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	renderStartTaskSummary(out, result)
	return nil
}

func renderStartTaskSummary(out io.Writer, r startTaskResult) {
	fmt.Fprintf(out, "start-task %s/%s\n", r.PlanID, r.TaskID)
	fmt.Fprintln(out, "  plan activated + focused")
	if r.DerivedScope {
		fmt.Fprintln(out, "  scope-evidence sidecar derived")
	} else {
		fmt.Fprintln(out, "  scope derivation skipped (--no-derive-scope)")
	}
	if r.WorkflowCommit {
		fmt.Fprintln(out, "  workflow state committed")
	} else {
		fmt.Fprintln(out, "  workflow state NOT committed (--no-commit)")
	}
}
