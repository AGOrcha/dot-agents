package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/kg/lockfile"
	"github.com/AGOrcha/dot-agents/internal/ui"
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
	// BridgeConsumerStatus is the §11.4-criterion-4 finding (spec
	// graph-backend-adapter-contract): one of consumers_found|clean|
	// not_a_kg_repo|error. See driftBridgeConsumerPhase.
	BridgeConsumerStatus string                      `json:"bridge_consumer_status,omitempty"`
	BridgeConsumers      []graphstore.BridgeConsumer `json:"bridge_consumers,omitempty"`
	Warnings             []string                    `json:"warnings"`
	Status               string                      `json:"status"` // healthy|warn|unreachable
}

// Bridge-consumer status values for RepoDriftReport.BridgeConsumerStatus —
// the §11.4-criterion-4 sweep (spec graph-backend-adapter-contract §11.4):
// "zero reads_from:[crg-bridge] across the managed repo set" before the CRG
// bridge (t6d) can be deleted.
const (
	bridgeConsumerStatusConsumersFound = "consumers_found"
	bridgeConsumerStatusClean          = "clean"
	bridgeConsumerStatusNotAKGRepo     = "not_a_kg_repo"
	bridgeConsumerStatusError          = "error"
)

// driftBridgeConsumerPhase wires graphstore.BridgeConsumers into workflow
// drift (§11.4 criterion 4, docs/crg-bridge-consumer-audit.md §[E]): a
// repo's .agentsrc.lock "adapters" section is scanned for materialized views
// that still declare a dependency on the crg-bridge mirror. Any live
// consumer is a drift finding — the CRG bridge cannot be decommissioned
// (t6d) while readers remain. A repo whose lockfile has no "adapters"
// section at all has never activated a graph-backend adapter; that is
// reported not_a_kg_repo rather than clean, so the sweep summary
// distinguishes "verified clean" from "nothing to verify".
func driftBridgeConsumerPhase(report *RepoDriftReport, project ManagedProject) {
	lf, err := lockfile.Load(config.AgentsLockPath(project.Path))
	if err != nil {
		report.BridgeConsumerStatus = bridgeConsumerStatusError
		report.Warnings = append(report.Warnings, fmt.Sprintf("could not read adapter lockfile: %v", err))
		return
	}
	if len(lf.Adapters) == 0 {
		report.BridgeConsumerStatus = bridgeConsumerStatusNotAKGRepo
		return
	}
	consumers := graphstore.BridgeConsumers(lf)
	if len(consumers) == 0 {
		report.BridgeConsumerStatus = bridgeConsumerStatusClean
		return
	}
	report.BridgeConsumerStatus = bridgeConsumerStatusConsumersFound
	report.BridgeConsumers = consumers
	names := make([]string, 0, len(consumers))
	for _, c := range consumers {
		names = append(names, fmt.Sprintf("%s/%s", c.Adapter, c.View))
	}
	report.Warnings = append(report.Warnings, fmt.Sprintf(
		"%d live crg-bridge consumer(s) — §11.4 criterion 4 not met: %s",
		len(consumers), strings.Join(names, ", ")))
}

// extractPlanStatus reads the status field from a PLAN.yaml byte slice.
// Returns empty string if parsing fails or status is absent.
func extractPlanStatus(data []byte) string {
	status, _ := extractPlanStatusChecked(data)
	return status
}

// extractPlanStatusChecked is extractPlanStatus's error-aware twin: it
// additionally reports whether the YAML failed to parse (vs. parsing fine
// but the status field simply being absent), so driftPlanScanPhase can
// distinguish "this PLAN.yaml is corrupt" from "no status field" instead of
// treating both identically as silent no-signal.
func extractPlanStatusChecked(data []byte) (string, error) {
	var plan struct {
		Status string `yaml:"status"`
	}
	if err := yaml.Unmarshal(data, &plan); err != nil {
		return "", err
	}
	return plan.Status, nil
}

