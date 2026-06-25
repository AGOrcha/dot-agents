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

func TestValidateReadsFrom_UnknownDepIgnored(t *testing.T) {
	reg := New()
	mustRegister(t, reg, migAdapter{name: compName, version: "1.0.0"})
	if err := reg.ValidateReadsFrom(compName, []string{"nope"}); err != nil {
		t.Fatalf("unknown dep is a separate concern, should not error here: %v", err)
	}
}

func mustRegister(t *testing.T, reg *Registry, a Adapter) {
	t.Helper()
	if err := reg.Register(a); err != nil {
		t.Fatalf("register %s: %v", a.Name(), err)
	}
}
