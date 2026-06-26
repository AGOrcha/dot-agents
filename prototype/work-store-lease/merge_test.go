package workstore

import (
	"testing"
	"time"
)

// baseTask is the common ancestor both planes diverge from in the conflict
// scenarios. Uses faithful schema fields (WriteScope []string, DependsOn).
func baseTask() Task {
	return Task{
		ID:         "t1",
		Status:     StatusInProgress,
		Version:    1,
		WriteScope: []string{"config/v2/a.go"},
		Notes:      "original",
		DependsOn:  []string{"p1a"},
	}
}

func sliceEq(a, b []string) bool { return slicesEqual(a, b) }

// H-conflict: concurrent edits — the backend changes status (e.g. a dependency
// regressed -> blocked), the worker changes content (notes + write_scope).
// Merge must keep the BACKEND status and the LOCAL content, no lost update.
func TestHConflict_BackendStatusLocalContent(t *testing.T) {
	base := baseTask()

	backend := base // backend advances status only
	backend.Status = StatusBlocked

	local := base // worker edits content only
	local.Notes = "worker added detail"
	local.WriteScope = []string{"config/v2/a.go", "config/v2/b.go"}

	got := Merge(base, local, backend)

	if got.Status != StatusBlocked {
		t.Fatalf("status should be backend-wins (%s), got %s", StatusBlocked, got.Status)
	}
	if got.Notes != "worker added detail" {
		t.Fatalf("notes should be local-wins, got %q", got.Notes)
	}
	if !sliceEq(got.WriteScope, []string{"config/v2/a.go", "config/v2/b.go"}) {
		t.Fatalf("writeScope should be local-wins, got %v", got.WriteScope)
	}
}

// H-conflict, true field-level race: BOTH planes touch the SAME field. The
// backend and the worker both write status -> backend must win; both write
// notes -> local must win. This is the case where ownership actually decides
// (a no-conflict merge can't distinguish a correct rule from a broken one).
func TestHConflict_SameFieldBothSidesOwnershipDecides(t *testing.T) {
	base := baseTask()

	backend := base
	backend.Status = StatusBlocked        // backend's status
	backend.Notes = "backend stale notes" // backend ALSO touched notes

	local := base
	local.Status = StatusCompleted    // worker's (losing) status
	local.Notes = "worker live notes" // worker's (winning) notes

	got := Merge(base, local, backend)

	if got.Status != StatusBlocked {
		t.Fatalf("status conflict must be backend-wins, got %s", got.Status)
	}
	if got.Notes != "worker live notes" {
		t.Fatalf("notes conflict must be local-wins, got %q", got.Notes)
	}
}

// Unchanged side never overrides: if only the worker edited notes and the
// backend left it at base, the worker's edit survives even though notes is a
// "local-wins" field (no conflict to resolve).
func TestHConflict_NoLostUpdateWhenOneSideUntouched(t *testing.T) {
	base := baseTask()
	backend := base
	backend.Status = StatusBlocked // backend only moved status
	local := base
	local.Notes = "only worker edit"

	got := Merge(base, local, backend)
	if got.Notes != "only worker edit" {
		t.Fatalf("worker's sole edit was lost: %q", got.Notes)
	}
	if got.Status != StatusBlocked {
		t.Fatalf("backend's sole status edit was lost: %s", got.Status)
	}
}

