package workflow

import (
	"encoding/json"
	"fmt"
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
		if action.RequiresConfirmation && !deps.Flags.Yes() {
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
