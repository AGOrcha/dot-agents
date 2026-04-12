package commands

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

const (
	workflowDefaultNextAction        = "Review active plan"
	workflowDefaultVerificationState = "unknown"
)

type workflowProjectRef struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
}

type workflowGitSummary struct {
	Branch         string   `json:"branch" yaml:"branch"`
	SHA            string   `json:"sha" yaml:"sha"`
	DirtyFileCount int      `json:"dirty_file_count" yaml:"dirty_file_count"`
	RecentCommits  []string `json:"recent_commits,omitempty" yaml:"-"`
}

type workflowPlanSummary struct {
	Path         string   `json:"path"`
	Title        string   `json:"title"`
	PendingItems []string `json:"pending_items"`
}

type workflowHandoffSummary struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

type workflowProposalSummary struct {
	PendingCount int `json:"pending_count"`
}

type workflowCheckpoint struct {
	SchemaVersion int                `json:"schema_version" yaml:"schema_version"`
	Timestamp     string             `json:"timestamp" yaml:"timestamp"`
	Project       workflowProjectRef `json:"project" yaml:"project"`
	Git           workflowGitSummary `json:"git" yaml:"git"`
	Files         struct {
		Modified []string `json:"modified" yaml:"modified"`
	} `json:"files" yaml:"files"`
	Message      string `json:"message" yaml:"message"`
	Verification struct {
		Status  string `json:"status" yaml:"status"`
		Summary string `json:"summary" yaml:"summary"`
	} `json:"verification" yaml:"verification"`
	NextAction string   `json:"next_action" yaml:"next_action"`
	Blockers   []string `json:"blockers" yaml:"blockers"`
}

type workflowOrientState struct {
	Project           workflowProjectRef             `json:"project"`
	Git               workflowGitSummary             `json:"git"`
	ActivePlans       []workflowPlanSummary          `json:"active_plans"`
	CanonicalPlans    []workflowCanonicalPlanSummary `json:"canonical_plans"`
	Checkpoint        *workflowCheckpoint            `json:"checkpoint"`
	Handoffs          []workflowHandoffSummary       `json:"handoffs"`
	Lessons           []string                       `json:"lessons"`
	Proposals         workflowProposalSummary        `json:"proposals"`
	NextAction        string                         `json:"next_action"`
	NextActionSource  string                         `json:"next_action_source"`
	Warnings          []string                       `json:"warnings"`
	Health            *WorkflowHealthSnapshot        `json:"health,omitempty"`
	Preferences       *WorkflowPreferences           `json:"preferences,omitempty"`
	ActiveDelegations workflowDelegationSummary      `json:"active_delegations"`    // Wave 6 Step 7
	PendingMergeBacks int                            `json:"pending_merge_backs"`   // Wave 6 Step 7
	LocalDrift        *RepoDriftReport               `json:"local_drift,omitempty"` // Wave 7 Step 7
}

// workflowDelegationSummary is a compact view of active delegation state for orient/status.
type workflowDelegationSummary struct {
	ActiveCount    int `json:"active_count"`
	PendingIntents int `json:"pending_intents"` // delegations with a non-empty pending_intent
}

// CanonicalPlan is the PLAN.yaml schema for .agents/workflow/plans/<id>/PLAN.yaml
type CanonicalPlan struct {
	SchemaVersion        int    `json:"schema_version" yaml:"schema_version"`
	ID                   string `json:"id" yaml:"id"`
	Title                string `json:"title" yaml:"title"`
	Status               string `json:"status" yaml:"status"` // draft|active|paused|completed|archived
	Summary              string `json:"summary" yaml:"summary"`
	CreatedAt            string `json:"created_at" yaml:"created_at"`
	UpdatedAt            string `json:"updated_at" yaml:"updated_at"`
	Owner                string `json:"owner" yaml:"owner"`
	SuccessCriteria      string `json:"success_criteria" yaml:"success_criteria"`
	VerificationStrategy string `json:"verification_strategy" yaml:"verification_strategy"`
	CurrentFocusTask     string `json:"current_focus_task" yaml:"current_focus_task"`
}

// CanonicalTaskFile is the TASKS.yaml schema for .agents/workflow/plans/<id>/TASKS.yaml
type CanonicalTaskFile struct {
	SchemaVersion int             `json:"schema_version" yaml:"schema_version"`
	PlanID        string          `json:"plan_id" yaml:"plan_id"`
	Tasks         []CanonicalTask `json:"tasks" yaml:"tasks"`
}

// CanonicalTask is one entry in TASKS.yaml
type CanonicalTask struct {
	ID                   string   `json:"id" yaml:"id"`
	Title                string   `json:"title" yaml:"title"`
	Status               string   `json:"status" yaml:"status"` // pending|in_progress|blocked|completed|cancelled
	DependsOn            []string `json:"depends_on" yaml:"depends_on"`
	Blocks               []string `json:"blocks" yaml:"blocks"`
	Owner                string   `json:"owner" yaml:"owner"`
	WriteScope           []string `json:"write_scope" yaml:"write_scope"`
	VerificationRequired bool     `json:"verification_required" yaml:"verification_required"`
	Notes                string   `json:"notes" yaml:"notes"`
}

// CanonicalSliceFile is the SLICES.yaml schema for .agents/workflow/plans/<id>/SLICES.yaml
type CanonicalSliceFile struct {
	SchemaVersion int              `json:"schema_version" yaml:"schema_version"`
	PlanID        string           `json:"plan_id" yaml:"plan_id"`
	Slices        []CanonicalSlice `json:"slices" yaml:"slices"`
}

// CanonicalSlice is one bounded sub-slice under a canonical task.
type CanonicalSlice struct {
	ID                string   `json:"id" yaml:"id"`
	ParentTaskID      string   `json:"parent_task_id" yaml:"parent_task_id"`
	Title             string   `json:"title" yaml:"title"`
	Summary           string   `json:"summary" yaml:"summary"`
	Status            string   `json:"status" yaml:"status"` // pending|in_progress|blocked|completed|cancelled
	DependsOn         []string `json:"depends_on" yaml:"depends_on"`
	WriteScope        []string `json:"write_scope" yaml:"write_scope"`
	VerificationFocus string   `json:"verification_focus" yaml:"verification_focus"`
	Owner             string   `json:"owner" yaml:"owner"`
}

// workflowCanonicalPlanSummary is a compact view used in orient/status output
type workflowCanonicalPlanSummary struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Status           string `json:"status"`
	CurrentFocusTask string `json:"current_focus_task"`
	PendingCount     int    `json:"pending_count"`
	BlockedCount     int    `json:"blocked_count"`
	CompletedCount   int    `json:"completed_count"`
}

// VerificationRecord is one line in verification-log.jsonl
type VerificationRecord struct {
	SchemaVersion int      `json:"schema_version"`
	Timestamp     string   `json:"timestamp"`
	Kind          string   `json:"kind"`   // test|lint|build|format|custom
	Status        string   `json:"status"` // pass|fail|partial|unknown
	Command       string   `json:"command"`
	Scope         string   `json:"scope"` // file|package|repo|custom
	Summary       string   `json:"summary"`
	Artifacts     []string `json:"artifacts"`
	RecordedBy    string   `json:"recorded_by"`
}

// WorkflowHealthSnapshot is the health.json schema
type WorkflowHealthSnapshot struct {
	SchemaVersion int    `json:"schema_version"`
	Timestamp     string `json:"timestamp"`
	Git           struct {
		InsideRepo     bool   `json:"inside_repo"`
		Branch         string `json:"branch"`
		DirtyFileCount int    `json:"dirty_file_count"`
	} `json:"git"`
	Workflow struct {
		HasActivePlan      bool `json:"has_active_plan"`
		HasCheckpoint      bool `json:"has_checkpoint"`
		PendingProposals   int  `json:"pending_proposals"`
		CanonicalPlanCount int  `json:"canonical_plan_count"`
	} `json:"workflow"`
	Tooling struct {
		MCP       string `json:"mcp"`
		Auth      string `json:"auth"`
		Formatter string `json:"formatter"`
	} `json:"tooling"`
	Status   string   `json:"status"` // healthy|warn|error
	Warnings []string `json:"warnings"`
}

func NewWorkflowCmd() *cobra.Command {
	var (
		checkpointMessage           string
		checkpointVerificationState string
		checkpointVerificationText  string
		logAll                      bool
	)

	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Inspect and persist workflow state",
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show workflow state for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowStatus()
		},
	}
	statusCmd.Flags().BoolP("json", "j", false, "Output as JSON")

	orientCmd := &cobra.Command{
		Use:   "orient",
		Short: "Render session orient context for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowOrient()
		},
	}

	checkpointCmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Write a checkpoint for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowCheckpoint(checkpointMessage, checkpointVerificationState, checkpointVerificationText)
		},
	}
	checkpointCmd.Flags().StringVar(&checkpointMessage, "message", "", "Checkpoint message")
	checkpointCmd.Flags().StringVar(&checkpointVerificationState, "verification-status", workflowDefaultVerificationState, "Verification status: pass, fail, partial, or unknown")
	checkpointCmd.Flags().StringVar(&checkpointVerificationText, "verification-summary", "", "Verification summary text")

	logCmd := &cobra.Command{
		Use:   "log",
		Short: "Show recent checkpoint log entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowLog(logAll)
		},
	}
	logCmd.Flags().BoolVar(&logAll, "all", false, "Show all log entries")

	// plan subcommand tree
	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "List canonical plans",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanList()
		},
	}
	planShowCmd := &cobra.Command{
		Use:   "show <plan-id>",
		Short: "Show details of a canonical plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPlanShow(args[0])
		},
	}
	planGraphCmd := &cobra.Command{
		Use:   "graph [plan-id]",
		Short: "Render a derived graph of canonical plans and tasks",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := ""
			if len(args) == 1 {
				planID = args[0]
			}
			return runWorkflowPlanGraph(planID)
		},
	}
	planCmd.AddCommand(planShowCmd, planGraphCmd)

	// tasks subcommand
	tasksCmd := &cobra.Command{
		Use:   "tasks <plan-id>",
		Short: "Show tasks for a canonical plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowTasks(args[0])
		},
	}

	slicesCmd := &cobra.Command{
		Use:   "slices <plan-id>",
		Short: "Show slices for a canonical plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowSlices(args[0])
		},
	}

	nextCmd := &cobra.Command{
		Use:   "next",
		Short: "Suggest the next actionable canonical task",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowNext()
		},
	}

	// advance subcommand
	var advanceTask, advanceStatus string
	advanceCmd := &cobra.Command{
		Use:   "advance <plan-id>",
		Short: "Advance a task's status within a canonical plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowAdvance(args[0], advanceTask, advanceStatus)
		},
	}
	advanceCmd.Flags().StringVar(&advanceTask, "task", "", "Task ID to advance (required)")
	advanceCmd.Flags().StringVar(&advanceStatus, "status", "", "New task status (required)")
	_ = advanceCmd.MarkFlagRequired("task")
	_ = advanceCmd.MarkFlagRequired("status")

	// health subcommand
	healthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show workflow health snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowHealth()
		},
	}

	// verify subcommand tree
	verifyCmd := &cobra.Command{
		Use:   "verify",
		Short: "Manage verification log",
	}
	var verifyKind, verifyStatus, verifyCommand, verifyScope, verifySummary string
	verifyRecordCmd := &cobra.Command{
		Use:   "record",
		Short: "Record a verification run",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowVerifyRecord(verifyKind, verifyStatus, verifyCommand, verifyScope, verifySummary)
		},
	}
	verifyRecordCmd.Flags().StringVar(&verifyKind, "kind", "", "Kind: test|lint|build|format|custom (required)")
	verifyRecordCmd.Flags().StringVar(&verifyStatus, "status", "", "Status: pass|fail|partial|unknown (required)")
	verifyRecordCmd.Flags().StringVar(&verifyCommand, "command", "", "Command that was run")
	verifyRecordCmd.Flags().StringVar(&verifyScope, "scope", "repo", "Scope: file|package|repo|custom")
	verifyRecordCmd.Flags().StringVar(&verifySummary, "summary", "", "Summary of the run (required)")
	_ = verifyRecordCmd.MarkFlagRequired("kind")
	_ = verifyRecordCmd.MarkFlagRequired("status")
	_ = verifyRecordCmd.MarkFlagRequired("summary")

	var verifyLogAll bool
	verifyLogCmd := &cobra.Command{
		Use:   "log",
		Short: "Show verification log entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowVerifyLog(verifyLogAll)
		},
	}
	verifyLogCmd.Flags().BoolVar(&verifyLogAll, "all", false, "Show all log entries")

	verifyCmd.AddCommand(verifyRecordCmd, verifyLogCmd)

	// prefs subcommand tree
	prefsCmd := &cobra.Command{
		Use:   "prefs",
		Short: "Show resolved workflow preferences",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefs()
		},
	}
	prefsCmd.Flags().BoolP("json", "j", false, "Output as JSON")

	prefsShowCmd := &cobra.Command{
		Use:   "show",
		Short: "Show resolved workflow preferences (alias for prefs)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefs()
		},
	}

	prefsSetLocalCmd := &cobra.Command{
		Use:   "set-local <key> <value>",
		Short: "Set a user-local workflow preference override",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefsSetLocal(args[0], args[1])
		},
	}

	prefsSetSharedCmd := &cobra.Command{
		Use:   "set-shared <key> <value>",
		Short: "Propose a shared workflow preference change (queued for review)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowPrefsSetShared(args[0], args[1])
		},
	}

	prefsCmd.AddCommand(prefsShowCmd, prefsSetLocalCmd, prefsSetSharedCmd)

	// graph subcommand tree (Wave 5)
	graphCmd := &cobra.Command{
		Use:   "graph",
		Short: "Query knowledge graph context",
	}
	graphQueryCmd := &cobra.Command{
		Use:   "query [query string]",
		Short: "Query graph context by bridge intent",
		RunE:  runWorkflowGraphQuery,
	}
	graphQueryCmd.Flags().String("intent", "", "Bridge intent: plan_context|decision_lookup|entity_context|workflow_memory|contradictions")
	graphQueryCmd.Flags().String("scope", "", "Optional scope filter")

	graphHealthCmd := &cobra.Command{
		Use:   "health",
		Short: "Show graph bridge adapter health",
		RunE:  runWorkflowGraphHealth,
	}
	graphCmd.AddCommand(graphQueryCmd, graphHealthCmd)

	// fanout subcommand (Wave 6)
	fanoutCmd := &cobra.Command{
		Use:   "fanout",
		Short: "Delegate a task to a sub-agent with a bounded write scope",
		RunE:  runWorkflowFanout,
	}
	fanoutCmd.Flags().String("plan", "", "Canonical plan ID (required)")
	fanoutCmd.Flags().String("task", "", "Task ID to delegate (required)")
	fanoutCmd.Flags().String("owner", "", "Delegate agent identity")
	fanoutCmd.Flags().String("write-scope", "", "Comma-separated file/dir patterns this delegate may touch")
	_ = fanoutCmd.MarkFlagRequired("plan")
	_ = fanoutCmd.MarkFlagRequired("task")

	// merge-back subcommand (Wave 6)
	mergeBackCmd := &cobra.Command{
		Use:   "merge-back",
		Short: "Record a sub-agent's completed work as a merge-back artifact",
		RunE:  runWorkflowMergeBack,
	}
	mergeBackCmd.Flags().String("task", "", "Task ID that was delegated (required)")
	mergeBackCmd.Flags().String("summary", "", "Summary of what was done (required)")
	mergeBackCmd.Flags().String("verification-status", "unknown", "pass|fail|partial|unknown")
	mergeBackCmd.Flags().String("integration-notes", "", "Guidance for the parent agent")
	_ = mergeBackCmd.MarkFlagRequired("task")
	_ = mergeBackCmd.MarkFlagRequired("summary")

	// drift subcommand (Wave 7)
	driftCmd := &cobra.Command{
		Use:   "drift",
		Short: "Detect workflow drift across managed repos (read-only)",
		RunE:  runWorkflowDrift,
	}
	driftCmd.Flags().Int("stale-days", defaultCheckpointStaleDays, "Checkpoint staleness threshold in days")
	driftCmd.Flags().Int("proposal-days", defaultProposalStaleDays, "Proposal staleness threshold in days")
	driftCmd.Flags().String("project", "", "Check only this project (by name)")

	// sweep subcommand (Wave 7)
	sweepCmd := &cobra.Command{
		Use:   "sweep",
		Short: "Plan and optionally apply fixes for workflow drift across managed repos",
		RunE:  runWorkflowSweep,
	}
	sweepCmd.Flags().Int("stale-days", defaultCheckpointStaleDays, "Checkpoint staleness threshold in days")
	sweepCmd.Flags().Int("proposal-days", defaultProposalStaleDays, "Proposal staleness threshold in days")
	sweepCmd.Flags().Bool("apply", false, "Execute sweep actions (default is dry-run)")

	cmd.AddCommand(statusCmd, orientCmd, checkpointCmd, logCmd, planCmd, tasksCmd, slicesCmd, nextCmd, advanceCmd, healthCmd, verifyCmd, prefsCmd, graphCmd, fanoutCmd, mergeBackCmd, driftCmd, sweepCmd)
	return cmd
}