func driftCheckpointPhase(report *RepoDriftReport, project ManagedProject, checkpointStaleDays int) {
	checkpointPath := filepath.Join(config.ProjectContextDir(project.Name), "checkpoint.yaml")
	checkpointData, err := os.ReadFile(checkpointPath)
	if err != nil {
		report.MissingCheckpoint = true
		if os.IsNotExist(err) {
			report.Warnings = append(report.Warnings, "no checkpoint found")
		} else {
			// A REAL read error (permission denied, etc.) is not the same as
			// "never checkpointed" — say so instead of reusing the generic
			// absence message, which would defeat the drift check's purpose.
			report.Warnings = append(report.Warnings, fmt.Sprintf("checkpoint.yaml unreadable: %v", err))
		}
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
		if !os.IsNotExist(err) {
			// MissingPlanStructure already covers "plans/ doesn't exist"; a
			// real ReadDir error here (permission denied, TOCTOU) must not
			// be silent — it means this scan ran zero plans with no signal.
			report.Warnings = append(report.Warnings, fmt.Sprintf("could not scan workflow/plans/: %v", err))
		}
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		planFile := filepath.Join(plansDir, e.Name(), "PLAN.yaml")
		data, err := os.ReadFile(planFile)
		if err != nil {
			if !os.IsNotExist(err) {
				report.Warnings = append(report.Warnings, fmt.Sprintf("could not read %s/PLAN.yaml: %v", e.Name(), err))
			}
			continue
		}
		status, err := extractPlanStatusChecked(data)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("could not parse %s/PLAN.yaml: %v", e.Name(), err))
			continue
		}
		recordPlanStatusDrift(report, e.Name(), status)
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
	driftBridgeConsumerPhase(&report, project)

	if len(report.Warnings) == 0 {
		report.Status = "healthy"
	} else {
		report.Status = "warn"
	}
	return report
}

// AggregateDriftReport summarizes drift across all managed projects.
type AggregateDriftReport struct {
	Timestamp        string             `json:"timestamp"`
	TotalProjects    int                `json:"total_projects"`
	ProjectsChecked  int                `json:"projects_checked"`
	Reports          []RepoDriftReport  `json:"reports"`
	HealthyCount     int                `json:"healthy_count"`
	WarnCount        int                `json:"warn_count"`
	UnreachableCount int                `json:"unreachable_count"`
	TopWarnings      []string           `json:"top_warnings"`
	BridgeSweep      BridgeSweepSummary `json:"bridge_sweep"`
}

// BridgeSweepSummary buckets each checked repo's §11.4-criterion-4
// bridge-consumer status (docs/crg-bridge-consumer-audit.md): the
// managed-repo-set companion to the per-repo finding on RepoDriftReport.
// Criterion 4 ("zero reads_from:[crg-bridge] across the managed repo set")
// is satisfied exactly when ConsumersFoundRepos is empty.
type BridgeSweepSummary struct {
	ConsumersFoundRepos []string `json:"consumers_found_repos"`
	CleanRepos          []string `json:"clean_repos"`
	NotAKGRepoRepos     []string `json:"not_a_kg_repo_repos"`
	ErrorRepos          []string `json:"error_repos,omitempty"`
}

