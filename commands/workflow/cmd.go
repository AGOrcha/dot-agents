package workflow

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const (
	workflowFlagVerifierType    = "verifier-type"
	workflowFlagLogToIter       = "log-to-iter"
	workflowFlagWriteScope      = "write-scope"
	workflowFlagCommitState     = "commit-state"
	cmdHintCanonicalPlanID      = "Pass a canonical plan ID from `da workflow plan`."
	cmdFlagCanonicalPlanIDDescr = "Canonical plan ID (required)"
)

// Command wiring for `da workflow`: cobra subtree and exported NewCmd(deps).
// Behavioral implementations live in sibling sources (state.go, plan_task.go, verification.go, …).

func newWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Inspect and persist workflow state",
		Long: `Captures the repository-local workflow state that helps both humans and
AI agents resume work safely: canonical plans, checkpoints, verification logs,
preferences, fanout artifacts, and bridge queries.`,
		Example: deps.ExampleBlock(
			"  da workflow status",
			"  da workflow orient",
			"  da workflow next",
			"  da workflow checkpoint --message \"Resume transport slice\"",
		),
	}

	cmd.AddCommand(
		newWorkflowStatusCmd(),
		newWorkflowOrientCmd(),
		newWorkflowCheckpointCmd(),
		newWorkflowLogCmd(),
		newWorkflowPlanCmd(),
		newWorkflowTaskCmd(),
		newWorkflowTasksCmd(),
		newWorkflowSlicesCmd(),
		newWorkflowEligibleCmd(),
		newWorkflowSlotsCmd(),
		newWorkflowNextCmd(),
		newWorkflowCompleteCmd(),
		newWorkflowAdvanceCmd(),
		newWorkflowHealthCmd(),
		newWorkflowVerifyCmd(),
		newWorkflowPrefsCmd(),
		newWorkflowAppTypesCmd(),
		newWorkflowResolvePromptCmd(),
		newWorkflowPipelineCmd(),
		newWorkflowGraphCmd(),
		newWorkflowFanoutCmd(),
		newWorkflowContractCmd(),
		newWorkflowMergeBackCmd(),
		newWorkflowFoldBackCmd(),
		newWorkflowDelegationCmd(),
		newWorkflowDriftCmd(),
		newWorkflowSweepCmd(),
		newWorkflowBundleCmd(),
		newWorkflowCommitCmd(),
		newWorkflowCloseTaskCmd(),
		newWorkflowStartTaskCmd(),
		newWorkflowHookSentinelCmd(),
		newWorkflowHookOutcomeCmd(),
		newWorkflowArchiveOrphansCmd(),
		newWorkflowJournalCmd(),
		newWorkflowStateRefCmd(),
	)
	return cmd
}

func newWorkflowStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show workflow state for the current project",
		Example: deps.ExampleBlock(
			"  da workflow status",
			"  da --json workflow status",
		),
		Args: deps.NoArgsWithHints("Run workflow status from inside the project repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowStatus()
		},
	}
}

func newWorkflowOrientCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "orient",
		Short: "Render session orient context for the current project",
		Example: deps.ExampleBlock(
			"  da workflow orient",
		),
		Args: deps.NoArgsWithHints("Run workflow orient from inside the project repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowOrient()
		},
	}
}

func newWorkflowCheckpointCmd() *cobra.Command {
	var (
		checkpointMessage               string
		checkpointVerificationState     string
		checkpointVerificationText      string
		checkpointLogToIter             int
		checkpointLogToIterRole         string
		checkpointLogToIterVerifierType string
	)
	checkpointCmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Write a checkpoint for the current project",
		Example: deps.ExampleBlock(
			"  da workflow checkpoint --message \"Resume plan graph work\"",
			"  da workflow checkpoint --verification-status pass --verification-summary \"go test ./...\"",
		),
		Args: deps.NoArgsWithHints("Use flags such as `--message` instead of positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("role") || cmd.Flags().Changed(workflowFlagVerifierType) {
				if !cmd.Flags().Changed(workflowFlagLogToIter) {
					return deps.UsageError(
						"--role and --verifier-type require --log-to-iter",
						"Pass --log-to-iter N (N >= 1) alongside --role / --verifier-type.",
					)
				}
			}
			if cmd.Flags().Changed(workflowFlagLogToIter) {
				if checkpointLogToIter < 1 {
					return deps.UsageError(
						"checkpoint --log-to-iter requires N >= 1 (schema workflow-iter-log enforces iteration.minimum: 1)",
						"Pass --log-to-iter with N >= 1, for example --log-to-iter 7.",
					)
				}
				if err := runWorkflowCheckpointLogToIter(checkpointLogToIter, checkpointLogToIterRole, checkpointLogToIterVerifierType); err != nil {
					return err
				}
			}
			return runWorkflowCheckpoint(checkpointMessage, checkpointVerificationState, checkpointVerificationText)
		},
	}
	checkpointCmd.Flags().StringVar(&checkpointMessage, "message", "", "Checkpoint message")
	checkpointCmd.Flags().StringVar(&checkpointVerificationState, "verification-status", workflowDefaultVerificationState, "Verification status: pass, fail, partial, or unknown")
	checkpointCmd.Flags().StringVar(&checkpointVerificationText, "verification-summary", "", "Verification summary text")
	checkpointCmd.Flags().IntVar(&checkpointLogToIter, workflowFlagLogToIter, 0, "Write a schema-validated iteration log stub for N (>=1) to .agents/active/iteration-log/iter-N.yaml")
	checkpointCmd.Flags().StringVar(&checkpointLogToIterRole, "role", "", "With --log-to-iter: merge only the impl, verifier, or review block")
	checkpointCmd.Flags().StringVar(&checkpointLogToIterVerifierType, workflowFlagVerifierType, "", "Verifier slug when --role verifier (for example unit)")
	return checkpointCmd
}

func newWorkflowLogCmd() *cobra.Command {
	var logAll bool
	logCmd := &cobra.Command{
		Use:   "log",
		Short: "Show recent checkpoint log entries",
		Example: deps.ExampleBlock(
			"  da workflow log",
			"  da workflow log --all",
		),
		Args: deps.NoArgsWithHints("Use `--all` to expand the log instead of passing a positional argument."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowLog(logAll)
		},
	}
	logCmd.Flags().BoolVar(&logAll, "all", false, "Show all log entries")
	return logCmd
}

func newWorkflowPlanCmd() *cobra.Command {
	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "List canonical plans",
		Example: deps.ExampleBlock(
			"  da workflow plan",
			"  da workflow plan show loop-orchestrator-layer",
			"  da workflow plan graph",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanList()
		},
	}
	planCmd.AddCommand(
		newWorkflowPlanShowCmd(),
		newWorkflowPlanGraphCmd(),
		newWorkflowPlanCreateCmd(),
		newWorkflowPlanUpdateCmd(),
		newWorkflowPlanArchiveCmd(),
		newWorkflowPlanScheduleCmd(),
		newWorkflowPlanDeriveScopeCmd(),
		newWorkflowPlanCheckScopeCmd(),
	)
	return planCmd
}

func newWorkflowPlanShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <plan-id>",
		Short: "Show details of a canonical plan",
		Example: deps.ExampleBlock(
			"  da workflow plan show loop-orchestrator-layer",
		),
		Args: deps.ExactArgsWithHints(1, cmdHintCanonicalPlanID),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanShow(args[0])
		},
	}
}

func newWorkflowPlanGraphCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "graph [plan-id]",
		Short: "Render a derived graph of canonical plans and tasks",
		Example: deps.ExampleBlock(
			"  da workflow plan graph",
			"  da workflow plan graph loop-orchestrator-layer",
		),
		Args: deps.MaximumNArgsWithHints(1, "Optionally pass one plan ID to limit the graph output."),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := ""
			if len(args) == 1 {
				planID = args[0]
			}
			return runWorkflowPlanGraph(planID)
		},
	}
}

func newWorkflowPlanCreateCmd() *cobra.Command {
	var planCreateTitle, planCreateSummary, planCreateOwner, planCreateSuccessCriteria, planCreateVerificationStrategy string
	planCreateCmd := &cobra.Command{
		Use:   "create <plan-id>",
		Short: "Create a new canonical plan directory with PLAN.yaml and TASKS.yaml stubs",
		Example: deps.ExampleBlock(
			"  da workflow plan create repo-cleanup --title \"Repository cleanup\" --summary \"Normalize stale plans\"",
		),
		Args: deps.ExactArgsWithHints(1, "Pass a new canonical plan ID such as `repo-cleanup`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanCreate(args[0], planCreateTitle, planCreateSummary, planCreateOwner, planCreateSuccessCriteria, planCreateVerificationStrategy)
		},
	}
	planCreateCmd.Flags().StringVar(&planCreateTitle, "title", "", "Plan title (required)")
	planCreateCmd.Flags().StringVar(&planCreateSummary, "summary", "", "Short summary of the plan goal")
	planCreateCmd.Flags().StringVar(&planCreateOwner, "owner", "dot-agents", "Plan owner")
	planCreateCmd.Flags().StringVar(&planCreateSuccessCriteria, "success-criteria", "", "Observable conditions that prove the plan is done")
	planCreateCmd.Flags().StringVar(&planCreateVerificationStrategy, "verification-strategy", "", "How completion will be verified (tests, smokes, manual checks)")
	_ = planCreateCmd.MarkFlagRequired("title")
	return planCreateCmd
}

func newWorkflowPlanUpdateCmd() *cobra.Command {
	var planUpdateStatus, planUpdateTitle, planUpdateSummary, planUpdateFocus, planUpdateSuccessCriteria, planUpdateVerificationStrategy string
	planUpdateCmd := &cobra.Command{
		Use:   "update <plan-id>",
		Short: "Update PLAN.yaml metadata fields",
		Example: deps.ExampleBlock(
			"  da workflow plan update repo-cleanup --status active --focus task-triage",
		),
		Args: deps.ExactArgsWithHints(1, "Pass an existing canonical plan ID."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanUpdate(args[0], planUpdateStatus, planUpdateTitle, planUpdateSummary, planUpdateFocus, planUpdateSuccessCriteria, planUpdateVerificationStrategy)
		},
	}
	planUpdateCmd.Flags().StringVar(&planUpdateStatus, "status", "", "New plan status (draft|active|paused|completed|archived)")
	planUpdateCmd.Flags().StringVar(&planUpdateTitle, "title", "", "New plan title")
	planUpdateCmd.Flags().StringVar(&planUpdateSummary, "summary", "", "New plan summary")
	planUpdateCmd.Flags().StringVar(&planUpdateFocus, "focus", "", "New current_focus_task value")
	planUpdateCmd.Flags().StringVar(&planUpdateSuccessCriteria, "success-criteria", "", "New success criteria (replaces existing)")
	planUpdateCmd.Flags().StringVar(&planUpdateVerificationStrategy, "verification-strategy", "", "New verification strategy (replaces existing)")
	return planUpdateCmd
}

func newWorkflowPlanArchiveCmd() *cobra.Command {
	var planArchivePlanIDs string
	var planArchiveForce bool
	var planArchiveNoCommit bool
	planArchiveCmd := &cobra.Command{
		Use:   "archive",
		Short: "Archive one or more completed canonical plans",
		Long: "Moves each completed plan from .agents/workflow/plans/<id> into\n" +
			".agents/history/<id>, stamps status=archived, and — by default —\n" +
			"commits the move as a single workflow-state commit so it survives the\n" +
			"fresh-clone / worktree loop model (an uncommitted move is discarded\n" +
			"before it lands on master). Pass --no-commit to leave the archived tree\n" +
			"uncommitted (for batching); the commit.disable workflow preference is\n" +
			"honored regardless.",
		Example: deps.ExampleBlock(
			"  da workflow plan archive --plan repo-cleanup",
			"  da workflow plan archive --plan plan-a,plan-b --force",
			"  da workflow plan archive --plan repo-cleanup --no-commit",
			"  da -n workflow plan archive --plan repo-cleanup",
		),
		Args: deps.NoArgsWithHints("Use --plan to specify one or more plan IDs (comma-separated)."),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := currentWorkflowProject()
			if err != nil {
				return err
			}
			ids := strings.Split(planArchivePlanIDs, ",")
			cleaned := ids[:0]
			for _, id := range ids {
				if s := strings.TrimSpace(id); s != "" {
					cleaned = append(cleaned, s)
				}
			}
			if len(cleaned) == 0 {
				return deps.UsageError(
					"--plan must specify at least one plan ID",
					"Pass --plan with one or more comma-separated plan IDs, for example --plan my-plan.",
				)
			}
			return runWorkflowPlanArchive(project.Path, cleaned, planArchiveForce, deps.Flags.DryRun(), planArchiveNoCommit)
		},
	}
	planArchiveCmd.Flags().StringVar(&planArchivePlanIDs, "plan", "", "Comma-separated plan IDs to archive (required)")
	planArchiveCmd.Flags().BoolVar(&planArchiveForce, "force", false, "Skip completed-status guard and archive regardless of plan status")
	planArchiveCmd.Flags().BoolVar(&planArchiveNoCommit, "no-commit", false, "Do not commit the archive move (leave the archived tree staged for a manual/batched commit)")
	_ = planArchiveCmd.MarkFlagRequired("plan")
	return planArchiveCmd
}

func newWorkflowPlanScheduleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schedule <plan-id>",
		Short: "Show wave schedule (Kahn BFS topological sort) for a plan's tasks",
		Example: deps.ExampleBlock(
			"  da workflow plan schedule plan-archive-command",
			"  da --json workflow plan schedule plan-archive-command",
		),
		Args: deps.ExactArgsWithHints(1, cmdHintCanonicalPlanID),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanSchedule(args[0])
		},
	}
}