func runWorkflowStatus() error {
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}
	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(state)
	}

	ui.Header("Workflow Status")
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Bold, state.Project.Name, ui.Reset)
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, state.Project.Path, ui.Reset)
	fmt.Fprintln(os.Stdout)

	ui.Section("Project")
	fmt.Fprintf(os.Stdout, "  branch: %s\n", state.Git.Branch)
	fmt.Fprintf(os.Stdout, "  sha: %s\n", state.Git.SHA)
	fmt.Fprintf(os.Stdout, "  dirty files: %d\n", state.Git.DirtyFileCount)
	fmt.Fprintf(os.Stdout, "  canonical plans: %d\n", len(state.CanonicalPlans))
	fmt.Fprintf(os.Stdout, "  active plans: %d\n", len(state.ActivePlans))
	fmt.Fprintf(os.Stdout, "  pending handoffs: %d\n", len(state.Handoffs))
	fmt.Fprintf(os.Stdout, "  lessons: %d\n", len(state.Lessons))
	fmt.Fprintf(os.Stdout, "  pending proposals: %d\n", state.Proposals.PendingCount)
	fmt.Fprintf(os.Stdout, "  active delegations: %d\n", state.ActiveDelegations.ActiveCount)
	fmt.Fprintf(os.Stdout, "  pending merge-backs: %d\n", state.PendingMergeBacks)
	fmt.Fprintln(os.Stdout)

	ui.Section("Last Checkpoint")
	if state.Checkpoint == nil {
		fmt.Fprintln(os.Stdout, "  none")
	} else {
		fmt.Fprintf(os.Stdout, "  timestamp: %s\n", state.Checkpoint.Timestamp)
		fmt.Fprintf(os.Stdout, "  verification: %s\n", state.Checkpoint.Verification.Status)
		if state.Checkpoint.Verification.Summary != "" {
			fmt.Fprintf(os.Stdout, "  summary: %s\n", state.Checkpoint.Verification.Summary)
		}
		fmt.Fprintf(os.Stdout, "  next action: %s\n", state.Checkpoint.NextAction)
	}
	fmt.Fprintln(os.Stdout)

	ui.Section("Next Action")
	fmt.Fprintf(os.Stdout, "  recommended: %s\n", state.NextAction)
	fmt.Fprintf(os.Stdout, "  source: %s\n", state.NextActionSource)

	if len(state.Warnings) > 0 {
		fmt.Fprintln(os.Stdout)
		ui.Section("Warnings")
		for _, warning := range state.Warnings {
			fmt.Fprintf(os.Stdout, "  - %s\n", warning)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowOrient() error {
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}
	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(state)
	}
	renderWorkflowOrientMarkdown(state, os.Stdout)
	return nil
}

func runWorkflowCheckpoint(message, verificationStatus, verificationSummary string) error {
	if verificationStatus == "" {
		verificationStatus = workflowDefaultVerificationState
	}
	if !isValidVerificationStatus(verificationStatus) {
		return fmt.Errorf("invalid verification status %q", verificationStatus)
	}

	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}

	checkpoint := workflowCheckpoint{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Project:       project,
		Git: workflowGitSummary{
			Branch:         state.Git.Branch,
			SHA:            state.Git.SHA,
			DirtyFileCount: state.Git.DirtyFileCount,
		},
		Message:    message,
		NextAction: state.NextAction,
		Blockers:   []string{},
	}
	checkpoint.Files.Modified, err = gitModifiedFiles(project.Path)
	if err != nil {
		checkpoint.Files.Modified = []string{}
	}
	checkpoint.Verification.Status = verificationStatus
	checkpoint.Verification.Summary = verificationSummary

	contextDir := config.ProjectContextDir(project.Name)
	if err := os.MkdirAll(contextDir, 0755); err != nil {
		return err
	}
	checkpointPath := filepath.Join(contextDir, "checkpoint.yaml")
	content, err := yaml.Marshal(checkpoint)
	if err != nil {
		return err
	}
	if err := os.WriteFile(checkpointPath, content, 0644); err != nil {
		return err
	}
	if err := appendWorkflowSessionLog(filepath.Join(contextDir, "session-log.md"), checkpoint); err != nil {
		return err
	}

	ui.Success("Checkpoint written")
	fmt.Fprintf(os.Stdout, "  %s\n\n", config.DisplayPath(checkpointPath))
	return nil
}

func runWorkflowLog(showAll bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	logPath := filepath.Join(config.ProjectContextDir(project.Name), "session-log.md")
	content, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Info("No session log found.")
			return nil
		}
		return err
	}

	entries := splitWorkflowLogEntries(string(content))
	if !showAll && len(entries) > 10 {
		entries = entries[len(entries)-10:]
	}

	ui.Header("Workflow Log")
	for _, entry := range entries {
		fmt.Fprintln(os.Stdout, entry)
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

// collectDelegationSummary loads active delegations and counts pending intents and merge-backs.
func collectDelegationSummary(projectPath string) (workflowDelegationSummary, int) {
	contracts, err := listDelegationContracts(projectPath)
	if err != nil {
		return workflowDelegationSummary{}, 0
	}
	summary := workflowDelegationSummary{}
	for _, c := range contracts {
		if c.Status == "pending" || c.Status == "active" {
			summary.ActiveCount++
			if c.PendingIntent != CoordinationIntentNone {
				summary.PendingIntents++
			}
		}
	}
	// Count unprocessed merge-back artifacts
	mergeBackEntries, err := os.ReadDir(mergeBackDir(projectPath))
	pendingMergebacks := 0
	if err == nil {
		for _, e := range mergeBackEntries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				pendingMergebacks++
			}
		}
	}
	return summary, pendingMergebacks
}

func collectWorkflowState() (*workflowOrientState, error) {
	project, err := currentWorkflowProject()
	if err != nil {
		return nil, err
	}

	gitSummary, gitWarnings := collectWorkflowGitSummary(project.Path)
	activePlans, err := collectWorkflowPlans(project.Path)
	if err != nil {
		return nil, err
	}
	canonicalPlans, canonicalWarnings := collectCanonicalPlans(project.Path)
	handoffs, err := collectWorkflowHandoffs(project.Path)
	if err != nil {
		return nil, err
	}
	lessons, lessonWarnings := collectWorkflowLessons(project.Path)
	checkpoint, checkpointWarnings := loadWorkflowCheckpoint(project.Name)
	proposals, err := countPendingWorkflowProposals()
	if err != nil {
		return nil, err
	}
	delegationSummary, pendingMergebacks := collectDelegationSummary(project.Path)

	warnings := append([]string{}, gitWarnings...)
	warnings = append(warnings, canonicalWarnings...)
	warnings = append(warnings, lessonWarnings...)
	warnings = append(warnings, checkpointWarnings...)

	state := &workflowOrientState{
		Project:        project,
		Git:            gitSummary,
		ActivePlans:    activePlans,
		CanonicalPlans: canonicalPlans,
		Checkpoint:     checkpoint,
		Handoffs:       handoffs,
		Lessons:        lessons,
		Proposals: workflowProposalSummary{
			PendingCount: proposals,
		},
		Warnings:          warnings,
		ActiveDelegations: delegationSummary,
		PendingMergeBacks: pendingMergebacks,
	}
	state.NextAction, state.NextActionSource = deriveWorkflowNextAction(gitSummary, checkpoint, canonicalPlans, activePlans)
	if checkpoint != nil && strings.TrimSpace(checkpoint.NextAction) != "" && !isCheckpointCurrent(gitSummary, checkpoint) && state.NextActionSource != "checkpoint" {
		warnings = append(warnings, fmt.Sprintf("checkpoint next action %q is stale relative to current git state; using %s", checkpoint.NextAction, state.NextActionSource))
		state.Warnings = warnings
	}

	// Wave 7 Step 7: local drift check for current project
	localDrift := detectRepoDrift(
		ManagedProject{Name: project.Name, Path: project.Path},
		defaultCheckpointStaleDays, defaultProposalStaleDays,
	)
	if localDrift.Status != "healthy" {
		state.LocalDrift = &localDrift
	}
	health := computeWorkflowHealth(state)
	state.Health = &health

	prefs, err := resolvePreferences(project.Path, project.Name)
	if err == nil {
		state.Preferences = &prefs
	}

	return state, nil
}

func currentWorkflowProject() (workflowProjectRef, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return workflowProjectRef{}, err
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return workflowProjectRef{}, err
	}

	project := filepath.Base(cwd)
	if rc, err := config.LoadAgentsRC(cwd); err == nil && strings.TrimSpace(rc.Project) != "" {
		project = strings.TrimSpace(rc.Project)
	}
	return workflowProjectRef{Name: project, Path: cwd}, nil
}

func collectWorkflowGitSummary(projectPath string) (workflowGitSummary, []string) {
	summary := workflowGitSummary{
		Branch:         "unknown",
		SHA:            "unknown",
		DirtyFileCount: 0,
	}
	var warnings []string
	if !isGitRepo(projectPath) {
		warnings = append(warnings, "git repo not detected")
		return summary, warnings
	}

	summary.Branch = strings.TrimSpace(gitOutput(projectPath, "rev-parse", "--abbrev-ref", "HEAD"))
	if summary.Branch == "" {
		summary.Branch = "unknown"
	}
	summary.SHA = strings.TrimSpace(gitOutput(projectPath, "rev-parse", "--short", "HEAD"))
	if summary.SHA == "" {
		summary.SHA = "unknown"
	}
	statusLines := strings.TrimSpace(gitOutput(projectPath, "status", "--short"))
	if statusLines != "" {
		summary.DirtyFileCount = len(strings.Split(statusLines, "\n"))
	}
	commits := strings.TrimSpace(gitOutput(projectPath, "log", "--oneline", "-5"))
	if commits != "" {
		summary.RecentCommits = strings.Split(commits, "\n")
	}
	return summary, warnings
}

func collectWorkflowPlans(projectPath string) ([]workflowPlanSummary, error) {
	paths, err := filepath.Glob(filepath.Join(projectPath, ".agents", "active", "*.plan.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	plans := make([]workflowPlanSummary, 0, len(paths))
	for _, path := range paths {
		plan, err := readWorkflowPlan(path)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func collectWorkflowHandoffs(projectPath string) ([]workflowHandoffSummary, error) {
	paths, err := filepath.Glob(filepath.Join(projectPath, ".agents", "active", "handoffs", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	handoffs := make([]workflowHandoffSummary, 0, len(paths))
	for _, path := range paths {
		title, err := firstMarkdownTitle(path)
		if err != nil {
			return nil, err
		}
		handoffs = append(handoffs, workflowHandoffSummary{Path: path, Title: title})
	}
	return handoffs, nil
}

func collectWorkflowLessons(projectPath string) ([]string, []string) {
	candidates := []string{
		filepath.Join(projectPath, ".agents", "lessons", "index.md"),
		filepath.Join(projectPath, ".agents", "lessons.md"),
	}
	for _, candidate := range candidates {
		content, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		lines := make([]string, 0)
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			lines = append(lines, line)
		}
		if len(lines) > 10 {
			lines = lines[len(lines)-10:]
		}
		return lines, nil
	}
	return []string{}, []string{"lessons index not found"}
}

func loadWorkflowCheckpoint(project string) (*workflowCheckpoint, []string) {
	checkpointPath := filepath.Join(config.ProjectContextDir(project), "checkpoint.yaml")
	content, err := os.ReadFile(checkpointPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{"checkpoint unreadable"}
	}
	var checkpoint workflowCheckpoint
	if err := yaml.Unmarshal(content, &checkpoint); err != nil {
		return nil, []string{"checkpoint unreadable"}
	}
	return &checkpoint, nil
}

func countPendingWorkflowProposals() (int, error) {
	dir := filepath.Join(config.AgentsHome(), "proposals")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".yaml") {
			count++
		}
	}
	return count, nil
}

func deriveWorkflowNextAction(git workflowGitSummary, checkpoint *workflowCheckpoint, canonicalPlans []workflowCanonicalPlanSummary, plans []workflowPlanSummary) (string, string) {
	if checkpoint != nil && strings.TrimSpace(checkpoint.NextAction) != "" && isCheckpointCurrent(git, checkpoint) {
		return strings.TrimSpace(checkpoint.NextAction), "checkpoint"
	}
	for _, cp := range canonicalPlans {
		if cp.Status == "active" && strings.TrimSpace(cp.CurrentFocusTask) != "" {
			return strings.TrimSpace(cp.CurrentFocusTask), "canonical_plan"
		}
	}
	for _, plan := range plans {
		if len(plan.PendingItems) > 0 {
			return plan.PendingItems[0], "active_plan"
		}
	}
	if checkpoint != nil && strings.TrimSpace(checkpoint.NextAction) != "" {
		return strings.TrimSpace(checkpoint.NextAction), "checkpoint_stale"
	}
	return workflowDefaultNextAction, "default"
}

func isCheckpointCurrent(git workflowGitSummary, checkpoint *workflowCheckpoint) bool {
	if checkpoint == nil {
		return false
	}
	if strings.TrimSpace(checkpoint.Git.Branch) == "" || strings.TrimSpace(checkpoint.Git.SHA) == "" {
		return false
	}
	return checkpoint.Git.Branch == git.Branch && checkpoint.Git.SHA == git.SHA
}

func readWorkflowPlan(path string) (workflowPlanSummary, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return workflowPlanSummary{}, err
	}
	lines := strings.Split(string(content), "\n")
	title := filepath.Base(path)
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if strings.HasPrefix(first, "#") {
			title = strings.TrimSpace(strings.TrimLeft(first, "# "))
		}
	}
	var pending []string
	var fallback []string
	completed := false
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Status:") {
			status := strings.TrimSpace(strings.TrimPrefix(trimmed, "Status:"))
			if strings.HasPrefix(strings.ToLower(status), "completed") {
				completed = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "- [ ] ") {
			pending = append(pending, strings.TrimSpace(strings.TrimPrefix(trimmed, "- [ ] ")))
			if len(pending) == 3 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(fallback) < 3 {
			fallback = append(fallback, trimmed)
		}
	}
	if completed {
		pending = nil
	} else if len(pending) == 0 {
		pending = fallback
	}
	return workflowPlanSummary{Path: path, Title: title, PendingItems: pending}, nil
}

func firstMarkdownTitle(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "# ")), nil
		}
	}
	return filepath.Base(path), nil
}