// aggregateDrift combines per-repo reports into a summary.
func aggregateDrift(reports []RepoDriftReport) AggregateDriftReport {
	agg := AggregateDriftReport{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		TotalProjects: len(reports),
		Reports:       reports,
		BridgeSweep: BridgeSweepSummary{
			ConsumersFoundRepos: []string{},
			CleanRepos:          []string{},
			NotAKGRepoRepos:     []string{},
		},
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
		switch r.BridgeConsumerStatus {
		case bridgeConsumerStatusConsumersFound:
			agg.BridgeSweep.ConsumersFoundRepos = append(agg.BridgeSweep.ConsumersFoundRepos, r.Project.Name)
		case bridgeConsumerStatusClean:
			agg.BridgeSweep.CleanRepos = append(agg.BridgeSweep.CleanRepos, r.Project.Name)
		case bridgeConsumerStatusNotAKGRepo:
			agg.BridgeSweep.NotAKGRepoRepos = append(agg.BridgeSweep.NotAKGRepoRepos, r.Project.Name)
		case bridgeConsumerStatusError:
			agg.BridgeSweep.ErrorRepos = append(agg.BridgeSweep.ErrorRepos, r.Project.Name)
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
	if err := osMkdirAll(config.AgentsContextDir(), 0755); err != nil {
		return err
	}
	data, err := jsonMarshalIndent(agg, "", "  ")
	if err != nil {
		return err
	}
	return osWriteFile(driftReportPath(), data, 0644)
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

	renderBridgeSweepSummary(agg.BridgeSweep)
}

// renderBridgeSweepSummary prints the §11.4-criterion-4 bridge-consumer
// sweep: which repos in the checked set still read the crg-bridge mirror,
// which are verified clean, and which have never activated a graph-backend
// adapter (not_a_kg_repo). Silent when nothing was evaluated (e.g. every
// project was unreachable).
func renderBridgeSweepSummary(sweep BridgeSweepSummary) {
	total := len(sweep.ConsumersFoundRepos) + len(sweep.CleanRepos) + len(sweep.NotAKGRepoRepos) + len(sweep.ErrorRepos)
	if total == 0 {
		return
	}
	ui.Section("crg-bridge Consumer Sweep (§11.4 criterion 4)")
	if len(sweep.ConsumersFoundRepos) > 0 {
		fmt.Fprintf(os.Stdout, "  %sconsumers found%s: %s\n", ui.Red, ui.Reset, joinIDs(sweep.ConsumersFoundRepos))
	} else {
		fmt.Fprintf(os.Stdout, "  %szero live crg-bridge consumers%s across the checked repo set — criterion 4 satisfied.\n", ui.Green, ui.Reset)
	}
	fmt.Fprintf(os.Stdout, "  clean: %d   not-a-kg-repo: %d", len(sweep.CleanRepos), len(sweep.NotAKGRepoRepos))
	if len(sweep.ErrorRepos) > 0 {
		fmt.Fprintf(os.Stdout, "   errors: %d (%s)", len(sweep.ErrorRepos), joinIDs(sweep.ErrorRepos))
	}
	fmt.Fprintln(os.Stdout)
}

// runWorkflowDrift is the read-only cross-repo drift detection command.
func runWorkflowDrift(cmd *cobra.Command, _ []string) error {
	return runWorkflowDriftWithLister(cmd, loadManagedProjects)
}

// runWorkflowDriftWithLister is the test-friendly inner form that accepts
// an injectable project source. Production code calls runWorkflowDrift,
// which forwards loadManagedProjects. Tests pass a stub returning a
// synthetic slice or an error to drive the previously-untestable
// load-projects failure branch without touching ~/.agents/projects/.
func runWorkflowDriftWithLister(cmd *cobra.Command, lister func() ([]ManagedProject, error)) error {
	checkpointDays, _ := cmd.Flags().GetInt("stale-days")
	proposalDays, _ := cmd.Flags().GetInt("proposal-days")
	projectFilter, _ := cmd.Flags().GetString("project")
	pathFlag, _ := cmd.Flags().GetString("path")

	var projects []ManagedProject
	if strings.TrimSpace(pathFlag) != "" {
		// --path checks exactly one local directory, independent of the
		// ~/.agents/config.json managed-project registry. This is what
		// scripts/crg-bridge-consumer-audit.sh drives: the §11.4-criterion-4
		// check must be reproducible from repo content alone, not gated on
		// this repo happening to be `da add`-registered under some name.
		// filepath.Abs only errors when os.Getwd fails, which is not
		// reachable on a live process (see lockfilePath's identical
		// rationale in commands/kg/lockfile.go) — no error branch to guard.
		abs, _ := filepath.Abs(pathFlag)
		projects = []ManagedProject{{Name: resolveProjectNameForPath(abs), Path: abs}}
	} else {
		var err error
		projects, err = lister()
		if err != nil {
			return fmt.Errorf("load managed projects: %w", err)
		}
		if len(projects) == 0 {
			ui.Info("No managed projects registered. Add one with: da add <path>")
			return nil
		}

		projects, err = filterDriftProjects(projects, projectFilter)
		if err != nil {
			return err
		}
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
