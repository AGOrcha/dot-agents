package config

import (
	"strings"
	"testing"
)

// unit_kinds_test.go covers the §15.9 items 7-8 kind guards: project-set is a
// synced/projectable unit; descriptor is recognized but internal-only (fail-
// closed) until source-shipped.

func TestKnownUnitKind(t *testing.T) {
	for _, k := range []string{UnitKindLayer, UnitKindArtifact, UnitKindProjectSet, UnitKindDescriptor} {
		if !KnownUnitKind(k) {
			t.Errorf("%q must be a known kind", k)
		}
	}
	if KnownUnitKind("bogus") {
		t.Error("unknown kind must be rejected")
	}
}

func TestIsProjectableKind(t *testing.T) {
	cases := map[string]bool{
		UnitKindLayer:      true,
		UnitKindArtifact:   true,
		UnitKindProjectSet: true,
		UnitKindDescriptor: false, // internal-only until source-shipped
		"bogus":            false,
	}
	for k, want := range cases {
		if got := IsProjectableKind(k); got != want {
			t.Errorf("IsProjectableKind(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestIsSyncedUnitKind(t *testing.T) {
	if !IsSyncedUnitKind(UnitKindProjectSet) {
		t.Error("project-set is synced portable identity")
	}
	if IsSyncedUnitKind(UnitKindDescriptor) {
		t.Error("descriptor is not synced until source-shipped")
	}
	if IsSyncedUnitKind("binding-table") {
		t.Error("the machine-local binding table is never a synced unit")
	}
}

func TestValidateUnitKind(t *testing.T) {
	if err := ValidateUnitKind(UnitKindLayer); err != nil {
		t.Errorf("layer must validate, got %v", err)
	}
	if err := ValidateUnitKind(UnitKindProjectSet); err != nil {
		t.Errorf("project-set must validate, got %v", err)
	}
	err := ValidateUnitKind(UnitKindDescriptor)
	if err == nil || !strings.Contains(err.Error(), "internal-only") {
		t.Errorf("descriptor must fail closed as internal-only, got %v", err)
	}
	if err := ValidateUnitKind("bogus"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("unknown kind must error, got %v", err)
	}
}
