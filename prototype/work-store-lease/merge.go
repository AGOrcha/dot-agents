package workstore

// FieldOwner names which plane is authoritative for a given task field when a
// local (worker) edit and a backend (lease/transition) edit race.
//
// The spec's starting rule (design.md §3 D2 / §6 OQ2):
//
//	status  -> backend-wins   (the lease/transition is authoritative)
//	content -> local-wins     (worker edits to write_scope/notes win)
type FieldOwner int

const (
	// BackendWins: take the backend value on conflict. Used for status.
	BackendWins FieldOwner = iota
	// LocalWins: take the local value on conflict. Used for content fields.
	LocalWins
)

// fieldOwnership is the per-field ownership map that drives Merge. Keeping it a
// data table (not branches in Merge) is what makes the mutation check in the
// proofs meaningful: flipping an entry here flips the merge outcome, and the
// H-conflict test then fails.
var fieldOwnership = map[string]FieldOwner{
	"status":     BackendWins,
	"version":    BackendWins,
	"lease":      BackendWins,
	"writeScope": LocalWins,
	"notes":      LocalWins,
	"dependsOn":  LocalWins,
}

// owner returns the configured owner for a field name, defaulting to LocalWins
// for unmapped content-like fields (content is the conservative default — never
// silently let the backend clobber an unknown worker edit).
func owner(field string) FieldOwner {
	if o, ok := fieldOwnership[field]; ok {
		return o
	}
	return LocalWins
}

// Merge resolves a concurrent edit between a local (worker) copy and a backend
// copy of the same task, applying fieldOwnership per field. base is the common
// ancestor both sides diverged from; it lets Merge tell "this side changed the
// field" from "this side left it untouched", so an unchanged side never
// overrides the side that actually edited.
//
// Resolution per field:
//   - only one side changed it      -> take that side (no conflict)
//   - both changed it (a conflict)  -> apply fieldOwnership
//   - neither changed it            -> keep base
func Merge(base, local, backend Task) Task {
	out := base
	out.Status = mergeStatus(base, local, backend)
	out.Version = mergeVersion(base, local, backend)
	out.Lease = mergeLease(base, local, backend)
	out.WriteScope = mergeWriteScope(base, local, backend)
	out.Notes = mergeNotes(base, local, backend)
	out.DependsOn = mergeDependsOn(base, local, backend)
	return out
}

// resolve3Slice is resolve3 for non-comparable []string fields (write_scope,
// depends_on). It uses equality-by-content to detect which side changed.
func resolve3Slice(field string, baseV, localV, backendV []string) []string {
	localChanged := !slicesEqual(localV, baseV)
	backendChanged := !slicesEqual(backendV, baseV)
	switch {
	case localChanged && backendChanged:
		if owner(field) == BackendWins {
			return backendV
		}
		return localV
	case localChanged:
		return localV
	case backendChanged:
		return backendV
	default:
		return baseV
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// resolve3 is the three-way field rule for a single comparable value, given the
// field's configured owner. localV/backendV are the two candidate values.
func resolve3[T comparable](field string, baseV, localV, backendV T) T {
	localChanged := localV != baseV
	backendChanged := backendV != baseV
	switch {
	case localChanged && backendChanged: // true conflict -> ownership decides
		if owner(field) == BackendWins {
			return backendV
		}
		return localV
	case localChanged:
		return localV
	case backendChanged:
		return backendV
	default:
		return baseV
	}
}

func mergeStatus(base, local, backend Task) Status {
	return resolve3("status", base.Status, local.Status, backend.Status)
}

func mergeVersion(base, local, backend Task) int {
	return resolve3("version", base.Version, local.Version, backend.Version)
}

func mergeWriteScope(base, local, backend Task) []string {
	return resolve3Slice("writeScope", base.WriteScope, local.WriteScope, backend.WriteScope)
}

func mergeDependsOn(base, local, backend Task) []string {
	return resolve3Slice("dependsOn", base.DependsOn, local.DependsOn, backend.DependsOn)
}

func mergeNotes(base, local, backend Task) string {
	return resolve3("notes", base.Notes, local.Notes, backend.Notes)
}

// mergeLease is owned by the backend: the lease/transition plane is
// authoritative, so a backend lease change always wins, otherwise keep local.
func mergeLease(base, local, backend Task) Lease {
	if backend.Lease != base.Lease {
		return backend.Lease
	}
	return local.Lease
}