func renderWorkflowOrientMarkdown(state *workflowOrientState, out io.Writer) {
	fmt.Fprintln(out, "# Project")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- name: %s\n", state.Project.Name)
	fmt.Fprintf(out, "- path: %s\n", state.Project.Path)
	fmt.Fprintf(out, "- branch: %s\n", state.Git.Branch)
	fmt.Fprintf(out, "- sha: %s\n", state.Git.SHA)
	fmt.Fprintf(out, "- dirty files: %d\n", state.Git.DirtyFileCount)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "# Canonical Plans")
	fmt.Fprintln(out)
	if len(state.CanonicalPlans) == 0 {
		fmt.Fprintln(out, "- none")
		fmt.Fprintln(out)
	} else {
		for _, cp := range state.CanonicalPlans {
			fmt.Fprintf(out, "## %s (%s)\n", cp.Title, cp.Status)
			fmt.Fprintf(out, "- id: %s\n", cp.ID)
			if cp.CurrentFocusTask != "" {
				fmt.Fprintf(out, "- focus: %s\n", cp.CurrentFocusTask)
			}
			fmt.Fprintf(out, "- tasks: %d pending, %d blocked, %d completed\n", cp.PendingCount, cp.BlockedCount, cp.CompletedCount)
			fmt.Fprintln(out)
		}
	}

	fmt.Fprintln(out, "# Active Plans")
	fmt.Fprintln(out)
	if len(state.ActivePlans) == 0 {
		fmt.Fprintln(out, "- none")
		fmt.Fprintln(out)
	} else {
		for _, plan := range state.ActivePlans {
			fmt.Fprintf(out, "## %s\n", plan.Title)
			fmt.Fprintf(out, "- path: %s\n", plan.Path)
			if len(plan.PendingItems) == 0 {
				fmt.Fprintln(out, "- no pending items found")
			} else {
				for _, item := range plan.PendingItems {
					fmt.Fprintf(out, "- %s\n", item)
				}
			}
			fmt.Fprintln(out)
		}
	}

	fmt.Fprintln(out, "# Last Checkpoint")
	fmt.Fprintln(out)
	if state.Checkpoint == nil {
		fmt.Fprintln(out, "- none")
		fmt.Fprintln(out)
	} else {
		fmt.Fprintf(out, "- timestamp: %s\n", state.Checkpoint.Timestamp)
		fmt.Fprintf(out, "- branch: %s\n", state.Checkpoint.Git.Branch)
		fmt.Fprintf(out, "- sha: %s\n", state.Checkpoint.Git.SHA)
		fmt.Fprintf(out, "- verification: %s\n", state.Checkpoint.Verification.Status)
		if state.Checkpoint.Verification.Summary != "" {
			fmt.Fprintf(out, "- summary: %s\n", state.Checkpoint.Verification.Summary)
		}
		fmt.Fprintf(out, "- next action: %s\n", state.Checkpoint.NextAction)
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "# Pending Handoffs")
	fmt.Fprintln(out)
	if len(state.Handoffs) == 0 {
		fmt.Fprintln(out, "- none")
	} else {
		for _, handoff := range state.Handoffs {
			fmt.Fprintf(out, "- %s (%s)\n", handoff.Title, handoff.Path)
		}
	}
	fmt.Fprintln(out)

	// Wave 6 Step 7: Delegations section
	fmt.Fprintln(out, "# Delegations")
	fmt.Fprintln(out)
	if state.ActiveDelegations.ActiveCount == 0 && state.PendingMergeBacks == 0 {
		fmt.Fprintln(out, "- none")
	} else {
		fmt.Fprintf(out, "- active delegations: %d\n", state.ActiveDelegations.ActiveCount)
		if state.ActiveDelegations.PendingIntents > 0 {
			fmt.Fprintf(out, "- pending intents: %d (check delegation contracts)\n", state.ActiveDelegations.PendingIntents)
		}
		fmt.Fprintf(out, "- pending merge-backs: %d\n", state.PendingMergeBacks)
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "# Recent Lessons")
	fmt.Fprintln(out)
	if len(state.Lessons) == 0 {
		fmt.Fprintln(out, "- none")
	} else {
		for _, lesson := range state.Lessons {
			fmt.Fprintf(out, "- %s\n", lesson)
		}
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "# Pending Proposals")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- count: %d\n", state.Proposals.PendingCount)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "# Next Action")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "- %s\n", state.NextAction)
	fmt.Fprintf(out, "- source: %s\n", state.NextActionSource)

	if len(state.Git.RecentCommits) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Recent Commits")
		fmt.Fprintln(out)
		for _, commit := range state.Git.RecentCommits {
			fmt.Fprintln(out, commit)
		}
	}
	if state.Health != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Health")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "- status: %s\n", state.Health.Status)
		for _, w := range state.Health.Warnings {
			fmt.Fprintf(out, "- warning: %s\n", w)
		}
	}
	if p := state.Preferences; p != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Preferences")
		fmt.Fprintln(out)
		fmt.Fprintf(out, "- test_command: %s\n", strPtrVal(p.Verification.TestCommand))
		fmt.Fprintf(out, "- lint_command: %s\n", strPtrVal(p.Verification.LintCommand))
		fmt.Fprintf(out, "- plan_directory: %s\n", strPtrVal(p.Planning.PlanDirectory))
		fmt.Fprintf(out, "- package_manager: %s\n", strPtrVal(p.Execution.PackageManager))
		fmt.Fprintf(out, "- formatter: %s\n", strPtrVal(p.Execution.Formatter))
	}
	if state.LocalDrift != nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Local Drift")
		fmt.Fprintln(out)
		for _, w := range state.LocalDrift.Warnings {
			fmt.Fprintf(out, "- warn: %s\n", w)
		}
		fmt.Fprintln(out, "  (run 'dot-agents workflow drift' for cross-repo view)")
	}
	if len(state.Warnings) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "# Warnings")
		fmt.Fprintln(out)
		for _, warning := range state.Warnings {
			fmt.Fprintf(out, "- %s\n", warning)
		}
	}
}

func appendWorkflowSessionLog(path string, checkpoint workflowCheckpoint) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "## %s\n", checkpoint.Timestamp); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "branch: %s\n", checkpoint.Git.Branch); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "sha: %s\n", checkpoint.Git.SHA); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "files: %d\n", len(checkpoint.Files.Modified)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "verification: %s\n", checkpoint.Verification.Status); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "message: %s\n", checkpoint.Message); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "next_action: %s\n\n", checkpoint.NextAction); err != nil {
		return err
	}
	return nil
}

func splitWorkflowLogEntries(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	parts := strings.Split(content, "\n## ")
	entries := make([]string, 0, len(parts))
	for i, part := range parts {
		entry := part
		if i > 0 {
			entry = "## " + part
		}
		entry = strings.TrimSpace(entry)
		if entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func isGitRepo(projectPath string) bool {
	cmd := exec.Command("git", "-C", projectPath, "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

func gitOutput(projectPath string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", projectPath}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func gitModifiedFiles(projectPath string) ([]string, error) {
	if !isGitRepo(projectPath) {
		return []string{}, nil
	}
	output := strings.TrimSpace(gitOutput(projectPath, "status", "--short"))
	if output == "" {
		return []string{}, nil
	}
	lines := strings.Split(output, "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) < 4 {
			continue
		}
		files = append(files, strings.TrimSpace(line[3:]))
	}
	return files, nil
}

func isValidVerificationStatus(status string) bool {
	switch status {
	case "pass", "fail", "partial", "unknown":
		return true
	default:
		return false
	}
}

var errNoWorkflowProject = errors.New("workflow commands must run inside a project directory")

// ── Canonical plan I/O ───────────────────────────────────────────────────────

func plansBaseDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "workflow", "plans")
}

func listCanonicalPlanIDs(projectPath string) ([]string, error) {
	base := plansBaseDir(projectPath)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func loadCanonicalPlan(projectPath, planID string) (*CanonicalPlan, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, "PLAN.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan CanonicalPlan
	if err := yaml.Unmarshal(content, &plan); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &plan, nil
}

func saveCanonicalPlan(projectPath string, plan *CanonicalPlan) error {
	dir := filepath.Join(plansBaseDir(projectPath), plan.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	content, err := yaml.Marshal(plan)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "PLAN.yaml"), content, 0644)
}

func loadCanonicalTasks(projectPath, planID string) (*CanonicalTaskFile, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, "TASKS.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tf CanonicalTaskFile
	if err := yaml.Unmarshal(content, &tf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &tf, nil
}

func loadCanonicalSlices(projectPath, planID string) (*CanonicalSliceFile, error) {
	path := filepath.Join(plansBaseDir(projectPath), planID, "SLICES.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sf CanonicalSliceFile
	if err := yaml.Unmarshal(content, &sf); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &sf, nil
}

func saveCanonicalTasks(projectPath string, tf *CanonicalTaskFile) error {
	dir := filepath.Join(plansBaseDir(projectPath), tf.PlanID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	content, err := yaml.Marshal(tf)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "TASKS.yaml"), content, 0644)
}

func collectCanonicalPlans(projectPath string) ([]workflowCanonicalPlanSummary, []string) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, []string{"canonical plans unreadable: " + err.Error()}
	}
	var summaries []workflowCanonicalPlanSummary
	var warnings []string
	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("plan %s unreadable: %v", id, err))
			continue
		}
		summary := workflowCanonicalPlanSummary{
			ID:               plan.ID,
			Title:            plan.Title,
			Status:           plan.Status,
			CurrentFocusTask: plan.CurrentFocusTask,
		}
		if tf, err := loadCanonicalTasks(projectPath, id); err == nil {
			for _, t := range tf.Tasks {
				switch t.Status {
				case "pending", "in_progress":
					summary.PendingCount++
				case "blocked":
					summary.BlockedCount++
				case "completed":
					summary.CompletedCount++
				}
			}
		}
		summaries = append(summaries, summary)
	}
	if summaries == nil {
		summaries = []workflowCanonicalPlanSummary{}
	}
	return summaries, warnings
}

func isValidPlanStatus(s string) bool {
	switch s {
	case "draft", "active", "paused", "completed", "archived":
		return true
	default:
		return false
	}
}

func isValidTaskStatus(s string) bool {
	switch s {
	case "pending", "in_progress", "blocked", "completed", "cancelled":
		return true
	default:
		return false
	}
}

// ── Run functions ─────────────────────────────────────────────────────────────

