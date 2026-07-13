package sdk

import (
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// gateStubAdapter is a minimal registry.Adapter used only to seed a real
// registry.Registry with a name + migration_only flag, so the tests below
// exercise the actual §11.2 rule (registry.Registry.ValidateReadsFrom)
// through SDK.MaterializeView, rather than a hand-rolled fake gate.
type gateStubAdapter struct {
	name          string
	migrationOnly bool
}

func (a gateStubAdapter) Name() string { return a.name }

func (a gateStubAdapter) Schema() registry.Schema {
	return registry.Schema{Name: a.name, Version: "1.0.0", MigrationOnly: a.migrationOnly}
}

func (a gateStubAdapter) ImpactRadius(req registry.ImpactRequest) (registry.ImpactResult, error) {
	return registry.ImpactResult{IDs: req.ChangedIDs}, nil
}

// TestMaterializeViewRejectsMigrationOnlyReadsFrom is the integration test t9
// requires: a view whose reads_from names a migration_only adapter must be
// rejected by SDK.MaterializeView itself (the runtime materialize_view path),
// not just by registry.EnforceReadsFrom at adapter-load time (t4). Before
// this task, MaterializeView never consulted the registry at all, so this
// reads_from bypassed the §11.2 gate entirely (Codex review of t4, PR #172).
func TestMaterializeViewRejectsMigrationOnlyReadsFrom(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(gateStubAdapter{name: "crg-bridge", migrationOnly: true}); err != nil {
		t.Fatalf("register crg-bridge: %v", err)
	}

	store := NewMemStore()
	if err := store.WriteNotes(BootstrapToken("crg-bridge"), "crg-bridge", []Note{
		{ID: "f1", Type: "function"},
	}); err != nil {
		t.Fatalf("seed crg-bridge: %v", err)
	}

	ranRunner := false
	s := For("compliance", store)
	err := s.MaterializeView("dep_view", []string{"crg-bridge"}, reg, func(read func(string) ([]Note, error)) ([]Note, error) {
		ranRunner = true
		return read("crg-bridge")
	})
	if err == nil {
		t.Fatal("MaterializeView must reject a view whose reads_from names a migration_only adapter (spec §11.2)")
	}
	if !strings.Contains(err.Error(), "migration_only") {
		t.Fatalf("error should name the §11.2 rule: %v", err)
	}
	if ranRunner {
		t.Fatal("the gate must reject BEFORE the runner executes — no cross-namespace read on a rejected view")
	}
	if got := store.Namespaces(); len(got) != 1 || got[0] != "crg-bridge" {
		t.Fatalf("a rejected view must not write anything to the adapter namespace; store.Namespaces() = %v", got)
	}
}

// TestMaterializeViewAcceptsLegitimateReadsFrom proves the gate is not
// overzealous: a view reading a registered, non-migration_only adapter still
// succeeds end-to-end (runner executes, output persists and reads back)
// through the real registry gate.
func TestMaterializeViewAcceptsLegitimateReadsFrom(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(gateStubAdapter{name: "crg", migrationOnly: false}); err != nil {
		t.Fatalf("register crg: %v", err)
	}

	store := NewMemStore()
	if err := store.WriteNotes(BootstrapToken("crg"), "crg", []Note{
		{ID: "f1", Type: "function"},
	}); err != nil {
		t.Fatalf("seed crg: %v", err)
	}

	s := For("compliance", store)
	err := s.MaterializeView("dep_view", []string{"crg"}, reg, func(read func(string) ([]Note, error)) ([]Note, error) {
		dep, err := read("crg")
		if err != nil {
			return nil, err
		}
		out := make([]Note, 0, len(dep))
		for _, n := range dep {
			out = append(out, Note{ID: "mirror-" + n.ID, Type: "mirror"})
		}
		return out, nil
	})
	if err != nil {
		t.Fatalf("MaterializeView with a legitimate reads_from must succeed: %v", err)
	}

	rows, err := s.Query(func(v NamespaceView) []Row {
		var out []Row
		for _, n := range v.NotesByType("mirror") {
			out = append(out, Row{"id": n.ID})
		}
		return out
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "mirror-f1" {
		t.Fatalf("view output = %v, want mirror-f1", rows)
	}
}

// TestMaterializeViewRequiresGate proves MaterializeView fails closed rather
// than silently skipping the §11.2 check when no gate is supplied — the exact
// failure mode this task closes (an omitted registry consult must be a
// compile-time-visible, loudly-rejected call, not a quiet no-op).
func TestMaterializeViewRequiresGate(t *testing.T) {
	s := For("compliance", NewMemStore())
	err := s.MaterializeView("v", []string{"crg"}, nil, func(func(string) ([]Note, error)) ([]Note, error) {
		t.Fatal("runner must not execute when no gate is configured")
		return nil, nil
	})
	if err == nil {
		t.Fatal("MaterializeView must reject a nil ReadsFromValidator rather than skip the reads_from check")
	}
}

// registryValidatingStore is a Store that is also registry-aware: it embeds an
// in-memory store for the note/edge plumbing and delegates the §11.2 reads_from
// consult to a live registry. It models the production gcc-backed store that
// wraps the registry, so sdk.For auto-binds the gate and callers need not
// thread it through every MaterializeView call.
type registryValidatingStore struct {
	*MemStore
	reg *registry.Registry
}

func (s registryValidatingStore) ValidateReadsFrom(dependent string, readsFrom []string) error {
	return s.reg.ValidateReadsFrom(dependent, readsFrom)
}

// TestMaterializeViewAutoBindsStoreValidator proves sdk.For auto-binds the
// reads_from gate when the store is registry-aware: MaterializeView called with
// a nil explicit gate still enforces §11.2 through the store-provided validator
// — rejecting a migration_only dependency, admitting a clean one, and never
// silently succeeding on a non-empty reads_from (the mutation-verify: drop the
// auto-bound consult and the migration_only view would slip straight through).
func TestMaterializeViewAutoBindsStoreValidator(t *testing.T) {
	reg := registry.New()
	if err := reg.Register(gateStubAdapter{name: "crg-bridge", migrationOnly: true}); err != nil {
		t.Fatalf("register crg-bridge: %v", err)
	}
	if err := reg.Register(gateStubAdapter{name: "crg", migrationOnly: false}); err != nil {
		t.Fatalf("register crg: %v", err)
	}

	store := registryValidatingStore{MemStore: NewMemStore(), reg: reg}
	if err := store.WriteNotes(BootstrapToken("crg"), "crg", []Note{
		{ID: "f1", Type: "function"},
	}); err != nil {
		t.Fatalf("seed crg: %v", err)
	}

	// No explicit gate: the SDK must use the validator For auto-bound from the
	// registry-aware store.
	s := For("compliance", store)

	// Migration-only dependency: rejected through the auto-bound gate.
	ranRunner := false
	err := s.MaterializeView("dep_view", []string{"crg-bridge"}, nil, func(read func(string) ([]Note, error)) ([]Note, error) {
		ranRunner = true
		return read("crg-bridge")
	})
	if err == nil {
		t.Fatal("auto-bound gate must reject a view whose reads_from names a migration_only adapter (spec §11.2)")
	}
	if !strings.Contains(err.Error(), "migration_only") {
		t.Fatalf("error should name the §11.2 rule: %v", err)
	}
	if ranRunner {
		t.Fatal("the auto-bound gate must reject BEFORE the runner executes")
	}

	assertCleanDependencyAdmitted(t, s)
}

// assertCleanDependencyAdmitted runs the "clean dependency" half of
// TestMaterializeViewAutoBindsStoreValidator: a non-migration_only reads_from
// must be admitted through the same auto-bound gate, with output persisting
// and reading back correctly.
func assertCleanDependencyAdmitted(t *testing.T, s *SDK) {
	t.Helper()
	err := s.MaterializeView("ok_view", []string{"crg"}, nil, func(read func(string) ([]Note, error)) ([]Note, error) {
		dep, err := read("crg")
		if err != nil {
			return nil, err
		}
		out := make([]Note, 0, len(dep))
		for _, n := range dep {
			out = append(out, Note{ID: "mirror-" + n.ID, Type: "mirror"})
		}
		return out, nil
	})
	if err != nil {
		t.Fatalf("auto-bound gate must admit a clean reads_from: %v", err)
	}

	rows, err := s.Query(func(v NamespaceView) []Row {
		var out []Row
		for _, n := range v.NotesByType("mirror") {
			out = append(out, Row{"id": n.ID})
		}
		return out
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 || rows[0]["id"] != "mirror-f1" {
		t.Fatalf("view output = %v, want mirror-f1", rows)
	}
}
