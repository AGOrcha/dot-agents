package journal

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// snapTestRepo points the journal at a fresh temp XDG state dir, freezes the
// clock seam to a fixed instant so determinism assertions hold, and returns a
// non-git repo path whose .agents tree the fixtures populate.
func snapTestRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	origNow := now
	now = func() time.Time { return time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = origNow })
	return t.TempDir()
}

// writeFixture writes content to <repo>/.agents/<rel>, creating parents.
func writeFixture(t *testing.T, repo, rel, content string) {
	t.Helper()
	path := filepath.Join(repo, ".agents", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", rel, err)
	}
}

// writePlanFixture writes a standard two-plan workflow tree exercising every
// status→locus arm, an eligible task gated on a satisfied dep, an in-flight and a
// terminal delegation, and a pending merge-back. Task `t2` carries a large notes
// body to prove the no-bodies bound.
func writePlanFixture(t *testing.T, repo string) {
	t.Helper()
	bigBody := strings.Repeat("SECRET-BODY-DO-NOT-CAPTURE ", 500)

	writeFixture(t, repo, "workflow/plans/alpha/PLAN.yaml", `
id: alpha
status: active
title: Alpha plan
current_focus_task: t2
`)
	writeFixture(t, repo, "workflow/plans/alpha/TASKS.yaml", `
plan_id: alpha
tasks:
  - id: t1
    status: completed
    notes: `+"|-\n      "+bigBody+`
  - id: t2
    status: awaiting_owner_review
  - id: t3
    status: pending
    depends_on: [t1]
  - id: t4
    status: pending
    depends_on: [t2, bravo/b1]
  - id: t5
    status: pending
    depends_on: [t1, t3]
`)
	writeFixture(t, repo, "workflow/plans/bravo/PLAN.yaml", `
id: bravo
status: active
`)
	writeFixture(t, repo, "workflow/plans/bravo/TASKS.yaml", `
plan_id: bravo
tasks:
  - id: b1
    status: in_progress
`)

	writeFixture(t, repo, "active/delegation/t-live.yaml", `
id: del-live
parent_plan_id: alpha
parent_task_id: t2
status: active
`)
	writeFixture(t, repo, "active/delegation/t-done.yaml", `
id: del-done
parent_plan_id: alpha
parent_task_id: t1
status: completed
`)
	writeFixture(t, repo, "active/merge-back/t1.md", "---\ntask_id: t1\n---\nbody\n")
}

func TestSnapshotPath(t *testing.T) {
	repo := snapTestRepo(t)
	got := SnapshotPath(repo)
	want := filepath.Join(RepoDir(repo), "snapshot.json")
	if got != want {
		t.Fatalf("SnapshotPath = %q, want %q", got, want)
	}
	if filepath.Base(got) != "snapshot.json" {
		t.Fatalf("snapshot file name = %q", filepath.Base(got))
	}
}

func TestSnapshotCapturesLiveState(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if snap.Schema != SnapshotSchema || snap.Version != SnapshotVersion {
		t.Fatalf("schema/version = %q/%d", snap.Schema, snap.Version)
	}
	if snap.CapturedAt != "2026-06-29T12:00:00Z" {
		t.Fatalf("captured_at = %q", snap.CapturedAt)
	}
	if snap.Identity.WorktreePath == "" {
		t.Fatalf("identity worktree path not populated")
	}

	if len(snap.Plans) != 2 || snap.Plans[0].ID != "alpha" || snap.Plans[1].ID != "bravo" {
		t.Fatalf("plans = %+v", snap.Plans)
	}
	if snap.Plans[0].CurrentFocusTask != "t2" {
		t.Fatalf("focus task = %q", snap.Plans[0].CurrentFocusTask)
	}

	// Tasks sorted by id within the plan.
	alphaIDs := []string{}
	for _, tk := range snap.Plans[0].Tasks {
		alphaIDs = append(alphaIDs, tk.ID)
	}
	if strings.Join(alphaIDs, ",") != "t1,t2,t3,t4,t5" {
		t.Fatalf("alpha task order = %v", alphaIDs)
	}
}

