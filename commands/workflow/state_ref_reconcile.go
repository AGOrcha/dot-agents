package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// osReadDir is the directory-read seam for working-copy plan enumeration.
// Default is os.ReadDir; tests rebind it to inject a synthetic non-IsNotExist
// error, because os.ReadDir on a non-directory path does not error portably
// (Windows diverges from POSIX), so the workingCopyPlanIDsWithTasks error leg
// cannot be driven by fixturing the filesystem alone.
var osReadDir = os.ReadDir

// stateRefReconcileOpts carries the flags for `da workflow state-ref reconcile`.
type stateRefReconcileOpts struct {
	dryRun bool
	json   bool
}

// stateRefReconcilePlanResult is the per-plan outcome of a reconcile pass.
type stateRefReconcilePlanResult struct {
	Plan       string   `json:"plan"`
	TasksOnRef int      `json:"tasks_on_ref"`
	TasksInWC  int      `json:"tasks_in_working_copy"`
	Seeded     []string `json:"seeded_tasks,omitempty"`
	Changed    bool     `json:"changed"`
	Skipped    string   `json:"skipped,omitempty"`
}

// stateRefReconcileResult is the whole-repo reconcile report.
type stateRefReconcileResult struct {
	DryRun       bool                          `json:"dry_run"`
	StateRef     string                        `json:"state_ref"`
	RefCreated   bool                          `json:"ref_created,omitempty"`
	PlansScanned int                           `json:"plans_scanned"`
	PlansChanged int                           `json:"plans_changed"`
	Plans        []stateRefReconcilePlanResult `json:"plans"`
}

// runWorkflowStateRefReconcile makes refs/agents/state a faithful mirror of
// every plan's WORKING-COPY coordination state. It reconciles TO the ref FROM
// the working copy unconditionally — independent of the active work_tracking
// backend — so plans transitioned before the choke-point mirror shipped (which
// left the ref holding a stale SUBSET of their tasks) get every working-copy
// task seeded onto the ref. Reconciliation reuses the shipped seed path
// (collectPlanTaskStateRefWrite + writePlanStateRefCAS): each working-copy task
// is seeded only when absent and the tree-equality guard in casWriteStateRef
// makes an already-consistent plan a no-op (no new ref commit), so the pass is
// idempotent.
func runWorkflowStateRefReconcile(out io.Writer, opts stateRefReconcileOpts) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	projectPath := project.Path

	planIDs, err := reconcilePlanUniverse(projectPath)
	if err != nil {
		return err
	}

	refExistedBefore := stateRefHead(projectPath) != ""
	result := stateRefReconcileResult{DryRun: opts.dryRun, StateRef: stateRefName}
	for _, planID := range planIDs {
		pr, err := reconcilePlanToStateRef(projectPath, planID, opts.dryRun)
		if err != nil {
			return fmt.Errorf("reconcile plan %q: %w", planID, err)
		}
		result.Plans = append(result.Plans, pr)
		result.PlansScanned++
		if pr.Changed {
			result.PlansChanged++
		}
	}
	if !refExistedBefore && !opts.dryRun && stateRefHead(projectPath) != "" {
		result.RefCreated = true
	}

	return emitStateRefReconcileResult(out, result, opts.json)
}

// reconcilePlanUniverse returns the sorted union of plans that need reconciling:
// every plan on refs/agents/state (so an already-mirrored plan is still probed
// for a stale subset) and every active working-copy plan (so a plan absent from
// the ref is discovered for a full seed).
func reconcilePlanUniverse(projectPath string) ([]string, error) {
	wcIDs, err := workingCopyPlanIDsWithTasks(projectPath)
	if err != nil {
		return nil, fmt.Errorf("enumerate working-copy plans: %w", err)
	}
	refIDs, err := stateRefResidentPlanIDs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("enumerate ref-resident plans: %w", err)
	}
	return unionSortedStrings(wcIDs, refIDs), nil
}

// reconcilePlanToStateRef seeds planID's working-copy tasks onto the ref and
// reports the delta. It reads the WORKING COPY directly (never the backend-aware
// loadCanonicalTasks) so reconcile is correct even under backend=git-ref, where
// the read path would otherwise return the very ref state we are repairing.
func reconcilePlanToStateRef(projectPath, planID string, dryRun bool) (stateRefReconcilePlanResult, error) {
	pr := stateRefReconcilePlanResult{Plan: planID}
	wcTaskIDs, exists, err := workingCopyTaskIDs(projectPath, planID)
	if err != nil {
		return pr, err
	}
	refSet, err := stateRefPlanTaskIDs(projectPath, planID)
	if err != nil {
		return pr, err
	}
	pr.TasksOnRef = len(refSet)
	if !exists {
		// A ref-resident plan with no working-copy TASKS.yaml cannot be
		// reconciled FROM the working copy — record it, change nothing.
		pr.Skipped = "no working-copy TASKS.yaml"
		return pr, nil
	}
	pr.TasksInWC = len(wcTaskIDs)
	pr.Seeded = missingTaskIDs(wcTaskIDs, refSet)

	if dryRun {
		pr.Changed = len(pr.Seeded) > 0
		return pr, nil
	}
	before := stateRefHead(projectPath)
	overwrite, seed, err := collectPlanTaskStateRefWrite(projectPath, planID, "")
	if err != nil {
		return pr, fmt.Errorf("collect state files for %s: %w", stateRefName, err)
	}
	if err := writePlanStateRefCAS(projectPath, overwrite, seed); err != nil {
		return pr, err
	}
	pr.Changed = stateRefHead(projectPath) != before
	return pr, nil
}

