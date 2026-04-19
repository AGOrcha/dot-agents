package workflow

import (
	"bufio"
	"encoding/json"
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
	"go.yaml.in/yaml/v3"
)

func runWorkflowStatus() error {
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}
	if deps.Flags.JSON() {
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
	if deps.Flags.JSON() {
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
		if !e.IsDir() {
			continue
		}
		planPath := filepath.Join(base, e.Name(), "PLAN.yaml")
		if _, err := os.Stat(planPath); err == nil {
			ids = append(ids, e.Name())
			continue
		} else if os.IsNotExist(err) {
			continue
		} else {
			return nil, err
		}
	}
	sort.Strings(ids)
	return ids, nil
}
