// close_task.go wires `da workflow close-task`, the T1-molecule client
// command that composes the end-of-iteration primitive chain:
//
//	checkpoint --log-to-iter N → score iteration N → advance task to
//	completed → plan update --focus <next eligible> → workflow commit
//
// Each step is a T0 atom that already exists in this package; this file
// orchestrates them and surfaces wrapped errors naming which step blew
// so the operator does not have to bisect by hand.
//
// tier: molecule
// calls:
//   - workflow-checkpoint-log-to-iter
//   - score-iteration
//   - workflow-advance
//   - workflow-plan-update
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
	"os"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/scoring"
	"github.com/spf13/cobra"
)

// closeTaskResult is the JSON shape `da workflow close-task --json` emits.
// Each field maps to one of the orchestrated steps so callers (skills,
// scripts) can verify exactly what landed without reparsing the text.
type closeTaskResult struct {
	PlanID         string  `json:"plan_id"`
	TaskID         string  `json:"task_id"`
	IterationN     int     `json:"iteration_n"`
	SidecarPath    string  `json:"sidecar_path"`
	ScoreValue     float64 `json:"score_value"`
	ScoreBand      string  `json:"score_band"`
	NextFocus      string  `json:"next_focus,omitempty"`
	WorkflowCommit bool    `json:"workflow_commit"`
}

func newWorkflowCloseTaskCmd() *cobra.Command {
	var (
		task           string
		scoreRecompute string
		noCommit       bool
		nextFocus      string
		repoDir        string
		transcriptDirs []string
	)
	cmd := &cobra.Command{
		Use:   "close-task <plan-id>",
		Short: "End-of-iteration client command: checkpoint → score → advance → focus → commit",
		Long: "Composes the end-of-iteration primitive chain into one invocation.\n\n" +
			"By default scores only the just-closed iteration (--score-recompute=current);\n" +
			"older sidecars are computed from immutable inputs and stay valid until the\n" +
			"next RubricVersion bump. Pass --no-commit to skip the workflow-state commit\n" +
			"step for callers that batch; the iteration-close skill leaves it on.",
		Example: deps.ExampleBlock(
			"  da workflow close-task my-plan --task t1",
			"  da workflow close-task my-plan --task t1 --next-focus t2",
			"  da workflow close-task my-plan --task t1 --no-commit --json",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the canonical plan ID that owns the task."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowCloseTask(cmd.OutOrStdout(), closeTaskOpts{
				planID:         args[0],
				taskID:         task,
				scoreRecompute: scoreRecompute,
				noCommit:       noCommit,
				nextFocus:      nextFocus,
				repoDir:        repoDir,
				transcriptDirs: transcriptDirs,
			})
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "Task ID to close (required)")
	cmdutil.RegisterEnum(cmd, &scoreRecompute, scoreRecomputeEnum)
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Skip the workflow-state commit step (caller will batch elsewhere)")
	cmd.Flags().StringVar(&nextFocus, "next-focus", "", "Override the auto-picked next focus task (default: first eligible task in the same plan)")
	cmd.Flags().StringVar(&repoDir, "repo-dir", "", "Repository root for git topology (default: current working directory)")
	cmd.Flags().StringSliceVar(&transcriptDirs, "transcript-dir", nil, "Agent transcript root for token backfill (repeatable)")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

type closeTaskOpts struct {
	planID         string
	taskID         string
	scoreRecompute string
	noCommit       bool
	nextFocus      string
	repoDir        string
	transcriptDirs []string
}

// closeTask*-prefixed function-var seams let tests trigger each step's
// error path without standing up the fixture each primitive otherwise
// needs (project bootstrap, plan validation, etc.). Defaults point at
// the real primitives; tests rebind to stubs.
var (
	closeTaskCheckpoint     = runWorkflowCheckpointLogToIter
	closeTaskResolveProject = currentWorkflowProject
	closeTaskPlanUpdate     = runWorkflowPlanUpdate
	closeTaskWriteSidecar   = scoring.WriteIterationScoreWithRecord
	closeTaskNextIter       = NextIterationNumber
)

// runWorkflowCloseTask is the orchestration body. Each step's failure
// surfaces as a "close-task: <step>: ..." error so log triage maps
// directly to the chain position.
//
// The score-recompute axis is intentionally restricted to "current" for
// the initial cut. "recent-N" and "all" are reserved values that map
// directly onto the existing `da score run` invocation; the wiring
// arrives once the skill needs them. Rejecting them up-front keeps the
// contract honest.
func runWorkflowCloseTask(out io.Writer, opts closeTaskOpts) error {
	setup, err := resolveCloseTaskSetup(opts)
	if err != nil {
		return err
	}
	input := &journal.CloseTaskInput{Plan: opts.planID, Task: opts.taskID, NextFocus: opts.nextFocus}
	observed := &journal.CloseTaskObserved{ToStatus: "completed"}
	ok := false
	defer func() { journalTier1(setup.project.Path, journal.CmdCloseTask, input, observed, ok) }()
	if err := closeTaskCheckpoint(setup.n, DefaultIterationRole(), ""); err != nil {
		return fmt.Errorf("close-task: checkpoint --log-to-iter %d: %w", setup.n, err)
	}
	score, rec, err := scoring.ScoreIteration(setup.iterDir, setup.repoDir, setup.n, opts.transcriptDirs...)
	if err != nil {
		return fmt.Errorf("close-task: score iteration %d: %w", setup.n, err)
	}
	sidecarPath, err := closeTaskWriteSidecar(setup.iterDir, score, rec)
	if err != nil {
		return fmt.Errorf("close-task: persist iter-%d sidecar: %w", setup.n, err)
	}
	publishScoreBestEffort(setup.project.Path, setup.n)
	if err := runWorkflowAdvance(opts.planID, opts.taskID, "completed"); err != nil {
		return fmt.Errorf("close-task: advance: %w", err)
	}
	nextFocus, err := applyCloseTaskNextFocus(opts, setup.project.Path)
	if err != nil {
		return err
	}
	committed, err := runCloseTaskCommit(out, opts, setup.project.Path)
	if err != nil {
		return err
	}
	observed.NextFocusSet = nextFocus != ""
	observed.Committed = committed
	ok = true
	return emitCloseTaskResult(out, closeTaskResult{
		PlanID:         opts.planID,
		TaskID:         opts.taskID,
		IterationN:     setup.n,
		SidecarPath:    sidecarPath,
		ScoreValue:     score.Value,
		ScoreBand:      score.Band,
		NextFocus:      nextFocus,
		WorkflowCommit: committed,
	})
}

