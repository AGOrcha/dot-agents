package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// ── Health snapshot ──────────────────────────────────────────────────────────

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
	if state.LocalDrift != nil {
		h.Workflow.CompletedPlansPendingArchive = len(state.LocalDrift.CompletedPlanIDs)
	}
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

func runWorkflowHealth() error {
	state, err := collectWorkflowState()
	if err != nil {
		return err
	}
	health := computeWorkflowHealth(state)
	// Persist the snapshot
	_ = writeHealthSnapshot(state.Project.Name, health)

	if deps.Flags.JSON() {
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
	if health.Workflow.CompletedPlansPendingArchive > 0 {
		fmt.Fprintf(os.Stdout, "  completed plans pending archive: %d\n", health.Workflow.CompletedPlansPendingArchive)
	}
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