func newWorkflowPlanDeriveScopeCmd() *cobra.Command {
	var deriveScopeSymbols, deriveScopePaths []string
	planDeriveScopeCmd := &cobra.Command{
		Use:   "derive-scope <plan-id> <task-id>",
		Short: "Derive a candidate scope-evidence sidecar for a task using KG/CRG graph queries",
		Example: deps.ExampleBlock(
			"  da workflow plan derive-scope my-plan my-task --seed-symbol RunWorkflowFanout --seed-symbol runWorkflowAdvance",
			"  da workflow plan derive-scope my-plan my-task --seed-path commands/workflow/delegation.go",
			"  da --json workflow plan derive-scope my-plan my-task --seed-symbol RunWorkflowFanout",
		),
		Args: deps.ExactArgsWithHints(2, "Pass a canonical plan ID and task ID."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanDeriveScope(args[0], args[1], deriveScopeSymbols, deriveScopePaths)
		},
	}
	planDeriveScopeCmd.Flags().StringArrayVar(&deriveScopeSymbols, "seed-symbol", nil, "Seed symbol for scope-lane queries (repeatable)")
	planDeriveScopeCmd.Flags().StringArrayVar(&deriveScopePaths, "seed-path", nil, "Seed file path for scope-lane queries (repeatable)")
	return planDeriveScopeCmd
}

func newWorkflowPlanCheckScopeCmd() *cobra.Command {
	var checkScopeFiles []string
	var checkScopeFromGitDiff bool
	planCheckScopeCmd := &cobra.Command{
		Use:   "check-scope <plan-id> <task-id>",
		Short: "Check changed files against the scope-evidence sidecar for a task",
		Example: deps.ExampleBlock(
			"  da workflow plan check-scope my-plan my-task --from-git-diff",
			"  da workflow plan check-scope my-plan my-task --changed-file commands/workflow/cmd.go --changed-file commands/workflow/plan_task.go",
			"  da --json workflow plan check-scope my-plan my-task --from-git-diff",
		),
		Args: deps.ExactArgsWithHints(2, "Pass a canonical plan ID and task ID."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanCheckScope(args[0], args[1], checkScopeFiles, checkScopeFromGitDiff)
		},
	}
	planCheckScopeCmd.Flags().StringArrayVar(&checkScopeFiles, "changed-file", nil, "Changed file path (repeatable)")
	planCheckScopeCmd.Flags().BoolVar(&checkScopeFromGitDiff, "from-git-diff", false, "Read changed files from git diff HEAD")
	return planCheckScopeCmd
}

func newWorkflowTaskCmd() *cobra.Command {
	taskCmd := &cobra.Command{
		Use:   "task",
		Short: "Add or update tasks within a canonical plan",
		Example: deps.ExampleBlock(
			"  da workflow task add loop-orchestrator-layer --id phase-5 --title \"Transport cleanup\"",
			"  da workflow task update loop-orchestrator-layer --task phase-5 --write-scope internal/platform",
		),
	}
	taskCmd.AddCommand(newWorkflowTaskAddCmd(), newWorkflowTaskUpdateCmd())
	return taskCmd
}

func newWorkflowTaskAddCmd() *cobra.Command {
	var taskAddID, taskAddTitle, taskAddNotes, taskAddOwner, taskAddDependsOn, taskAddBlocks, taskAddWriteScope, taskAddAppType string
	var taskAddVerification bool
	taskAddCmd := &cobra.Command{
		Use:   "add <plan-id>",
		Short: "Append a new task to a canonical plan's TASKS.yaml",
		Example: deps.ExampleBlock(
			"  da workflow task add loop-orchestrator-layer --id phase-5 --title \"Transport cleanup\"",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the canonical plan ID that should receive the new task."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowTaskAdd(taskAddInputs{
				PlanID:               args[0],
				TaskID:               taskAddID,
				Title:                taskAddTitle,
				Notes:                taskAddNotes,
				Owner:                taskAddOwner,
				DependsOn:            taskAddDependsOn,
				Blocks:               taskAddBlocks,
				WriteScope:           taskAddWriteScope,
				AppType:              taskAddAppType,
				VerificationRequired: taskAddVerification,
			})
		},
	}
	taskAddCmd.Flags().StringVar(&taskAddID, "id", "", "Task ID (required)")
	taskAddCmd.Flags().StringVar(&taskAddTitle, "title", "", "Task title (required)")
	taskAddCmd.Flags().StringVar(&taskAddNotes, "notes", "", "Implementation notes")
	taskAddCmd.Flags().StringVar(&taskAddOwner, "owner", "dot-agents", "Task owner")
	taskAddCmd.Flags().StringVar(&taskAddDependsOn, "depends-on", "", "Comma-separated list of task IDs this task depends on")
	taskAddCmd.Flags().StringVar(&taskAddBlocks, "blocks", "", "Comma-separated list of task IDs this task blocks")
	taskAddCmd.Flags().StringVar(&taskAddWriteScope, workflowFlagWriteScope, "", "Comma-separated file/dir patterns this task may touch")
	taskAddCmd.Flags().StringVar(&taskAddAppType, "app-type", "", "App type for verifier dispatch (e.g. go-cli, go-http-service)")
	taskAddCmd.Flags().BoolVar(&taskAddVerification, "verification-required", true, "Whether verification is required before marking complete")
	_ = taskAddCmd.MarkFlagRequired("id")
	_ = taskAddCmd.MarkFlagRequired("title")
	return taskAddCmd
}

func newWorkflowTaskUpdateCmd() *cobra.Command {
	var taskUpdateID, taskUpdateNotes, taskUpdateWriteScope, taskUpdateTitle string
	var taskUpdateDependsOn, taskUpdateBlocks string
	taskUpdateCmd := &cobra.Command{
		Use:   "update <plan-id>",
		Short: "Update notes, write-scope, or title for an existing task",
		Example: deps.ExampleBlock(
			"  da workflow task update loop-orchestrator-layer --task phase-5 --notes \"Needs provider-consumer pairing\"",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the canonical plan ID that owns the task."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowTaskUpdate(args[0], taskUpdateID, taskUpdateTitle, taskUpdateNotes, taskUpdateWriteScope, taskUpdateDependsOn, taskUpdateBlocks)
		},
	}
	taskUpdateCmd.Flags().StringVar(&taskUpdateID, "task", "", "Task ID to update (required)")
	taskUpdateCmd.Flags().StringVar(&taskUpdateTitle, "title", "", "New task title")
	taskUpdateCmd.Flags().StringVar(&taskUpdateNotes, "notes", "", "New implementation notes (replaces existing)")
	taskUpdateCmd.Flags().StringVar(&taskUpdateWriteScope, workflowFlagWriteScope, "", "New comma-separated write-scope patterns (replaces existing)")
	taskUpdateCmd.Flags().StringVar(&taskUpdateDependsOn, "depends-on", "", "Comma-separated list of task IDs this task depends on (replaces existing)")
	taskUpdateCmd.Flags().StringVar(&taskUpdateBlocks, "blocks", "", "Comma-separated list of task IDs this task blocks (replaces existing)")
	_ = taskUpdateCmd.MarkFlagRequired("task")
	return taskUpdateCmd
}

func newWorkflowTasksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tasks <plan-id>",
		Short: "Show tasks for a canonical plan",
		Example: deps.ExampleBlock(
			"  da workflow tasks loop-orchestrator-layer",
		),
		Args: deps.ExactArgsWithHints(1, cmdHintCanonicalPlanID),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowTasks(args[0])
		},
	}
}

func newWorkflowSlicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "slices <plan-id>",
		Short: "Show slices for a canonical plan",
		Example: deps.ExampleBlock(
			"  da workflow slices loop-orchestrator-layer",
		),
		Args: deps.ExactArgsWithHints(1, cmdHintCanonicalPlanID),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowSlices(args[0])
		},
	}
}

func newWorkflowEligibleCmd() *cobra.Command {
	var eligiblePlanFilter string
	var eligibleLimit int
	eligibleCmd := &cobra.Command{
		Use:   "eligible",
		Short: "List all unblocked eligible tasks across active plans with conflict detection",
		Example: deps.ExampleBlock(
			"  da workflow eligible",
			"  da workflow eligible --plan loop-agent-pipeline",
			"  da workflow eligible --plan loop-agent-pipeline,resource-command-parity",
			"  da workflow eligible --limit 3",
			"  da --json workflow eligible --plan loop-agent-pipeline",
		),
		Args: deps.NoArgsWithHints("`da workflow eligible` works on the current repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowEligible(eligiblePlanFilter, eligibleLimit)
		},
	}
	eligibleCmd.Flags().StringVar(&eligiblePlanFilter, "plan", "", "Only consider tasks from these canonical plan ids (comma-separated)")
	eligibleCmd.Flags().IntVar(&eligibleLimit, "limit", 0, "Override max_parallel_workers pref (0 = use pref, >0 = explicit limit)")
	return eligibleCmd
}

func newWorkflowSlotsCmd() *cobra.Command {
	var slotsPlanFilter string
	slotsCmd := &cobra.Command{
		Use:   "slots",
		Short: "Show the slot ledger (occupied / awaiting-owner / blocked) across active plans",
		Long: `Renders the layered-PR-fanout slot accounting (design.md §2.8, §3.4.3):
in_progress and awaiting_agent_review occupy a slot against max_parallel_tasks;
awaiting_owner_review and blocked-on:<ref> free the slot. The blocked bucket is
tracked separately so an all-blocked DAG is not mistaken for idle capacity.`,
		Example: deps.ExampleBlock(
			"  da workflow slots",
			"  da workflow slots --plan layered-pr-fanout",
			"  da --json workflow slots",
		),
		Args: deps.NoArgsWithHints("`da workflow slots` works on the current repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowSlots(slotsPlanFilter)
		},
	}
	slotsCmd.Flags().StringVar(&slotsPlanFilter, "plan", "", "Only account tasks from these canonical plan ids (comma-separated)")
	return slotsCmd
}

func newWorkflowNextCmd() *cobra.Command {
	var workflowNextPlanID string
	nextCmd := &cobra.Command{
		Use:   "next",
		Short: "Suggest the next actionable canonical task",
		Example: deps.ExampleBlock(
			"  da workflow next",
			"  da workflow next --plan loop-agent-pipeline",
			"  da workflow next --plan loop-agent-pipeline,resource-command-parity",
		),
		Args: deps.NoArgsWithHints("`da workflow next` works on the current repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowNext(workflowNextPlanID)
		},
	}
	nextCmd.Flags().StringVar(&workflowNextPlanID, "plan", "", "Only consider tasks from this canonical plan id")
	nextCmd.Flags().Lookup("plan").Usage = "Only consider tasks from these canonical plan ids (comma-separated)"
	return nextCmd
}

func newWorkflowCompleteCmd() *cobra.Command {
	var workflowCompletePlanID string
	completeCmd := &cobra.Command{
		Use:   "complete",
		Short: "Probe scoped plan-completion state",
		Example: deps.ExampleBlock(
			"  da workflow complete --plan loop-agent-pipeline",
			"  da --json workflow complete --plan loop-agent-pipeline,resource-command-parity",
		),
		Args: deps.NoArgsWithHints("Use `--plan` to scope plan-completion mode."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(workflowCompletePlanID) == "" {
				return fmt.Errorf("--plan must not be empty")
			}
			return runWorkflowComplete(workflowCompletePlanID)
		},
	}
	completeCmd.Flags().StringVar(&workflowCompletePlanID, "plan", "", "Only consider these canonical plan ids (comma-separated)")
	_ = completeCmd.MarkFlagRequired("plan")
	return completeCmd
}

func newWorkflowAdvanceCmd() *cobra.Command {
	var (
		advanceTask        string
		advanceStatus      string
		advanceCommitState bool
	)
	advanceCmd := &cobra.Command{
		Use:   "advance <plan-id>",
		Short: "Advance a task's status within a canonical plan",
		Example: deps.ExampleBlock(
			"  da workflow advance loop-orchestrator-layer --task phase-5 --status in_progress",
			"  da workflow advance my-plan --task t1 --status completed --commit-state",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the canonical plan ID that owns the task."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runWorkflowAdvance(args[0], advanceTask, advanceStatus); err != nil {
				return err
			}
			if !advanceCommitState {
				return nil
			}
			// Iteration-close integration: stage + commit the workflow-state
			// mutation atomically with the status flip. The auto-commit honors
			// the per-project commit.disable opt-out (handled inside the
			// close-path runWorkflowCommit, not here).
			return iterationCloseCommit(cmd.OutOrStdout())
		},
	}
	advanceCmd.Flags().StringVar(&advanceTask, "task", "", "Task ID to advance (required)")
	advanceCmd.Flags().StringVar(&advanceStatus, "status", "", "New task status (required)")
	advanceCmd.Flags().BoolVar(&advanceCommitState, workflowFlagCommitState, false, "After advancing, stage + commit the workflow-state mutation (iteration-close integration)")
	_ = advanceCmd.MarkFlagRequired("task")
	_ = advanceCmd.MarkFlagRequired("status")
	return advanceCmd
}

func newWorkflowHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Show workflow health snapshot",
		Example: deps.ExampleBlock(
			"  da workflow health",
		),
		Args: deps.NoArgsWithHints("`da workflow health` works on the current repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowHealth()
		},
	}
}

