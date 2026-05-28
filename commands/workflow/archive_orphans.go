package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// `da workflow archive-orphans` sweeps stale active artifacts left behind
// after `workflow plan archive` runs. The plan-archive merge intentionally
// skips DMA files (delegation.yaml, merge-back.md) under active/ — they live
// outside the plan source dir — so a partially-merged-back delegation can
// leave the active/ tree littered with paperwork the closeout pipeline can no
// longer touch (the parent plan is archived and out of the active set).
//
// The four orphan classes this command resolves (each surfaced by the
// backlog-hygiene worker) are documented on archiveOrphanClass below.

// archiveOrphanClass classifies one orphan candidate. See classifyOrphan.
type archiveOrphanClass string

const (
	// orphanClassRemoveDuplicate: a merge-back.md exists in active/ but the
	// task already has a delegate-merge-back-archive entry in history/. The
	// active artifact is a duplicate left behind by an incomplete closeout —
	// safe to remove.
	orphanClassRemoveDuplicate archiveOrphanClass = "remove_duplicate"
	// orphanClassMoveMergeBack: merge-back.md in active/, no delegation
	// contract, no archive entry. The artifact is the canonical record and
	// must be promoted to history/<plan>/delegate-merge-back-archive/.
	orphanClassMoveMergeBack archiveOrphanClass = "move_merge_back"
	// orphanClassMoveDelegation: delegation.yaml in active/, no merge-back,
	// but the parent task is `completed` in an archived plan. The contract is
	// trailing paperwork — promote it to history/ alongside other DMA files.
	orphanClassMoveDelegation archiveOrphanClass = "move_delegation"
	// orphanClassUnknownTask: a delegation.yaml or merge-back.md in active/
	// references a task that is not in any TASKS.yaml (active or archived).
	// The artifact is truly orphaned; we leave it in place and record a
	// fold-back so the orchestrator can adjudicate.
	orphanClassUnknownTask archiveOrphanClass = "unknown_task"
)