func runWorkflowPlanList() error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	ids, err := listCanonicalPlanIDs(project.Path)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Fprintln(os.Stdout, "No canonical plans found.")
		fmt.Fprintf(os.Stdout, "  Create one at: %s\n", config.DisplayPath(filepath.Join(plansBaseDir(project.Path), "<plan-id>", "PLAN.yaml")))
		return nil
	}
	if Flags.JSON {
		summaries, _ := collectCanonicalPlans(project.Path)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}
	ui.Header("Canonical Plans")
	for _, id := range ids {
		plan, err := loadCanonicalPlan(project.Path, id)
		if err != nil {
			fmt.Fprintf(os.Stdout, "  %s (unreadable: %v)\n", id, err)
			continue
		}
		focus := ""
		if plan.CurrentFocusTask != "" {
			focus = "  focus: " + plan.CurrentFocusTask
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s (%s)%s\n", plan.ID, plan.Title, plan.Status, focus)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowPlanShow(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return fmt.Errorf("plan %q not found: %w", planID, err)
	}
	tf, tasksErr := loadCanonicalTasks(project.Path, planID)
	sf, slicesErr := loadCanonicalSlices(project.Path, planID)

	if Flags.JSON {
		out := map[string]interface{}{"plan": plan}
		if tasksErr == nil {
			out["tasks"] = tf
		}
		if slicesErr == nil {
			out["slices"] = sf
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	ui.Header(plan.Title)
	ui.Section("Plan")
	fmt.Fprintf(os.Stdout, "  id: %s\n", plan.ID)
	fmt.Fprintf(os.Stdout, "  status: %s\n", plan.Status)
	fmt.Fprintf(os.Stdout, "  created: %s\n", plan.CreatedAt)
	fmt.Fprintf(os.Stdout, "  updated: %s\n", plan.UpdatedAt)
	if plan.Owner != "" {
		fmt.Fprintf(os.Stdout, "  owner: %s\n", plan.Owner)
	}
	if plan.Summary != "" {
		fmt.Fprintf(os.Stdout, "  summary: %s\n", plan.Summary)
	}
	if plan.SuccessCriteria != "" {
		fmt.Fprintf(os.Stdout, "  success criteria: %s\n", plan.SuccessCriteria)
	}
	if plan.CurrentFocusTask != "" {
		fmt.Fprintf(os.Stdout, "  focus task: %s\n", plan.CurrentFocusTask)
	}
	fmt.Fprintln(os.Stdout)

	if tasksErr != nil {
		fmt.Fprintln(os.Stdout, "  (no TASKS.yaml found)")
		return nil
	}

	var pending, blocked, completed, total int
	for _, t := range tf.Tasks {
		total++
		switch t.Status {
		case "pending", "in_progress":
			pending++
		case "blocked":
			blocked++
		case "completed":
			completed++
		}
	}
	ui.Section("Tasks")
	fmt.Fprintf(os.Stdout, "  total: %d   pending: %d   blocked: %d   completed: %d\n\n", total, pending, blocked, completed)
	for _, t := range tf.Tasks {
		marker := " "
		switch t.Status {
		case "completed":
			marker = "✓"
		case "in_progress":
			marker = "▶"
		case "blocked":
			marker = "✗"
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s  %s\n", marker, t.ID, t.Title)
	}
	fmt.Fprintln(os.Stdout)
	if slicesErr == nil {
		ui.Section("Slices")
		fmt.Fprintf(os.Stdout, "  total: %d\n\n", len(sf.Slices))
		for _, slice := range sf.Slices {
			fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)  task: %s\n", slice.ID, slice.Title, slice.Status, slice.ParentTaskID)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

type workflowPlanGraphNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	PlanID string `json:"plan_id,omitempty"`
	TaskID string `json:"task_id,omitempty"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
}

type workflowPlanGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type workflowPlanGraph struct {
	PlanFilter string                  `json:"plan_filter,omitempty"`
	Nodes      []workflowPlanGraphNode `json:"nodes"`
	Edges      []workflowPlanGraphEdge `json:"edges"`
	Warnings   []string                `json:"warnings,omitempty"`
}

func runWorkflowPlanGraph(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	graph, err := buildWorkflowPlanGraph(project.Path, planID)
	if err != nil {
		return err
	}

	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(graph)
	}

	title := "Canonical Plan Graph"
	if planID != "" {
		title += ": " + planID
	}
	ui.Header(title)

	nodeByID := make(map[string]workflowPlanGraphNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodeByID[node.ID] = node
	}

	for _, node := range graph.Nodes {
		if node.Kind != "plan" {
			continue
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s (%s)\n", strings.TrimPrefix(node.ID, "plan:"), node.Label, node.Status)
		for _, edge := range graph.Edges {
			if edge.Type != "contains" || edge.From != node.ID {
				continue
			}
			taskNode := workflowPlanGraphNode{}
			found := false
			for _, candidate := range graph.Nodes {
				if candidate.ID == edge.To {
					taskNode = candidate
					found = true
					break
				}
			}
			if !found {
				continue
			}
			fmt.Fprintf(os.Stdout, "      -> [%s] %s (%s)\n", strings.TrimPrefix(strings.TrimPrefix(taskNode.ID, "task:"+taskNode.PlanID+"/"), "task:"), taskNode.Label, taskNode.Status)
			for _, taskEdge := range graph.Edges {
				if taskEdge.From == taskNode.ID && taskEdge.Type == "contains" {
					sliceNode, ok := nodeByID[taskEdge.To]
					if ok && sliceNode.Kind == "slice" {
						fmt.Fprintf(os.Stdout, "         => [%s] %s (%s)\n", strings.TrimPrefix(strings.TrimPrefix(sliceNode.ID, "slice:"+sliceNode.PlanID+"/"), "slice:"), sliceNode.Label, sliceNode.Status)
						for _, sliceEdge := range graph.Edges {
							if sliceEdge.From != sliceNode.ID || sliceEdge.Type != "depends_on" {
								continue
							}
							targetLabel := sliceEdge.To
							if targetNode, ok := nodeByID[sliceEdge.To]; ok {
								targetLabel = targetNode.Label
							}
							fmt.Fprintf(os.Stdout, "            depends_on: %s\n", targetLabel)
						}
					}
				}
				if taskEdge.From != taskNode.ID || (taskEdge.Type != "depends_on" && taskEdge.Type != "blocks") {
					continue
				}
				targetLabel := taskEdge.To
				if targetNode, ok := nodeByID[taskEdge.To]; ok {
					targetLabel = targetNode.Label
				}
				fmt.Fprintf(os.Stdout, "         %s: %s\n", taskEdge.Type, targetLabel)
			}
		}
	}

	for _, warning := range graph.Warnings {
		ui.Warn(warning)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func buildWorkflowPlanGraph(projectPath, planID string) (*workflowPlanGraph, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	if planID != "" {
		found := false
		for _, id := range ids {
			if id == planID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("plan %q not found", planID)
		}
		ids = []string{planID}
	}

	graph := &workflowPlanGraph{
		PlanFilter: planID,
		Nodes:      []workflowPlanGraphNode{},
		Edges:      []workflowPlanGraphEdge{},
		Warnings:   []string{},
	}

	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil {
			return nil, fmt.Errorf("load plan %q: %w", id, err)
		}
		tf, err := loadCanonicalTasks(projectPath, id)
		if err != nil {
			return nil, fmt.Errorf("load tasks for plan %q: %w", id, err)
		}
		sf, slicesErr := loadCanonicalSlices(projectPath, id)
		if slicesErr != nil && !os.IsNotExist(slicesErr) {
			return nil, fmt.Errorf("load slices for plan %q: %w", id, slicesErr)
		}

		planNodeID := "plan:" + plan.ID
		graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
			ID:     planNodeID,
			Kind:   "plan",
			Label:  plan.Title,
			Status: plan.Status,
		})

		taskIDs := make(map[string]string, len(tf.Tasks))
		for _, task := range tf.Tasks {
			taskNodeID := "task:" + plan.ID + "/" + task.ID
			taskIDs[task.ID] = taskNodeID
			graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
				ID:     taskNodeID,
				Kind:   "task",
				PlanID: plan.ID,
				Label:  task.Title,
				Status: task.Status,
			})
			graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
				From: planNodeID,
				To:   taskNodeID,
				Type: "contains",
			})
		}

		if slicesErr == nil {
			sliceIDs := make(map[string]string, len(sf.Slices))
			for _, slice := range sf.Slices {
				parentTaskNodeID, ok := taskIDs[slice.ParentTaskID]
				if !ok {
					graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s slice %s references unknown parent task %s", plan.ID, slice.ID, slice.ParentTaskID))
					continue
				}
				sliceNodeID := "slice:" + plan.ID + "/" + slice.ID
				sliceIDs[slice.ID] = sliceNodeID
				graph.Nodes = append(graph.Nodes, workflowPlanGraphNode{
					ID:     sliceNodeID,
					Kind:   "slice",
					PlanID: plan.ID,
					TaskID: slice.ParentTaskID,
					Label:  slice.Title,
					Status: slice.Status,
				})
				graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
					From: parentTaskNodeID,
					To:   sliceNodeID,
					Type: "contains",
				})
			}
			for _, slice := range sf.Slices {
				fromID, ok := sliceIDs[slice.ID]
				if !ok {
					continue
				}
				for _, dep := range slice.DependsOn {
					toID, ok := sliceIDs[dep]
					if !ok {
						graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s slice %s depends on unknown slice %s", plan.ID, slice.ID, dep))
						continue
					}
					graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
						From: fromID,
						To:   toID,
						Type: "depends_on",
					})
				}
			}
		}

		for _, task := range tf.Tasks {
			fromID := taskIDs[task.ID]
			for _, dep := range task.DependsOn {
				toID, ok := taskIDs[dep]
				if !ok {
					graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s task %s depends on unknown task %s", plan.ID, task.ID, dep))
					continue
				}
				graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
					From: fromID,
					To:   toID,
					Type: "depends_on",
				})
			}
			for _, blocked := range task.Blocks {
				toID, ok := taskIDs[blocked]
				if !ok {
					graph.Warnings = append(graph.Warnings, fmt.Sprintf("plan %s task %s blocks unknown task %s", plan.ID, task.ID, blocked))
					continue
				}
				graph.Edges = append(graph.Edges, workflowPlanGraphEdge{
					From: fromID,
					To:   toID,
					Type: "blocks",
				})
			}
		}
	}

	return graph, nil
}

func runWorkflowTasks(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if _, err := loadCanonicalPlan(project.Path, planID); err != nil {
		return fmt.Errorf("plan %q not found: %w", planID, err)
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %q not found: %w", planID, err)
	}
	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tf)
	}
	ui.Header("Tasks: " + planID)
	for _, t := range tf.Tasks {
		deps := ""
		if len(t.DependsOn) > 0 {
			deps = "  depends: " + strings.Join(t.DependsOn, ", ")
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)%s\n", t.ID, t.Title, t.Status, deps)
		if t.Notes != "" {
			fmt.Fprintf(os.Stdout, "      note: %s\n", t.Notes)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowSlices(planID string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if _, err := loadCanonicalPlan(project.Path, planID); err != nil {
		return fmt.Errorf("plan %q not found: %w", planID, err)
	}
	sf, err := loadCanonicalSlices(project.Path, planID)
	if err != nil {
		return fmt.Errorf("slices for plan %q not found: %w", planID, err)
	}
	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(sf)
	}
	ui.Header("Slices: " + planID)
	for _, slice := range sf.Slices {
		deps := ""
		if len(slice.DependsOn) > 0 {
			deps = "  depends: " + strings.Join(slice.DependsOn, ", ")
		}
		fmt.Fprintf(os.Stdout, "  [%s] %s  (%s)  task: %s%s\n", slice.ID, slice.Title, slice.Status, slice.ParentTaskID, deps)
		if slice.Summary != "" {
			fmt.Fprintf(os.Stdout, "      summary: %s\n", slice.Summary)
		}
		if len(slice.WriteScope) > 0 {
			fmt.Fprintf(os.Stdout, "      write scope: %s\n", strings.Join(slice.WriteScope, ", "))
		}
		if slice.VerificationFocus != "" {
			fmt.Fprintf(os.Stdout, "      verification: %s\n", slice.VerificationFocus)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

type workflowNextTaskSuggestion struct {
	PlanID               string   `json:"plan_id"`
	PlanTitle            string   `json:"plan_title"`
	TaskID               string   `json:"task_id"`
	TaskTitle            string   `json:"task_title"`
	Status               string   `json:"status"`
	Reason               string   `json:"reason"`
	WriteScope           []string `json:"write_scope,omitempty"`
	VerificationRequired bool     `json:"verification_required"`
	DependsOn            []string `json:"depends_on,omitempty"`
}

func runWorkflowNext() error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	suggestion, err := selectNextCanonicalTask(project.Path)
	if err != nil {
		return err
	}
	if suggestion == nil {
		fmt.Fprintln(os.Stdout, "No actionable canonical task found.")
		fmt.Fprintln(os.Stdout, "  Active plans are completed, blocked by dependencies, already delegated, or missing TASKS.yaml.")
		return nil
	}

	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(suggestion)
	}

	ui.Header("Next Canonical Task")
	fmt.Fprintf(os.Stdout, "  plan: %s  [%s]\n", suggestion.PlanTitle, suggestion.PlanID)
	fmt.Fprintf(os.Stdout, "  task: %s  [%s]\n", suggestion.TaskTitle, suggestion.TaskID)
	fmt.Fprintf(os.Stdout, "  status: %s\n", suggestion.Status)
	fmt.Fprintf(os.Stdout, "  reason: %s\n", suggestion.Reason)
	if len(suggestion.DependsOn) > 0 {
		fmt.Fprintf(os.Stdout, "  depends on: %s\n", strings.Join(suggestion.DependsOn, ", "))
	}
	if len(suggestion.WriteScope) > 0 {
		fmt.Fprintf(os.Stdout, "  write scope: %s\n", strings.Join(suggestion.WriteScope, ", "))
	}
	if suggestion.VerificationRequired {
		fmt.Fprintln(os.Stdout, "  verification: required")
	} else {
		fmt.Fprintln(os.Stdout, "  verification: optional")
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func selectNextCanonicalTask(projectPath string) (*workflowNextTaskSuggestion, error) {
	ids, err := listCanonicalPlanIDs(projectPath)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	delegations, err := listDelegationContracts(projectPath)
	if err != nil {
		return nil, err
	}
	activeDelegations := make(map[string]bool, len(delegations))
	for _, c := range delegations {
		if c.Status == "pending" || c.Status == "active" {
			activeDelegations[c.ParentTaskID] = true
		}
	}

	type candidate struct {
		suggestion workflowNextTaskSuggestion
		priority   int
	}

	var best *candidate
	for _, id := range ids {
		plan, err := loadCanonicalPlan(projectPath, id)
		if err != nil || plan.Status != "active" {
			continue
		}
		tf, err := loadCanonicalTasks(projectPath, id)
		if err != nil {
			return nil, fmt.Errorf("load tasks for plan %q: %w", id, err)
		}
		for _, task := range tf.Tasks {
			if activeDelegations[task.ID] {
				continue
			}
			if task.Status != "in_progress" && task.Status != "pending" {
				continue
			}
			if len(incompleteCanonicalDependencies(tf.Tasks, task.DependsOn)) > 0 {
				continue
			}

			c := candidate{
				suggestion: workflowNextTaskSuggestion{
					PlanID:               plan.ID,
					PlanTitle:            plan.Title,
					TaskID:               task.ID,
					TaskTitle:            task.Title,
					Status:               task.Status,
					WriteScope:           append([]string(nil), task.WriteScope...),
					VerificationRequired: task.VerificationRequired,
					DependsOn:            append([]string(nil), task.DependsOn...),
				},
				priority: 3,
			}

			switch {
			case task.Status == "in_progress" && plan.CurrentFocusTask == task.Title:
				c.priority = 0
				c.suggestion.Reason = "current focus task is already in progress"
			case task.Status == "in_progress":
				c.priority = 1
				c.suggestion.Reason = "task is already in progress and unblocked"
			case plan.CurrentFocusTask == task.Title:
				c.priority = 2
				c.suggestion.Reason = "current focus task is pending and all dependencies are complete"
			default:
				c.priority = 3
				c.suggestion.Reason = "first pending unblocked task in an active canonical plan"
			}

			if best == nil || c.priority < best.priority {
				tmp := c
				best = &tmp
			}
		}
	}

	if best == nil {
		return nil, nil
	}
	return &best.suggestion, nil
}

func incompleteCanonicalDependencies(tasks []CanonicalTask, deps []string) []string {
	if len(deps) == 0 {
		return nil
	}

	statusByID := make(map[string]string, len(tasks))
	for _, task := range tasks {
		statusByID[task.ID] = task.Status
	}

	var incomplete []string
	for _, dep := range deps {
		if statusByID[dep] != "completed" {
			incomplete = append(incomplete, dep)
		}
	}
	return incomplete
}

func runWorkflowAdvance(planID, taskID, newStatus string) error {
	if !isValidTaskStatus(newStatus) {
		return fmt.Errorf("invalid task status %q: must be pending, in_progress, blocked, completed, or cancelled", newStatus)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %q not found: %w", planID, err)
	}
	found := false
	var taskTitle string
	for i, t := range tf.Tasks {
		if t.ID == taskID {
			tf.Tasks[i].Status = newStatus
			taskTitle = t.Title
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in plan %q", taskID, planID)
	}
	if err := saveCanonicalTasks(project.Path, tf); err != nil {
		return err
	}
	// Update PLAN.yaml metadata
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return err
	}
	plan.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if newStatus == "in_progress" {
		plan.CurrentFocusTask = taskTitle
	}
	if err := saveCanonicalPlan(project.Path, plan); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Task %q advanced to %q", taskTitle, newStatus))
	return nil
}

// ── Wave 3: Verification log ──────────────────────────────────────────────────

func isValidVerificationKind(k string) bool {
	switch k {
	case "test", "lint", "build", "format", "custom":
		return true
	default:
		return false
	}
}

func isValidVerificationScope(s string) bool {
	switch s {
	case "file", "package", "repo", "custom":
		return true
	default:
		return false
	}
}

func verificationLogPath(project string) string {
	return filepath.Join(config.ProjectContextDir(project), "verification-log.jsonl")
}

func appendVerificationLog(project string, rec VerificationRecord) error {
	if err := os.MkdirAll(config.ProjectContextDir(project), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(verificationLogPath(project), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

func readVerificationLog(project string, limit int) ([]VerificationRecord, error) {
	content, err := os.ReadFile(verificationLogPath(project))
	if err != nil {
		if os.IsNotExist(err) {
			return []VerificationRecord{}, nil
		}
		return nil, err
	}
	var records []VerificationRecord
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec VerificationRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // skip malformed lines
		}
		records = append(records, rec)
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return records, nil
}

// ── Wave 3: Health snapshot ───────────────────────────────────────────────────

func healthSnapshotPath(project string) string {
	return filepath.Join(config.ProjectContextDir(project), "health.json")
}

func computeWorkflowHealth(state *workflowOrientState) WorkflowHealthSnapshot {
	h := WorkflowHealthSnapshot{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Status:        "healthy",
	}
	h.Git.InsideRepo = state.Git.Branch != "unknown"
	h.Git.Branch = state.Git.Branch
	h.Git.DirtyFileCount = state.Git.DirtyFileCount
	h.Workflow.HasActivePlan = len(state.ActivePlans) > 0 || len(state.CanonicalPlans) > 0
	h.Workflow.HasCheckpoint = state.Checkpoint != nil
	h.Workflow.PendingProposals = state.Proposals.PendingCount
	h.Workflow.CanonicalPlanCount = len(state.CanonicalPlans)
	h.Tooling.MCP = "unknown"
	h.Tooling.Auth = "unknown"
	h.Tooling.Formatter = "unknown"

	var warnings []string
	if state.Git.DirtyFileCount > 20 {
		warnings = append(warnings, fmt.Sprintf("%d dirty files — consider a checkpoint", state.Git.DirtyFileCount))
	}
	if state.Proposals.PendingCount > 0 {
		warnings = append(warnings, fmt.Sprintf("%d pending proposal(s) need review", state.Proposals.PendingCount))
	}
	if !h.Workflow.HasCheckpoint {
		warnings = append(warnings, "no checkpoint recorded")
	}
	if len(warnings) > 0 {
		h.Status = "warn"
		h.Warnings = warnings
	} else {
		h.Warnings = []string{}
	}
	return h
}

func writeHealthSnapshot(project string, h WorkflowHealthSnapshot) error {
	if err := os.MkdirAll(config.ProjectContextDir(project), 0755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(healthSnapshotPath(project), content, 0644)
}

func readHealthSnapshot(project string) (*WorkflowHealthSnapshot, error) {
	content, err := os.ReadFile(healthSnapshotPath(project))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var h WorkflowHealthSnapshot
	if err := json.Unmarshal(content, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// ── Wave 3: Run functions ─────────────────────────────────────────────────────

func runWorkflowHealth() error {
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}
	health := computeWorkflowHealth(state)
	// Persist the snapshot
	_ = writeHealthSnapshot(state.Project.Name, health)

	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(health)
	}

	ui.Header("Workflow Health")
	statusIcon := "✓"
	if health.Status == "warn" {
		statusIcon = "⚠"
	} else if health.Status == "error" {
		statusIcon = "✗"
	}
	fmt.Fprintf(os.Stdout, "  %s status: %s\n\n", statusIcon, health.Status)

	ui.Section("Git")
	fmt.Fprintf(os.Stdout, "  branch: %s\n", health.Git.Branch)
	fmt.Fprintf(os.Stdout, "  dirty files: %d\n", health.Git.DirtyFileCount)
	fmt.Fprintln(os.Stdout)

	ui.Section("Workflow")
	fmt.Fprintf(os.Stdout, "  has active plan: %v\n", health.Workflow.HasActivePlan)
	fmt.Fprintf(os.Stdout, "  canonical plans: %d\n", health.Workflow.CanonicalPlanCount)
	fmt.Fprintf(os.Stdout, "  has checkpoint: %v\n", health.Workflow.HasCheckpoint)
	fmt.Fprintf(os.Stdout, "  pending proposals: %d\n", health.Workflow.PendingProposals)
	fmt.Fprintln(os.Stdout)

	if len(health.Warnings) > 0 {
		ui.Section("Warnings")
		for _, w := range health.Warnings {
			fmt.Fprintf(os.Stdout, "  - %s\n", w)
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

func runWorkflowVerifyRecord(kind, status, command, scope, summary string) error {
	if !isValidVerificationKind(kind) {
		return fmt.Errorf("invalid kind %q: must be test, lint, build, format, or custom", kind)
	}
	if !isValidVerificationStatus(status) {
		return fmt.Errorf("invalid status %q: must be pass, fail, partial, or unknown", status)
	}
	if !isValidVerificationScope(scope) {
		return fmt.Errorf("invalid scope %q: must be file, package, repo, or custom", scope)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	rec := VerificationRecord{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Kind:          kind,
		Status:        status,
		Command:       command,
		Scope:         scope,
		Summary:       summary,
		Artifacts:     []string{},
		RecordedBy:    "dot-agents workflow verify record",
	}
	if err := appendVerificationLog(project.Name, rec); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Verification recorded: %s %s (%s)", kind, status, summary))
	return nil
}

func runWorkflowVerifyLog(all bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	limit := 10
	if all {
		limit = 0
	}
	records, err := readVerificationLog(project.Name, limit)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Fprintln(os.Stdout, "No verification records found.")
		return nil
	}
	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(records)
	}
	ui.Header("Verification Log")
	for _, r := range records {
		icon := "✓"
		if r.Status == "fail" {
			icon = "✗"
		} else if r.Status == "partial" {
			icon = "~"
		} else if r.Status == "unknown" {
			icon = "?"
		}
		fmt.Fprintf(os.Stdout, "  %s [%s] %s  %s\n", icon, r.Kind, r.Timestamp, r.Summary)
		if r.Command != "" {
			fmt.Fprintf(os.Stdout, "    cmd: %s\n", r.Command)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

// ── Wave 4: Shared preferences ────────────────────────────────────────────────

// WorkflowPreferences holds all workflow preference fields. Every field is a
// pointer so that "not set" is distinguishable from the zero value during merge.
type WorkflowPreferences struct {
	Verification WorkflowVerificationPrefs `json:"verification" yaml:"verification"`
	Planning     WorkflowPlanningPrefs     `json:"planning"     yaml:"planning"`
	Review       WorkflowReviewPrefs       `json:"review"       yaml:"review"`
	Execution    WorkflowExecutionPrefs    `json:"execution"    yaml:"execution"`
}

type WorkflowVerificationPrefs struct {
	TestCommand                    *string `json:"test_command,omitempty"                      yaml:"test_command,omitempty"`
	LintCommand                    *string `json:"lint_command,omitempty"                      yaml:"lint_command,omitempty"`
	RequireRegressionBeforeHandoff *bool   `json:"require_regression_before_handoff,omitempty" yaml:"require_regression_before_handoff,omitempty"`
}

type WorkflowPlanningPrefs struct {
	PlanDirectory         *string `json:"plan_directory,omitempty"          yaml:"plan_directory,omitempty"`
	RequirePlanBeforeCode *bool   `json:"require_plan_before_code,omitempty" yaml:"require_plan_before_code,omitempty"`
}

type WorkflowReviewPrefs struct {
	ReviewOrder          *string `json:"review_order,omitempty"           yaml:"review_order,omitempty"`
	RequireFindingsFirst *bool   `json:"require_findings_first,omitempty" yaml:"require_findings_first,omitempty"`
}

type WorkflowExecutionPrefs struct {
	PackageManager *string `json:"package_manager,omitempty" yaml:"package_manager,omitempty"`
	Formatter      *string `json:"formatter,omitempty"       yaml:"formatter,omitempty"`
}

// WorkflowPreferencesFile is the on-disk wrapper for preferences.yaml.
type WorkflowPreferencesFile struct {
	SchemaVersion       int `json:"schema_version" yaml:"schema_version"`
	WorkflowPreferences `yaml:",inline"      json:",inline"`
}

// preferenceSource records where a resolved preference value came from.
type preferenceSource struct {
	Key    string
	Value  string
	Source string // "default" | "repo" | "local"
}

func defaultWorkflowPreferences() WorkflowPreferences {
	trueVal := true
	testCmd := "go test ./..."
	lintCmd := "go vet ./..."
	planDir := ".agents/active"
	reviewOrder := "findings-first"
	pkgMgr := "go"
	formatter := "gofmt"
	return WorkflowPreferences{
		Verification: WorkflowVerificationPrefs{
			TestCommand:                    &testCmd,
			LintCommand:                    &lintCmd,
			RequireRegressionBeforeHandoff: &trueVal,
		},
		Planning: WorkflowPlanningPrefs{
			PlanDirectory:         &planDir,
			RequirePlanBeforeCode: &trueVal,
		},
		Review: WorkflowReviewPrefs{
			ReviewOrder:          &reviewOrder,
			RequireFindingsFirst: &trueVal,
		},
		Execution: WorkflowExecutionPrefs{
			PackageManager: &pkgMgr,
			Formatter:      &formatter,
		},
	}
}

func loadRepoPreferences(projectPath string) (*WorkflowPreferencesFile, error) {
	path := filepath.Join(projectPath, ".agents", "workflow", "preferences.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f WorkflowPreferencesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse repo preferences: %w", err)
	}
	return &f, nil
}

func loadLocalPreferences(project string) (*WorkflowPreferencesFile, error) {
	path := filepath.Join(config.ProjectContextDir(project), "preferences.local.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f WorkflowPreferencesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse local preferences: %w", err)
	}
	return &f, nil
}

// mergePreferences applies precedence: local > repo > defaults.
// Only non-nil pointer fields override.
func mergePreferences(defaults, repo, local WorkflowPreferences) WorkflowPreferences {
	out := defaults
	mergeVerificationPrefs(&out.Verification, repo.Verification)
	mergePlanningPrefs(&out.Planning, repo.Planning)
	mergeReviewPrefs(&out.Review, repo.Review)
	mergeExecutionPrefs(&out.Execution, repo.Execution)
	mergeVerificationPrefs(&out.Verification, local.Verification)
	mergePlanningPrefs(&out.Planning, local.Planning)
	mergeReviewPrefs(&out.Review, local.Review)
	mergeExecutionPrefs(&out.Execution, local.Execution)
	return out
}

func mergeVerificationPrefs(dst *WorkflowVerificationPrefs, src WorkflowVerificationPrefs) {
	if src.TestCommand != nil {
		dst.TestCommand = src.TestCommand
	}
	if src.LintCommand != nil {
		dst.LintCommand = src.LintCommand
	}
	if src.RequireRegressionBeforeHandoff != nil {
		dst.RequireRegressionBeforeHandoff = src.RequireRegressionBeforeHandoff
	}
}

func mergePlanningPrefs(dst *WorkflowPlanningPrefs, src WorkflowPlanningPrefs) {
	if src.PlanDirectory != nil {
		dst.PlanDirectory = src.PlanDirectory
	}
	if src.RequirePlanBeforeCode != nil {
		dst.RequirePlanBeforeCode = src.RequirePlanBeforeCode
	}
}

func mergeReviewPrefs(dst *WorkflowReviewPrefs, src WorkflowReviewPrefs) {
	if src.ReviewOrder != nil {
		dst.ReviewOrder = src.ReviewOrder
	}
	if src.RequireFindingsFirst != nil {
		dst.RequireFindingsFirst = src.RequireFindingsFirst
	}
}

func mergeExecutionPrefs(dst *WorkflowExecutionPrefs, src WorkflowExecutionPrefs) {
	if src.PackageManager != nil {
		dst.PackageManager = src.PackageManager
	}
	if src.Formatter != nil {
		dst.Formatter = src.Formatter
	}
}

func resolvePreferences(projectPath, project string) (WorkflowPreferences, error) {
	defaults := defaultWorkflowPreferences()
	var repo WorkflowPreferences
	repoFile, err := loadRepoPreferences(projectPath)
	if err != nil {
		return defaults, err
	}
	if repoFile != nil {
		repo = repoFile.WorkflowPreferences
	}
	var local WorkflowPreferences
	localFile, err := loadLocalPreferences(project)
	if err != nil {
		return defaults, err
	}
	if localFile != nil {
		local = localFile.WorkflowPreferences
	}
	return mergePreferences(defaults, repo, local), nil
}

var knownPreferenceKeys = map[string]struct{}{
	"verification.test_command":                      {},
	"verification.lint_command":                      {},
	"verification.require_regression_before_handoff": {},
	"planning.plan_directory":                        {},
	"planning.require_plan_before_code":              {},
	"review.review_order":                            {},
	"review.require_findings_first":                  {},
	"execution.package_manager":                      {},
	"execution.formatter":                            {},
}

func isValidPreferenceKey(key string) bool {
	_, ok := knownPreferenceKeys[key]
	return ok
}

func setLocalPreference(project, key, value string) error {
	path := filepath.Join(config.ProjectContextDir(project), "preferences.local.yaml")
	var f WorkflowPreferencesFile
	data, err := os.ReadFile(path)
	if err == nil {
		if err := yaml.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("parse local preferences: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f.SchemaVersion = 1
	if err := applyPreferenceKey(&f.WorkflowPreferences, key, value); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	out, err := yaml.Marshal(&f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func applyPreferenceKey(p *WorkflowPreferences, key, value string) error {
	switch key {
	case "verification.test_command":
		p.Verification.TestCommand = &value
	case "verification.lint_command":
		p.Verification.LintCommand = &value
	case "verification.require_regression_before_handoff":
		b := value == "true"
		p.Verification.RequireRegressionBeforeHandoff = &b
	case "planning.plan_directory":
		p.Planning.PlanDirectory = &value
	case "planning.require_plan_before_code":
		b := value == "true"
		p.Planning.RequirePlanBeforeCode = &b
	case "review.review_order":
		p.Review.ReviewOrder = &value
	case "review.require_findings_first":
		b := value == "true"
		p.Review.RequireFindingsFirst = &b
	case "execution.package_manager":
		p.Execution.PackageManager = &value
	case "execution.formatter":
		p.Execution.Formatter = &value
	default:
		return fmt.Errorf("unknown preference key %q", key)
	}
	return nil
}

func resolvePreferencesWithSources(projectPath, project string) ([]preferenceSource, error) {
	defaults := defaultWorkflowPreferences()
	var repo WorkflowPreferences
	repoFile, err := loadRepoPreferences(projectPath)
	if err != nil {
		return nil, err
	}
	if repoFile != nil {
		repo = repoFile.WorkflowPreferences
	}
	var local WorkflowPreferences
	localFile, err := loadLocalPreferences(project)
	if err != nil {
		return nil, err
	}
	if localFile != nil {
		local = localFile.WorkflowPreferences
	}
	resolved := mergePreferences(defaults, repo, local)

	strSrc := func(_, r, l *string) string {
		if l != nil {
			return "local"
		}
		if r != nil {
			return "repo"
		}
		return "default"
	}
	boolSrc := func(_, r, l *bool) string {
		if l != nil {
			return "local"
		}
		if r != nil {
			return "repo"
		}
		return "default"
	}

	return []preferenceSource{
		{"verification.test_command", strPtrVal(resolved.Verification.TestCommand), strSrc(defaults.Verification.TestCommand, repo.Verification.TestCommand, local.Verification.TestCommand)},
		{"verification.lint_command", strPtrVal(resolved.Verification.LintCommand), strSrc(defaults.Verification.LintCommand, repo.Verification.LintCommand, local.Verification.LintCommand)},
		{"verification.require_regression_before_handoff", boolPtrStr(resolved.Verification.RequireRegressionBeforeHandoff), boolSrc(defaults.Verification.RequireRegressionBeforeHandoff, repo.Verification.RequireRegressionBeforeHandoff, local.Verification.RequireRegressionBeforeHandoff)},
		{"planning.plan_directory", strPtrVal(resolved.Planning.PlanDirectory), strSrc(defaults.Planning.PlanDirectory, repo.Planning.PlanDirectory, local.Planning.PlanDirectory)},
		{"planning.require_plan_before_code", boolPtrStr(resolved.Planning.RequirePlanBeforeCode), boolSrc(defaults.Planning.RequirePlanBeforeCode, repo.Planning.RequirePlanBeforeCode, local.Planning.RequirePlanBeforeCode)},
		{"review.review_order", strPtrVal(resolved.Review.ReviewOrder), strSrc(defaults.Review.ReviewOrder, repo.Review.ReviewOrder, local.Review.ReviewOrder)},
		{"review.require_findings_first", boolPtrStr(resolved.Review.RequireFindingsFirst), boolSrc(defaults.Review.RequireFindingsFirst, repo.Review.RequireFindingsFirst, local.Review.RequireFindingsFirst)},
		{"execution.package_manager", strPtrVal(resolved.Execution.PackageManager), strSrc(defaults.Execution.PackageManager, repo.Execution.PackageManager, local.Execution.PackageManager)},
		{"execution.formatter", strPtrVal(resolved.Execution.Formatter), strSrc(defaults.Execution.Formatter, repo.Execution.Formatter, local.Execution.Formatter)},
	}, nil
}

func strPtrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolPtrStr(p *bool) string {
	if p == nil {
		return ""
	}
	if *p {
		return "true"
	}
	return "false"
}

func runWorkflowPrefs() error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if Flags.JSON {
		prefs, err := resolvePreferences(project.Path, project.Name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(prefs)
	}
	sources, err := resolvePreferencesWithSources(project.Path, project.Name)
	if err != nil {
		return err
	}
	ui.Header("Workflow Preferences")
	currentCategory := ""
	for _, s := range sources {
		parts := strings.SplitN(s.Key, ".", 2)
		if len(parts) == 2 && parts[0] != currentCategory {
			currentCategory = parts[0]
			fmt.Fprintf(os.Stdout, "\n[%s]\n", currentCategory)
		}
		fmt.Fprintf(os.Stdout, "  %-48s %s  (%s)\n", s.Key, s.Value, s.Source)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runWorkflowPrefsSetLocal(key, value string) error {
	if !isValidPreferenceKey(key) {
		return fmt.Errorf("unknown preference key %q; run 'workflow prefs' to see valid keys", key)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	if err := setLocalPreference(project.Name, key, value); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Set %s = %s  (local)", key, value))
	return nil
}

func runWorkflowPrefsSetShared(key, value string) error {
	if !isValidPreferenceKey(key) {
		return fmt.Errorf("unknown preference key %q; run 'workflow prefs' to see valid keys", key)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	sources, err := resolvePreferencesWithSources(project.Path, project.Name)
	if err != nil {
		return err
	}
	currentVal := ""
	for _, s := range sources {
		if s.Key == key {
			currentVal = s.Value
			break
		}
	}
	id := fmt.Sprintf("pref-%s-%s", strings.ReplaceAll(key, ".", "-"), time.Now().UTC().Format("20060102T150405Z"))
	targetPath := filepath.Join(".agents", "workflow", "preferences.yaml")
	proposal := &config.Proposal{
		SchemaVersion: 1,
		ID:            id,
		Status:        "pending",
		Type:          "setting",
		Action:        "modify",
		Target:        targetPath,
		Rationale:     fmt.Sprintf("Set shared workflow preference %s to %q (was %q)", key, value, currentVal),
		Content:       fmt.Sprintf("%s: %s\n", key, value),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		CreatedBy:     "workflow prefs set-shared",
	}
	if err := config.SaveProposal(proposal, config.ProposalPath(id)); err != nil {
		return fmt.Errorf("save proposal: %w", err)
	}
	ui.Info(fmt.Sprintf("Proposal %s created for shared preference change.", id))
	ui.Info("Run 'dot-agents review' to approve and apply.")
	return nil
}

// ── Wave 5: Graph bridge types ─────────────────────────────────────────────────

// ContextMapping maps a repo concept to a graph query scope.
type ContextMapping struct {
	RepoScope  string `json:"repo_scope" yaml:"repo_scope"`
	GraphScope string `json:"graph_scope" yaml:"graph_scope"`
	Intent     string `json:"intent" yaml:"intent"`
}

// GraphBridgeConfig is the schema for .agents/workflow/graph-bridge.yaml.
type GraphBridgeConfig struct {
	SchemaVersion   int              `json:"schema_version" yaml:"schema_version"`
	Enabled         bool             `json:"enabled" yaml:"enabled"`
	GraphHome       string           `json:"graph_home" yaml:"graph_home"`
	AllowedIntents  []string         `json:"allowed_intents" yaml:"allowed_intents"`
	ContextMappings []ContextMapping `json:"context_mappings" yaml:"context_mappings"`
}

var validWorkflowBridgeIntents = map[string]bool{
	"plan_context":    true,
	"decision_lookup": true,
	"entity_context":  true,
	"workflow_memory": true,
	"contradictions":  true,
}

func isValidWorkflowBridgeIntent(intent string) bool { return validWorkflowBridgeIntents[intent] }

// loadGraphBridgeConfig reads .agents/workflow/graph-bridge.yaml. If absent, bridge is disabled.
func loadGraphBridgeConfig(projectPath string) (*GraphBridgeConfig, error) {
	p := filepath.Join(projectPath, ".agents", "workflow", "graph-bridge.yaml")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &GraphBridgeConfig{Enabled: false}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg GraphBridgeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse graph-bridge.yaml: %w", err)
	}
	return &cfg, nil
}

// ── Wave 5: Bridge query contract ────────────────────────────────────────────

// GraphBridgeQuery is the input to a bridge query.
type GraphBridgeQuery struct {
	Intent  string `json:"intent"`
	Project string `json:"project"`
	Scope   string `json:"scope,omitempty"`
	Query   string `json:"query"`
}

// GraphBridgeResult is one result item.
type GraphBridgeResult struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Path       string   `json:"path"`
	SourceRefs []string `json:"source_refs,omitempty"`
}

// GraphBridgeResponse is the normalized response envelope.
type GraphBridgeResponse struct {
	SchemaVersion int                 `json:"schema_version"`
	Intent        string              `json:"intent"`
	Query         string              `json:"query"`
	Results       []GraphBridgeResult `json:"results"`
	Warnings      []string            `json:"warnings"`
	Provider      string              `json:"provider"`
	Timestamp     string              `json:"timestamp"`
}

// ── Wave 5: Graph bridge adapter ─────────────────────────────────────────────

// GraphBridgeAdapter is the interface for bridge backends.
type GraphBridgeAdapter interface {
	Query(query GraphBridgeQuery) (GraphBridgeResponse, error)
	Health() (GraphBridgeHealth, error)
}

// GraphBridgeHealth is the adapter availability and last-query status.
type GraphBridgeHealth struct {
	SchemaVersion    int      `json:"schema_version"`
	Timestamp        string   `json:"timestamp"`
	AdapterAvailable bool     `json:"adapter_available"`
	GraphHomeExists  bool     `json:"graph_home_exists"`
	NoteCount        int      `json:"note_count"`
	LastQueryTime    string   `json:"last_query_time,omitempty"`
	LastQueryStatus  string   `json:"last_query_status,omitempty"`
	Status           string   `json:"status"` // healthy|warn|error
	Warnings         []string `json:"warnings,omitempty"`
}

// writeGraphBridgeHealth writes health to ~/.agents/context/<project>/graph-bridge-health.json.
func writeGraphBridgeHealth(project string, health GraphBridgeHealth) error {
	dir := config.ProjectContextDir(project)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(health, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "graph-bridge-health.json"), data, 0644)
}

// readGraphBridgeHealth reads the cached health snapshot.
func readGraphBridgeHealth(project string) (*GraphBridgeHealth, error) {
	p := filepath.Join(config.ProjectContextDir(project), "graph-bridge-health.json")
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h GraphBridgeHealth
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// LocalGraphAdapter scans KG_HOME filesystem using simple string matching.
// This is intentionally independent of the kg package — agents use it without
// needing the kg subcommand installed.
type LocalGraphAdapter struct {
	graphHome  string
	lastQuery  string
	lastStatus string
}

func NewLocalGraphAdapter(graphHome string) *LocalGraphAdapter {
	return &LocalGraphAdapter{graphHome: graphHome}
}

func (a *LocalGraphAdapter) Health() (GraphBridgeHealth, error) {
	h := GraphBridgeHealth{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	info, err := os.Stat(a.graphHome)
	h.GraphHomeExists = err == nil && info.IsDir()
	configExists := false
	if _, err := os.Stat(filepath.Join(a.graphHome, "self", "config.yaml")); err == nil {
		configExists = true
	}
	h.AdapterAvailable = h.GraphHomeExists && configExists
	if !h.AdapterAvailable {
		h.Status = "warn"
		h.Warnings = append(h.Warnings, fmt.Sprintf("graph not initialized at %s", a.graphHome))
		return h, nil
	}
	// Count notes
	noteDirs := []string{"sources", "entities", "concepts", "synthesis", "decisions", "repos", "sessions"}
	for _, sub := range noteDirs {
		entries, err := os.ReadDir(filepath.Join(a.graphHome, "notes", sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				h.NoteCount++
			}
		}
	}
	h.LastQueryTime = a.lastQuery
	h.LastQueryStatus = a.lastStatus
	h.Status = "healthy"
	return h, nil
}

func (a *LocalGraphAdapter) Query(query GraphBridgeQuery) (GraphBridgeResponse, error) {
	resp := GraphBridgeResponse{
		SchemaVersion: 1,
		Intent:        query.Intent,
		Query:         query.Query,
		Provider:      "local-graph",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Results:       []GraphBridgeResult{},
	}

	// Map bridge intents to note types
	noteTypes := map[string][]string{
		"plan_context":    {"decisions", "synthesis"},
		"decision_lookup": {"decisions"},
		"entity_context":  {"entities"},
		"workflow_memory": {"sources", "sessions"},
		"contradictions":  {"decisions"},
	}
	subdirs, ok := noteTypes[query.Intent]
	if !ok {
		return resp, fmt.Errorf("unsupported bridge intent: %s", query.Intent)
	}

	seen := make(map[string]bool)
	q := strings.ToLower(query.Query)
	for _, sub := range subdirs {
		dir := filepath.Join(a.graphHome, "notes", sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			content := strings.ToLower(string(data))
			if q == "" || strings.Contains(content, q) {
				// Parse frontmatter for id/title/summary
				id, title, summary, srcRefs := parseNoteMetadata(string(data))
				if id == "" {
					id = strings.TrimSuffix(e.Name(), ".md")
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				resp.Results = append(resp.Results, GraphBridgeResult{
					ID:         id,
					Type:       strings.TrimSuffix(sub, "s"),
					Title:      title,
					Summary:    summary,
					Path:       filepath.Join("notes", sub, e.Name()),
					SourceRefs: srcRefs,
				})
				if len(resp.Results) >= 10 {
					break
				}
			}
		}
	}

	a.lastQuery = time.Now().UTC().Format(time.RFC3339)
	a.lastStatus = "ok"
	return resp, nil
}

// parseNoteMetadata extracts id/title/summary/source_refs from YAML frontmatter.
func parseNoteMetadata(content string) (id, title, summary string, sourceRefs []string) {
	if !strings.HasPrefix(content, "---") {
		return
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return
	}
	fm := rest[:idx]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "id: "); ok {
			id = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "title: "); ok {
			title = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "summary: "); ok {
			summary = strings.Trim(after, "\"'")
		} else if after, ok := strings.CutPrefix(line, "- "); ok && strings.Contains(fm, "source_refs:") {
			sourceRefs = append(sourceRefs, strings.Trim(after, "\"'"))
		}
	}
	return
}

// ── Wave 5: workflow graph subcommands ────────────────────────────────────────

func runWorkflowGraphQuery(cmd *cobra.Command, args []string) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := loadGraphBridgeConfig(projectPath)
	if err != nil {
		return fmt.Errorf("load bridge config: %w", err)
	}
	if !cfg.Enabled {
		return fmt.Errorf("graph bridge not configured — create .agents/workflow/graph-bridge.yaml with enabled: true")
	}

	intent, _ := cmd.Flags().GetString("intent")
	if intent == "" {
		return fmt.Errorf("--intent is required")
	}
	if !isValidWorkflowBridgeIntent(intent) {
		return fmt.Errorf("unknown intent %q — valid: plan_context, decision_lookup, entity_context, workflow_memory, contradictions", intent)
	}
	// Validate against allowed intents
	allowed := cfg.AllowedIntents
	if len(allowed) > 0 {
		ok := false
		for _, a := range allowed {
			if a == intent {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("intent %q not in allowed_intents for this repo", intent)
		}
	}

	scope, _ := cmd.Flags().GetString("scope")
	query := strings.Join(args, " ")
	graphHome := cfg.GraphHome
	if graphHome == "" {
		home, _ := os.UserHomeDir()
		graphHome = filepath.Join(home, "knowledge-graph")
	}
	adapter := NewLocalGraphAdapter(graphHome)
	resp, err := adapter.Query(GraphBridgeQuery{
		Intent:  intent,
		Project: filepath.Base(projectPath),
		Scope:   scope,
		Query:   query,
	})
	if err != nil {
		return err
	}

	// Update health
	health, _ := adapter.Health()
	health.LastQueryTime = time.Now().UTC().Format(time.RFC3339)
	health.LastQueryStatus = "ok"
	_ = writeGraphBridgeHealth(filepath.Base(projectPath), health)

	if Flags.JSON {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Graph Query: %s  [%s]", intent, query))
	if len(resp.Results) == 0 {
		ui.Info("No results found.")
	} else {
		for _, r := range resp.Results {
			ui.Bullet("found", fmt.Sprintf("[%s] %s — %s", r.Type, r.Title, r.Summary))
		}
	}
	for _, w := range resp.Warnings {
		ui.Warn(w)
	}
	return nil
}

func runWorkflowGraphHealth(_ *cobra.Command, _ []string) error {
	projectPath, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := loadGraphBridgeConfig(projectPath)
	if err != nil {
		return fmt.Errorf("load bridge config: %w", err)
	}

	graphHome := cfg.GraphHome
	if graphHome == "" {
		home, _ := os.UserHomeDir()
		graphHome = filepath.Join(home, "knowledge-graph")
	}
	adapter := NewLocalGraphAdapter(graphHome)
	health, err := adapter.Health()
	if err != nil {
		return err
	}
	_ = writeGraphBridgeHealth(filepath.Base(projectPath), health)

	if Flags.JSON {
		data, _ := json.MarshalIndent(health, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	badge := ui.ColorText(ui.Green, health.Status)
	if health.Status != "healthy" {
		badge = ui.ColorText(ui.Yellow, health.Status)
	}
	ui.Header(fmt.Sprintf("Graph Bridge Health  [%s]", badge))
	ui.Info(fmt.Sprintf("  Graph home: %s", graphHome))
	ui.Info(fmt.Sprintf("  Adapter available: %v", health.AdapterAvailable))
	ui.Info(fmt.Sprintf("  Notes: %d", health.NoteCount))
	ui.Info(fmt.Sprintf("  Bridge enabled: %v", cfg.Enabled))
	if !cfg.Enabled {
		ui.Warn("Bridge not enabled — create .agents/workflow/graph-bridge.yaml to enable")
	}
	for _, w := range health.Warnings {
		ui.Warn(w)
	}
	return nil
}

// ── Wave 6: Delegation & Merge-back ──────────────────────────────────────────

// CoordinationIntent is transport-neutral coordination between parent and delegate.
// Stored as enum field in DelegationContract, never as chat syntax or @mentions.
type CoordinationIntent string

const (
	CoordinationIntentNone             CoordinationIntent = ""
	CoordinationIntentStatusRequest    CoordinationIntent = "status_request"
	CoordinationIntentReviewRequest    CoordinationIntent = "review_request"
	CoordinationIntentEscalationNotice CoordinationIntent = "escalation_notice"
	CoordinationIntentAck              CoordinationIntent = "ack"
)

var validCoordinationIntents = map[CoordinationIntent]bool{
	CoordinationIntentNone:             true,
	CoordinationIntentStatusRequest:    true,
	CoordinationIntentReviewRequest:    true,
	CoordinationIntentEscalationNotice: true,
	CoordinationIntentAck:              true,
}

// DelegationContract declares a bounded task delegation from parent to sub-agent.
// Stored at .agents/active/delegation/<task-id>.yaml
type DelegationContract struct {
	SchemaVersion            int                `json:"schema_version" yaml:"schema_version"`
	ID                       string             `json:"id" yaml:"id"`
	ParentPlanID             string             `json:"parent_plan_id" yaml:"parent_plan_id"`
	ParentTaskID             string             `json:"parent_task_id" yaml:"parent_task_id"`
	Title                    string             `json:"title" yaml:"title"`
	Summary                  string             `json:"summary" yaml:"summary"`
	WriteScope               []string           `json:"write_scope" yaml:"write_scope"` // immutable after creation
	SuccessCriteria          string             `json:"success_criteria" yaml:"success_criteria"`
	VerificationExpectations string             `json:"verification_expectations" yaml:"verification_expectations"`
	MayMutateWorkflowState   bool               `json:"may_mutate_workflow_state" yaml:"may_mutate_workflow_state"`
	Owner                    string             `json:"owner" yaml:"owner"`   // delegate agent identity
	Status                   string             `json:"status" yaml:"status"` // pending|active|completed|failed|cancelled
	PendingIntent            CoordinationIntent `json:"pending_intent,omitempty" yaml:"pending_intent,omitempty"`
	CreatedAt                string             `json:"created_at" yaml:"created_at"`
	UpdatedAt                string             `json:"updated_at" yaml:"updated_at"`
}

var validDelegationStatuses = map[string]bool{
	"pending": true, "active": true, "completed": true, "failed": true, "cancelled": true,
}

func isValidDelegationStatus(s string) bool { return validDelegationStatuses[s] }

func delegationDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "active", "delegation")
}

func mergeBackDir(projectPath string) string {
	return filepath.Join(projectPath, ".agents", "active", "merge-back")
}

func loadDelegationContract(projectPath, taskID string) (*DelegationContract, error) {
	path := filepath.Join(delegationDir(projectPath), taskID+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c DelegationContract
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse delegation contract %s: %w", taskID, err)
	}
	return &c, nil
}

func saveDelegationContract(projectPath string, c *DelegationContract) error {
	dir := delegationDir(projectPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	c.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, c.ParentTaskID+".yaml"), data, 0644)
}

func listDelegationContracts(projectPath string) ([]DelegationContract, error) {
	dir := delegationDir(projectPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var contracts []DelegationContract
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		taskID := strings.TrimSuffix(e.Name(), ".yaml")
		c, err := loadDelegationContract(projectPath, taskID)
		if err != nil {
			continue // skip unreadable contracts
		}
		contracts = append(contracts, *c)
	}
	return contracts, nil
}

// ── Write-scope overlap detection (Wave 6 Step 2) ────────────────────────────

// writeScopeOverlaps returns conflict descriptions for any overlapping write scopes
// between active delegations and the proposed new scope.
// Detection strategy: prefix containment covers 90%+ of real cases (per RFC).
// Full glob intersection is deferred.
func writeScopeOverlaps(existing []DelegationContract, newScope []string, excludeTaskID string) []string {
	var conflicts []string
	for _, c := range existing {
		if c.Status != "pending" && c.Status != "active" {
			continue // only check live delegations
		}
		if c.ParentTaskID == excludeTaskID {
			continue
		}
		for _, np := range newScope {
			for _, ep := range c.WriteScope {
				if scopePathsOverlap(np, ep) {
					conflicts = append(conflicts, fmt.Sprintf(
						"task %s has overlapping write scope: %q overlaps %q (existing delegation for task %s)",
						excludeTaskID, np, ep, c.ParentTaskID,
					))
				}
			}
		}
	}
	return conflicts
}

// scopePathsOverlap returns true if path a and path b overlap.
// Two paths overlap if one is a prefix of the other, or they are identical.
// This handles the 90%+ case of disjoint directory trees.
func scopePathsOverlap(a, b string) bool {
	// Normalize: ensure directory paths end with /
	na := filepath.ToSlash(filepath.Clean(a))
	nb := filepath.ToSlash(filepath.Clean(b))
	// Identical
	if na == nb {
		return true
	}
	// a is prefix of b: commands/ vs commands/workflow.go
	if strings.HasPrefix(nb, na+"/") || strings.HasPrefix(na, nb+"/") {
		return true
	}
	return false
}

// ── MergeBackSummary (Wave 6 Step 3) ─────────────────────────────────────────

// MergeBackSummary is produced by the delegate and consumed by the parent.
// Stored at .agents/active/merge-back/<task-id>.md with YAML frontmatter.
type MergeBackSummary struct {
	SchemaVersion       int                   `json:"schema_version" yaml:"schema_version"`
	TaskID              string                `json:"task_id" yaml:"task_id"`
	ParentPlanID        string                `json:"parent_plan_id" yaml:"parent_plan_id"`
	Title               string                `json:"title" yaml:"title"`
	Summary             string                `json:"summary" yaml:"summary"`
	FilesChanged        []string              `json:"files_changed" yaml:"files_changed"`
	VerificationResult  MergeBackVerification `json:"verification_result" yaml:"verification_result"`
	IntegrationNotes    string                `json:"integration_notes" yaml:"integration_notes"`
	BlockersEncountered []string              `json:"blockers_encountered,omitempty" yaml:"blockers_encountered,omitempty"`
	CreatedAt           string                `json:"created_at" yaml:"created_at"`
}

// MergeBackVerification captures the delegate's self-reported verification.
type MergeBackVerification struct {
	Status  string `json:"status" yaml:"status"` // pass|fail|partial|unknown
	Summary string `json:"summary" yaml:"summary"`
}

func saveMergeBack(projectPath string, s *MergeBackSummary) error {
	dir := mergeBackDir(projectPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Render as markdown with YAML frontmatter
	frontmatter, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	content := fmt.Sprintf("---\n%s---\n\n## Summary\n\n%s\n\n## Integration Notes\n\n%s\n",
		string(frontmatter), s.Summary, s.IntegrationNotes)
	return os.WriteFile(filepath.Join(dir, s.TaskID+".md"), []byte(content), 0644)
}

func loadMergeBack(projectPath, taskID string) (*MergeBackSummary, error) {
	path := filepath.Join(mergeBackDir(projectPath), taskID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Extract YAML frontmatter
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return nil, fmt.Errorf("merge-back %s: missing frontmatter", taskID)
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil, fmt.Errorf("merge-back %s: unterminated frontmatter", taskID)
	}
	var s MergeBackSummary
	if err := yaml.Unmarshal([]byte(rest[:end]), &s); err != nil {
		return nil, fmt.Errorf("parse merge-back %s: %w", taskID, err)
	}
	return &s, nil
}

// ── workflow fanout subcommand (Wave 6 Step 5) ───────────────────────────────

func runWorkflowFanout(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	planID, _ := cmd.Flags().GetString("plan")
	taskID, _ := cmd.Flags().GetString("task")
	owner, _ := cmd.Flags().GetString("owner")
	writeScopeCSV, _ := cmd.Flags().GetString("write-scope")

	// Validate plan exists
	plan, err := loadCanonicalPlan(project.Path, planID)
	if err != nil {
		return fmt.Errorf("plan %s not found: %w", planID, err)
	}

	// Validate task exists in plan
	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %s not found: %w", planID, err)
	}
	var targetTask *CanonicalTask
	for i := range tf.Tasks {
		if tf.Tasks[i].ID == taskID {
			targetTask = &tf.Tasks[i]
			break
		}
	}
	if targetTask == nil {
		return fmt.Errorf("task %s not found in plan %s", taskID, planID)
	}
	if targetTask.Status != "pending" && targetTask.Status != "in_progress" {
		return fmt.Errorf("task %s has status %q — only pending or in_progress tasks can be delegated", taskID, targetTask.Status)
	}

	// Check for existing delegation for this task
	if _, err := loadDelegationContract(project.Path, taskID); err == nil {
		return fmt.Errorf("task %s already has an active delegation contract", taskID)
	}

	// Parse write scope
	var writeScope []string
	if writeScopeCSV != "" {
		for _, p := range strings.Split(writeScopeCSV, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				writeScope = append(writeScope, p)
			}
		}
	}

	// Check write-scope overlap
	existing, err := listDelegationContracts(project.Path)
	if err != nil {
		return fmt.Errorf("list delegations: %w", err)
	}
	if conflicts := writeScopeOverlaps(existing, writeScope, taskID); len(conflicts) > 0 {
		for _, c := range conflicts {
			ui.Warn(c)
		}
		return fmt.Errorf("delegation rejected: write scope overlaps with existing active delegation(s)")
	}

	// Create delegation contract
	now := time.Now().UTC().Format(time.RFC3339)
	contract := &DelegationContract{
		SchemaVersion:   1,
		ID:              fmt.Sprintf("del-%s-%d", taskID, time.Now().Unix()),
		ParentPlanID:    planID,
		ParentTaskID:    taskID,
		Title:           targetTask.Title,
		Summary:         fmt.Sprintf("Delegated from plan %s", plan.Title),
		WriteScope:      writeScope,
		SuccessCriteria: targetTask.Notes,
		Owner:           owner,
		Status:          "active",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := saveDelegationContract(project.Path, contract); err != nil {
		return fmt.Errorf("save delegation contract: %w", err)
	}

	// Advance task to in_progress
	if targetTask.Status == "pending" {
		targetTask.Status = "in_progress"
		if err := saveCanonicalTasks(project.Path, tf); err != nil {
			ui.Warn(fmt.Sprintf("delegation created but failed to advance task status: %v", err))
		}
	}

	ui.SuccessBox(
		fmt.Sprintf("Delegation created for task %s", taskID),
		fmt.Sprintf("Contract: .agents/active/delegation/%s.yaml", taskID),
		fmt.Sprintf("Write scope: %s", strings.Join(writeScope, ", ")),
	)
	return nil
}

// ── workflow merge-back subcommand (Wave 6 Step 6) ───────────────────────────

func runWorkflowMergeBack(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}

	taskID, _ := cmd.Flags().GetString("task")
	summary, _ := cmd.Flags().GetString("summary")
	verificationStatus, _ := cmd.Flags().GetString("verification-status")
	integrationNotes, _ := cmd.Flags().GetString("integration-notes")

	// Load delegation contract
	contract, err := loadDelegationContract(project.Path, taskID)
	if err != nil {
		return fmt.Errorf("delegation contract for task %s not found: %w", taskID, err)
	}
	if contract.Status == "completed" || contract.Status == "cancelled" {
		return fmt.Errorf("delegation for task %s is already %s", taskID, contract.Status)
	}

	// Collect changed files via git diff
	var filesChanged []string
	gitOut, err := exec.Command("git", "-C", project.Path, "diff", "--name-only", "HEAD").Output()
	if err == nil {
		for _, f := range strings.Split(strings.TrimSpace(string(gitOut)), "\n") {
			if f != "" {
				filesChanged = append(filesChanged, f)
			}
		}
	}

	// Create merge-back summary
	now := time.Now().UTC().Format(time.RFC3339)
	mergeBack := &MergeBackSummary{
		SchemaVersion: 1,
		TaskID:        taskID,
		ParentPlanID:  contract.ParentPlanID,
		Title:         contract.Title,
		Summary:       summary,
		FilesChanged:  filesChanged,
		VerificationResult: MergeBackVerification{
			Status:  verificationStatus,
			Summary: integrationNotes,
		},
		IntegrationNotes: integrationNotes,
		CreatedAt:        now,
	}
	if err := saveMergeBack(project.Path, mergeBack); err != nil {
		return fmt.Errorf("save merge-back: %w", err)
	}

	// Update delegation status to completed
	contract.Status = "completed"
	if err := saveDelegationContract(project.Path, contract); err != nil {
		ui.Warn(fmt.Sprintf("merge-back created but failed to update delegation status: %v", err))
	}

	ui.SuccessBox(
		fmt.Sprintf("Merge-back created for task %s", taskID),
		fmt.Sprintf("Artifact: .agents/active/merge-back/%s.md", taskID),
		"Parent agent should review this artifact before advancing task to completed",
	)
	return nil
}

// ── Wave 7: Cross-Repo Sweep and Drift ───────────────────────────────────────

const (
	defaultCheckpointStaleDays = 7
	defaultProposalStaleDays   = 30
)

// ManagedProject is one entry from ~./agents/config.json loaded for drift checks.
type ManagedProject struct {
	Name string
	Path string
}

// loadManagedProjects returns all registered projects from the global config.
func loadManagedProjects() ([]ManagedProject, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	names := cfg.ListProjects()
	sort.Strings(names)
	projects := make([]ManagedProject, 0, len(names))
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		if path == "" {
			continue
		}
		projects = append(projects, ManagedProject{Name: name, Path: path})
	}
	return projects, nil
}

// RepoDriftReport captures drift conditions for one managed project.
type RepoDriftReport struct {
	Project              ManagedProject `json:"project"`
	Reachable            bool           `json:"reachable"`              // false if path doesn't exist
	MissingCheckpoint    bool           `json:"missing_checkpoint"`     // no checkpoint file
	StaleCheckpoint      bool           `json:"stale_checkpoint"`       // checkpoint older than threshold
	CheckpointAgeDays    int            `json:"checkpoint_age_days"`    // -1 if no checkpoint
	StaleProposalCount   int            `json:"stale_proposal_count"`   // proposals older than threshold
	MissingWorkflowDir   bool           `json:"missing_workflow_dir"`   // no .agents/workflow/
	MissingPlanStructure bool           `json:"missing_plan_structure"` // no .agents/workflow/plans/
	Warnings             []string       `json:"warnings"`
	Status               string         `json:"status"` // healthy|warn|unreachable
}

// detectRepoDrift inspects one managed project for workflow drift.
// All checks are read-only.
func detectRepoDrift(project ManagedProject, checkpointStaleDays, proposalStaleDays int) RepoDriftReport {
	report := RepoDriftReport{Project: project, CheckpointAgeDays: -1}

	// 1. Reachability
	if _, err := os.Stat(project.Path); err != nil {
		report.Reachable = false
		report.Status = "unreachable"
		report.Warnings = append(report.Warnings, fmt.Sprintf("project path %q does not exist or is not accessible", project.Path))
		return report
	}
	report.Reachable = true

	// 2. Checkpoint existence and age
	checkpointPath := filepath.Join(config.ProjectContextDir(project.Name), "checkpoint.yaml")
	checkpointData, err := os.ReadFile(checkpointPath)
	if err != nil {
		report.MissingCheckpoint = true
		report.Warnings = append(report.Warnings, "no checkpoint found")
	} else {
		var cp workflowCheckpoint
		if err := yaml.Unmarshal(checkpointData, &cp); err == nil && cp.Timestamp != "" {
			t, err := time.Parse(time.RFC3339, cp.Timestamp)
			if err == nil {
				ageDays := int(time.Since(t).Hours() / 24)
				report.CheckpointAgeDays = ageDays
				if ageDays > checkpointStaleDays {
					report.StaleCheckpoint = true
					report.Warnings = append(report.Warnings, fmt.Sprintf("checkpoint is %d days old (threshold: %d)", ageDays, checkpointStaleDays))
				}
			}
		}
	}

	// 3. Stale proposals
	proposals, err := config.ListPendingProposals()
	if err == nil {
		cutoff := time.Now().UTC().AddDate(0, 0, -proposalStaleDays)
		for _, p := range proposals {
			t, err := time.Parse(time.RFC3339, p.CreatedAt)
			if err == nil && t.Before(cutoff) {
				report.StaleProposalCount++
			}
		}
		if report.StaleProposalCount > 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%d stale proposals (older than %d days)", report.StaleProposalCount, proposalStaleDays))
		}
	}

	// 4. Workflow directory presence
	workflowDir := filepath.Join(project.Path, ".agents", "workflow")
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		report.MissingWorkflowDir = true
		report.Warnings = append(report.Warnings, "no .agents/workflow/ directory — workflow not initialized")
	}

	// 5. Canonical plan structure
	plansDir := filepath.Join(project.Path, ".agents", "workflow", "plans")
	if _, err := os.Stat(plansDir); os.IsNotExist(err) {
		report.MissingPlanStructure = true
		// Only warn if workflow dir exists (otherwise workflow dir warning is enough)
		if !report.MissingWorkflowDir {
			report.Warnings = append(report.Warnings, "no .agents/workflow/plans/ directory — no canonical plans")
		}
	}

	if len(report.Warnings) == 0 {
		report.Status = "healthy"
	} else {
		report.Status = "warn"
	}
	return report
}

// AggregateDriftReport summarizes drift across all managed projects.
type AggregateDriftReport struct {
	Timestamp        string            `json:"timestamp"`
	TotalProjects    int               `json:"total_projects"`
	ProjectsChecked  int               `json:"projects_checked"`
	Reports          []RepoDriftReport `json:"reports"`
	HealthyCount     int               `json:"healthy_count"`
	WarnCount        int               `json:"warn_count"`
	UnreachableCount int               `json:"unreachable_count"`
	TopWarnings      []string          `json:"top_warnings"`
}

// aggregateDrift combines per-repo reports into a summary.
func aggregateDrift(reports []RepoDriftReport) AggregateDriftReport {
	agg := AggregateDriftReport{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		TotalProjects: len(reports),
		Reports:       reports,
	}
	seen := make(map[string]bool)
	for _, r := range reports {
		agg.ProjectsChecked++
		switch r.Status {
		case "healthy":
			agg.HealthyCount++
		case "unreachable":
			agg.UnreachableCount++
		default:
			agg.WarnCount++
		}
		for _, w := range r.Warnings {
			if !seen[w] {
				seen[w] = true
				agg.TopWarnings = append(agg.TopWarnings, fmt.Sprintf("[%s] %s", r.Project.Name, w))
			}
		}
	}
	return agg
}

// sweepLogPath returns the path for the sweep operation log.
func sweepLogPath() string {
	return filepath.Join(config.AgentsContextDir(), "sweep-log.jsonl")
}

// driftReportPath returns the path for the persisted drift report.
func driftReportPath() string {
	return filepath.Join(config.AgentsContextDir(), "drift-report.json")
}

// saveDriftReport writes the aggregate drift report to disk.
func saveDriftReport(agg AggregateDriftReport) error {
	if err := os.MkdirAll(config.AgentsContextDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(agg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(driftReportPath(), data, 0644)
}

// runWorkflowDrift is the read-only cross-repo drift detection command.
func runWorkflowDrift(cmd *cobra.Command, _ []string) error {
	checkpointDays, _ := cmd.Flags().GetInt("stale-days")
	proposalDays, _ := cmd.Flags().GetInt("proposal-days")
	projectFilter, _ := cmd.Flags().GetString("project")

	projects, err := loadManagedProjects()
	if err != nil {
		return fmt.Errorf("load managed projects: %w", err)
	}
	if len(projects) == 0 {
		ui.Info("No managed projects registered. Add one with: dot-agents add <path>")
		return nil
	}

	// Filter to single project if requested
	if projectFilter != "" {
		var filtered []ManagedProject
		for _, p := range projects {
			if p.Name == projectFilter {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("project %q not found in managed projects", projectFilter)
		}
		projects = filtered
	}

	// Run drift detection
	reports := make([]RepoDriftReport, 0, len(projects))
	for _, p := range projects {
		reports = append(reports, detectRepoDrift(p, checkpointDays, proposalDays))
	}
	agg := aggregateDrift(reports)

	// Save to disk
	_ = saveDriftReport(agg)

	if Flags.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(agg)
	}

	// Human-readable output
	ui.Header("Workflow Drift Report")
	fmt.Fprintf(os.Stdout, "  %s projects checked%s\n\n", ui.Bold, ui.Reset)

	for _, r := range reports {
		statusBadge := ui.ColorText(ui.Green, "healthy")
		if r.Status == "warn" {
			statusBadge = ui.ColorText(ui.Yellow, "warn")
		} else if r.Status == "unreachable" {
			statusBadge = ui.ColorText(ui.Red, "unreachable")
		}
		fmt.Fprintf(os.Stdout, "  %-20s [%s]\n", r.Project.Name, statusBadge)
		for _, w := range r.Warnings {
			fmt.Fprintf(os.Stdout, "    %s↳ %s%s\n", ui.Dim, ui.Reset, w)
		}
	}
	fmt.Fprintln(os.Stdout)

	ui.Section("Summary")
	fmt.Fprintf(os.Stdout, "  healthy: %d  warnings: %d  unreachable: %d\n",
		agg.HealthyCount, agg.WarnCount, agg.UnreachableCount)
	fmt.Fprintf(os.Stdout, "  report saved: %s\n", config.DisplayPath(driftReportPath()))
	return nil
}

// ── Wave 7: Sweep types ───────────────────────────────────────────────────────

// SweepActionType enumerates the kinds of fixes the sweep can apply.
type SweepActionType string

const (
	SweepActionScaffoldWorkflowDir      SweepActionType = "scaffold_workflow_dir"
	SweepActionCreatePlanStructure      SweepActionType = "create_plan_structure"
	SweepActionCreateCheckpointReminder SweepActionType = "create_checkpoint_reminder"
	SweepActionFlagStaleProposals       SweepActionType = "flag_stale_proposals"
)

// SweepActionItem is one actionable fix in a sweep plan.
type SweepActionItem struct {
	Project              ManagedProject  `json:"project"`
	Action               SweepActionType `json:"action"`
	Description          string          `json:"description"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
}

// SweepPlan is the collection of planned actions for a sweep run.
type SweepPlan struct {
	CreatedAt string            `json:"created_at"`
	Actions   []SweepActionItem `json:"actions"`
}

// planSweep generates a sweep plan from drift reports.
func planSweep(reports []RepoDriftReport) SweepPlan {
	plan := SweepPlan{CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, r := range reports {
		if !r.Reachable {
			continue // can't fix unreachable projects
		}
		if r.MissingWorkflowDir {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionScaffoldWorkflowDir,
				Description:          fmt.Sprintf("Create .agents/workflow/ directory in %s", r.Project.Name),
				RequiresConfirmation: true,
			})
		}
		if r.MissingPlanStructure && !r.MissingWorkflowDir {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionCreatePlanStructure,
				Description:          fmt.Sprintf("Create .agents/workflow/plans/ directory in %s", r.Project.Name),
				RequiresConfirmation: true,
			})
		}
		if r.MissingCheckpoint || r.StaleCheckpoint {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionCreateCheckpointReminder,
				Description:          fmt.Sprintf("Add checkpoint reminder annotation for %s", r.Project.Name),
				RequiresConfirmation: false, // read-only annotation, no mutation
			})
		}
		if r.StaleProposalCount > 0 {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionFlagStaleProposals,
				Description:          fmt.Sprintf("Flag %d stale proposal(s) in %s for review", r.StaleProposalCount, r.Project.Name),
				RequiresConfirmation: false, // flagging only, not deleting
			})
		}
	}
	return plan
}