func newWorkflowVerifyCmd() *cobra.Command {
	verifyCmd := &cobra.Command{
		Use:   "verify",
		Short: "Manage verification log",
		Example: deps.ExampleBlock(
			"  da workflow verify record --kind test --status pass --summary \"go test ./...\"",
			"  da workflow verify record --kind review --phase1-decision accept --phase2-decision accept --summary \"LGTM\"",
			"  da workflow verify log",
		),
	}
	verifyCmd.AddCommand(newWorkflowVerifyRecordCmd(), newWorkflowVerifyLogCmd())
	return verifyCmd
}

func newWorkflowVerifyRecordCmd() *cobra.Command {
	var verifyKind, verifyStatus, verifyCommand, verifyScope, verifySummary string
	var reviewPhase1, reviewPhase2, reviewOverall, reviewEscalation, reviewNotes, reviewTask string
	var reviewFailedGates []string
	var verifyVerifierType string
	verifyRecordCmd := &cobra.Command{
		Use:   "record",
		Short: "Record a verification run",
		Example: deps.ExampleBlock(
			"  da workflow verify record --kind test --status pass --command \"go test ./...\" --summary \"all packages passed\"",
			"  da workflow verify record --kind test --status pass --task t1 --verifier-type unit --summary \"go test ./...\"",
			"  da workflow verify record --kind review --phase1-decision accept --phase2-decision accept --summary \"ready to merge\"",
		),
		Args: deps.NoArgsWithHints("Provide verification details through flags such as `--kind`, `--status`, and `--summary`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowVerifyRecordDispatch(verifyRecordDispatchInputs{
				Kind:         verifyKind,
				Status:       verifyStatus,
				Command:      verifyCommand,
				Scope:        verifyScope,
				Summary:      verifySummary,
				Phase1:       reviewPhase1,
				Phase2:       reviewPhase2,
				Overall:      reviewOverall,
				Escalation:   reviewEscalation,
				Notes:        reviewNotes,
				Task:         reviewTask,
				VerifierType: verifyVerifierType,
				FailedGates:  reviewFailedGates,
			})
		},
	}
	verifyRecordCmd.Flags().StringVar(&verifyKind, "kind", "", "Kind: test|lint|build|format|custom|review (required)")
	verifyRecordCmd.Flags().StringVar(&verifyStatus, "status", "", "Status: pass|fail|partial|unknown (required unless --kind review)")
	verifyRecordCmd.Flags().StringVar(&verifyCommand, "command", "", "Command that was run")
	verifyRecordCmd.Flags().StringVar(&verifyScope, "scope", "repo", "Scope: file|package|repo|custom")
	verifyRecordCmd.Flags().StringVar(&verifySummary, "summary", "", "Summary of the run (required)")
	verifyRecordCmd.Flags().StringVar(&reviewPhase1, "phase1-decision", "", "When --kind review: first phase decision (accept|reject|escalate)")
	verifyRecordCmd.Flags().StringVar(&reviewPhase2, "phase2-decision", "", "When --kind review: second phase decision (accept|reject|escalate)")
	verifyRecordCmd.Flags().StringVar(&reviewOverall, "overall-decision", "", "When --kind review: optional; must match derived consolidation from phase decisions if set")
	verifyRecordCmd.Flags().StringSliceVar(&reviewFailedGates, "failed-gate", nil, "When --kind review: failed verifier or gate slug (repeatable)")
	verifyRecordCmd.Flags().StringVar(&reviewEscalation, "escalation-reason", "", "When --kind review: required when overall decision is escalate")
	verifyRecordCmd.Flags().StringVar(&reviewNotes, "reviewer-notes", "", "When --kind review: optional reviewer notes")
	verifyRecordCmd.Flags().StringVar(&reviewTask, "task", "", "Task id for delegation contract lookup (required for --kind review; optional for other kinds to write a typed result artifact)")
	verifyRecordCmd.Flags().StringVar(&verifyVerifierType, workflowFlagVerifierType, "", "Verifier profile id for typed result artifact stem (e.g. unit, api, batch); defaults to --kind when --task is set")
	_ = verifyRecordCmd.MarkFlagRequired("kind")
	_ = verifyRecordCmd.MarkFlagRequired("summary")
	return verifyRecordCmd
}

// verifyRecordDispatchInputs bundles all flags forwarded from the cobra command
// into runWorkflowVerifyRecordDispatch so the function stays under the parameter
// limit while keeping each field individually addressable.
type verifyRecordDispatchInputs struct {
	Kind         string
	Status       string
	Command      string
	Scope        string
	Summary      string
	Phase1       string
	Phase2       string
	Overall      string
	Escalation   string
	Notes        string
	Task         string
	VerifierType string
	FailedGates  []string
}

// runWorkflowVerifyRecordDispatch validates flag combinations and dispatches to the
// appropriate runWorkflowVerifyRecord{Review,} runner.
func runWorkflowVerifyRecordDispatch(in verifyRecordDispatchInputs) error {
	k := strings.TrimSpace(strings.ToLower(in.Kind))
	if k == "" {
		return deps.UsageError(
			"--kind is required",
			"Pass --kind with test, lint, build, format, custom, or review.",
		)
	}
	if strings.TrimSpace(in.Summary) == "" {
		return deps.UsageError(
			"--summary is required",
			"Pass --summary with a short description of the verification outcome.",
		)
	}
	if k == "review" {
		return dispatchVerifyRecordReview(in)
	}
	if strings.TrimSpace(in.Status) == "" {
		return deps.UsageError(
			"--status is required when --kind is not review",
			"Pass --status with pass, fail, partial, or unknown.",
		)
	}
	return runWorkflowVerifyRecord(verifyRecordInputs{
		Kind:         in.Kind,
		Status:       in.Status,
		Command:      in.Command,
		Scope:        in.Scope,
		Summary:      in.Summary,
		TaskID:       in.Task,
		VerifierType: in.VerifierType,
	})
}

// dispatchVerifyRecordReview validates the review-specific flag combination and
// forwards to runWorkflowVerifyRecordReview.
func dispatchVerifyRecordReview(in verifyRecordDispatchInputs) error {
	if strings.TrimSpace(in.Status) != "" {
		return deps.UsageError(
			"--status must not be set when --kind review (status is derived from phase decisions)",
			"Drop --status; for review pass --phase1-decision and --phase2-decision instead.",
		)
	}
	if strings.TrimSpace(in.Phase1) == "" || strings.TrimSpace(in.Phase2) == "" {
		return deps.ErrorWithHints(
			"--phase1-decision and --phase2-decision are required when --kind review",
			"Example: da workflow verify record --kind review --phase1-decision accept --phase2-decision accept --summary \"LGTM\"",
		)
	}
	return runWorkflowVerifyRecordReview(reviewRecordInputs{
		Command:       in.Command,
		Scope:         in.Scope,
		Summary:       in.Summary,
		Phase1In:      in.Phase1,
		Phase2In:      in.Phase2,
		OverallIn:     in.Overall,
		Escalation:    in.Escalation,
		ReviewerNotes: in.Notes,
		TaskFlag:      in.Task,
		FailedGates:   in.FailedGates,
	})
}

