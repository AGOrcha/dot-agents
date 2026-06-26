package workstore

import "time"

// DispatchResult records what a scout did with one eligible task: either it won
// the claim and dispatched (Dispatched=true), or it observed the task already
// claimed/leased and backed off (BackedOff=true). Exactly one scout in a wave
// must report Dispatched=true — the no-double-dispatch invariant.
type DispatchResult struct {
	Scout      string
	TaskID     string
	Dispatched bool
	BackedOff  bool
	Err        error
}

// A scout in this model has TWO layers:
//
//	1. its own LocalView   — a private, isolated worktree checkout of TASKS.yaml
//	2. (optionally) a shared *Store — the proposed single source of truth
//
// The negative control uses ONLY layer 1. The fix consults layer 2 before
// dispatching. This is the faithful split: the real bug is the ABSENCE of a
// shared claim, not a lost in-process race on one object.

// ScoutNaive is the NEGATIVE CONTROL and the FAITHFUL model of the real 5xp1c
// storm. The scout decides eligibility and dispatches off its OWN LOCAL VIEW,
// with NO shared store and NO claim. It does not — cannot — see what any other
// worktree decided. N scouts in N isolated worktrees each read p1c as `pending`
// in their private copy and each dispatch it. The local mark it writes
// (markLocalInProgress) updates only its own worktree and never propagates.
//
// Crucially this path touches NO shared coordination object: its double-dispatch
// cannot be blamed on a missing mutex, because there is nothing to lock. The bug
// is architectural (no shared SOT), exactly as spec §1 describes.
func ScoutNaive(v *LocalView, taskID string) DispatchResult {
	if !v.Eligible(taskID) {
		return DispatchResult{Scout: v.Owner(), TaskID: taskID, BackedOff: true}
	}
	v.markLocalInProgress(taskID) // writes only this worktree's copy; invisible to peers
	return DispatchResult{Scout: v.Owner(), TaskID: taskID, Dispatched: true}
}

// ScoutTTL is the FIX under the TTL-lease mechanism. The scout still decides
// eligibility from its (possibly stale) local view, but before dispatching it
// must CLAIM the task in the SHARED store. The shared claim is the coordination
// point: even when all scouts' local views say `pending`, the store grants the
// lease to exactly one and rejects the rest, so exactly one dispatches.
func ScoutTTL(s *Store, v *LocalView, taskID string, now time.Time, ttl time.Duration) DispatchResult {
	if !v.Eligible(taskID) { // local pre-filter, as a real scout does
		return DispatchResult{Scout: v.Owner(), TaskID: taskID, BackedOff: true}
	}
	_, err := s.ClaimTTL(taskID, v.Owner(), v.clock(now), ttl)
	return classify(v.Owner(), taskID, err)
}

// ScoutCAS is the FIX under the compare-and-set mechanism. The scout carries its
// LOCAL (stale) version as the expected version. Because views are isolated, a
// loser's local version is stale relative to the winner's bump, so the SHARED
// store's CAS check rejects it — the rejection happens at the store, not in the
// scout. Exactly one CAS succeeds.
func ScoutCAS(s *Store, v *LocalView, taskID string) DispatchResult {
	if !v.Eligible(taskID) {
		return DispatchResult{Scout: v.Owner(), TaskID: taskID, BackedOff: true}
	}
	_, err := s.ClaimCAS(taskID, v.Owner(), v.version(taskID))
	return classify(v.Owner(), taskID, err)
}

// classify turns a claim error into a dispatch outcome. A nil error is a
// dispatch; the back-off sentinels are a correct back-off; anything else is a
// hard error the caller must surface.
func classify(scout, taskID string, err error) DispatchResult {
	r := DispatchResult{Scout: scout, TaskID: taskID}
	switch err {
	case nil:
		r.Dispatched = true
	case ErrAlreadyClaimed, ErrVersionConflict:
		r.BackedOff = true
	default:
		r.Err = err
	}
	return r
}
