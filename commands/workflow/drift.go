package workflow

import (
	"encoding/json"
	"fmt"
	"os"
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
	defaultCheckpointStaleDays = 7
	defaultProposalStaleDays   = 30
	driftAgentsDir             = ".agents"
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
	Project                     ManagedProject `json:"project"`
	Reachable                   bool           `json:"reachable"`                      // false if path doesn't exist
	MissingCheckpoint           bool           `json:"missing_checkpoint"`             // no checkpoint file
	StaleCheckpoint             bool           `json:"stale_checkpoint"`               // checkpoint older than threshold
	CheckpointAgeDays           int            `json:"checkpoint_age_days"`            // -1 if no checkpoint
	StaleProposalCount          int            `json:"stale_proposal_count"`           // proposals older than threshold
	MissingWorkflowDir          bool           `json:"missing_workflow_dir"`           // no .agents/workflow/
	MissingPlanStructure        bool           `json:"missing_plan_structure"`         // no .agents/workflow/plans/
	CompletedPlanIDs            []string       `json:"completed_plan_ids"`             // plans with status==completed (hygiene signal)
	InconsistentArchivedPlanIDs []string       `json:"inconsistent_archived_plan_ids"` // plans with status==archived still in workflow/plans/ (error-level)
	Warnings                    []string       `json:"warnings"`
	Status                      string         `json:"status"` // healthy|warn|unreachable
}

// extractPlanStatus reads the status field from a PLAN.yaml byte slice.
// Returns empty string if parsing fails or status is absent.
func extractPlanStatus(data []byte) string {
	var plan struct {
		Status string `yaml:"status"`
	}
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return ""
	}
	return plan.Status
}

func driftCheckpointPhase(report *RepoDriftReport, project ManagedProject, checkpointStaleDays int) {
	checkpointPath := filepath.Join(config.ProjectContextDir(project.Name), "checkpoint.yaml")
	checkpointData, err := os.ReadFile(checkpointPath)
	if err != nil {
		report.MissingCheckpoint = true
		report.Warnings = append(report.Warnings, "no checkpoint found")
		return
	}
	var cp workflowCheckpoint
	if err := yaml.Unmarshal(checkpointData, &cp); err != nil || cp.Timestamp == "" {
		return
	}
	t, err := time.Parse(time.RFC3339, cp.Timestamp)
	if err != nil {
		return
	}
	ageDays := int(time.Since(t).Hours() / 24)
	report.CheckpointAgeDays = ageDays
	if ageDays > checkpointStaleDays {
		report.StaleCheckpoint = true
		report.Warnings = append(report.Warnings, fmt.Sprintf("checkpoint is %d days old (threshold: %d)", ageDays, checkpointStaleDays))
	}
}

func driftStaleProposalPhase(report *RepoDriftReport, proposalStaleDays int) {
	proposals, err := config.ListPendingProposals()
	if err != nil {
		return
	}
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

func driftWorkflowDirPhase(report *RepoDriftReport, project ManagedProject) {
	workflowDir := filepath.Join(project.Path, driftAgentsDir, "workflow")
	if _, err := os.Stat(workflowDir); os.IsNotExist(err) {
		report.MissingWorkflowDir = true
		report.Warnings = append(report.Warnings, "no .agents/workflow/ directory — workflow not initialized")
	}
	plansDir := filepath.Join(project.Path, driftAgentsDir, "workflow", "plans")
	if _, err := os.Stat(plansDir); os.IsNotExist(err) {
		report.MissingPlanStructure = true
		if !report.MissingWorkflowDir {
			report.Warnings = append(report.Warnings, "no .agents/workflow/plans/ directory — no canonical plans")
		}
	}
}

func recordPlanStatusDrift(report *RepoDriftReport, planID, status string) {
	switch status {
	case "completed":
		report.CompletedPlanIDs = append(report.CompletedPlanIDs, planID)
		report.Warnings = append(report.Warnings, fmt.Sprintf("plan %q is completed but not archived", planID))
	case "archived":
		report.InconsistentArchivedPlanIDs = append(report.InconsistentArchivedPlanIDs, planID)
		report.Warnings = append(report.Warnings, fmt.Sprintf("plan %q has status=archived but still exists in workflow/plans/ — archive may be incomplete", planID))
	}
}

func driftPlanScanPhase(report *RepoDriftReport, project ManagedProject) {
	if report.MissingPlanStructure {
		return
	}
	plansDir := filepath.Join(project.Path, driftAgentsDir, "workflow", "plans")
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		planFile := filepath.Join(plansDir, e.Name(), "PLAN.yaml")
		data, err := os.ReadFile(planFile)
		if err != nil {
			continue
		}
		recordPlanStatusDrift(report, e.Name(), extractPlanStatus(data))
	}
}