func newWorkflowVerifyLogCmd() *cobra.Command {
	var verifyLogAll bool
	verifyLogCmd := &cobra.Command{
		Use:   "log",
		Short: "Show verification log entries",
		Example: deps.ExampleBlock(
			"  da workflow verify log",
			"  da workflow verify log --all",
		),
		Args: deps.NoArgsWithHints("Use `--all` to expand the log instead of passing a positional argument."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowVerifyLog(verifyLogAll)
		},
	}
	verifyLogCmd.Flags().BoolVar(&verifyLogAll, "all", false, "Show all log entries")
	return verifyLogCmd
}

func newWorkflowPrefsCmd() *cobra.Command {
	prefsCmd := &cobra.Command{
		Use:   "prefs",
		Short: "Show resolved workflow preferences",
		Example: deps.ExampleBlock(
			"  da workflow prefs",
			"  da workflow prefs set-local review.depth high",
			"  da workflow prefs set-shared model.default gpt-5.4",
		),
		Args: deps.NoArgsWithHints("Use `set-local` or `set-shared` subcommands to change values."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefs()
		},
	}

	prefsShowCmd := &cobra.Command{
		Use:   "show",
		Short: "Show resolved workflow preferences (alias for prefs)",
		Example: deps.ExampleBlock(
			"  da workflow prefs show",
		),
		Args: deps.NoArgsWithHints("`da workflow prefs show` does not accept positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefs()
		},
	}

	prefsSetLocalCmd := &cobra.Command{
		Use:   "set-local <key> <value>",
		Short: "Set a user-local workflow preference override",
		Example: deps.ExampleBlock(
			"  da workflow prefs set-local review.depth high",
		),
		Args: deps.ExactArgsWithHints(2, "Pass a preference key and the value to store locally."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefsSetLocal(args[0], args[1])
		},
	}

	prefsSetSharedCmd := &cobra.Command{
		Use:   "set-shared <key> <value>",
		Short: "Propose a shared workflow preference change (queued for review)",
		Example: deps.ExampleBlock(
			"  da workflow prefs set-shared model.default gpt-5.4",
		),
		Args: deps.ExactArgsWithHints(2, "Pass a preference key and the value to propose for the shared config."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefsSetShared(args[0], args[1])
		},
	}

	prefsCmd.AddCommand(prefsShowCmd, prefsSetLocalCmd, prefsSetSharedCmd)
	return prefsCmd
}

func newWorkflowGraphCmd() *cobra.Command {
	graphCmd := &cobra.Command{
		Use:   "graph",
		Short: "Query knowledge graph context",
		Example: deps.ExampleBlock(
			"  da workflow graph query --intent plan_context \"loop orchestrator\"",
			"  da workflow graph health",
		),
	}
	graphQueryCmd := &cobra.Command{
		Use:   "query [query string]",
		Short: "Query graph context by bridge intent",
		Example: deps.ExampleBlock(
			"  da workflow graph query --intent plan_context \"loop orchestrator\"",
			"  da workflow graph query --intent contradictions \"resource plan\"",
		),
		RunE: runWorkflowGraphQuery,
	}
	graphQueryCmd.Flags().String("intent", "", "Bridge intent: plan_context|decision_lookup|entity_context|workflow_memory|contradictions; code-structure intents are forwarded to `da kg bridge query`")
	graphQueryCmd.Flags().String("scope", "", "Optional scope filter")

	graphHealthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show graph bridge adapter health",
		Example: deps.ExampleBlock(
			"  da workflow graph health",
		),
		Args: deps.NoArgsWithHints("`da workflow graph health` reports the current repository bridge state."),
		RunE: runWorkflowGraphHealth,
	}
	graphCmd.AddCommand(graphQueryCmd, graphHealthCmd)
	return graphCmd
}

func newWorkflowFanoutCmd() *cobra.Command {
	fanoutCmd := &cobra.Command{
		Use:   "fanout",
		Short: "Delegate a task to a sub-agent with a bounded write scope",
		Example: deps.ExampleBlock(
			"  da workflow fanout --plan loop-orchestrator-layer --task phase-5 --owner transport-worker --write-scope internal/platform",
			"  da workflow fanout --plan loop-orchestrator-layer --slice phase-5-transport",
		),
		Args: deps.NoArgsWithHints("Use `--plan`, `--task`, and related flags instead of positional arguments."),
		RunE: runWorkflowFanout,
	}
	fanoutCmd.Flags().String("plan", "", cmdFlagCanonicalPlanIDDescr)
	fanoutCmd.Flags().String("task", "", "Task ID to delegate (required)")
	fanoutCmd.Flags().String("slice", "", "Slice ID from SLICES.yaml; auto-fills task and write scope")
	fanoutCmd.Flags().String("owner", "", "Delegate agent identity")
	fanoutCmd.Flags().String(workflowFlagWriteScope, "", "Comma-separated file/dir patterns this delegate may touch")
	fanoutCmd.Flags().Bool("with-tests", false, "Auto-add each .go write_scope entry's sibling _test.go path (same directory, create-if-absent — the file need not exist yet)")
	fanoutCmd.Flags().String("delegate-profile", defaultDelegateProfile, "Worker profile label stored in the delegation bundle")
	fanoutCmd.Flags().StringSlice("project-overlay", nil, "Repeatable repo-relative project overlay guidance files")
	fanoutCmd.Flags().StringSlice("prompt", nil, "Repeatable inline prompt lines for the delegate")
	fanoutCmd.Flags().StringSlice("prompt-file", nil, "Repeatable repo-relative prompt files")
	fanoutCmd.Flags().StringSlice("context-file", nil, "Repeatable repo-relative context files (bundle context.required_files)")
	fanoutCmd.Flags().String("feedback-goal", "", "Verification question this delegation must answer (default if empty)")
	fanoutCmd.Flags().StringSlice("scenario-tag", nil, "Repeatable scenario / coverage tags")
	fanoutCmd.Flags().StringSlice("regression-artifact", nil, "Repeatable regression matrix or artifact paths")
	fanoutCmd.Flags().String("validation-queue", "", "Higher-layer validation queue file path")
	fanoutCmd.Flags().String("selection-reason", "", "Optional human-readable reason this task was delegated")
	fanoutCmd.Flags().Bool("require-negative-coverage", false, "Set verification.evidence_policy.require_negative_coverage in the bundle")
	fanoutCmd.Flags().Bool("sandbox-mutations", false, "Set verification.evidence_policy.sandbox_mutations in the bundle")
	fanoutCmd.Flags().Int("verifier-retry-max", 0, "If > 0, set verification.evidence_policy.verifier_chain_max (verifier retry budget)")
	fanoutCmd.Flags().Int("lens-retry-max", 0, "If > 0, set verification.evidence_policy.lens_chain_max (per-lens review retry budget)")
	fanoutCmd.Flags().String("verifier-sequence", "", "Comma-separated verifier profile ids (overrides app_type resolution from .agentsrc.json)")
	fanoutCmd.Flags().Bool("skip-tdd-gate", false, "Skip pre-verifier check that Go write_scope has *_test.go coverage")
	fanoutCmd.Flags().Bool("skip-evidence-check", false, "Suppress scope-evidence sidecar warnings (missing sidecar or low confidence)")
	fanoutCmd.Flags().Bool("skip-asserting-test-gate", false, "Skip §0d asserting-test-scope gate (for doc-only or deliberate cross-scope changes)")
	fanoutCmd.Flags().String("base-branch", "", "Override layered base-resolution (§4): branch the new bundle off this base instead of the resolved/master default; required to sequence multiple in-flight dep PRs (read by fanoutResolveBase)")
	_ = fanoutCmd.MarkFlagRequired("plan")
	return fanoutCmd
}

