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
	Project        workflowProjectRef             `json:"project"`
	Git            workflowGitSummary             `json:"git"`
	ActivePlans    []workflowPlanSummary          `json:"active_plans"`
	CanonicalPlans []workflowCanonicalPlanSummary `json:"canonical_plans"`
	Checkpoint     *workflowCheckpoint            `json:"checkpoint"`
	Handoffs       []workflowHandoffSummary       `json:"handoffs"`
	Lessons        []string                       `json:"lessons"`
	Proposals      workflowProposalSummary        `json:"proposals"`
	NextAction     string                         `json:"next_action"`
	Warnings       []string                       `json:"warnings"`
	Health         *WorkflowHealthSnapshot        `json:"health,omitempty"`
	Preferences    *WorkflowPreferences           `json:"preferences,omitempty"`
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
	Kind          string   `json:"kind"`     // test|lint|build|format|custom
	Status        string   `json:"status"`   // pass|fail|partial|unknown
	Command       string   `json:"command"`
	Scope         string   `json:"scope"`    // file|package|repo|custom
	Summary       string   `json:"summary"`
	Artifacts     []string `json:"artifacts"`
	RecordedBy    string   `json:"recorded_by"`
}

// WorkflowHealthSnapshot is the health.json schema
type WorkflowHealthSnapshot struct {
	SchemaVersion int `json:"schema_version"`
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
	planCmd.AddCommand(planShowCmd)

	// tasks subcommand
	tasksCmd := &cobra.Command{
		Use:   "tasks <plan-id>",
		Short: "Show tasks for a canonical plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowTasks(args[0])
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

	cmd.AddCommand(statusCmd, orientCmd, checkpointCmd, logCmd, planCmd, tasksCmd, advanceCmd, healthCmd, verifyCmd, prefsCmd)
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
	fmt.Fprintf(os.Stdout, "  %s\n", state.NextAction)

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
		NextAction: deriveWorkflowNextAction(checkpoint, canonicalPlans, activePlans),
		Warnings:   warnings,
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

func deriveWorkflowNextAction(checkpoint *workflowCheckpoint, canonicalPlans []workflowCanonicalPlanSummary, plans []workflowPlanSummary) string {
	if checkpoint != nil && strings.TrimSpace(checkpoint.NextAction) != "" {
		return strings.TrimSpace(checkpoint.NextAction)
	}
	// Prefer canonical plan focus task over legacy plan pending items
	for _, cp := range canonicalPlans {
		if cp.Status == "active" && strings.TrimSpace(cp.CurrentFocusTask) != "" {
			return strings.TrimSpace(cp.CurrentFocusTask)
		}
	}
	for _, plan := range plans {
		if len(plan.PendingItems) > 0 {
			return plan.PendingItems[0]
		}
	}
	return workflowDefaultNextAction
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
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
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
	if len(pending) == 0 {
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

	if Flags.JSON {
		out := map[string]interface{}{"plan": plan}
		if tasksErr == nil {
			out["tasks"] = tf
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
	return nil
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
	SchemaVersion       int                  `json:"schema_version" yaml:"schema_version"`
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