// Mutation check: flip the field-ownership rule and assert the H-conflict
// outcome inverts. This proves the test is sensitive to the production merge
// rule rather than coincidentally passing. We restore the table afterward.
func TestHConflict_MutationSensitiveToOwnershipFlip(t *testing.T) {
	base := baseTask()
	backend := base
	backend.Status = StatusBlocked
	backend.Notes = "backend notes"
	local := base
	local.Status = StatusCompleted
	local.Notes = "worker notes"

	// Sanity: correct rule.
	if got := Merge(base, local, backend); got.Status != StatusBlocked || got.Notes != "worker notes" {
		t.Fatalf("baseline rule wrong before mutation: %+v", got)
	}

	// Mutate the production ownership table: swap status<->content owners.
	restore := swapOwnership(t, "status", LocalWins)
	defer restore()
	restoreNotes := swapOwnership(t, "notes", BackendWins)
	defer restoreNotes()

	got := Merge(base, local, backend)
	if got.Status == StatusBlocked {
		t.Fatal("mutation not detected: status still backend-wins after flipping rule to local-wins")
	}
	if got.Notes == "worker notes" {
		t.Fatal("mutation not detected: notes still local-wins after flipping rule to backend-wins")
	}
	if got.Status != StatusCompleted || got.Notes != "backend notes" {
		t.Fatalf("flipped rule should invert outcome, got status=%s notes=%q", got.Status, got.Notes)
	}
}

// ---------------------------------------------------------------------------
// Design-finding RE-VALIDATION against the model. These tests demonstrate the
// cases where the plain "status = backend-wins" rule is WRONG, validating the
// findings carried over from the first pass. They DOCUMENT the gap by asserting
// the (undesirable) current behaviour and naming the rule that should replace
// it — so a future fix flips a red expectation, not silent prose.
// ---------------------------------------------------------------------------

// FINDING: terminal states want idempotent FIRST-WRITER, not backend-wins. A
// worker just completed the task locally; the backend concurrently moved it
// back to in_progress (e.g. a re-dispatch from a stale eligibility view). Plain
// backend-wins RESURRECTS dead work — the merge throws away the completion.
// This test asserts the current (wrong-for-terminal) behaviour to prove the
// finding is real and exercised, not hypothetical.
func TestFinding_TerminalStateBackendWinsResurrectsWork(t *testing.T) {
	// base = pending. BOTH planes move status (a true conflict): the worker
	// completed it; the backend concurrently re-dispatched it to in_progress
	// (the exact storm shape — a stale eligibility view re-activating done work).
	base := baseTask()
	base.Status = StatusPending
	local := base
	local.Status = StatusCompleted // worker finished
	backend := base
	backend.Status = StatusInProgress // backend re-activated it (conflict)

	got := Merge(base, local, backend)
	// Documented gap: with both sides changing status it is a true conflict, so
	// plain backend-wins RESURRECTS the work, discarding the completion.
	if got.Status != StatusInProgress {
		t.Fatalf("expected backend-wins to clobber the completion (the gap), got %s", got.Status)
	}
	// The CORRECT rule (terminal = idempotent first-writer) preserves Completed.
	if want := firstWriterTerminal(base, local, backend); want.Status != StatusCompleted {
		t.Fatalf("recommended terminal rule should preserve Completed, got %s", want.Status)
	}
}

// firstWriterTerminal is the recommended rule for terminal transitions: if
// EITHER side reached a terminal state, that wins and is idempotent (a terminal
// state is never reverted by the other plane). This is the fix the finding
// recommends; validated by the test above.
func firstWriterTerminal(base, local, backend Task) Task {
	if isTerminal(local.Status) {
		return local
	}
	if isTerminal(backend.Status) {
		return backend
	}
	return Merge(base, local, backend)
}

func isTerminal(s Status) bool { return s == StatusCompleted || s == StatusCancelled }

// FINDING: `blocked` wants UNION/OR, not last-writer. If either plane believes
// the task is blocked, it should be blocked (you must not dispatch a task one
// side knows is blocked). Plain backend-wins drops a LOCAL block when the
// backend left status untouched-or-forward. Validate the recommended union rule.
func TestFinding_BlockedWantsUnionNotLastWriter(t *testing.T) {
	// base = pending. Worker discovered a blocker (-> blocked); the backend
	// concurrently advanced it (-> in_progress). Both changed status, so it is a
	// true conflict and plain backend-wins WRONGLY unblocks a task the worker
	// knows is blocked.
	base := baseTask()
	base.Status = StatusPending
	local := base
	local.Status = StatusBlocked
	backend := base
	backend.Status = StatusInProgress

	got := Merge(base, local, backend)
	if got.Status != StatusInProgress {
		t.Fatalf("expected backend-wins to (wrongly) unblock (the gap), got %s", got.Status)
	}
	// Recommended union rule keeps blocked.
	if want := blockedUnion(base, local, backend); want.Status != StatusBlocked {
		t.Fatalf("recommended blocked-union should keep Blocked, got %s", want.Status)
	}
}

