package workflow

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newWorkflowCmd() *cobra.Command {
	var (
		checkpointMessage               string
		checkpointVerificationState     string
		checkpointVerificationText      string
		checkpointLogToIter             int
		checkpointLogToIterRole         string
		checkpointLogToIterVerifierType string
		logAll                          bool
	)

	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Inspect and persist workflow state",
		Long: `Captures the repository-local workflow state that helps both humans and
AI agents resume work safely: canonical plans, checkpoints, verification logs,
preferences, fanout artifacts, and bridge queries.`,
		Example: deps.ExampleBlock(
			"  dot-agents workflow status",
			"  dot-agents workflow orient",
			"  dot-agents workflow next",
			"  dot-agents workflow checkpoint --message \"Resume transport slice\"",
		),
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show workflow state for the current project",
		Example: deps.ExampleBlock(
			"  dot-agents workflow status",
			"  dot-agents --json workflow status",
		),
		Args: deps.NoArgsWithHints("Run workflow status from inside the project repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowStatus()
		},
	}

	orientCmd := &cobra.Command{
		Use:   "orient",
		Short: "Render session orient context for the current project",
		Example: deps.ExampleBlock(
			"  dot-agents workflow orient",
		),
		Args: deps.NoArgsWithHints("Run workflow orient from inside the project repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowOrient()
		},
	}

	checkpointCmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Write a checkpoint for the current project",
		Example: deps.ExampleBlock(
			"  dot-agents workflow checkpoint --message \"Resume plan graph work\"",
			"  dot-agents workflow checkpoint --verification-status pass --verification-summary \"go test ./...\"",
		),
		Args: deps.NoArgsWithHints("Use flags such as `--message` instead of positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("role") || cmd.Flags().Changed("verifier-type") {
				if !cmd.Flags().Changed("log-to-iter") {
					return fmt.Errorf("--role and --verifier-type require --log-to-iter")
				}
			}
			if cmd.Flags().Changed("log-to-iter") {
				if checkpointLogToIter < 1 {
					return fmt.Errorf("checkpoint --log-to-iter requires N >= 1 (schema workflow-iter-log enforces iteration.minimum: 1)")
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
	checkpointCmd.Flags().IntVar(&checkpointLogToIter, "log-to-iter", 0, "Write a schema-validated iteration log stub for N (>=1) to .agents/active/iteration-log/iter-N.yaml")
	checkpointCmd.Flags().StringVar(&checkpointLogToIterRole, "role", "", "With --log-to-iter: merge only the impl, verifier, or review block")
	checkpointCmd.Flags().StringVar(&checkpointLogToIterVerifierType, "verifier-type", "", "Verifier slug when --role verifier (for example unit)")

	logCmd := &cobra.Command{
		Use:   "log",
		Short: "Show recent checkpoint log entries",
		Example: deps.ExampleBlock(
			"  dot-agents workflow log",
			"  dot-agents workflow log --all",
		),
		Args: deps.NoArgsWithHints("Use `--all` to expand the log instead of passing a positional argument."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowLog(logAll)
		},
	}
	logCmd.Flags().BoolVar(&logAll, "all", false, "Show all log entries")

	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "List canonical plans",
		Example: deps.ExampleBlock(
			"  dot-agents workflow plan",
			"  dot-agents workflow plan show loop-orchestrator-layer",
			"  dot-agents workflow plan graph",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanList()
		},
	}
	planShowCmd := &cobra.Command{
		Use:   "show <plan-id>",
		Short: "Show details of a canonical plan",
		Example: deps.ExampleBlock(
			"  dot-agents workflow plan show loop-orchestrator-layer",
		),
		Args: deps.ExactArgsWithHints(1, "Pass a canonical plan ID from `dot-agents workflow plan`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanShow(args[0])
		},
	}
	planGraphCmd := &cobra.Command{
		Use:   "graph [plan-id]",
		Short: "Render a derived graph of canonical plans and tasks",
		Example: deps.ExampleBlock(
			"  dot-agents workflow plan graph",
			"  dot-agents workflow plan graph loop-orchestrator-layer",
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
	var planCreateTitle, planCreateSummary, planCreateOwner string
	planCreateCmd := &cobra.Command{
		Use:   "create <plan-id>",
		Short: "Create a new canonical plan directory with PLAN.yaml and TASKS.yaml stubs",
		Example: deps.ExampleBlock(
			"  dot-agents workflow plan create repo-cleanup --title \"Repository cleanup\" --summary \"Normalize stale plans\"",
		),
		Args: deps.ExactArgsWithHints(1, "Pass a new canonical plan ID such as `repo-cleanup`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanCreate(args[0], planCreateTitle, planCreateSummary, planCreateOwner)
		},
	}
	planCreateCmd.Flags().StringVar(&planCreateTitle, "title", "", "Plan title (required)")
	planCreateCmd.Flags().StringVar(&planCreateSummary, "summary", "", "Short summary of the plan goal")
	planCreateCmd.Flags().StringVar(&planCreateOwner, "owner", "dot-agents", "Plan owner")
	_ = planCreateCmd.MarkFlagRequired("title")

	var planUpdateStatus, planUpdateTitle, planUpdateSummary, planUpdateFocus string
	planUpdateCmd := &cobra.Command{
		Use:   "update <plan-id>",
		Short: "Update PLAN.yaml metadata fields",
		Example: deps.ExampleBlock(
			"  dot-agents workflow plan update repo-cleanup --status active --focus task-triage",
		),
		Args: deps.ExactArgsWithHints(1, "Pass an existing canonical plan ID."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanUpdate(args[0], planUpdateStatus, planUpdateTitle, planUpdateSummary, planUpdateFocus)
		},
	}
	planUpdateCmd.Flags().StringVar(&planUpdateStatus, "status", "", "New plan status (draft|active|paused|completed|archived)")
	planUpdateCmd.Flags().StringVar(&planUpdateTitle, "title", "", "New plan title")
	planUpdateCmd.Flags().StringVar(&planUpdateSummary, "summary", "", "New plan summary")
	planUpdateCmd.Flags().StringVar(&planUpdateFocus, "focus", "", "New current_focus_task value")

	planCmd.AddCommand(planShowCmd, planGraphCmd, planCreateCmd, planUpdateCmd)

	taskCmd := &cobra.Command{
		Use:   "task",
		Short: "Add or update tasks within a canonical plan",
		Example: deps.ExampleBlock(
			"  dot-agents workflow task add loop-orchestrator-layer --id phase-5 --title \"Transport cleanup\"",
			"  dot-agents workflow task update loop-orchestrator-layer --task phase-5 --write-scope internal/platform",
		),
	}
	var taskAddID, taskAddTitle, taskAddNotes, taskAddOwner, taskAddDependsOn, taskAddBlocks, taskAddWriteScope string
	var taskAddVerification bool
	taskAddCmd := &cobra.Command{
		Use:   "add <plan-id>",
		Short: "Append a new task to a canonical plan's TASKS.yaml",
		Example: deps.ExampleBlock(
			"  dot-agents workflow task add loop-orchestrator-layer --id phase-5 --title \"Transport cleanup\"",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the canonical plan ID that should receive the new task."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowTaskAdd(args[0], taskAddID, taskAddTitle, taskAddNotes, taskAddOwner, taskAddDependsOn, taskAddBlocks, taskAddWriteScope, taskAddVerification)
		},
	}
	taskAddCmd.Flags().StringVar(&taskAddID, "id", "", "Task ID (required)")
	taskAddCmd.Flags().StringVar(&taskAddTitle, "title", "", "Task title (required)")
	taskAddCmd.Flags().StringVar(&taskAddNotes, "notes", "", "Implementation notes")
	taskAddCmd.Flags().StringVar(&taskAddOwner, "owner", "dot-agents", "Task owner")
	taskAddCmd.Flags().StringVar(&taskAddDependsOn, "depends-on", "", "Comma-separated list of task IDs this task depends on")
	taskAddCmd.Flags().StringVar(&taskAddBlocks, "blocks", "", "Comma-separated list of task IDs this task blocks")
	taskAddCmd.Flags().StringVar(&taskAddWriteScope, "write-scope", "", "Comma-separated file/dir patterns this task may touch")
	taskAddCmd.Flags().BoolVar(&taskAddVerification, "verification-required", true, "Whether verification is required before marking complete")
	_ = taskAddCmd.MarkFlagRequired("id")
	_ = taskAddCmd.MarkFlagRequired("title")

	var taskUpdateID, taskUpdateNotes, taskUpdateWriteScope, taskUpdateTitle string
	taskUpdateCmd := &cobra.Command{
		Use:   "update <plan-id>",
		Short: "Update notes, write-scope, or title for an existing task",
		Example: deps.ExampleBlock(
			"  dot-agents workflow task update loop-orchestrator-layer --task phase-5 --notes \"Needs provider-consumer pairing\"",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the canonical plan ID that owns the task."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowTaskUpdate(args[0], taskUpdateID, taskUpdateTitle, taskUpdateNotes, taskUpdateWriteScope)
		},
	}
	taskUpdateCmd.Flags().StringVar(&taskUpdateID, "task", "", "Task ID to update (required)")
	taskUpdateCmd.Flags().StringVar(&taskUpdateTitle, "title", "", "New task title")
	taskUpdateCmd.Flags().StringVar(&taskUpdateNotes, "notes", "", "New implementation notes (replaces existing)")
	taskUpdateCmd.Flags().StringVar(&taskUpdateWriteScope, "write-scope", "", "New comma-separated write-scope patterns (replaces existing)")
	_ = taskUpdateCmd.MarkFlagRequired("task")

	taskCmd.AddCommand(taskAddCmd, taskUpdateCmd)

	tasksCmd := &cobra.Command{
		Use:   "tasks <plan-id>",
		Short: "Show tasks for a canonical plan",
		Example: deps.ExampleBlock(
			"  dot-agents workflow tasks loop-orchestrator-layer",
		),
		Args: deps.ExactArgsWithHints(1, "Pass a canonical plan ID from `dot-agents workflow plan`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowTasks(args[0])
		},
	}

	slicesCmd := &cobra.Command{
		Use:   "slices <plan-id>",
		Short: "Show slices for a canonical plan",
		Example: deps.ExampleBlock(
			"  dot-agents workflow slices loop-orchestrator-layer",
		),
		Args: deps.ExactArgsWithHints(1, "Pass a canonical plan ID from `dot-agents workflow plan`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowSlices(args[0])
		},
	}

	var workflowNextPlanID string
	nextCmd := &cobra.Command{
		Use:   "next",
		Short: "Suggest the next actionable canonical task",
		Example: deps.ExampleBlock(
			"  dot-agents workflow next",
			"  dot-agents workflow next --plan loop-agent-pipeline",
		),
		Args: deps.NoArgsWithHints("`dot-agents workflow next` works on the current repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowNext(workflowNextPlanID)
		},
	}
	nextCmd.Flags().StringVar(&workflowNextPlanID, "plan", "", "Only consider tasks from this canonical plan id")

	var advanceTask, advanceStatus string
	advanceCmd := &cobra.Command{
		Use:   "advance <plan-id>",
		Short: "Advance a task's status within a canonical plan",
		Example: deps.ExampleBlock(
			"  dot-agents workflow advance loop-orchestrator-layer --task phase-5 --status in_progress",
		),
		Args: deps.ExactArgsWithHints(1, "Pass the canonical plan ID that owns the task."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowAdvance(args[0], advanceTask, advanceStatus)
		},
	}
	advanceCmd.Flags().StringVar(&advanceTask, "task", "", "Task ID to advance (required)")
	advanceCmd.Flags().StringVar(&advanceStatus, "status", "", "New task status (required)")
	_ = advanceCmd.MarkFlagRequired("task")
	_ = advanceCmd.MarkFlagRequired("status")

	healthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show workflow health snapshot",
		Example: deps.ExampleBlock(
			"  dot-agents workflow health",
		),
		Args: deps.NoArgsWithHints("`dot-agents workflow health` works on the current repository."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowHealth()
		},
	}

	verifyCmd := &cobra.Command{
		Use:   "verify",
		Short: "Manage verification log",
		Example: deps.ExampleBlock(
			"  dot-agents workflow verify record --kind test --status pass --summary \"go test ./...\"",
			"  dot-agents workflow verify record --kind review --phase1-decision accept --phase2-decision accept --summary \"LGTM\"",
			"  dot-agents workflow verify log",
		),
	}
	var verifyKind, verifyStatus, verifyCommand, verifyScope, verifySummary string
	var reviewPhase1, reviewPhase2, reviewOverall, reviewEscalation, reviewNotes, reviewTask string
	var reviewFailedGates []string
	verifyRecordCmd := &cobra.Command{
		Use:   "record",
		Short: "Record a verification run",
		Example: deps.ExampleBlock(
			"  dot-agents workflow verify record --kind test --status pass --command \"go test ./...\" --summary \"all packages passed\"",
			"  dot-agents workflow verify record --kind review --phase1-decision accept --phase2-decision accept --summary \"ready to merge\"",
		),
		Args: deps.NoArgsWithHints("Provide verification details through flags such as `--kind`, `--status`, and `--summary`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			k := strings.TrimSpace(strings.ToLower(verifyKind))
			if k == "" {
				return fmt.Errorf("--kind is required")
			}
			if strings.TrimSpace(verifySummary) == "" {
				return fmt.Errorf("--summary is required")
			}
			if k == "review" {
				if strings.TrimSpace(verifyStatus) != "" {
					return fmt.Errorf("--status must not be set when --kind review (status is derived from phase decisions)")
				}
				if strings.TrimSpace(reviewPhase1) == "" || strings.TrimSpace(reviewPhase2) == "" {
					return deps.ErrorWithHints(
						"--phase1-decision and --phase2-decision are required when --kind review",
						"Example: dot-agents workflow verify record --kind review --phase1-decision accept --phase2-decision accept --summary \"LGTM\"",
					)
				}
				return runWorkflowVerifyRecordReview(verifyCommand, verifyScope, verifySummary, reviewPhase1, reviewPhase2, reviewOverall, reviewEscalation, reviewNotes, reviewTask, reviewFailedGates)
			}
			if strings.TrimSpace(verifyStatus) == "" {
				return fmt.Errorf("--status is required when --kind is not review")
			}
			return runWorkflowVerifyRecord(verifyKind, verifyStatus, verifyCommand, verifyScope, verifySummary)
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
	verifyRecordCmd.Flags().StringVar(&reviewTask, "task", "", "When --kind review: task id under .agents/active/delegation/<task>.yaml (defaults to sole active delegation)")
	_ = verifyRecordCmd.MarkFlagRequired("kind")
	_ = verifyRecordCmd.MarkFlagRequired("summary")

	var verifyLogAll bool
	verifyLogCmd := &cobra.Command{
		Use:   "log",
		Short: "Show verification log entries",
		Example: deps.ExampleBlock(
			"  dot-agents workflow verify log",
			"  dot-agents workflow verify log --all",
		),
		Args: deps.NoArgsWithHints("Use `--all` to expand the log instead of passing a positional argument."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowVerifyLog(verifyLogAll)
		},
	}
	verifyLogCmd.Flags().BoolVar(&verifyLogAll, "all", false, "Show all log entries")

	verifyCmd.AddCommand(verifyRecordCmd, verifyLogCmd)

	prefsCmd := &cobra.Command{
		Use:   "prefs",
		Short: "Show resolved workflow preferences",
		Example: deps.ExampleBlock(
			"  dot-agents workflow prefs",
			"  dot-agents workflow prefs set-local review.depth high",
			"  dot-agents workflow prefs set-shared model.default gpt-5.4",
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
			"  dot-agents workflow prefs show",
		),
		Args: deps.NoArgsWithHints("`dot-agents workflow prefs show` does not accept positional arguments."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefs()
		},
	}

	prefsSetLocalCmd := &cobra.Command{
		Use:   "set-local <key> <value>",
		Short: "Set a user-local workflow preference override",
		Example: deps.ExampleBlock(
			"  dot-agents workflow prefs set-local review.depth high",
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
			"  dot-agents workflow prefs set-shared model.default gpt-5.4",
		),
		Args: deps.ExactArgsWithHints(2, "Pass a preference key and the value to propose for the shared config."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefsSetShared(args[0], args[1])
		},
	}

	prefsCmd.AddCommand(prefsShowCmd, prefsSetLocalCmd, prefsSetSharedCmd)

	graphCmd := &cobra.Command{
		Use:   "graph",
		Short: "Query knowledge graph context",
		Example: deps.ExampleBlock(
			"  dot-agents workflow graph query --intent plan_context \"loop orchestrator\"",
			"  dot-agents workflow graph health",
		),
	}
	graphQueryCmd := &cobra.Command{
		Use:   "query [query string]",
		Short: "Query graph context by bridge intent",
		Example: deps.ExampleBlock(
			"  dot-agents workflow graph query --intent plan_context \"loop orchestrator\"",
			"  dot-agents workflow graph query --intent contradictions \"resource plan\"",
		),
		RunE: runWorkflowGraphQuery,
	}
	graphQueryCmd.Flags().String("intent", "", "Bridge intent: plan_context|decision_lookup|entity_context|workflow_memory|contradictions; code-structure intents are forwarded to `dot-agents kg bridge query`")
	graphQueryCmd.Flags().String("scope", "", "Optional scope filter")

	graphHealthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show graph bridge adapter health",
		Example: deps.ExampleBlock(
			"  dot-agents workflow graph health",
		),
		Args: deps.NoArgsWithHints("`dot-agents workflow graph health` reports the current repository bridge state."),
		RunE: runWorkflowGraphHealth,
	}
	graphCmd.AddCommand(graphQueryCmd, graphHealthCmd)

	fanoutCmd := &cobra.Command{
		Use:   "fanout",
		Short: "Delegate a task to a sub-agent with a bounded write scope",
		Example: deps.ExampleBlock(
			"  dot-agents workflow fanout --plan loop-orchestrator-layer --task phase-5 --owner transport-worker --write-scope internal/platform",
			"  dot-agents workflow fanout --plan loop-orchestrator-layer --slice phase-5-transport",
		),
		Args: deps.NoArgsWithHints("Use `--plan`, `--task`, and related flags instead of positional arguments."),
		RunE: runWorkflowFanout,
	}
	fanoutCmd.Flags().String("plan", "", "Canonical plan ID (required)")
	fanoutCmd.Flags().String("task", "", "Task ID to delegate (required)")
	fanoutCmd.Flags().String("slice", "", "Slice ID from SLICES.yaml; auto-fills task and write scope")
	fanoutCmd.Flags().String("owner", "", "Delegate agent identity")
	fanoutCmd.Flags().String("write-scope", "", "Comma-separated file/dir patterns this delegate may touch")
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
	fanoutCmd.Flags().Int("verifier-retry-max", 0, "If > 0, set verification.evidence_policy.primary_chain_max (verifier retry budget)")
	fanoutCmd.Flags().String("verifier-sequence", "", "Comma-separated verifier profile ids (overrides app_type resolution from .agentsrc.json)")
	fanoutCmd.Flags().Bool("skip-tdd-gate", false, "Skip pre-verifier check that Go write_scope has *_test.go coverage")
	_ = fanoutCmd.MarkFlagRequired("plan")

	mergeBackCmd := &cobra.Command{
		Use:   "merge-back",
		Short: "Record a sub-agent's completed work as a merge-back artifact",
		Example: deps.ExampleBlock(
			"  dot-agents workflow merge-back --task phase-5 --summary \"worker finished transport slice\" --verification-status pass",
		),
		Args: deps.NoArgsWithHints("Use `--task` and `--summary` flags instead of positional arguments."),
		RunE: runWorkflowMergeBack,
	}
	mergeBackCmd.Flags().String("task", "", "Task ID that was delegated (required)")
	mergeBackCmd.Flags().String("summary", "", "Summary of what was done (required)")
	mergeBackCmd.Flags().String("verification-status", "unknown", "pass|fail|partial|unknown")
	mergeBackCmd.Flags().String("integration-notes", "", "Guidance for the parent agent")
	_ = mergeBackCmd.MarkFlagRequired("task")
	_ = mergeBackCmd.MarkFlagRequired("summary")

	foldBackCmd := &cobra.Command{
		Use:   "fold-back",
		Short: "Route loop observations into durable plan artifacts or proposals",
		Long:  `Records loop observations and routes them into TASKS.yaml notes, plan summary, or a ~/.agents proposal file.`,
	}
	foldBackCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Record and route a loop observation",
		Example: deps.ExampleBlock(
			"  dot-agents workflow fold-back create --plan my-plan --task my-task --observation \"API edge case\"",
			"  dot-agents workflow fold-back create --plan my-plan --observation \"plan-level note\"",
			"  dot-agents workflow fold-back create --plan my-plan --task my-task --observation \"needs design\" --propose",
		),
		Args: deps.NoArgsWithHints("Use `--plan` and `--observation` flags instead of positional arguments."),
		RunE: runWorkflowFoldBackCreate,
	}
	foldBackCreateCmd.Flags().String("plan", "", "Canonical plan ID (required)")
	foldBackCreateCmd.Flags().String("task", "", "Task ID to append note to (optional)")
	foldBackCreateCmd.Flags().String("observation", "", "Observation text (required)")
	foldBackCreateCmd.Flags().String("slug", "", "Stable id for create-or-update (D2.a); one tagged line per slug in TASKS/plan notes")
	foldBackCreateCmd.Flags().Bool("propose", false, "Route as proposal rather than inline task note")
	_ = foldBackCreateCmd.MarkFlagRequired("plan")
	_ = foldBackCreateCmd.MarkFlagRequired("observation")
	foldBackUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "Refine an existing slug-scoped fold-back observation",
		Example: deps.ExampleBlock(
			"  dot-agents workflow fold-back update --plan my-plan --slug schema-drift-my-plan --observation \"refined note\"",
			"  dot-agents workflow fold-back update --plan my-plan --slug coverage-regression-my-plan-t1 --task t1 --observation \"latest\"",
		),
		Args: deps.NoArgsWithHints("Requires --plan, --slug, and --observation."),
		RunE: runWorkflowFoldBackUpdate,
	}
	foldBackUpdateCmd.Flags().String("plan", "", "Canonical plan ID (required)")
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
			"  dot-agents workflow fold-back list",
			"  dot-agents workflow fold-back list --plan my-plan",
		),
		Args: deps.NoArgsWithHints("Use `--plan` to filter by canonical plan ID."),
		RunE: runWorkflowFoldBackList,
	}
	foldBackListCmd.Flags().String("plan", "", "Filter by canonical plan ID")
	foldBackCmd.AddCommand(foldBackCreateCmd, foldBackUpdateCmd, foldBackListCmd)

	delegationCmd := &cobra.Command{
		Use:   "delegation",
		Short: "Parent-driven delegation lifecycle helpers",
	}
	delegationCloseoutCmd := &cobra.Command{
		Use:   "closeout",
		Short: "Archive completed merge-back artifacts and reconcile canonical task state",
		Example: deps.ExampleBlock(
			"  dot-agents workflow delegation closeout --plan my-plan --task my-task --decision accept",
			"  dot-agents workflow delegation closeout --plan my-plan --task my-task --decision reject --note \"rework error handling\"",
		),
		Args: deps.NoArgsWithHints("Use `--plan`, `--task`, and `--decision` flags instead of positional arguments."),
		RunE: runWorkflowDelegationCloseout,
	}
	delegationCloseoutCmd.Flags().String("plan", "", "Canonical plan ID (required)")
	delegationCloseoutCmd.Flags().String("task", "", "Delegated task ID (required)")
	delegationCloseoutCmd.Flags().String("decision", "", "accept|reject — parent integration decision (required)")
	delegationCloseoutCmd.Flags().String("note", "", "Optional note (typically used with --decision reject)")
	_ = delegationCloseoutCmd.MarkFlagRequired("plan")
	_ = delegationCloseoutCmd.MarkFlagRequired("task")
	_ = delegationCloseoutCmd.MarkFlagRequired("decision")
	delegationCmd.AddCommand(delegationCloseoutCmd)

	driftCmd := &cobra.Command{
		Use:   "drift",
		Short: "Detect workflow drift across managed repos (read-only)",
		Example: deps.ExampleBlock(
			"  dot-agents workflow drift",
			"  dot-agents workflow drift --project billing-api",
		),
		Args: deps.NoArgsWithHints("Use flags such as `--project` instead of positional arguments."),
		RunE: runWorkflowDrift,
	}
	driftCmd.Flags().Int("stale-days", defaultCheckpointStaleDays, "Checkpoint staleness threshold in days")
	driftCmd.Flags().Int("proposal-days", defaultProposalStaleDays, "Proposal staleness threshold in days")
	driftCmd.Flags().String("project", "", "Check only this project (by name)")

	sweepCmd := &cobra.Command{
		Use:   "sweep",
		Short: "Plan and optionally apply fixes for workflow drift across managed repos",
		Example: deps.ExampleBlock(
			"  dot-agents workflow sweep",
			"  dot-agents workflow sweep --apply",
		),
		Args: deps.NoArgsWithHints("Use flags such as `--apply` instead of positional arguments."),
		RunE: runWorkflowSweep,
	}
	sweepCmd.Flags().Int("stale-days", defaultCheckpointStaleDays, "Checkpoint staleness threshold in days")
	sweepCmd.Flags().Int("proposal-days", defaultProposalStaleDays, "Proposal staleness threshold in days")
	sweepCmd.Flags().Bool("apply", false, "Execute sweep actions (default is dry-run)")

	cmd.AddCommand(statusCmd, orientCmd, checkpointCmd, logCmd, planCmd, taskCmd, tasksCmd, slicesCmd, nextCmd, advanceCmd, healthCmd, verifyCmd, prefsCmd, graphCmd, fanoutCmd, mergeBackCmd, foldBackCmd, delegationCmd, driftCmd, sweepCmd)
	return cmd
}

// NewCmd builds the `dot-agents workflow` command tree. Callers must supply Deps from package commands to avoid an import cycle.
func NewCmd(d Deps) *cobra.Command {
	deps = d
	return newWorkflowCmd()
}

// NewCmdForTest returns the workflow command tree using deps registered by InitTestDeps (see workflow_test.go init).
func NewCmdForTest() *cobra.Command {
	return newWorkflowCmd()
}