func TestSnapshotLocusRecordedPerStatus(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	byID := map[string]TaskState{}
	for _, tk := range snap.Plans[0].Tasks {
		byID[tk.ID] = tk
	}

	// completed → canonical arm only.
	if l := byID["t1"].Locus; l == nil || l.Canonical == nil || l.InOpenPR != nil {
		t.Fatalf("t1 locus = %+v, want canonical", l)
	}
	if byID["t1"].Locus.Canonical.Ref != canonicalLocusRef {
		t.Fatalf("t1 canonical ref = %q", byID["t1"].Locus.Canonical.Ref)
	}
	// awaiting_owner_review → in-PR arm only.
	if l := byID["t2"].Locus; l == nil || l.InOpenPR == nil || l.Canonical != nil {
		t.Fatalf("t2 locus = %+v, want in_open_pr", l)
	}
	if byID["t2"].Locus.InOpenPR.Status != statusAwaitingOwnerReview {
		t.Fatalf("t2 in-pr status = %q", byID["t2"].Locus.InOpenPR.Status)
	}
	// pending → no locus.
	if l := byID["t3"].Locus; l != nil {
		t.Fatalf("t3 locus = %+v, want nil", l)
	}
	// Every recorded locus is a well-formed sum type.
	for _, tk := range snap.Plans[0].Tasks {
		if err := tk.Locus.Validate(); err != nil {
			t.Fatalf("task %s locus invalid: %v", tk.ID, err)
		}
	}
}

func TestSnapshotEligibleSet(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// t3: dep t1 completed → eligible.
	// t4: deps t2 (awaiting_owner_review → satisfies) + bravo/b1 (in_progress →
	//     not satisfied) → NOT eligible (cross-plan dep unsatisfied).
	// t5: deps t1 completed + t3 pending → NOT eligible.
	// b1: in_progress, not pending → NOT eligible.
	got := []string{}
	for _, e := range snap.Eligible {
		got = append(got, e.Plan+"/"+e.Task)
	}
	if strings.Join(got, ",") != "alpha/t3" {
		t.Fatalf("eligible = %v, want [alpha/t3]", got)
	}
}

func TestSnapshotCrossPlanDepSatisfied(t *testing.T) {
	repo := snapTestRepo(t)
	// b1 completed so alpha/t4's cross-plan dep is satisfied; t2 is also satisfied.
	writeFixture(t, repo, "workflow/plans/alpha/PLAN.yaml", "id: alpha\nstatus: active\n")
	writeFixture(t, repo, "workflow/plans/alpha/TASKS.yaml", `
tasks:
  - id: t2
    status: completed
  - id: t4
    status: pending
    depends_on: [t2, bravo/b1]
`)
	writeFixture(t, repo, "workflow/plans/bravo/PLAN.yaml", "id: bravo\nstatus: active\n")
	writeFixture(t, repo, "workflow/plans/bravo/TASKS.yaml", "tasks:\n  - id: b1\n    status: completed\n")

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Eligible) != 1 || snap.Eligible[0].Task != "t4" {
		t.Fatalf("eligible = %+v, want [alpha/t4]", snap.Eligible)
	}
}

func TestSnapshotEligibleSortedAcrossAndWithinPlans(t *testing.T) {
	repo := snapTestRepo(t)
	// Two plans, each with two dependency-free pending tasks, written out of
	// order so the sort must order both across plans and within a plan.
	writeFixture(t, repo, "workflow/plans/zeta/PLAN.yaml", "id: zeta\nstatus: active\n")
	writeFixture(t, repo, "workflow/plans/zeta/TASKS.yaml", "tasks:\n  - id: z2\n    status: pending\n  - id: z1\n    status: pending\n")
	writeFixture(t, repo, "workflow/plans/alpha/PLAN.yaml", "id: alpha\nstatus: active\n")
	writeFixture(t, repo, "workflow/plans/alpha/TASKS.yaml", "tasks:\n  - id: a2\n    status: pending\n  - id: a1\n    status: pending\n")

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	got := []string{}
	for _, e := range snap.Eligible {
		got = append(got, e.Plan+"/"+e.Task)
	}
	want := "alpha/a1,alpha/a2,zeta/z1,zeta/z2"
	if strings.Join(got, ",") != want {
		t.Fatalf("eligible order = %v, want %s", got, want)
	}
}

func TestSnapshotDelegationsAndMergeBacks(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Only the in-flight (active) delegation is captured; the completed one is omitted.
	if len(snap.Delegations) != 1 {
		t.Fatalf("delegations = %+v, want 1 in-flight", snap.Delegations)
	}
	if snap.Delegations[0].ID != "del-live" || snap.Delegations[0].Status != "active" {
		t.Fatalf("delegation = %+v", snap.Delegations[0])
	}
	if snap.Delegations[0].ParentTaskID != "t2" {
		t.Fatalf("delegation parent task = %q", snap.Delegations[0].ParentTaskID)
	}

	if len(snap.PendingMergeBacks) != 1 || snap.PendingMergeBacks[0] != "t1" {
		t.Fatalf("pending merge-backs = %+v", snap.PendingMergeBacks)
	}
}

