package workstore

import "time"

// LocalView is a scout's PRIVATE, isolated copy of task state — the prototype's
// model of git worktree isolation. Each scout runs in its own worktree with its
// own checked-out `.agents/workflow/TASKS.yaml`; it reads eligibility from THIS
// copy, not from any shared place. Two scouts in two worktrees therefore both
// see the same task as `pending` and both believe they may dispatch it.
//
// This is the structural root of the real 5xp1c storm (spec §1): "The scout
// reads the main repo's TASKS.yaml each wave. It kept seeing the same tasks as
// pending ... so it re-dispatched them every wave." There is no single shared
// source of truth, so per-worktree views diverge from reality.
type LocalView struct {
	owner string
	tasks map[string]Task
}

// Worktree returns a fresh, independent local view seeded with the given tasks.
// The tasks are deep-copied so this view shares NO memory with any other view
// or with the shared store — exactly like a separate worktree checkout.
func Worktree(owner string, seed ...Task) *LocalView {
	v := &LocalView{owner: owner, tasks: make(map[string]Task, len(seed))}
	for _, t := range seed {
		v.tasks[t.ID] = t.clone()
	}
	return v
}

// Eligible reports whether the task is dispatchable ACCORDING TO THIS LOCAL
// VIEW. It is the per-worktree eligibility decision the scout makes today. It is
// deliberately unaware of any other scout or the shared store — that ignorance
// is the bug being modeled, not a flaw in the test.
func (v *LocalView) Eligible(taskID string) bool {
	t, ok := v.tasks[taskID]
	return ok && t.Status == StatusPending
}

// Owner returns the scout/worktree identity of this view.
func (v *LocalView) Owner() string { return v.owner }

// Task returns this view's local copy of a task (a value, never aliased).
func (v *LocalView) Task(taskID string) (Task, bool) {
	t, ok := v.tasks[taskID]
	return t, ok
}

// markLocalInProgress mutates ONLY this view's private copy. In the real bug,
// when a worker advances status it writes its own worktree's TASKS.yaml, which
// "never propagates to the main repo" (spec §1) — so this local write is
// invisible to every other view. That invisibility is the point.
func (v *LocalView) markLocalInProgress(taskID string) {
	t := v.tasks[taskID]
	t.Status = StatusInProgress
	t.Owner = v.owner
	v.tasks[taskID] = t
}

// version returns this view's local version snapshot for a task — the value a
// CAS claim carries as its expected version. Because views are isolated, a
// scout's local version can be stale relative to the shared store; the shared
// store's CAS check (not the local view) is what rejects a stale claim.
func (v *LocalView) version(taskID string) int {
	return v.tasks[taskID].Version
}

// nowOffset is a tiny per-view clock skew so the TTL scenarios don't all share
// one instant; kept here so scenarios can model independent wall clocks.
func (v *LocalView) clock(base time.Time) time.Time { return base }
