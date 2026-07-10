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

	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/spf13/cobra"
)

type startTaskResult struct {
	PlanID         string `json:"plan_id"`
	TaskID         string `json:"task_id"`
	ActivatedPlan  bool   `json:"activated_plan"`
	FocusedTask    bool   `json:"focused_task"`
	DerivedScope   bool   `json:"derived_scope"`
	WorkflowCommit bool   `json:"workflow_commit"`
	DryRun         bool   `json:"dry_run"`
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
			"sidecar; --no-commit batches the state commit elsewhere. --dry-run (or the\n" +
			"global -n) previews the chain without touching disk or the journal.\n\n" +
			"Fanout is intentionally not wired here — the orchestrator typically\n" +
			"decides direct-vs-delegated explicitly via `da workflow fanout` as a\n" +
			"separate step.",
		Example: deps.ExampleBlock(
			"  da workflow start-task my-plan --task t1",
			"  da workflow start-task my-plan --task t1 --no-derive-scope",
			"  da workflow start-task my-plan --task t1 --seed-symbol RunWorkflowFanout",
			"  da workflow start-task my-plan --task t1 --dry-run",
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
				dryRun:        startTaskDryRun(cmd),
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "Task ID to start (required)")
	cmd.Flags().BoolVar(&noDeriveScope, "no-derive-scope", false, "Skip the plan derive-scope step (operator will hand-author the sidecar)")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Skip the workflow-state commit step")
	cmd.Flags().StringSliceVar(&seedSymbols, "seed-symbol", nil, "Symbol seed for derive-scope (repeatable; same as `plan derive-scope`)")
	cmd.Flags().StringSliceVar(&seedPaths, "seed-path", nil, "Path seed for derive-scope (repeatable; same as `plan derive-scope`)")
	cmd.Flags().Bool("dry-run", false, "Preview the chain without activating the plan, focusing the task, deriving scope, or committing")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

// startTaskDryRun reports whether start-task should preview its chain
// instead of running it. The local --dry-run flag is OR-merged with the
// global -n/--dry-run so `da -n workflow start-task ...` is honored the
// same way as every other mutating command (sibling of foldBackDryRun /
// commit's dry-run wiring).
func startTaskDryRun(cmd *cobra.Command) bool {
	local, _ := cmd.Flags().GetBool("dry-run")
	return local || safeDryRun()
}

// startTask*-prefixed function-var seams let tests trigger each step's
// error path without standing up the fixture each primitive otherwise
// needs. Defaults point at the real primitives; tests rebind to stubs.
var (
	startTaskPlanUpdate  = runWorkflowPlanUpdate
	startTaskDeriveScope = runWorkflowPlanDeriveScope
)

type startTaskOpts struct {
	planID        string
	taskID        string
	noDeriveScope bool
	noCommit      bool
	dryRun        bool
	seedSymbols   []string
	seedPaths     []string
}

// runWorkflowStartTask drives the chain. Each step's failure surfaces as
// "start-task: <step>: ..." so log triage maps to the chain position.
// opts.dryRun short-circuits before any primitive call — none of
// startTaskPlanUpdate / startTaskDeriveScope / iterationCloseCommit are
// dry-run aware, so calling them "to preview" would mutate for real; the
// preview is built from opts alone and emits no journal event, mirroring
// runFoldBackUpsertDryRun's no-write / no-journal guarantee.
func runWorkflowStartTask(out io.Writer, opts startTaskOpts) error {
	if opts.dryRun {
		result := startTaskResult{
			PlanID:         opts.planID,
			TaskID:         opts.taskID,
			ActivatedPlan:  true,
			FocusedTask:    true,
			DerivedScope:   !opts.noDeriveScope,
			WorkflowCommit: !opts.noCommit,
			DryRun:         true,
		}
		if deps.Flags.JSON() {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}
		renderStartTaskSummary(out, result)
		return nil
	}
	// start-task is a molecule: its constituent plan-update / derive-scope /
	// commit calls journal their own typed events. This event records the
	// molecule-level intent. The runner does not resolve a project itself, so we
	// resolve it best-effort for the journal key (empty path skips emission).
	repoPath := ""
	if project, perr := currentWorkflowProject(); perr == nil {
		repoPath = project.Path
	}
	input := &journal.StartTaskInput{Plan: opts.planID, Task: opts.taskID, SeedSymbols: opts.seedSymbols, SeedPaths: opts.seedPaths}
	observed := &journal.StartTaskObserved{}
	ok := false
	defer func() { journalTier1(repoPath, journal.CmdStartTask, input, observed, ok) }()
	if err := startTaskPlanUpdate(opts.planID, "active", "", "", "", "", ""); err != nil {
		return fmt.Errorf("start-task: plan update --status active: %w", err)
	}
	if err := startTaskPlanUpdate(opts.planID, "", "", "", opts.taskID, "", ""); err != nil {
		return fmt.Errorf("start-task: plan update --focus %s: %w", opts.taskID, err)
	}

	derived := false
	if !opts.noDeriveScope {
		if err := startTaskDeriveScope(opts.planID, opts.taskID, opts.seedSymbols, opts.seedPaths); err != nil {
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

	observed.Committed = committed
	ok = true
	result := startTaskResult{
		PlanID:         opts.planID,
		TaskID:         opts.taskID,
		ActivatedPlan:  true,
		FocusedTask:    true,
		DerivedScope:   derived,
		WorkflowCommit: committed,
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	renderStartTaskSummary(out, result)
	return nil
}

func renderStartTaskSummary(out io.Writer, r startTaskResult) {
	if r.DryRun {
		fmt.Fprintf(out, "start-task %s/%s [dry-run]\n", r.PlanID, r.TaskID)
		fmt.Fprintln(out, "  [dry-run] would activate plan")
		fmt.Fprintf(out, "  [dry-run] would focus task %s\n", r.TaskID)
		if r.DerivedScope {
			fmt.Fprintln(out, "  [dry-run] would derive scope-evidence sidecar")
		} else {
			fmt.Fprintln(out, "  scope derivation skipped (--no-derive-scope)")
		}
		if r.WorkflowCommit {
			fmt.Fprintln(out, "  [dry-run] would commit workflow state")
		} else {
			fmt.Fprintln(out, "  workflow state NOT committed (--no-commit)")
		}
		return
	}
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