// closeTaskSetup bundles the four pieces of context the orchestration
// chain needs: the resolved project, the effective repo dir (cwd
// fallback), the canonical iter-log dir, and the next iteration number.
// Extracted from runWorkflowCloseTask to keep its cognitive complexity
// under the gate threshold.
type closeTaskSetup struct {
	project workflowProjectRef
	repoDir string
	iterDir string
	n       int
}

func resolveCloseTaskSetup(opts closeTaskOpts) (closeTaskSetup, error) {
	if opts.scoreRecompute != "current" {
		return closeTaskSetup{}, fmt.Errorf("close-task: --score-recompute=%q not yet implemented (only \"current\" supported in this slice)", opts.scoreRecompute)
	}
	project, err := closeTaskResolveProject()
	if err != nil {
		return closeTaskSetup{}, fmt.Errorf("close-task: resolve project: %w", err)
	}
	repoDir := opts.repoDir
	if repoDir == "" {
		// See runScoreRun for why we treat Getwd as infallible.
		cwd, _ := os.Getwd()
		repoDir = cwd
	}
	iterDir := IterationLogDir(project.Path)
	n, err := closeTaskNextIter(iterDir)
	if err != nil {
		return closeTaskSetup{}, fmt.Errorf("close-task: pick iteration N: %w", err)
	}
	return closeTaskSetup{project: project, repoDir: repoDir, iterDir: iterDir, n: n}, nil
}

// applyCloseTaskNextFocus picks (or accepts the override of) the next
// focus task and updates the plan if non-empty. Returns the picked
// nextFocus so the caller can include it in the result snapshot.
func applyCloseTaskNextFocus(opts closeTaskOpts, projectPath string) (string, error) {
	nextFocus := opts.nextFocus
	if nextFocus == "" {
		nextFocus = pickNextFocus(projectPath, opts.planID)
	}
	if nextFocus == "" {
		return "", nil
	}
	if err := closeTaskPlanUpdate(opts.planID, "", "", "", nextFocus, "", ""); err != nil {
		return "", fmt.Errorf("close-task: plan update --focus %s: %w", nextFocus, err)
	}
	return nextFocus, nil
}

// runCloseTaskCommit fires the workflow-state commit unless --no-commit is set.
// It names the current iteration's iter-log artifacts as explicit includes so
// they land in the same commit even though they sit outside the auto-managed
// roots. Returns (didCommit, err) so the caller can record the outcome in the
// result snapshot.
func runCloseTaskCommit(out io.Writer, opts closeTaskOpts, projectPath string) (bool, error) {
	if opts.noCommit {
		return false, nil
	}
	if err := iterationCloseCommitWithIncludes(out, currentIterationIncludePaths(projectPath)); err != nil {
		return false, fmt.Errorf("close-task: workflow commit: %w", err)
	}
	return true, nil
}

// emitCloseTaskResult renders or emits the structured result depending
// on --json, matching the existing close-task contract.
func emitCloseTaskResult(out io.Writer, result closeTaskResult) error {
	if deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	renderCloseTaskSummary(out, result)
	return nil
}

// pickNextFocus returns the first eligible task ID in the same plan, or
// "" if none remain. close-task's caller may override via --next-focus.
//
// Silent on errors — picking is best-effort; a failure to compute next
// eligibles is not worth aborting the whole close over.
func pickNextFocus(projectPath, planID string) string {
	tasks, err := selectAllEligibleTasks(projectPath, []string{planID})
	if err != nil {
		return ""
	}
	for _, t := range tasks {
		if t.PlanID == planID {
			return t.TaskID
		}
	}
	return ""
}

func renderCloseTaskSummary(out io.Writer, r closeTaskResult) {
	fmt.Fprintf(out, "close-task %s/%s — iteration %d, score %.3f (%s)\n",
		r.PlanID, r.TaskID, r.IterationN, r.ScoreValue, r.ScoreBand)
	fmt.Fprintf(out, "  sidecar: %s\n", r.SidecarPath)
	if r.NextFocus != "" {
		fmt.Fprintf(out, "  next focus: %s\n", r.NextFocus)
	}
	if r.WorkflowCommit {
		fmt.Fprintln(out, "  workflow state committed")
	} else {
		fmt.Fprintln(out, "  workflow state NOT committed (--no-commit)")
	}
}