// archiveOrphanAction is one orphan resolution recorded for the run summary.
type archiveOrphanAction struct {
	TaskID  string             `json:"task_id" yaml:"task_id"`
	PlanID  string             `json:"plan_id,omitempty" yaml:"plan_id,omitempty"`
	Class   archiveOrphanClass `json:"class" yaml:"class"`
	Source  string             `json:"source" yaml:"source"`
	Target  string             `json:"target,omitempty" yaml:"target,omitempty"`
	Applied bool               `json:"applied" yaml:"applied"`
	Notes   string             `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// archiveOrphansResult is the structured run summary, also used as the JSON
// output payload when `--json` is set.
type archiveOrphansResult struct {
	DryRun  bool                  `json:"dry_run" yaml:"dry_run"`
	Actions []archiveOrphanAction `json:"actions" yaml:"actions"`
}

// newWorkflowArchiveOrphansCmd wires the `da workflow archive-orphans` cobra
// subcommand. See archiveOrphanClass for the four orphan classes handled.
func newWorkflowArchiveOrphansCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive-orphans",
		Short: "Sweep stale active merge-back/delegation artifacts after plan archive",
		Long: `Reconciles stale .agents/active/{merge-back,delegation}/<task>.{md,yaml}
artifacts left behind by ` + "`workflow plan archive`" + ` against the canonical
history/ tree. Four classes are resolved:

  - duplicate merge-back already archived → remove the active orphan
  - lone merge-back never archived       → move to history/<plan>/delegate-merge-back-archive/<date>/<task>/
  - lone delegation for completed task   → move to history alongside DMA files
  - lone artifact for unknown task       → leave in place + record a fold-back

Use ` + "`--dry-run`" + ` (global) to preview without mutation; ` + "`--json`" + ` for machine output.`,
		Example: deps.ExampleBlock(
			"  da workflow archive-orphans",
			"  da -n workflow archive-orphans",
			"  da --json workflow archive-orphans",
		),
		Args: deps.NoArgsWithHints("Run archive-orphans from inside the project repository."),
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, err := currentWorkflowProject()
			if err != nil {
				return err
			}
			dryRun := false
			if deps.Flags.DryRun != nil {
				dryRun = deps.Flags.DryRun()
			}
			return runWorkflowArchiveOrphans(cmd, project.Path, dryRun)
		},
	}
}

// runWorkflowArchiveOrphans is the testable inner form. It scans the active
// tree, classifies each candidate, applies the resolution (or simulates it
// when dryRun is set), and renders the result.
func runWorkflowArchiveOrphans(cmd *cobra.Command, projectPath string, dryRun bool) error {
	candidates, err := collectArchiveOrphanCandidates(projectPath)
	if err != nil {
		return fmt.Errorf("collect orphan candidates: %w", err)
	}
	taskPlanIdx, err := buildTaskPlanIndex(projectPath)
	if err != nil {
		return fmt.Errorf("index tasks across plans: %w", err)
	}

	now := time.Now().UTC()
	dateStamp := now.Format("2006-01-02")
	createdAtRFC := now.Format(time.RFC3339)
	tsNano := now.UnixNano()

	result := archiveOrphansResult{DryRun: dryRun}
	for _, cand := range candidates {
		action, applyErr := resolveArchiveOrphan(projectPath, cand, taskPlanIdx, dateStamp, createdAtRFC, tsNano, dryRun)
		if applyErr != nil {
			return applyErr
		}
		result.Actions = append(result.Actions, action)
	}

	return renderArchiveOrphansResult(cmd, result)
}

// orphanCandidate is one active/<task>.{md,yaml} file under inspection.
type orphanCandidate struct {
	taskID           string
	mergeBackPath    string // absolute path or "" if absent
	delegationPath   string // absolute path or "" if absent
	mergeBackPlanID  string // parent_plan_id from merge-back frontmatter
	delegationPlanID string // parent_plan_id from delegation YAML
}

// effectivePlanID picks the most authoritative plan-id signal we have for the
// candidate. The merge-back is preferred when both files exist because it is
// always written by `workflow merge-back` against the live contract.
func (c orphanCandidate) effectivePlanID() string {
	if c.mergeBackPlanID != "" {
		return c.mergeBackPlanID
	}
	return c.delegationPlanID
}

// collectArchiveOrphanCandidates walks active/{merge-back,delegation} and
// returns one candidate per task ID that appears in either dir. Tasks with
// both files are still reported once; classification handles the combination.
func collectArchiveOrphanCandidates(projectPath string) ([]orphanCandidate, error) {
	candidates := map[string]*orphanCandidate{}

	if err := collectMergeBackOrphanFiles(projectPath, candidates); err != nil {
		return nil, err
	}
	if err := collectDelegationOrphanFiles(projectPath, candidates); err != nil {
		return nil, err
	}

	out := make([]orphanCandidate, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].taskID < out[j].taskID })
	return out, nil
}

func collectMergeBackOrphanFiles(projectPath string, candidates map[string]*orphanCandidate) error {
	dir := mergeBackDir(projectPath)
	entries, err := readDirIfExists(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		taskID := strings.TrimSuffix(e.Name(), ".md")
		c := ensureOrphanCandidate(candidates, taskID)
		c.mergeBackPath = filepath.Join(dir, e.Name())
		// loadMergeBack is best-effort here; a parse error means we cannot
		// determine the parent plan id but the file still counts as an orphan
		// candidate (classifyOrphan will route it to the unknown_task class).
		mb, mbErr := loadMergeBack(projectPath, taskID)
		if mbErr == nil {
			c.mergeBackPlanID = mb.ParentPlanID
		}
	}
	return nil
}

func collectDelegationOrphanFiles(projectPath string, candidates map[string]*orphanCandidate) error {
	dir := delegationDir(projectPath)
	entries, err := readDirIfExists(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		taskID := strings.TrimSuffix(e.Name(), ".yaml")
		c := ensureOrphanCandidate(candidates, taskID)
		c.delegationPath = filepath.Join(dir, e.Name())
		dc, dcErr := loadDelegationContract(projectPath, taskID)
		if dcErr == nil {
			c.delegationPlanID = dc.ParentPlanID
		}
	}
	return nil
}

func ensureOrphanCandidate(m map[string]*orphanCandidate, taskID string) *orphanCandidate {
	if c, ok := m[taskID]; ok {
		return c
	}
	c := &orphanCandidate{taskID: taskID}
	m[taskID] = c
	return c
}

func readDirIfExists(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}

// taskPlanIndex maps task-id → (planID, status, source). Source is "active"
// or "history" — used by classifyOrphan to decide whether case-3 applies
// (delegation contract for a `completed` task in an archived plan).
type taskPlanIndex map[string]taskPlanInfo

type taskPlanInfo struct {
	PlanID string
	Status string
	Source string // "active" | "history"
}

// buildTaskPlanIndex scans both .agents/workflow/plans/* and .agents/history/*
// TASKS.yaml files and returns a flattened task-id → plan/status/source map.
// When a task id collides (same id in two plans), the active-plan entry wins
// so case-4 (unknown task) only fires when the id is truly absent everywhere.
func buildTaskPlanIndex(projectPath string) (taskPlanIndex, error) {
	idx := taskPlanIndex{}
	if err := indexTasksFromDir(plansBaseDir(projectPath), "active", idx); err != nil {
		return nil, err
	}
	if err := indexTasksFromDir(historyBaseDir(projectPath), "history", idx); err != nil {
		return nil, err
	}
	return idx, nil
}

func indexTasksFromDir(base, source string, idx taskPlanIndex) error {
	entries, err := readDirIfExists(base)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		planID := e.Name()
		tasksPath := filepath.Join(base, planID, workflowTasksFileName)
		data, readErr := os.ReadFile(tasksPath)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return readErr
		}
		var tf CanonicalTaskFile
		if uerr := yaml.Unmarshal(data, &tf); uerr != nil {
			// A malformed TASKS.yaml in one plan should not block sweep on
			// the rest of the tree; we treat it as if the plan contributes
			// no tasks.
			continue
		}
		for _, t := range tf.Tasks {
			if t.ID == "" {
				continue
			}
			if existing, ok := idx[t.ID]; ok && existing.Source == "active" {
				continue
			}
			idx[t.ID] = taskPlanInfo{PlanID: planID, Status: t.Status, Source: source}
		}
	}
	return nil
}

// classifyOrphan applies the four-class rule documented on archiveOrphanClass.
// The returned class is what resolveArchiveOrphan dispatches on.
func classifyOrphan(projectPath string, cand orphanCandidate, idx taskPlanIndex) (archiveOrphanClass, string) {
	planID := cand.effectivePlanID()

	// Case 1 + 2: merge-back present, no delegation.
	if cand.mergeBackPath != "" && cand.delegationPath == "" {
		if planID == "" {
			return orphanClassUnknownTask, "merge-back missing parent_plan_id"
		}
		if archivedMergeBackExists(projectPath, planID, cand.taskID) {
			return orphanClassRemoveDuplicate, ""
		}
		if !planHasHistoryDir(projectPath, planID) {
			return orphanClassUnknownTask, fmt.Sprintf("plan %s has no history dir; not safe to archive automatically", planID)
		}
		return orphanClassMoveMergeBack, ""
	}

	// Case 3 + 4: delegation present, no merge-back.
	if cand.delegationPath != "" && cand.mergeBackPath == "" {
		info, known := idx[cand.taskID]
		if !known {
			return orphanClassUnknownTask, "task id not present in any TASKS.yaml"
		}
		if info.Source != "history" || info.Status != "completed" {
			return orphanClassUnknownTask, fmt.Sprintf("task %s/%s is %s in %s plan (not a sweep candidate)", info.PlanID, cand.taskID, info.Status, info.Source)
		}
		if !planHasHistoryDir(projectPath, info.PlanID) {
			return orphanClassUnknownTask, fmt.Sprintf("plan %s has no history dir; not safe to archive automatically", info.PlanID)
		}
		return orphanClassMoveDelegation, ""
	}

	// Both files present together is not an orphan — the live merge-back
	// pipeline still owns the closeout. Mark as unknown so the action shows
	// up in the report without mutation.
	return orphanClassUnknownTask, "both delegation and merge-back present; not an archive orphan"
}

func archivedMergeBackExists(projectPath, planID, taskID string) bool {
	dmaBase := filepath.Join(historyBaseDir(projectPath), planID, "delegate-merge-back-archive")
	entries, err := readDirIfExists(dmaBase)
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(dmaBase, e.Name(), taskID, "merge-back.md")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return true
		}
	}
	return false
}

func planHasHistoryDir(projectPath, planID string) bool {
	st, err := os.Stat(filepath.Join(historyBaseDir(projectPath), planID))
	return err == nil && st.IsDir()
}

// resolveArchiveOrphan dispatches one candidate to the appropriate handler
// and returns the recorded action.
func resolveArchiveOrphan(projectPath string, cand orphanCandidate, idx taskPlanIndex, dateStamp, createdAtRFC string, tsNano int64, dryRun bool) (archiveOrphanAction, error) {
	class, notes := classifyOrphan(projectPath, cand, idx)
	planID := planForOrphanAction(cand, idx)
	action := archiveOrphanAction{TaskID: cand.taskID, PlanID: planID, Class: class, Notes: notes}

	switch class {
	case orphanClassRemoveDuplicate:
		action.Source = cand.mergeBackPath
		if dryRun {
			return action, nil
		}
		if err := os.Remove(cand.mergeBackPath); err != nil && !os.IsNotExist(err) {
			return action, fmt.Errorf("remove %s: %w", cand.mergeBackPath, err)
		}
		action.Applied = true
		return action, nil

	case orphanClassMoveMergeBack:
		target := filepath.Join(historyBaseDir(projectPath), planID, "delegate-merge-back-archive", dateStamp, cand.taskID, "merge-back.md")
		action.Source = cand.mergeBackPath
		action.Target = target
		if dryRun {
			return action, nil
		}
		if err := movePath(cand.mergeBackPath, target); err != nil {
			return action, err
		}
		action.Applied = true
		return action, nil

	case orphanClassMoveDelegation:
		target := filepath.Join(historyBaseDir(projectPath), planID, "delegate-merge-back-archive", dateStamp, cand.taskID, "delegation.yaml")
		action.Source = cand.delegationPath
		action.Target = target
		if dryRun {
			return action, nil
		}
		if err := movePath(cand.delegationPath, target); err != nil {
			return action, err
		}
		action.Applied = true
		return action, nil

	case orphanClassUnknownTask:
		return recordUnknownTaskFoldBack(projectPath, cand, planID, action, createdAtRFC, tsNano, dryRun)
	}
	return action, nil
}

func planForOrphanAction(cand orphanCandidate, idx taskPlanIndex) string {
	if p := cand.effectivePlanID(); p != "" {
		return p
	}
	if info, ok := idx[cand.taskID]; ok {
		return info.PlanID
	}
	return ""
}

// recordUnknownTaskFoldBack writes a fold-back artifact for a truly-orphaned
// task and leaves the active file in place so an operator can adjudicate. The
// fold-back is keyed by a slug derived from the task id + nanosecond
// timestamp so repeated sweeps do not collide.
func recordUnknownTaskFoldBack(projectPath string, cand orphanCandidate, planID string, action archiveOrphanAction, createdAtRFC string, tsNano int64, dryRun bool) (archiveOrphanAction, error) {
	if cand.mergeBackPath != "" {
		action.Source = cand.mergeBackPath
	} else {
		action.Source = cand.delegationPath
	}
	if dryRun {
		return action, nil
	}
	obs := fmt.Sprintf("archive-orphan-sweep: %s — %s", cand.taskID, action.Notes)
	id := fmt.Sprintf("archive-orphan-%s-%d", sanitizeFoldBackID(cand.taskID), tsNano)
	artifact := foldBackArtifact{
		SchemaVersion:  1,
		ID:             id,
		PlanID:         planID,
		TaskID:         cand.taskID,
		Observation:    obs,
		Classification: "small",
		RoutedTo:       "active/fold-back",
		CreatedAt:      createdAtRFC,
	}
	if err := writeFoldBackArtifact(projectPath, artifact); err != nil {
		return action, fmt.Errorf("write fold-back for orphan %s: %w", cand.taskID, err)
	}
	action.Target = filepath.Join(foldBackDir(projectPath), id+".yaml")
	action.Applied = true
	return action, nil
}

// sanitizeFoldBackID strips any characters that validateFoldBackSlug would
// reject so the derived fold-back id is always parseable by `workflow
// fold-back` consumers.
func sanitizeFoldBackID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

// movePath copies src to dst (creating dst parents) then removes src. A
// straight os.Rename would fail with EXDEV across filesystem boundaries; a
// copy+delete keeps the sweep portable inside .agents/.
func movePath(src, dst string) error {
	if err := osMkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	if err := copyWorkflowArtifact(src, dst); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove %s after copy: %w", src, err)
	}
	return nil
}

func renderArchiveOrphansResult(cmd *cobra.Command, result archiveOrphansResult) error {
	out := cmd.OutOrStdout()
	if deps.Flags.JSON() {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	if len(result.Actions) == 0 {
		ui.Success("No archive orphans found — active/ tree is clean.")
		return nil
	}
	mode := "applied"
	if result.DryRun {
		mode = "dry-run"
	}
	ui.Header(fmt.Sprintf("Archive Orphan Sweep [%s]", mode))
	for _, a := range result.Actions {
		marker := "○"
		if a.Applied {
			marker = "✓"
		}
		plan := a.PlanID
		if plan == "" {
			plan = "—"
		}
		fmt.Fprintf(out, "  %s [%s] %s/%s\n", marker, a.Class, plan, a.TaskID)
		if a.Source != "" {
			fmt.Fprintf(out, "      src: %s\n", a.Source)
		}
		if a.Target != "" {
			fmt.Fprintf(out, "      dst: %s\n", a.Target)
		}
		if a.Notes != "" {
			fmt.Fprintf(out, "      note: %s\n", a.Notes)
		}
	}
	if result.DryRun {
		ui.Info("Re-run without --dry-run to apply.")
	}
	return nil
}