// missingTaskIDs returns the sorted working-copy task ids not present on the ref.
func missingTaskIDs(wcTaskIDs []string, refSet map[string]bool) []string {
	var missing []string
	for _, id := range wcTaskIDs {
		if !refSet[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return missing
}

// workingCopyTaskIDs reads planID's WORKING-COPY TASKS.yaml directly (os.ReadFile
// + unmarshal, matching collectPlanTaskStateRefWrite) and returns its task ids.
// exists is false when the plan has no TASKS.yaml on disk.
func workingCopyTaskIDs(projectPath, planID string) (ids []string, exists bool, err error) {
	raw, err := os.ReadFile(filepath.Join(plansBaseDir(projectPath), planID, workflowTasksFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var tf CanonicalTaskFile
	if err := yaml.Unmarshal(raw, &tf); err != nil {
		return nil, false, fmt.Errorf("parse %s TASKS.yaml: %w", planID, err)
	}
	ids = make([]string, 0, len(tf.Tasks))
	for i := range tf.Tasks {
		ids = append(ids, tf.Tasks[i].ID)
	}
	return ids, true, nil
}

// stateRefPlanTaskIDs returns the set of task ids that currently have a per-task
// blob for planID on refs/agents/state (empty when the ref or plan is absent).
func stateRefPlanTaskIDs(projectPath, planID string) (map[string]bool, error) {
	records, err := readPlanTaskRecordsFromStateRef(projectPath, planID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(records))
	for i := range records {
		set[records[i].Task.ID] = true
	}
	return set, nil
}

// workingCopyPlanIDsWithTasks lists plan ids under .agents/workflow/plans whose
// working-copy directory holds a TASKS.yaml.
func workingCopyPlanIDsWithTasks(projectPath string) ([]string, error) {
	base := plansBaseDir(projectPath)
	entries, err := osReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		_, statErr := os.Stat(filepath.Join(base, e.Name(), workflowTasksFileName))
		if statErr == nil {
			ids = append(ids, e.Name())
			continue
		}
		if !os.IsNotExist(statErr) {
			return nil, statErr
		}
	}
	return ids, nil
}

// stateRefResidentPlanIDs lists the plan directory names recorded under
// .agents/workflow/plans on refs/agents/state. A missing ref (or missing plans
// tree on the ref) yields no ids.
func stateRefResidentPlanIDs(projectPath string) ([]string, error) {
	head := stateRefHead(projectPath)
	if head == "" {
		return nil, nil
	}
	rel, err := plansBaseRel(projectPath)
	if err != nil {
		return nil, err
	}
	out, lsErr := gitStateExec(projectPath, nil, nil, "ls-tree", gitFlagNameOnly, head+":"+rel)
	if lsErr != nil {
		return nil, nil // plans/ absent on the ref → nothing resident
	}
	var ids []string
	for _, name := range splitNonEmptyLines(out) {
		ids = append(ids, filepath.Base(name))
	}
	return ids, nil
}

// plansBaseRel returns the repo-relative (slash-separated) path of the canonical
// plans directory, the tree path used to address plans on the state ref.
func plansBaseRel(projectPath string) (string, error) {
	rel, err := filepath.Rel(projectPath, plansBaseDir(projectPath))
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// splitNonEmptyLines splits git output into trimmed, non-empty lines.
func splitNonEmptyLines(out string) []string {
	var lines []string
	for _, raw := range strings.Split(out, "\n") {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// unionSortedStrings returns the sorted, de-duplicated union of two id slices.
func unionSortedStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		seen[s] = true
	}
	ids := make([]string, 0, len(seen))
	for s := range seen {
		ids = append(ids, s)
	}
	sort.Strings(ids)
	return ids
}

// emitStateRefReconcileResult renders the reconcile report as JSON (when either
// the local --json flag or the global --json flag is set) or as human text.
func emitStateRefReconcileResult(out io.Writer, result stateRefReconcileResult, asJSON bool) error {
	if asJSON || deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	renderStateRefReconcileText(out, result)
	return nil
}

// renderStateRefReconcileText prints the human-readable reconcile summary.
func renderStateRefReconcileText(out io.Writer, result stateRefReconcileResult) {
	suffix := ""
	if result.DryRun {
		suffix = "  [dry-run]"
	}
	fmt.Fprintf(out, "State-ref reconcile (%s)%s\n", result.StateRef, suffix)
	if len(result.Plans) == 0 {
		fmt.Fprintln(out, "  no plans found")
	}
	for _, pr := range result.Plans {
		fmt.Fprintf(out, "  %s: %s\n", pr.Plan, reconcilePlanStatusLine(pr, result.DryRun))
	}
	fmt.Fprintf(out, "Summary: %d plan(s) scanned, %d changed", result.PlansScanned, result.PlansChanged)
	if result.RefCreated {
		fmt.Fprint(out, ", ref created")
	}
	fmt.Fprintln(out)
}

// reconcilePlanStatusLine renders one plan's status phrase for the text report.
func reconcilePlanStatusLine(pr stateRefReconcilePlanResult, dryRun bool) string {
	if pr.Skipped != "" {
		return "skipped (" + pr.Skipped + ")"
	}
	verb := "reconciled"
	if dryRun {
		verb = "would reconcile"
	}
	if len(pr.Seeded) > 0 {
		return fmt.Sprintf("%s (%d task(s) seeded: %s)", verb, len(pr.Seeded), strings.Join(pr.Seeded, ", "))
	}
	if pr.Changed {
		return verb + " (plan metadata refreshed)"
	}
	return fmt.Sprintf("already-consistent (%d task(s))", pr.TasksInWC)
}