// blockedUnion is the recommended rule: blocked if EITHER side is blocked.
func blockedUnion(base, local, backend Task) Task {
	out := Merge(base, local, backend)
	if local.Status == StatusBlocked || backend.Status == StatusBlocked {
		out.Status = StatusBlocked
	}
	return out
}

// FINDING: lease expiry MID-WORK needs FENCING. A worker holds a lease, the TTL
// expires while it is still working, a second scout reclaims (new lease), then
// the original worker tries to Complete. Without a fencing check the stale
// holder's completion is accepted -> two PRs (the storm via a different door).
// Validate that completing under a SUPERSEDED lease must be rejected.
func TestFinding_LeaseExpiryMidWorkNeedsFencing(t *testing.T) {
	const ttl = time.Minute
	t0 := time.Now()
	s := New()
	s.Add("t1", []string{"pkg/a"}, "notes")
	va := Worktree("scout-a", eligibleSeed("t1"))
	vb := Worktree("scout-b", eligibleSeed("t1"))

	claimed, err := s.ClaimTTL("t1", va.Owner(), t0, ttl)
	if err != nil {
		t.Fatalf("scout-a claim failed: %v", err)
	}
	aFence := claimed.Version // scout-a's fencing token

	// TTL expires; scout-b reclaims with a NEW lease (version bumps).
	if _, err := s.ClaimTTL("t1", vb.Owner(), t0.Add(ttl+time.Second), ttl); err != nil {
		t.Fatalf("scout-b reclaim failed: %v", err)
	}

	// scout-a (stale holder) tries to complete using its OLD fencing token.
	// This MUST be rejected — that is the fix the finding recommends.
	if err := s.CompleteFenced("t1", va.Owner(), aFence); err == nil {
		t.Fatal("FENCING GAP: stale leaseholder's completion was accepted (would double-dispatch)")
	}
	// scout-b's current-token completion is accepted.
	cur, _ := s.Get("t1")
	if err := s.CompleteFenced("t1", vb.Owner(), cur.Version); err != nil {
		t.Fatalf("current holder should complete: %v", err)
	}
}

// The VERSION fence is the load-bearing check, independent of owner. Even when
// the same owner presents a STALE token (e.g. a renew bumped the version under
// it), CompleteFenced must reject with ErrVersionConflict — proving the fix is
// the fencing token, not merely the owner identity.
func TestFinding_FenceRejectsStaleTokenSameOwner(t *testing.T) {
	const ttl = time.Minute
	t0 := time.Now()
	s := New()
	s.Add("t1", []string{"pkg/a"}, "notes")
	va := Worktree("scout-a", eligibleSeed("t1"))

	claimed, err := s.ClaimTTL("t1", va.Owner(), t0, ttl)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	staleToken := claimed.Version
	// A backend-side status bump (e.g. daemon recorded a transition) moves the
	// version under the SAME owner's still-live lease.
	if err := s.AdvanceStatus("t1", StatusBlocked); err != nil {
		t.Fatalf("advance failed: %v", err)
	}
	if err := s.CompleteFenced("t1", va.Owner(), staleToken); err != ErrVersionConflict {
		t.Fatalf("fence must reject a stale token even for the same owner, got %v", err)
	}
}

// swapOwnership mutates the package-level fieldOwnership table for a single
// field and returns a restore func. Used only by the mutation-sensitivity test.
func swapOwnership(t *testing.T, field string, to FieldOwner) func() {
	t.Helper()
	prev, had := fieldOwnership[field]
	fieldOwnership[field] = to
	return func() {
		if had {
			fieldOwnership[field] = prev
		} else {
			delete(fieldOwnership, field)
		}
	}
}