func newWorkflowMergeBackCmd() *cobra.Command {
	mergeBackCmd := &cobra.Command{
		Use:   "merge-back",
		Short: "Record a sub-agent's completed work as a merge-back artifact",
		Example: deps.ExampleBlock(
			"  da workflow merge-back --task phase-5 --summary \"worker finished transport slice\" --verification-status pass",
			"  da workflow merge-back --task t1 --summary \"...\" --commit-state",
		),
		Args: deps.NoArgsWithHints("Use `--task` and `--summary` flags instead of positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runWorkflowMergeBack(cmd, args); err != nil {
				return err
			}
			commitState, _ := cmd.Flags().GetBool(workflowFlagCommitState)
			if !commitState {
				return nil
			}
			// Iteration-close integration for delegated workers: the merge-
			// back artifact + the workflow-state commit land atomically. The
			// auto-commit honors the per-project commit.disable opt-out.
			return iterationCloseCommit(cmd.OutOrStdout())
		},
	}
	mergeBackCmd.Flags().String("task", "", "Task ID that was delegated (required)")
	mergeBackCmd.Flags().String("summary", "", "Summary of what was done (required)")
	mergeBackCmd.Flags().String("verification-status", "unknown", "pass|fail|partial|unknown")
	mergeBackCmd.Flags().String("integration-notes", "", "Guidance for the parent agent")
	mergeBackCmd.Flags().Bool(workflowFlagCommitState, false, "After recording the merge-back, stage + commit the workflow-state mutation (iteration-close integration)")
	_ = mergeBackCmd.MarkFlagRequired("task")
	_ = mergeBackCmd.MarkFlagRequired("summary")
	return mergeBackCmd
}

func newWorkflowFoldBackCmd() *cobra.Command {
	foldBackCmd := &cobra.Command{
		Use:   "fold-back",
		Short: "Route loop observations into durable plan artifacts or proposals",
		Long:  `Records loop observations and routes them into TASKS.yaml notes, plan summary, or a ~/.agents proposal file.`,
	}
	foldBackCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Record and route a loop observation",
		Example: deps.ExampleBlock(
			"  da workflow fold-back create --plan my-plan --task my-task --observation \"API edge case\"",
			"  da workflow fold-back create --plan my-plan --observation \"plan-level note\"",
			"  da workflow fold-back create --plan my-plan --task my-task --observation \"needs design\" --propose",
		),
		Args: deps.NoArgsWithHints("Use `--plan` and `--observation` flags instead of positional arguments."),
		RunE: runWorkflowFoldBackCreate,
	}
	foldBackCreateCmd.Flags().String("plan", "", cmdFlagCanonicalPlanIDDescr)
	foldBackCreateCmd.Flags().String("task", "", "Task ID to append note to (optional)")
	foldBackCreateCmd.Flags().String("observation", "", "Observation text (required)")
	foldBackCreateCmd.Flags().String("slug", "", "Stable id for create-or-update (D2.a); one tagged line per slug in TASKS/plan notes")
	foldBackCreateCmd.Flags().Bool("propose", false, "Route as proposal rather than inline task note")
	foldBackCreateCmd.Flags().StringSlice("write-scope", nil, "Declared diff-scope path(s) the proposal writes into (repeatable; proposal only)")
	foldBackCreateCmd.Flags().Bool("dry-run", false, "Print the routing decision and paths without writing anything")
	_ = foldBackCreateCmd.MarkFlagRequired("plan")
	_ = foldBackCreateCmd.MarkFlagRequired("observation")
	foldBackUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "Refine an existing slug-scoped fold-back observation",
		Example: deps.ExampleBlock(
			"  da workflow fold-back update --plan my-plan --slug schema-drift-my-plan --observation \"refined note\"",
			"  da workflow fold-back update --plan my-plan --slug coverage-regression-my-plan-t1 --task t1 --observation \"latest\"",
		),
		Args: deps.NoArgsWithHints("Requires --plan, --slug, and --observation."),
		RunE: runWorkflowFoldBackUpdate,
	}
	foldBackUpdateCmd.Flags().String("plan", "", cmdFlagCanonicalPlanIDDescr)
	foldBackUpdateCmd.Flags().String("slug", "", "Stable fold-back id (required; must match an existing artifact)")
	foldBackUpdateCmd.Flags().String("task", "", "Task ID (required when the existing fold-back is task-scoped)")
	foldBackUpdateCmd.Flags().String("observation", "", "Replacement observation text (required)")
	_ = foldBackUpdateCmd.MarkFlagRequired("plan")
	_ = foldBackUpdateCmd.MarkFlagRequired("slug")
	_ = foldBackUpdateCmd.MarkFlagRequired("observation")
	foldBackListCmd := &cobra.Command{
		Use:   "list",
		Short: "List recorded fold-back observations",
		Example: deps.ExampleBlock(
			"  da workflow fold-back list",
			"  da workflow fold-back list --plan my-plan",
		),
		Args: deps.NoArgsWithHints("Use `--plan` to filter by canonical plan ID."),
		RunE: runWorkflowFoldBackList,
	}
	foldBackListCmd.Flags().String("plan", "", "Filter by canonical plan ID")
	foldBackCmd.AddCommand(foldBackCreateCmd, foldBackUpdateCmd, foldBackListCmd)
	return foldBackCmd
}

