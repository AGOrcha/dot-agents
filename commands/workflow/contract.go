package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// Direct-contract materialization. Companion to `workflow fanout`:
//
//   - `workflow fanout` writes a contract with Mode="delegated" and spawns a
//     sub-agent (loop-worker, codex helper, …) to execute the task.
//   - `workflow contract create --direct` writes a contract with Mode="direct"
//     for work the orchestrator itself will perform.
//
// Both contracts flow through the same closeout pipeline: `workflow merge-back`
// flips Status to "completed" and `workflow delegation closeout` auto-advances
// the canonical task. Audit trail and parent-gate semantics are identical;
// the Mode field is the only on-disk differentiator.
//
// See `.agents/proposals/verify-record-review-direct-iteration.md` for the
// motivating use case (direct iterations producing the same artifacts as
// delegated ones).

// runWorkflowContractCreate materializes a delegation contract for direct or
// delegated work. Direct mode is the primary use case (manual orchestrator
// work); delegated mode exists for tests + parity with the fanout writer.
func runWorkflowContractCreate(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	planID, _ := cmd.Flags().GetString("plan")
	taskID, _ := cmd.Flags().GetString("task")
	owner, _ := cmd.Flags().GetString("owner")
	writeScopeCSV, _ := cmd.Flags().GetString(workflowFlagWriteScope)
	writeScopeExplicit := cmd.Flags().Changed(workflowFlagWriteScope)
	force, _ := cmd.Flags().GetBool("force")
	mode, err := resolveContractCreateMode(cmd)
	if err != nil {
		return err
	}

	if _, err := loadCanonicalPlan(project.Path, planID); err != nil {
		return fmt.Errorf("plan %s not found: %w", planID, err)
	}

	tf, err := loadCanonicalTasks(project.Path, planID)
	if err != nil {
		return fmt.Errorf("tasks for plan %s not found: %w", planID, err)
	}
	targetTask, err := resolveContractTargetTask(tf, taskID, planID)
	if err != nil {
		return err
	}

	if existing, err := loadDelegationContract(project.Path, taskID); err == nil {
		if !force {
			return fmt.Errorf(
				"task %s already has an active delegation contract (mode=%s, status=%s); pass --force to overwrite",
				taskID, existing.Mode, existing.Status,
			)
		}
	}

	writeScope := resolveContractWriteScope(writeScopeCSV, writeScopeExplicit, targetTask.WriteScope)
	if len(writeScope) == 0 {
		return fmt.Errorf("write scope is empty: pass --write-scope or set write_scope on task %s in TASKS.yaml", taskID)
	}

	if err := checkFanoutWriteScopeConflicts(project.Path, writeScope, taskID); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	contract := &DelegationContract{
		SchemaVersion:   1,
		ID:              fmt.Sprintf("del-%s-%d", taskID, time.Now().Unix()),
		Mode:            mode,
		ParentPlanID:    planID,
		ParentTaskID:    taskID,
		Title:           targetTask.Title,
		Summary:         contractSummaryFor(mode, planID),
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

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(contract)
	}

	ui.SuccessBox(
		fmt.Sprintf("Contract created for task %s (mode=%s)", taskID, mode),
		fmt.Sprintf("Contract: .agents/active/delegation/%s.yaml", taskID),
		fmt.Sprintf("Write scope: %s", strings.Join(writeScope, ", ")),
		"Next: do the work, then `da workflow merge-back` → `da workflow delegation closeout`",
	)
	return nil
}

// resolveContractCreateMode reads --mode / --direct / --delegated flags and
// returns the resolved DelegationContractMode. Defaults to direct since the
// new CLI surface exists primarily for direct-mode contracts (fanout already
// covers delegated mode).
func resolveContractCreateMode(cmd *cobra.Command) (DelegationContractMode, error) {
	direct, _ := cmd.Flags().GetBool("direct")
	delegated, _ := cmd.Flags().GetBool("delegated")
	modeFlag, _ := cmd.Flags().GetString("mode")
	if direct && delegated {
		return "", fmt.Errorf("--direct and --delegated are mutually exclusive")
	}
	switch {
	case modeFlag != "":
		m := DelegationContractMode(strings.ToLower(strings.TrimSpace(modeFlag)))
		if !isValidDelegationContractMode(m) {
			return "", fmt.Errorf("invalid --mode %q (expected direct or delegated)", modeFlag)
		}
		// Cross-check against boolean shortcut flags if they were also set.
		if direct && m != DelegationContractModeDirect {
			return "", fmt.Errorf("--direct conflicts with --mode %s", modeFlag)
		}
		if delegated && m != DelegationContractModeDelegated {
			return "", fmt.Errorf("--delegated conflicts with --mode %s", modeFlag)
		}
		return m, nil
	case delegated:
		return DelegationContractModeDelegated, nil
	default:
		// --direct (the documented default) or no flag at all both land here.
		return DelegationContractModeDirect, nil
	}
}

// resolveContractTargetTask is a stricter sibling of resolveFanoutTargetTask:
// completed/blocked tasks reject contract creation since neither merge-back
// nor closeout would make sense for them.
func resolveContractTargetTask(tasks *CanonicalTaskFile, taskID, planID string) (*CanonicalTask, error) {
	if taskID == "" {
		return nil, fmt.Errorf("--task is required")
	}
	for i := range tasks.Tasks {
		if tasks.Tasks[i].ID != taskID {
			continue
		}
		t := &tasks.Tasks[i]
		if t.Status != "pending" && t.Status != "in_progress" {
			return nil, fmt.Errorf("task %s has status %q — only pending or in_progress tasks can have a contract materialized", taskID, t.Status)
		}
		return t, nil
	}
	return nil, fmt.Errorf("task %s not found in plan %s", taskID, planID)
}