// SweepLogEntry is one record in sweep-log.jsonl.
type SweepLogEntry struct {
	Timestamp   string          `json:"timestamp"`
	Project     string          `json:"project"`
	Action      SweepActionType `json:"action"`
	Description string          `json:"description"`
	Applied     bool            `json:"applied"`
	DryRun      bool            `json:"dry_run"`
}

// appendSweepLog appends one entry to the sweep log.
func appendSweepLog(entry SweepLogEntry) {
	_ = os.MkdirAll(filepath.Dir(sweepLogPath()), 0755)
	f, err := os.OpenFile(sweepLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	data, _ := json.Marshal(entry)
	_, _ = f.Write(append(data, '\n'))
}

// applySweepAction executes one sweep action.
func applySweepAction(item SweepActionItem) error {
	switch item.Action {
	case SweepActionScaffoldWorkflowDir:
		return os.MkdirAll(filepath.Join(item.Project.Path, ".agents", "workflow"), 0755)
	case SweepActionCreatePlanStructure:
		return os.MkdirAll(filepath.Join(item.Project.Path, ".agents", "workflow", "plans"), 0755)
	case SweepActionCreateCheckpointReminder, SweepActionFlagStaleProposals:
		// These are informational; logged but no filesystem mutation
		return nil
	default:
		return fmt.Errorf("unknown sweep action %q", item.Action)
	}
}

// runWorkflowSweep runs drift detection and optionally applies fixes.
func runWorkflowSweep(cmd *cobra.Command, _ []string) error {
	checkpointDays, _ := cmd.Flags().GetInt("stale-days")
	proposalDays, _ := cmd.Flags().GetInt("proposal-days")
	applyFlag, _ := cmd.Flags().GetBool("apply")
	dryRun := !applyFlag

	projects, err := loadManagedProjects()
	if err != nil {
		return fmt.Errorf("load managed projects: %w", err)
	}
	if len(projects) == 0 {
		ui.Info("No managed projects registered.")
		return nil
	}

	// Run drift detection
	reports := make([]RepoDriftReport, 0, len(projects))
	for _, p := range projects {
		reports = append(reports, detectRepoDrift(p, checkpointDays, proposalDays))
	}

	plan := planSweep(reports)
	if len(plan.Actions) == 0 {
		ui.Success("No sweep actions needed — all projects look healthy.")
		return nil
	}

	modeLabel := "dry-run"
	if !dryRun {
		modeLabel = "apply"
	}
	ui.Header(fmt.Sprintf("Sweep Plan [%s]", modeLabel))
	fmt.Fprintln(os.Stdout)

	for i, action := range plan.Actions {
		marker := "○"
		if action.RequiresConfirmation && !dryRun {
			marker = "⚡"
		}
		fmt.Fprintf(os.Stdout, "  %s %d. [%s] %s\n", marker, i+1, action.Project.Name, action.Description)
	}
	fmt.Fprintln(os.Stdout)

	if dryRun {
		ui.Info("Run with --apply to execute these actions.")
		for _, action := range plan.Actions {
			appendSweepLog(SweepLogEntry{
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
				Project:     action.Project.Name,
				Action:      action.Action,
				Description: action.Description,
				Applied:     false,
				DryRun:      true,
			})
		}
		return nil
	}

	// Apply with per-action confirmation for destructive actions
	applied := 0
	for _, action := range plan.Actions {
		if action.RequiresConfirmation && !Flags.Yes {
			fmt.Fprintf(os.Stdout, "  Apply: %s? [y/N] ", action.Description)
			var resp string
			fmt.Scanln(&resp)
			if strings.ToLower(strings.TrimSpace(resp)) != "y" {
				ui.Info(fmt.Sprintf("  Skipped: %s", action.Description))
				appendSweepLog(SweepLogEntry{
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
					Project:     action.Project.Name,
					Action:      action.Action,
					Description: action.Description,
					Applied:     false,
					DryRun:      false,
				})
				continue
			}
		}
		if err := applySweepAction(action); err != nil {
			ui.Warn(fmt.Sprintf("Failed: %s — %v", action.Description, err))
		} else {
			applied++
			ui.Success(fmt.Sprintf("Applied: %s", action.Description))
		}
		appendSweepLog(SweepLogEntry{
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			Project:     action.Project.Name,
			Action:      action.Action,
			Description: action.Description,
			Applied:     true,
			DryRun:      false,
		})
	}
	fmt.Fprintln(os.Stdout)
	ui.Success(fmt.Sprintf("Sweep complete: %d/%d actions applied.", applied, len(plan.Actions)))
	return nil
}
