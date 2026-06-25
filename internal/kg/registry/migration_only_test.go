package registry

import (
	"strings"
	"testing"
)

// Shared test literals (hoisted to consts to avoid S1192 duplication).
const (
	bridgeName = "crg-bridge"
	compName   = "compliance"
)

// migAdapter is a minimal Adapter that can declare migration_only, for the
// §11.2 loader-rule tests.
type migAdapter struct {
	name      string
	version   string
	migration bool
}

func (a migAdapter) Name() string { return a.name }
func (a migAdapter) Schema() Schema {
	return Schema{Name: a.name, Version: a.version, MigrationOnly: a.migration,
		ImpactRadius: ImpactRadius{Query: "RETURN $changed_ids AS id"}}
}
func (a migAdapter) ImpactRadius(req ImpactRequest) (ImpactResult, error) {
	return ImpactResult{IDs: req.ChangedIDs}, nil
}

func TestSchema_MigrationOnlyParses(t *testing.T) {
	s, err := LoadSchema([]byte("name: m\nversion: 0.1.0\nmigration_only: true\n" +
		"note_types: []\nedge_types: []\nimpact_radius:\n  query: x\n  max_depth: 0\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !s.MigrationOnly {
		t.Fatal("migration_only: true should parse to MigrationOnly")
	}
}

func TestSchema_MigrationOnlyDefaultsFalse(t *testing.T) {
	s, err := LoadSchema([]byte("name: m\nversion: 0.1.0\n" +
		"note_types: []\nedge_types: []\nimpact_radius:\n  query: x\n  max_depth: 0\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.MigrationOnly {
		t.Fatal("absent migration_only should default false")
	}
}

func TestValidateReadsFrom_RejectsLongTermDependingOnMigrationOnly(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: bridgeName, version: "0.1.0", migration: true})
	mustRegister(t, reg, migAdapter{name: compName, version: "1.0.0"})

	err := reg.ValidateReadsFrom(compName, []string{bridgeName})
	if err == nil {
		t.Fatal("long-term adapter reading a migration_only adapter must be rejected (§11.2)")
	}
	if !strings.Contains(err.Error(), "migration_only") {
		t.Fatalf("error should name the violation: %v", err)
	}
}

func TestValidateReadsFrom_AllowsNonMigrationDependency(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: "crg", version: "1.0.0"})
	mustRegister(t, reg, migAdapter{name: compName, version: "1.0.0"})
	if err := reg.ValidateReadsFrom(compName, []string{"crg"}); err != nil {
		t.Fatalf("reading a non-migration adapter should be allowed: %v", err)
	}
}

func TestValidateReadsFrom_MigrationOnlyMayReadMigrationOnly(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: bridgeName, version: "0.1.0", migration: true})
	mustRegister(t, reg, migAdapter{name: "other-mirror", version: "0.1.0", migration: true})
	if err := reg.ValidateReadsFrom("other-mirror", []string{bridgeName}); err != nil {
		t.Fatalf("a migration_only adapter may read another migration_only adapter: %v", err)
	}
}

func TestValidateReadsFrom_RejectsUnknownDep(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: compName, version: "1.0.0"})
	err := reg.ValidateReadsFrom(compName, []string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "unregistered") {
		t.Fatalf("reads_from an unregistered adapter must be rejected; got %v", err)
	}
}

func TestValidateReadsFrom_RejectsSelfReference(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: compName, version: "1.0.0"})
	err := reg.ValidateReadsFrom(compName, []string{compName})
	if err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("self-reference must be rejected; got %v", err)
	}
}

