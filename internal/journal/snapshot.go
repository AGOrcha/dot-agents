package journal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/AGOrcha/dot-agents/internal/fsops"
)

// snapshot.go writes and reads the deterministic live-state SNAPSHOT that sits
// beside the append-only event log under the per-repo journal home
// (<journal-home>/snapshot.json). The event log records what HAPPENED; the
// snapshot is a queryable capture of the CURRENT workflow state — the active
// plans and their task statuses, the eligible/next set, the in-flight
// delegations, and the merge-backs awaiting integration. The PreCompact hook
// (p7) writes it before a context window is compacted and the recovery view (p5)
// reads it back and re-verifies it, so a session that resumes after a
// compaction or crash re-injects state from durable file state rather than
// re-grounding from scratch.
//
// # Determinism (locked decision)
//
// Two snapshots of the same underlying workflow state are byte-identical. The
// serialized shape is built only from structs and sorted slices — never a Go map
// — so there is no iteration-order nondeterminism, and every collection (plans,
// tasks, eligible refs, delegations, merge-backs) is sorted by a stable key
// before marshaling. The single varying field is CapturedAt, taken from the
// package `now` clock seam so a test can freeze it and assert byte-equality.
//
// # Durability (mirrors the appender)
//
// The snapshot is a FULL-FILE write performed the same way append.go writes a
// line: the bytes are marshaled, then written via fsops.WriteFileAtomic
// (temp-then-rename) under the package interprocess advisory lock
// (agentslock.AcquireFileLock, the shared `acquireLock` seam) so a concurrent da
// process can never observe a half-written snapshot or race another writer.
//
// # No bodies (mirrors the event log)
//
// The snapshot captures ids, statuses, dependency edges, loci, and counts — the
// bounded, queryable surface recovery re-verifies. It deliberately never reads a
// task's notes/summary/title or any other free-text body, so it cannot grow into
// a body dump.

const (
	// SnapshotSchema namespaces the snapshot record so a reader can reject a
	// foreign file rather than guess. It is distinct from the event-log Schema.
	SnapshotSchema = "session-handoff-journal/snapshot"
	// SnapshotVersion is the snapshot schema version; readers gate on it. Bump it
	// when the captured shape changes incompatibly.
	SnapshotVersion = 1

	// snapshotFileName is the snapshot file within a repo's journal directory.
	snapshotFileName = "snapshot.json"
)

// Workflow state lives under <repo>/.agents; these segments name the canonical
// plan/task store, the delegation contracts, and the pending merge-backs. They
// are centralized here so the path joins below read from one place (and so the
// literals are not repeated — no S1192).
const (
	agentsDirName    = ".agents"
	workflowDirName  = "workflow"
	plansDirName     = "plans"
	activeDirName    = "active"
	delegationSub    = "delegation"
	mergeBackSub     = "merge-back"
	planFileName     = "PLAN.yaml"
	tasksFileName    = "TASKS.yaml"
	delegationSuffix = ".yaml"
	mergeBackSuffix  = ".md"
)

// Task status vocabulary the snapshot needs to reason about. These mirror the
// canonical TASKS.yaml vocabulary owned by commands/workflow; the journal
// package cannot import that package (the command layer imports the journal, so
// the dependency runs the other way), so the load-bearing few are restated here.
const (
	statusPending             = "pending"
	statusCompleted           = "completed"
	statusAwaitingOwnerReview = "awaiting_owner_review"
)

// in-flight delegation statuses: a delegation still owns work only while it is
// pending or active. Terminal contracts (completed/failed/cancelled) are not
// live state a recovery needs to re-verify.
const (
	delegationPending = "pending"
	delegationActive  = "active"
)

// canonicalLocusRef is the placeholder ref recorded on a completed task's
// canonical locus when the precise branch is not locally known. The snapshot
// records WHICH locus a task lives in (canonical vs in-PR) deterministically from
// file state; the precise coordinates a recovery re-verifies (the merge SHA, the
// PR number) require a git/gh read and are enriched by the p5/p7 recovery layer.
const canonicalLocusRef = "canonical"