func newWorkflowDelegationCmd() *cobra.Command {
	delegationCmd := &cobra.Command{
		Use:   "delegation",
		Short: "Parent-driven delegation lifecycle helpers",
	}
	delegationCloseoutCmd := &cobra.Command{
		Use:   "closeout",
		Short: "Archive completed merge-back artifacts and reconcile canonical task state",
		Example: deps.ExampleBlock(
			"  da workflow delegation closeout --plan my-plan --task my-task --decision accept",
			"  da workflow delegation closeout --plan my-plan --task my-task --decision reject --note \"rework error handling\"",
		),
		Args: deps.NoArgsWithHints("Use `--plan`, `--task`, and `--decision` flags instead of positional arguments."),
		RunE: runWorkflowDelegationCloseout,
	}
	delegationCloseoutCmd.Flags().String("plan", "", cmdFlagCanonicalPlanIDDescr)
	delegationCloseoutCmd.Flags().String("task", "", "Delegated task ID (required)")
	delegationCloseoutCmd.Flags().String("decision", "", "accept|reject — parent integration decision (required)")
	delegationCloseoutCmd.Flags().String("note", "", "Optional note (typically used with --decision reject)")
	_ = delegationCloseoutCmd.MarkFlagRequired("plan")
	_ = delegationCloseoutCmd.MarkFlagRequired("task")
	_ = delegationCloseoutCmd.MarkFlagRequired("decision")
	delegationGateCmd := &cobra.Command{
		Use:   "gate",
		Short: "Evaluate task-local review evidence into an accept/reject/escalate parent-gate outcome",
		Example: deps.ExampleBlock(
			"  da workflow delegation gate --task my-task",
			"  da --json workflow delegation gate --plan my-plan --task my-task",
		),
		Args: deps.NoArgsWithHints("Use `--task` and optional `--plan` instead of positional arguments."),
		RunE: runWorkflowDelegationGate,
	}
	delegationGateCmd.Flags().String("plan", "", "Canonical plan ID (optional; validated against the delegation contract when set)")
	delegationGateCmd.Flags().String("task", "", "Delegated task ID (required)")
	_ = delegationGateCmd.MarkFlagRequired("task")
	delegationCmd.AddCommand(delegationCloseoutCmd, delegationGateCmd)
	return delegationCmd
}

func newWorkflowDriftCmd() *cobra.Command {
	driftCmd := &cobra.Command{
		Use:   "drift",
		Short: "Detect workflow drift across managed repos (read-only)",
		Example: deps.ExampleBlock(
			"  da workflow drift",
			"  da workflow drift --project billing-api",
		),
		Args: deps.NoArgsWithHints("Use flags such as `--project` instead of positional arguments."),
		RunE: runWorkflowDrift,
	}
	driftCmd.Flags().Int("stale-days", defaultCheckpointStaleDays, "Checkpoint staleness threshold in days")
	driftCmd.Flags().Int("proposal-days", defaultProposalStaleDays, "Proposal staleness threshold in days")
	driftCmd.Flags().String("project", "", "Check only this project (by name)")
	return driftCmd
}

func newWorkflowSweepCmd() *cobra.Command {
	sweepCmd := &cobra.Command{
		Use:   "sweep",
		Short: "Plan and optionally apply fixes for workflow drift across managed repos",
		Example: deps.ExampleBlock(
			"  da workflow sweep",
			"  da workflow sweep --apply",
		),
		Args: deps.NoArgsWithHints("Use flags such as `--apply` instead of positional arguments."),
		RunE: runWorkflowSweep,
	}
	sweepCmd.Flags().Int("stale-days", defaultCheckpointStaleDays, "Checkpoint staleness threshold in days")
	sweepCmd.Flags().Int("proposal-days", defaultProposalStaleDays, "Proposal staleness threshold in days")
	sweepCmd.Flags().Bool("apply", false, "Execute sweep actions (default is dry-run)")
	return sweepCmd
}

func newWorkflowBundleCmd() *cobra.Command {
	bundleCmd := &cobra.Command{
		Use:   "bundle",
		Short: "Inspect delegation bundle artifacts",
		Example: deps.ExampleBlock(
			"  da workflow bundle stages .agents/active/delegation-bundles/del-task-001.yaml",
		),
	}
	bundleStagesCmd := &cobra.Command{
		Use:   "stages <bundle-path>",
		Short: "Expand a delegation bundle into the ordered impl → verifier(s) → review stage list",
		Example: deps.ExampleBlock(
			"  da workflow bundle stages .agents/active/delegation-bundles/del-task-001.yaml",
			"  da --json workflow bundle stages <bundle-path>",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the path to a delegation bundle YAML file."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowBundleStages(args[0])
		},
	}
	bundleCmd.AddCommand(bundleStagesCmd)
	return bundleCmd
}

func newWorkflowAppTypesCmd() *cobra.Command {
	var format string
	var verbose bool
	cmd := &cobra.Command{
		Use:   "app-types",
		Short: "List available app_type values for the current repo",
		Example: deps.ExampleBlock(
			"  da workflow app-types",
			"  da --json workflow app-types",
			"  da workflow app-types --verbose",
			"  da workflow app-types --format flag",
		),
		Args: deps.NoArgsWithHints("Run workflow app-types from inside the project repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowAppTypes(format, verbose)
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "Print only the recommended authoring snippet: flag, task, plan, or doc")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show source and recommendation details for each app_type")
	return cmd
}

func newWorkflowStateRefCmd() *cobra.Command {
	stateRefCmd := &cobra.Command{
		Use:   "state-ref",
		Short: "Inspect and reconcile the machine coordination state ref (refs/agents/state)",
	}
	stateRefCmd.AddCommand(newWorkflowStateRefReconcileCmd())
	return stateRefCmd
}

func newWorkflowStateRefReconcileCmd() *cobra.Command {
	var dryRun, asJSON bool
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Re-mirror every plan's working-copy coordination state onto refs/agents/state",
		Long: `Makes refs/agents/state a faithful mirror of every plan's working-copy
coordination state. Reads each plan's working-copy TASKS.yaml/PLAN.yaml directly
and re-runs the shipped seed mirror so every working-copy task has a per-task
blob on the ref, closing the stale-subset gap left by plans transitioned before
the choke-point mirror shipped. Reconciles TO the ref FROM the working copy
regardless of the active work_tracking backend and is idempotent: an
already-consistent run makes no new ref commit.`,
		Example: deps.ExampleBlock(
			"  da workflow state-ref reconcile",
			"  da workflow state-ref reconcile --dry-run",
			"  da workflow state-ref reconcile --json",
		),
		Args: deps.NoArgsWithHints("Run workflow state-ref reconcile from inside the project repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := stateRefReconcileOpts{dryRun: dryRun || deps.Flags.DryRun(), json: asJSON}
			return runWorkflowStateRefReconcile(cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be seeded without writing to the ref")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit the reconcile report as JSON (also honours the global --json flag)")
	return cmd
}

// NewCmd builds the `da workflow` command tree. Callers must supply Deps from package commands to avoid an import cycle.
func NewCmd(d Deps) *cobra.Command {
	deps = d
	return newWorkflowCmd()
}

// NewCmdForTest returns the workflow command tree using deps registered by InitTestDeps (see workflow_testutil_test.go init).
func NewCmdForTest() *cobra.Command {
	return newWorkflowCmd()
}
