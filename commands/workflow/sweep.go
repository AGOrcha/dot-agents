package workflow

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// SweepActionType enumerates the kinds of fixes the sweep can apply.
type SweepActionType string

const (
	SweepActionScaffoldWorkflowDir      SweepActionType = "scaffold_workflow_dir"
	SweepActionCreatePlanStructure      SweepActionType = "create_plan_structure"
	SweepActionCreateCheckpointReminder SweepActionType = "create_checkpoint_reminder"
	SweepActionFlagStaleProposals       SweepActionType = "flag_stale_proposals"
	SweepActionArchiveCompletedPlans    SweepActionType = "archive_completed_plans"
)

// SweepActionItem is one actionable fix in a sweep plan.
type SweepActionItem struct {
	Project              ManagedProject  `json:"project"`
	Action               SweepActionType `json:"action"`
	Description          string          `json:"description"`
	RequiresConfirmation bool            `json:"requires_confirmation"`
	PlanID               string          `json:"plan_id,omitempty"`
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
		for _, planID := range r.CompletedPlanIDs {
			plan.Actions = append(plan.Actions, SweepActionItem{
				Project:              r.Project,
				Action:               SweepActionArchiveCompletedPlans,
				Description:          fmt.Sprintf("Archive completed plan %q in %s", planID, r.Project.Name),
				RequiresConfirmation: true,
				PlanID:               planID,
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

// sweepLogPath returns the path for the sweep operation log.
func sweepLogPath() string {
	return filepath.Join(config.AgentsContextDir(), "sweep-log.jsonl")
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
	case SweepActionArchiveCompletedPlans:
		return runWorkflowPlanArchive(item.Project.Path, []string{item.PlanID}, false, false)
	default:
		return fmt.Errorf("unknown sweep action %q", item.Action)
	}
}

func sweepLogEntry(action SweepActionItem, applied, dryRun bool) SweepLogEntry {
	return SweepLogEntry{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Project:     action.Project.Name,
		Action:      action.Action,
		Description: action.Description,
		Applied:     applied,
		DryRun:      dryRun,
	}
}

func renderSweepPlanHeader(plan SweepPlan, dryRun bool) {
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
}

func runSweepDryRun(plan SweepPlan) {
	ui.Info("Run with --apply to execute these actions.")
	for _, action := range plan.Actions {
		appendSweepLog(sweepLogEntry(action, false, true))
	}
}

// confirmSweepAction returns true if the action should be applied. It also
// records a skip log entry when the user declines. The confirmer reader is
// the input source for the interactive y/N prompt; production callers pass
// os.Stdin (via workflowStdin), tests inject a strings.Reader directly.
//
// A nil confirmer falls back to the package-level workflowStdin seam so the
// helper can still be invoked without a reader in legacy call sites. The
// reader is wrapped in a fresh bufio.Reader per call so the entire input
// stream is not preemptively buffered between successive prompts when the
// same underlying io.Reader is shared.
func confirmSweepAction(action SweepActionItem, confirmer io.Reader) bool {
	if !action.RequiresConfirmation || deps.Flags.Yes() {
		return true
	}
	if confirmer == nil {
		confirmer = workflowStdin
	}
	fmt.Fprintf(os.Stdout, "  Apply: %s? [y/N] ", action.Description)
	reader := bufio.NewReader(confirmer)
	line, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(line)) == "y" {
		return true
	}
	ui.Info(fmt.Sprintf("  Skipped: %s", action.Description))
	appendSweepLog(sweepLogEntry(action, false, false))
	return false
}

// runSweepApply executes confirmed actions sequentially. To avoid the
// bufio-buffering edge case where a per-action bufio.Reader would gobble
// the rest of the underlying io.Reader on the first call, the confirmer is
// wrapped once here and reused across actions.
func runSweepApply(plan SweepPlan, confirmer io.Reader) {
	if confirmer == nil {
		confirmer = workflowStdin
	}
	shared := bufio.NewReader(confirmer)
	applied := 0
	for _, action := range plan.Actions {
		if !confirmSweepActionFromReader(action, shared) {
			continue
		}
		if err := applySweepAction(action); err != nil {
			ui.Warn(fmt.Sprintf("Failed: %s — %v", action.Description, err))
		} else {
			applied++
			ui.Success(fmt.Sprintf("Applied: %s", action.Description))
		}
		appendSweepLog(sweepLogEntry(action, true, false))
	}
	fmt.Fprintln(os.Stdout)
	ui.Success(fmt.Sprintf("Sweep complete: %d/%d actions applied.", applied, len(plan.Actions)))
}

// confirmSweepActionFromReader is the inner form used by runSweepApply with
// a pre-wrapped bufio.Reader so successive prompts can share buffer state.
func confirmSweepActionFromReader(action SweepActionItem, reader *bufio.Reader) bool {
	if !action.RequiresConfirmation || deps.Flags.Yes() {
		return true
	}
	fmt.Fprintf(os.Stdout, "  Apply: %s? [y/N] ", action.Description)
	line, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(line)) == "y" {
		return true
	}
	ui.Info(fmt.Sprintf("  Skipped: %s", action.Description))
	appendSweepLog(sweepLogEntry(action, false, false))
	return false
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

	renderSweepPlanHeader(plan, dryRun)

	if dryRun {
		runSweepDryRun(plan)
		return nil
	}

	runSweepApply(plan, workflowStdin)
	return nil
}