func TestSnapshotDelegationsSortedByID(t *testing.T) {
	repo := snapTestRepo(t)
	writeFixture(t, repo, "active/delegation/x.yaml", "id: del-z\nparent_plan_id: p\nparent_task_id: tz\nstatus: pending\n")
	writeFixture(t, repo, "active/delegation/y.yaml", "id: del-a\nparent_plan_id: p\nparent_task_id: ta\nstatus: active\n")

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Delegations) != 2 || snap.Delegations[0].ID != "del-a" || snap.Delegations[1].ID != "del-z" {
		t.Fatalf("delegations not sorted by id: %+v", snap.Delegations)
	}
}

func TestSnapshotNoBodiesBound(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	if _, err := Snapshot(repo); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data, err := os.ReadFile(SnapshotPath(repo))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if bytes.Contains(data, []byte("SECRET-BODY-DO-NOT-CAPTURE")) {
		t.Fatalf("snapshot captured a task body — no-bodies bound violated")
	}
	if bytes.Contains(data, []byte("notes")) || bytes.Contains(data, []byte("title")) {
		t.Fatalf("snapshot captured a free-text body field")
	}
}

func TestSnapshotDeterministicByteIdentical(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	if _, err := Snapshot(repo); err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}
	first, err := os.ReadFile(SnapshotPath(repo))
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	if _, err := Snapshot(repo); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}
	second, err := os.ReadFile(SnapshotPath(repo))
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("snapshots of the same state are not byte-identical:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestSnapshotAtomicWriteFullFile(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// The persisted file decodes to a complete, valid snapshot (no torn write).
	loaded, err := LoadSnapshot(repo)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loaded.Schema != snap.Schema || len(loaded.Plans) != len(snap.Plans) {
		t.Fatalf("loaded snapshot differs from captured")
	}
	data, err := os.ReadFile(SnapshotPath(repo))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("snapshot file is not valid JSON (torn write?)")
	}
	if data[len(data)-1] != '\n' {
		t.Fatalf("snapshot file missing trailing newline")
	}
}

func TestSnapshotInterprocessLocked(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	var lockedPath string
	released := false
	orig := acquireLock
	acquireLock = func(path string) (func() error, error) {
		lockedPath = path
		return func() error { released = true; return nil }, nil
	}
	defer func() { acquireLock = orig }()

	if _, err := Snapshot(repo); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if lockedPath != SnapshotPath(repo) {
		t.Fatalf("lock taken on %q, want %q", lockedPath, SnapshotPath(repo))
	}
	if !released {
		t.Fatalf("lock not released")
	}
}

func TestSnapshotEmptyWorkflowTree(t *testing.T) {
	repo := snapTestRepo(t)

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Empty-but-valid: all collections non-nil so they marshal to [] not null.
	data, err := os.ReadFile(SnapshotPath(repo))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	for _, key := range []string{`"plans": []`, `"eligible": []`, `"delegations": []`, `"pending_merge_backs": []`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("empty snapshot missing %s in:\n%s", key, data)
		}
	}
	if len(snap.Plans) != 0 || len(snap.Eligible) != 0 {
		t.Fatalf("empty snapshot has state: %+v", snap)
	}
}

func TestSnapshotSkipsUnparseablePlan(t *testing.T) {
	repo := snapTestRepo(t)
	// alpha is valid; broken has unparseable PLAN.yaml (skipped); notasks has no
	// TASKS.yaml (kept with empty tasks). A stray file (not a dir) is ignored.
	writeFixture(t, repo, "workflow/plans/alpha/PLAN.yaml", "id: alpha\nstatus: active\n")
	writeFixture(t, repo, "workflow/plans/alpha/TASKS.yaml", "tasks: []\n")
	writeFixture(t, repo, "workflow/plans/broken/PLAN.yaml", "id: broken\nstatus: : : not yaml\n\t- bad\n")
	writeFixture(t, repo, "workflow/plans/notasks/PLAN.yaml", "id: notasks\nstatus: active\n")
	writeFixture(t, repo, "workflow/plans/stray.txt", "not a dir")

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	ids := []string{}
	for _, p := range snap.Plans {
		ids = append(ids, p.ID)
	}
	// broken is skipped; alpha and notasks survive.
	if strings.Join(ids, ",") != "alpha,notasks" {
		t.Fatalf("plan ids = %v, want [alpha notasks]", ids)
	}
	for _, p := range snap.Plans {
		if p.Tasks == nil {
			t.Fatalf("plan %s tasks is nil, want non-nil", p.ID)
		}
	}
}

