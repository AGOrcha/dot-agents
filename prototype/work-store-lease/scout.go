package workstore

import "time"

// DispatchResult records what a scout did with one eligible task: either it won
// the claim and dispatched (Dispatched=true), or it observed the task already
// claimed and backed off (Dispatched=false, BackedOff=true). Exactly one scout
// in a race must report Dispatched=true — that is the no-double-dispatch
// invariant the storm regression asserts.
type DispatchResult struct {
	Scout      string
	TaskID     string
	Dispatched bool
	BackedOff  bool
	Err        error
}

// ScoutNaive is the NEGATIVE CONTROL: the exact broken logic that caused the
// real 5xp1c re-dispatch storm. It reads the task, sees it is eligible
// (pending), and dispatches — with NO atomic claim. Two concurrent scouts both
// read "pending" before either marks it in_progress, so BOTH dispatch. The
// MarkInProgress write is best-effort and races; the read-then-dispatch gap is
// the bug. The proofs assert this path PRODUCES double-dispatch (it must fail
// the exactly-one-winner invariant) — proving the lease/CAS paths actually fix
// something, not just pass a weak test.
func ScoutNaive(s *Store, scout, taskID string) DispatchResult {
	cur, err := s.Get(taskID)
	if err != nil {
		return DispatchResult{Scout: scout, TaskID: taskID, Err: err}
	}
	if cur.Status != StatusPending {
		return DispatchResult{Scout: scout, TaskID: taskID, BackedOff: true}
	}
	// THE BUG: dispatch decided on a stale read; no atomic guard between the
	// eligibility check above and the mark below. A concurrent scout passes the
	// same check before this write lands.
	s.MarkInProgress(taskID, scout)
	return DispatchResult{Scout: scout, TaskID: taskID, Dispatched: true}
}

// ScoutTTL is the production claim+dispatch path under the TTL-lease mechanism.
// A real scout calls exactly this: lease the eligible task, and only dispatch
// if the lease was won. The proofs drive THIS function (not ClaimTTL directly)
// so the test exercises the same entry point a wave scout uses.
func ScoutTTL(s *Store, scout, taskID string, now time.Time, ttl time.Duration) DispatchResult {
	_, err := s.ClaimTTL(taskID, scout, now, ttl)
	return classify(scout, taskID, err)
}

// ScoutCAS is the production claim+dispatch path under the CAS mechanism. The
// scout reads the current version (its eligibility snapshot), then claims with
// that expected version. A concurrent winner bumps the version, so every loser
// gets ErrVersionConflict or ErrAlreadyClaimed and backs off.
func ScoutCAS(s *Store, scout, taskID string) DispatchResult {
	cur, err := s.Get(taskID)
	if err != nil {
		return DispatchResult{Scout: scout, TaskID: taskID, Err: err}
	}
	_, err = s.ClaimCAS(taskID, scout, cur.Version)
	return classify(scout, taskID, err)
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