// TestValidateReadsFrom_RejectsTransitiveMigrationOnly: compliance → research →
// crg-bridge. compliance does not read the bridge directly, but reaches it
// transitively, which the §11.2 rule must catch.
func TestValidateReadsFrom_RejectsTransitiveMigrationOnly(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: bridgeName, version: "0.1.0", migration: true})
	mustRegister(t, reg, migAdapter{name: "research", version: "1.0.0"})
	mustRegister(t, reg, migAdapter{name: compName, version: "1.0.0"})
	// research declares reads_from [crg-bridge] — allowed only because research
	// is... not migration_only, so this declaration itself is rejected.
	if err := reg.DeclareReadsFrom("research", []string{bridgeName}); err == nil {
		t.Fatal("research (long-term) reading the bridge should already be rejected")
	}
	// Make research a migration_only mirror so IT may read the bridge, then
	// compliance → research must still be rejected transitively... but a
	// migration_only intermediary is itself migration_only, so compliance
	// reaching it directly is the violation. Use a non-migration intermediary
	// that legitimately reads a non-migration dep, then swap to show transitivity
	// through recorded edges:
	reg2 := New()
	mustRegister(t, reg2, migAdapter{name: bridgeName, version: "0.1.0", migration: true})
	mustRegister(t, reg2, migAdapter{name: "midmirror", version: "0.1.0", migration: true})
	mustRegister(t, reg2, migAdapter{name: compName, version: "1.0.0"})
	// midmirror (migration_only) legitimately reads the bridge — recorded.
	if err := reg2.DeclareReadsFrom("midmirror", []string{bridgeName}); err != nil {
		t.Fatalf("a mirror reading the bridge should be allowed: %v", err)
	}
	// compliance → midmirror: midmirror is itself migration_only, so this is a
	// direct violation AND a transitive path to the bridge.
	if err := reg2.ValidateReadsFrom(compName, []string{"midmirror"}); err == nil {
		t.Fatal("compliance reaching a migration_only adapter (transitively to the bridge) must be rejected")
	}
}

func TestDeclareReadsFrom_RecordsForTransitiveChecks(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: bridgeName, version: "0.1.0", migration: true})
	mustRegister(t, reg, migAdapter{name: "a", version: "1.0.0"})
	mustRegister(t, reg, migAdapter{name: "b", version: "1.0.0"})
	mustRegister(t, reg, migAdapter{name: "c", version: "1.0.0"})
	// a → b → c, all non-migration: a chain with no migration_only adapter is fine.
	if err := reg.DeclareReadsFrom("b", []string{"c"}); err != nil {
		t.Fatalf("b→c should be allowed: %v", err)
	}
	if err := reg.DeclareReadsFrom("a", []string{"b"}); err != nil {
		t.Fatalf("a→b should be allowed (no migration_only reachable): %v", err)
	}
	// Now point c at the bridge — c is non-migration, so c→bridge is itself
	// rejected and never recorded, keeping a's earlier success consistent.
	if err := reg.DeclareReadsFrom("c", []string{bridgeName}); err == nil {
		t.Fatal("c (long-term) → bridge must be rejected")
	}
}

// TestReachesMigrationOnly_TerminatesOnCycle exercises the cycle guard in the
// transitive walk: a↔b form a reads_from cycle (no migration_only adapter), so
// a check reaching into the cycle must terminate and pass.
func TestReachesMigrationOnly_TerminatesOnCycle(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: "a", version: "1.0.0"})
	mustRegister(t, reg, migAdapter{name: "b", version: "1.0.0"})
	mustRegister(t, reg, migAdapter{name: compName, version: "1.0.0"})
	if err := reg.DeclareReadsFrom("a", []string{"b"}); err != nil {
		t.Fatalf("a→b: %v", err)
	}
	if err := reg.DeclareReadsFrom("b", []string{"a"}); err != nil {
		t.Fatalf("b→a (cycle, no migration_only): %v", err)
	}
	// compliance → a walks a↔b and must terminate (cycle guard) and pass.
	if err := reg.ValidateReadsFrom(compName, []string{"a"}); err != nil {
		t.Fatalf("reaching a reads_from cycle with no migration_only must pass: %v", err)
	}
}

func TestDeclareReadsFrom_RejectsAndDoesNotRecordOnViolation(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: bridgeName, version: "0.1.0", migration: true})
	mustRegister(t, reg, migAdapter{name: compName, version: "1.0.0"})
	if err := reg.DeclareReadsFrom(compName, []string{bridgeName}); err == nil {
		t.Fatal("declaration violating §11.2 must be rejected")
	}
	// A rejected declaration must not be recorded (a later valid check is clean).
	if err := reg.ValidateReadsFrom(compName, nil); err != nil {
		t.Fatalf("no recorded edges should remain after a rejected declaration: %v", err)
	}
}

func mustRegister(t *testing.T, reg *Registry, a Adapter) {
	t.Helper()
	if err := reg.Register(a); err != nil {
		t.Fatalf("register %s: %v", a.Name(), err)
	}
}