func TestSnapshotSkipsUnparseableDelegation(t *testing.T) {
	repo := snapTestRepo(t)
	writeFixture(t, repo, "active/delegation/good.yaml", "id: ok\nparent_plan_id: a\nparent_task_id: t\nstatus: pending\n")
	writeFixture(t, repo, "active/delegation/bad.yaml", "id: : : not yaml\n\t- x\n")
	writeFixture(t, repo, "active/delegation/notyaml.txt", "ignored")
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "active", "delegation", "sub.yaml"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Delegations) != 1 || snap.Delegations[0].ID != "ok" {
		t.Fatalf("delegations = %+v, want [ok]", snap.Delegations)
	}
}

func TestSnapshotPendingMergeBackIgnoresNonMarkdown(t *testing.T) {
	repo := snapTestRepo(t)
	writeFixture(t, repo, "active/merge-back/t9.md", "body")
	writeFixture(t, repo, "active/merge-back/notes.txt", "ignored")
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "active", "merge-back", "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	snap, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.PendingMergeBacks) != 1 || snap.PendingMergeBacks[0] != "t9" {
		t.Fatalf("merge-backs = %+v, want [t9]", snap.PendingMergeBacks)
	}
}

func TestLoadSnapshotRoundTrip(t *testing.T) {
	repo := snapTestRepo(t)
	writePlanFixture(t, repo)

	captured, err := Snapshot(repo)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded, err := LoadSnapshot(repo)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}

	a, _ := json.Marshal(captured)
	b, _ := json.Marshal(loaded)
	if !bytes.Equal(a, b) {
		t.Fatalf("round-trip mismatch:\ncaptured=%s\nloaded=%s", a, b)
	}
}

func TestLoadSnapshotMissingFile(t *testing.T) {
	repo := snapTestRepo(t)
	if _, err := LoadSnapshot(repo); err == nil || !strings.Contains(err.Error(), "read snapshot") {
		t.Fatalf("want read error, got %v", err)
	}
}

func TestLoadSnapshotMalformedJSON(t *testing.T) {
	repo := snapTestRepo(t)
	if err := os.MkdirAll(RepoDir(repo), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(SnapshotPath(repo), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadSnapshot(repo); err == nil || !strings.Contains(err.Error(), "decode snapshot") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestSnapshotMarshalError(t *testing.T) {
	repo := snapTestRepo(t)
	orig := marshalSnapshot
	marshalSnapshot = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }
	defer func() { marshalSnapshot = orig }()

	if _, err := Snapshot(repo); err == nil || !strings.Contains(err.Error(), "marshal snapshot") {
		t.Fatalf("want marshal error, got %v", err)
	}
}

func TestSnapshotMkdirError(t *testing.T) {
	repo := snapTestRepo(t)
	orig := mkdirAll
	mkdirAll = func(string, os.FileMode) error { return errors.New("mkdir boom") }
	defer func() { mkdirAll = orig }()

	if _, err := Snapshot(repo); err == nil || !strings.Contains(err.Error(), "create dir") {
		t.Fatalf("want mkdir error, got %v", err)
	}
}

func TestSnapshotLockError(t *testing.T) {
	repo := snapTestRepo(t)
	orig := acquireLock
	acquireLock = func(string) (func() error, error) { return nil, errors.New("lock boom") }
	defer func() { acquireLock = orig }()

	if _, err := Snapshot(repo); err == nil || !strings.Contains(err.Error(), "lock") {
		t.Fatalf("want lock error, got %v", err)
	}
}

func TestSnapshotWriteError(t *testing.T) {
	repo := snapTestRepo(t)
	orig := writeFileAtomic
	writeFileAtomic = func(string, []byte) error { return errors.New("write boom") }
	defer func() { writeFileAtomic = orig }()

	if _, err := Snapshot(repo); err == nil || !strings.Contains(err.Error(), "write snapshot") {
		t.Fatalf("want write error, got %v", err)
	}
}

func TestSnapshotReleaseErrorSurfaced(t *testing.T) {
	repo := snapTestRepo(t)
	orig := acquireLock
	acquireLock = func(string) (func() error, error) {
		return func() error { return errors.New("release boom") }, nil
	}
	defer func() { acquireLock = orig }()

	// The atomic write succeeds, so the deferred release error becomes the
	// returned error.
	if _, err := Snapshot(repo); err == nil || !strings.Contains(err.Error(), "release lock") {
		t.Fatalf("want release error surfaced, got %v", err)
	}
}
