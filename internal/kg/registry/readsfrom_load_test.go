package registry

import (
	"strings"
	"testing"
)

// schemaWithView builds an adapter schema YAML that declares one materialized
// view reading from the given dependencies. This is the on-disk form a loader
// parses, so the test exercises the real LoadSchema → register → EnforceReadsFrom
// path rather than constructing Schema values by hand.
func schemaWithView(name, version string, migration bool, readsFrom []string) string {
	var b strings.Builder
	b.WriteString("name: " + name + "\nversion: " + version + "\n")
	if migration {
		b.WriteString("migration_only: true\n")
	}
	b.WriteString("note_types: []\nedge_types: []\n")
	if len(readsFrom) > 0 {
		b.WriteString("materialized_views:\n  - name: v\n    reads_from: [" + strings.Join(readsFrom, ", ") + "]\n")
	}
	b.WriteString("impact_radius:\n  query: RETURN $changed_ids AS id\n  max_depth: 0\n")
	return b.String()
}

// loadAndRegister parses a schema from YAML (the real loader) and registers it.
func loadAndRegister(t *testing.T, reg *Registry, yaml string) {
	t.Helper()
	s, err := LoadSchema([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadSchema(%s): %v", yaml, err)
	}
	if err := reg.Register(schemaOnlyAdapter{s}); err != nil {
		t.Fatalf("register %s: %v", s.Name, err)
	}
}

// schemaOnlyAdapter wraps a loaded Schema as an Adapter so a parsed schema can
// be registered like a built-in.
type schemaOnlyAdapter struct{ s Schema }

func (a schemaOnlyAdapter) Name() string   { return a.s.Name }
func (a schemaOnlyAdapter) Schema() Schema { return a.s }
func (a schemaOnlyAdapter) ImpactRadius(req ImpactRequest) (ImpactResult, error) {
	return ImpactResult{IDs: req.ChangedIDs}, nil
}

// TestEnforceReadsFrom_LoadRejectsMigrationOnlyDependency is the integration
// test the gate needed: an adapter loaded with reads_from a migration_only
// adapter must FAIL enforcement (the load is rejected). A helper that no load
// path invokes provides no protection — this proves the path invokes it.
func TestEnforceReadsFrom_LoadRejectsMigrationOnlyDependency(t *testing.T) {
	reg := New()
	loadAndRegister(t, reg, schemaWithView("crg-bridge", "0.1.0", true, nil))
	loadAndRegister(t, reg, schemaWithView("compliance", "1.0.0", false, []string{"crg-bridge"}))

	err := reg.EnforceReadsFrom()
	if err == nil {
		t.Fatal("EnforceReadsFrom must reject a long-term adapter reading a migration_only adapter (§11.2)")
	}
	if !strings.Contains(err.Error(), "migration_only") || !strings.Contains(err.Error(), "compliance") {
		t.Fatalf("error should name the violating adapter + rule: %v", err)
	}
}

// TestEnforceReadsFrom_LoadAcceptsCleanGraph proves enforcement passes when no
// long-term adapter reads a migration_only one.
func TestEnforceReadsFrom_LoadAcceptsCleanGraph(t *testing.T) {
	reg := New()
	loadAndRegister(t, reg, schemaWithView("crg", "1.0.0", false, nil))
	loadAndRegister(t, reg, schemaWithView("compliance", "1.0.0", false, []string{"crg"}))
	if err := reg.EnforceReadsFrom(); err != nil {
		t.Fatalf("clean reads_from graph must pass enforcement: %v", err)
	}
}

// TestEnforceReadsFrom_LoadRejectsTransitive: compliance → midmirror →
// crg-bridge. compliance doesn't read the bridge directly, but enforcement must
// reject it transitively regardless of adapter sweep order.
func TestEnforceReadsFrom_LoadRejectsTransitive(t *testing.T) {
	reg := New()
	loadAndRegister(t, reg, schemaWithView("crg-bridge", "0.1.0", true, nil))
	loadAndRegister(t, reg, schemaWithView("midmirror", "0.1.0", true, []string{"crg-bridge"}))
	loadAndRegister(t, reg, schemaWithView("compliance", "1.0.0", false, []string{"midmirror"}))
	if err := reg.EnforceReadsFrom(); err == nil {
		t.Fatal("EnforceReadsFrom must reject a transitive migration_only dependency")
	}
}

// TestEnforceReadsFrom_LoadRejectsUnknownDep proves the structural edge check
// fires during enforcement.
func TestEnforceReadsFrom_LoadRejectsUnknownDep(t *testing.T) {
	reg := New()
	loadAndRegister(t, reg, schemaWithView("compliance", "1.0.0", false, []string{"ghost"}))
	err := reg.EnforceReadsFrom()
	if err == nil || !strings.Contains(err.Error(), "unregistered") {
		t.Fatalf("enforcement must reject reads_from an unregistered adapter; got %v", err)
	}
}

// TestEnforceReadsFrom_LoadRejectsSelfReference proves self-reference is caught.
func TestEnforceReadsFrom_LoadRejectsSelfReference(t *testing.T) {
	reg := New()
	loadAndRegister(t, reg, schemaWithView("compliance", "1.0.0", false, []string{"compliance"}))
	err := reg.EnforceReadsFrom()
	if err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("enforcement must reject a self-reference; got %v", err)
	}
}

// TestEnforceReadsFrom_NoViews is the trivial pass (no reads_from anywhere).
func TestEnforceReadsFrom_NoViews(t *testing.T) {
	reg := New()
	loadAndRegister(t, reg, schemaWithView("none", "1.0.0", false, nil))
	if err := reg.EnforceReadsFrom(); err != nil {
		t.Fatalf("a registry with no reads_from must pass: %v", err)
	}
}

// TestEnforceReadsFrom_MirrorReadingMirrorPasses exercises the pass-2 skip for a
// migration_only adapter: a mirror reading another mirror is allowed, so
// enforcement passes (and the migration_only adapter is skipped, not rejected).
func TestEnforceReadsFrom_MirrorReadingMirrorPasses(t *testing.T) {
	reg := New()
	loadAndRegister(t, reg, schemaWithView("crg-bridge", "0.1.0", true, nil))
	loadAndRegister(t, reg, schemaWithView("zz-mirror", "0.1.0", true, []string{"crg-bridge"}))
	if err := reg.EnforceReadsFrom(); err != nil {
		t.Fatalf("a migration_only mirror reading another mirror must pass: %v", err)
	}
}

// TestSchemaReadsFrom_UnionDedup proves Schema.ReadsFrom unions + dedups across
// views.
func TestSchemaReadsFrom_UnionDedup(t *testing.T) {
	s := Schema{MaterializedViews: []MaterializedView{
		{Name: "v1", ReadsFrom: []string{"crg", "crg-bridge"}},
		{Name: "v2", ReadsFrom: []string{"crg"}}, // dup crg
	}}
	got := s.ReadsFrom()
	if len(got) != 2 {
		t.Fatalf("ReadsFrom = %v, want 2 de-duped entries", got)
	}
}