// detectRepoDrift inspects one managed project for workflow drift.
// All checks are read-only.
func detectRepoDrift(project ManagedProject, checkpointStaleDays, proposalStaleDays int) RepoDriftReport {
	report := RepoDriftReport{
		Project:                     project,
		CheckpointAgeDays:           -1,
		CompletedPlanIDs:            []string{},
		InconsistentArchivedPlanIDs: []string{},
	}

	if _, err := os.Stat(project.Path); err != nil {
		report.Reachable = false
		report.Status = "unreachable"
		report.Warnings = append(report.Warnings, fmt.Sprintf("project path %q does not exist or is not accessible", project.Path))
		return report
	}
	report.Reachable = true

	driftCheckpointPhase(&report, project, checkpointStaleDays)
	driftStaleProposalPhase(&report, proposalStaleDays)
	driftWorkflowDirPhase(&report, project)
	driftPlanScanPhase(&report, project)

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

func filterDriftProjects(projects []ManagedProject, projectFilter string) ([]ManagedProject, error) {
	if projectFilter == "" {
		return projects, nil
	}
	var filtered []ManagedProject
	for _, p := range projects {
		if p.Name == projectFilter {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("project %q not found in managed projects", projectFilter)
	}
	return filtered, nil
}

func driftStatusBadge(status string) string {
	switch status {
	case "warn":
		return ui.ColorText(ui.Yellow, "warn")
	case "unreachable":
		return ui.ColorText(ui.Red, "unreachable")
	default:
		return ui.ColorText(ui.Green, "healthy")
	}
}

func renderDriftReport(reports []RepoDriftReport, agg AggregateDriftReport) {
	ui.Header("Workflow Drift Report")
	fmt.Fprintf(os.Stdout, "  %s projects checked%s\n\n", ui.Bold, ui.Reset)

	for _, r := range reports {
		fmt.Fprintf(os.Stdout, "  %-20s [%s]\n", r.Project.Name, driftStatusBadge(r.Status))
		for _, w := range r.Warnings {
			fmt.Fprintf(os.Stdout, "    %s↳ %s%s\n", ui.Dim, ui.Reset, w)
		}
		if len(r.CompletedPlanIDs) > 0 {
			fmt.Fprintf(os.Stdout, "    %s↳ completed plans pending archive: %s%s\n", ui.Dim, ui.Reset, joinIDs(r.CompletedPlanIDs))
		}
		if len(r.InconsistentArchivedPlanIDs) > 0 {
			fmt.Fprintf(os.Stdout, "    %s↳ %sinconsistent archived plans: %s\n", ui.Dim, ui.Reset, joinIDs(r.InconsistentArchivedPlanIDs))
		}
	}
	fmt.Fprintln(os.Stdout)

	ui.Section("Summary")
	fmt.Fprintf(os.Stdout, "  healthy: %d  warnings: %d  unreachable: %d\n",
		agg.HealthyCount, agg.WarnCount, agg.UnreachableCount)
	fmt.Fprintf(os.Stdout, "  report saved: %s\n", config.DisplayPath(driftReportPath()))
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

	projects, err = filterDriftProjects(projects, projectFilter)
	if err != nil {
		return err
	}

	reports := make([]RepoDriftReport, 0, len(projects))
	for _, p := range projects {
		reports = append(reports, detectRepoDrift(p, checkpointDays, proposalDays))
	}
	agg := aggregateDrift(reports)

	_ = saveDriftReport(agg)

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(agg)
	}

	renderDriftReport(reports, agg)
	return nil
}

// joinIDs joins a slice of IDs with ", " for display.
func joinIDs(ids []string) string {
	return strings.Join(ids, ", ")
}