// Seams over the failure-prone steps of the write path, so tests can drive the
// marshal / mkdir / lock / atomic-write error branches deterministically without
// staging real filesystem faults. acquireLock is shared with the appender
// (append.go).
var (
	marshalSnapshot = func(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
	mkdirAll        = fsops.MkdirAll
	writeFileAtomic = fsops.WriteFileAtomic
)

// SnapshotPath returns the snapshot.json path for the repository at repoPath:
// <journal-home>/snapshot.json.
func SnapshotPath(repoPath string) string {
	return filepath.Join(RepoDir(repoPath), snapshotFileName)
}

// TaskRef names a single task within a plan — the bounded identity recovery uses
// to re-verify a task without carrying its body.
type TaskRef struct {
	Plan string `json:"plan"`
	Task string `json:"task"`
}

// TaskState is one task's live, bounded state: its id, status, dependency edges,
// and the locus that records whether its work is landed on the canonical branch
// or lives in an open PR (nil when the status implies neither).
type TaskState struct {
	ID        string   `json:"id"`
	Status    string   `json:"status"`
	DependsOn []string `json:"depends_on,omitempty"`
	Locus     *Locus   `json:"locus,omitempty"`
}

// PlanState is one active plan's live state: its id, status, current focus task,
// and its tasks (sorted by id for a canonical, order-independent capture).
type PlanState struct {
	ID               string      `json:"id"`
	Status           string      `json:"status"`
	CurrentFocusTask string      `json:"current_focus_task,omitempty"`
	Tasks            []TaskState `json:"tasks"`
}

// DelegationState is one in-flight delegation: the parent plan/task it serves and
// its status. Bounded — no write scope, prompt, or summary body.
type DelegationState struct {
	ID           string `json:"id"`
	ParentPlanID string `json:"parent_plan_id"`
	ParentTaskID string `json:"parent_task_id"`
	Status       string `json:"status"`
}

// SnapshotState is the deterministic capture of current workflow state written
// to <journal-home>/snapshot.json. It is the value the Snapshot capture function
// returns and the LoadSnapshot read path decodes. The type is named distinctly
// from the Snapshot function (Go forbids a type and func sharing a name) while
// keeping the capture entrypoint the plan named: journal.Snapshot(repoPath).
type SnapshotState struct {
	// Schema/Version namespace and version the record.
	Schema  string `json:"schema"`
	Version int    `json:"version"`
	// CapturedAt is the RFC3339 UTC nanosecond capture time (the one varying
	// field; everything else is a pure function of the captured state).
	CapturedAt string `json:"captured_at"`
	// Identity pins the snapshot to the repo it was captured against, so a
	// recovery can quarantine a snapshot taken against a different checkout.
	Identity Identity `json:"identity"`
	// Plans is the captured active plans, sorted by id.
	Plans []PlanState `json:"plans"`
	// Eligible is the next/eligible task set: pending tasks whose dependencies
	// are all satisfied, sorted by (plan, task).
	Eligible []TaskRef `json:"eligible"`
	// Delegations is the in-flight delegations (pending/active), sorted by id.
	Delegations []DelegationState `json:"delegations"`
	// PendingMergeBacks is the task ids with a merge-back awaiting integration,
	// sorted.
	PendingMergeBacks []string `json:"pending_merge_backs"`
}

// Snapshot captures the current workflow state for the repository at repoPath and
// writes it to <journal-home>/snapshot.json, returning the captured value. The
// capture itself never fails — a missing or unreadable workflow tree yields an
// empty-but-valid snapshot, mirroring ResolveIdentity's "never fails" contract —
// so the only error paths are the marshal/mkdir/lock/write of the persisted file.
func Snapshot(repoPath string) (SnapshotState, error) {
	snap := buildSnapshot(repoPath)
	if err := writeSnapshot(repoPath, snap); err != nil {
		return SnapshotState{}, err
	}
	return snap, nil
}

// LoadSnapshot reads and decodes the snapshot for the repository at repoPath. It
// is the read path p5 (recovery view) and p7 (PreCompact hook) use to re-inject
// state. A missing file surfaces the os.ReadFile error; malformed JSON is wrapped.
func LoadSnapshot(repoPath string) (SnapshotState, error) {
	data, err := os.ReadFile(SnapshotPath(repoPath))
	if err != nil {
		return SnapshotState{}, fmt.Errorf("journal: read snapshot: %w", err)
	}
	var snap SnapshotState
	if err := json.Unmarshal(data, &snap); err != nil {
		return SnapshotState{}, fmt.Errorf("journal: decode snapshot: %w", err)
	}
	return snap, nil
}

// buildSnapshot reads the live workflow state from <repo>/.agents and assembles a
// deterministic Snapshot. Every collection is sorted by a stable key and every
// slice is non-nil so the marshaled bytes are order-independent and never null.
func buildSnapshot(repoPath string) SnapshotState {
	plans := capturePlans(repoPath)
	return SnapshotState{
		Schema:            SnapshotSchema,
		Version:           SnapshotVersion,
		CapturedAt:        now().UTC().Format(timeFormat),
		Identity:          ResolveIdentity(repoPath),
		Plans:             plans,
		Eligible:          computeEligible(plans),
		Delegations:       captureDelegations(repoPath),
		PendingMergeBacks: capturePendingMergeBacks(repoPath),
	}
}

// agentsSubdir joins repoPath with the given .agents subpath segments.
func agentsSubdir(repoPath string, parts ...string) string {
	return filepath.Join(append([]string{repoPath, agentsDirName}, parts...)...)
}

// capturePlans reads every plan under .agents/workflow/plans and returns their
// bounded state sorted by id. A plan whose PLAN.yaml is missing or unparseable is
// skipped; a plan whose TASKS.yaml is missing or unparseable is kept with no
// tasks (its status is still useful live state).
func capturePlans(repoPath string) []PlanState {
	base := agentsSubdir(repoPath, workflowDirName, plansDirName)
	entries, err := os.ReadDir(base)
	if err != nil {
		return []PlanState{}
	}
	plans := make([]PlanState, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		plan, ok := readPlan(filepath.Join(base, e.Name()))
		if !ok {
			continue
		}
		plans = append(plans, plan)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	return plans
}

// planYAML/tasksYAML/taskYAML are the minimal slices of PLAN.yaml and TASKS.yaml
// the snapshot reads. They intentionally omit every free-text body field (title,
// summary, notes) so a body can never reach the snapshot.
type planYAML struct {
	ID               string `yaml:"id"`
	Status           string `yaml:"status"`
	CurrentFocusTask string `yaml:"current_focus_task"`
}

type tasksYAML struct {
	Tasks []taskYAML `yaml:"tasks"`
}

type taskYAML struct {
	ID        string   `yaml:"id"`
	Status    string   `yaml:"status"`
	DependsOn []string `yaml:"depends_on"`
}

// readPlan loads one plan directory's PLAN.yaml + TASKS.yaml into a PlanState.
// The bool is false (skip) only when PLAN.yaml is absent or unparseable.
func readPlan(planDir string) (PlanState, bool) {
	var p planYAML
	if !readYAML(filepath.Join(planDir, planFileName), &p) {
		return PlanState{}, false
	}
	plan := PlanState{
		ID:               p.ID,
		Status:           p.Status,
		CurrentFocusTask: p.CurrentFocusTask,
		Tasks:            []TaskState{},
	}
	var tf tasksYAML
	if readYAML(filepath.Join(planDir, tasksFileName), &tf) {
		for _, t := range tf.Tasks {
			plan.Tasks = append(plan.Tasks, TaskState{
				ID:        t.ID,
				Status:    t.Status,
				DependsOn: t.DependsOn,
				Locus:     locusForStatus(t.Status),
			})
		}
		sort.Slice(plan.Tasks, func(i, j int) bool { return plan.Tasks[i].ID < plan.Tasks[j].ID })
	}
	return plan, true
}

// readYAML reads and unmarshals a YAML file into out, returning false when the
// file is missing or unparseable.
func readYAML(path string, out any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return yaml.Unmarshal(data, out) == nil
}

// locusForStatus derives where a task's state currently lives from its canonical
// status (the layered-fanout semantics): a completed task is merged on the
// canonical branch; an awaiting_owner_review task lives in an open, lens-accepted
// PR. Every other status (pending/in_progress/blocked/...) has no settled
// landed-or-PR locus and returns nil — so recovery never conflates done-on-master
// with in-PR work (spec R8). The precise coordinates (merge SHA, PR number)
// require a gh/git read and are filled by the p5/p7 recovery layer.
func locusForStatus(status string) *Locus {
	switch status {
	case statusCompleted:
		return &Locus{Canonical: &CanonicalRef{Ref: canonicalLocusRef}}
	case statusAwaitingOwnerReview:
		return &Locus{InOpenPR: &InOpenPRRef{Status: status}}
	default:
		return nil
	}
}

// computeEligible returns the next/eligible task set across the captured plans: a
// pending task whose every dependency is satisfied. A dependency is satisfied
// when the upstream task is completed or awaiting_owner_review (an open,
// lens-accepted PR unblocks its dependents before the maintainer merges). Deps
// are resolved within the captured set; an unknown dep is treated as unsatisfied.
// The result is sorted by (plan, task) for a deterministic capture.
func computeEligible(plans []PlanState) []TaskRef {
	statusByKey := make(map[string]string)
	for _, p := range plans {
		for _, t := range p.Tasks {
			statusByKey[p.ID+"/"+t.ID] = t.Status
		}
	}
	eligible := []TaskRef{}
	for _, p := range plans {
		for _, t := range p.Tasks {
			if t.Status == statusPending && depsSatisfied(t.DependsOn, p.ID, statusByKey) {
				eligible = append(eligible, TaskRef{Plan: p.ID, Task: t.ID})
			}
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].Plan != eligible[j].Plan {
			return eligible[i].Plan < eligible[j].Plan
		}
		return eligible[i].Task < eligible[j].Task
	})
	return eligible
}

// depsSatisfied reports whether every dependency of a task in plan planID is
// satisfied. A bare dep id resolves within the same plan; a `plan/task` dep is
// cross-plan.
func depsSatisfied(deps []string, planID string, statusByKey map[string]string) bool {
	for _, d := range deps {
		key := d
		if !strings.Contains(d, "/") {
			key = planID + "/" + d
		}
		if !depSatisfies(statusByKey[key]) {
			return false
		}
	}
	return true
}

// depSatisfies reports whether an upstream status satisfies a downstream
// dependency: completed (merged) or awaiting_owner_review (open accepted PR).
func depSatisfies(status string) bool {
	return status == statusCompleted || status == statusAwaitingOwnerReview
}

// delegationYAML is the minimal slice of a delegation contract the snapshot reads.
type delegationYAML struct {
	ID           string `yaml:"id"`
	ParentPlanID string `yaml:"parent_plan_id"`
	ParentTaskID string `yaml:"parent_task_id"`
	Status       string `yaml:"status"`
}

// captureDelegations reads in-flight delegation contracts (status pending/active)
// from .agents/active/delegation, sorted by id. Terminal contracts are omitted —
// they are not live state to re-verify.
func captureDelegations(repoPath string) []DelegationState {
	dir := agentsSubdir(repoPath, activeDirName, delegationSub)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []DelegationState{}
	}
	out := []DelegationState{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), delegationSuffix) {
			continue
		}
		var c delegationYAML
		if !readYAML(filepath.Join(dir, e.Name()), &c) {
			continue
		}
		if c.Status != delegationPending && c.Status != delegationActive {
			continue
		}
		out = append(out, DelegationState{
			ID:           c.ID,
			ParentPlanID: c.ParentPlanID,
			ParentTaskID: c.ParentTaskID,
			Status:       c.Status,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// capturePendingMergeBacks lists the task ids with a merge-back awaiting
// integration (one <task>.md per pending merge-back) from .agents/active/merge-back,
// sorted. Only the bounded id is recorded — never the merge-back body.
func capturePendingMergeBacks(repoPath string) []string {
	dir := agentsSubdir(repoPath, activeDirName, mergeBackSub)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), mergeBackSuffix) {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), mergeBackSuffix))
	}
	sort.Strings(out)
	return out
}

// writeSnapshot marshals snap and writes it to <journal-home>/snapshot.json as a
// full-file atomic write under the package interprocess lock — the same
// durability contract the appender uses for a line. The journal directory is
// created on first write; the lock is always released, and a write failure is
// returned even though release still runs so a partial state is never reported as
// success.
func writeSnapshot(repoPath string, snap SnapshotState) (err error) {
	data, err := marshalSnapshot(snap)
	if err != nil {
		return fmt.Errorf("journal: marshal snapshot: %w", err)
	}
	data = append(data, '\n')

	dir := RepoDir(repoPath)
	if err := mkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("journal: create dir %s: %w", dir, err)
	}

	path := SnapshotPath(repoPath)
	release, err := acquireLock(path)
	if err != nil {
		return fmt.Errorf("journal: lock %s: %w", path, err)
	}
	defer func() {
		if relErr := release(); relErr != nil && err == nil {
			err = fmt.Errorf("journal: release lock %s: %w", path, relErr)
		}
	}()

	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("journal: write snapshot %s: %w", path, err)
	}
	return nil
}