// resolveContractWriteScope picks the explicit --write-scope CSV when set,
// otherwise falls back to TASKS.yaml write_scope for the task.
func resolveContractWriteScope(csv string, explicit bool, fallback []string) []string {
	if explicit {
		var ws []string
		for _, p := range strings.Split(csv, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				ws = append(ws, p)
			}
		}
		return ws
	}
	if len(fallback) > 0 {
		return append([]string(nil), fallback...)
	}
	return nil
}

func contractSummaryFor(mode DelegationContractMode, planID string) string {
	switch mode {
	case DelegationContractModeDirect:
		return fmt.Sprintf("Direct-mode contract for orchestrator-owned work on plan %s", planID)
	default:
		return fmt.Sprintf("Delegated contract for plan %s", planID)
	}
}

// runWorkflowContractList prints active contracts grouped by mode so the
// orchestrator can see at a glance which tasks have an in-flight contract.
func runWorkflowContractList(_ *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	contracts, err := listDelegationContracts(project.Path)
	if err != nil {
		return fmt.Errorf("list contracts: %w", err)
	}
	// Stable order: direct first, then delegated, alphabetical by task id within each group.
	sort.SliceStable(contracts, func(i, j int) bool {
		mi := normalizeDelegationContractMode(contracts[i].Mode)
		mj := normalizeDelegationContractMode(contracts[j].Mode)
		if mi != mj {
			return mi == DelegationContractModeDirect
		}
		return contracts[i].ParentTaskID < contracts[j].ParentTaskID
	})

	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(contracts)
	}

	if len(contracts) == 0 {
		fmt.Fprintln(os.Stdout, "No active delegation contracts.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MODE\tTASK\tPLAN\tSTATUS\tOWNER\tCREATED")
	for _, c := range contracts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			normalizeDelegationContractMode(c.Mode),
			c.ParentTaskID, c.ParentPlanID, c.Status, c.Owner, c.CreatedAt,
		)
	}
	return w.Flush()
}

func newWorkflowContractCmd() *cobra.Command {
	contractCmd := &cobra.Command{
		Use:   "contract",
		Short: "Materialize and inspect delegation contracts for direct orchestrator work",
		Long: `Manage delegation contracts directly, without going through workflow fanout.

A "direct" contract is the orchestrator-owned counterpart to a fanout delegation:
the same on-disk shape and the same closeout pipeline (merge-back → closeout →
auto-advance), but for work the orchestrator performs itself rather than spawning
a sub-agent. This makes orchestrator-driven work first-class in the workflow
audit trail.

See .agents/proposals/verify-record-review-direct-iteration.md for the
motivating use case.`,
		Example: deps.ExampleBlock(
			"  da workflow contract create --plan my-plan --task my-task",
			"  da workflow contract create --plan my-plan --task my-task --direct --write-scope commands/foo.go",
			"  da workflow contract create --plan my-plan --task my-task --delegated --owner worker-x",
			"  da workflow contract list",
		),
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a delegation contract for direct orchestrator work (or delegated parity)",
		Example: deps.ExampleBlock(
			"  da workflow contract create --plan my-plan --task my-task",
			"  da workflow contract create --plan my-plan --task my-task --write-scope commands/foo.go,internal/bar/",
			"  da workflow contract create --plan my-plan --task my-task --force",
		),
		Args: deps.NoArgsWithHints("Use `--plan` and `--task` flags instead of positional arguments."),
		RunE: runWorkflowContractCreate,
	}
	createCmd.Flags().String("plan", "", cmdFlagCanonicalPlanIDDescr)
	createCmd.Flags().String("task", "", "Task ID for which to materialize a contract (required)")
	createCmd.Flags().String("owner", "", "Owner identity stored on the contract (defaults to empty for direct work)")
	createCmd.Flags().String(workflowFlagWriteScope, "", "Comma-separated file/dir patterns this contract covers (defaults to TASKS.yaml write_scope)")
	createCmd.Flags().Bool("direct", false, "Materialize a direct-mode contract (default; orchestrator owns the work)")
	createCmd.Flags().Bool("delegated", false, "Materialize a delegated-mode contract (for tests / parity with fanout)")
	createCmd.Flags().String("mode", "", "Contract mode: direct or delegated (overrides --direct/--delegated when set)")
	createCmd.Flags().Bool("force", false, "Overwrite any existing contract for this task")
	_ = createCmd.MarkFlagRequired("plan")
	_ = createCmd.MarkFlagRequired("task")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List active delegation contracts grouped by mode",
		Example: deps.ExampleBlock(
			"  da workflow contract list",
			"  da --json workflow contract list",
		),
		Args: deps.NoArgsWithHints("Run workflow contract list from inside the project repository."),
		RunE: runWorkflowContractList,
	}

	contractCmd.AddCommand(createCmd, listCmd)
	return contractCmd
}

// contractsPath is a small helper for tests that need the active delegation
// directory path. Kept here (not exported globally) because the only public
// API consumers go through the CLI.
func contractsPath(projectPath string) string {
	return filepath.Join(delegationDir(projectPath))
}
