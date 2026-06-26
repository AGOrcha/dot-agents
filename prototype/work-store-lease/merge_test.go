package workstore

import "testing"

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
