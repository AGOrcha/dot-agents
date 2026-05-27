package cmdutil

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// rrMicro records the per-verb runner invocations the cobra RunE
// dispatcher exercises after SpecForResource assembly. Mirrors the
// runRecorder in canonical_resource_cmd_test.go but local to this file
// so the builder-focused tests do not depend on that sampleSpec fixture.
type rrMicro struct {
	listScope  string
	showScope  string
	showName   string
	rmScope    string
	rmName     string
	listCalled bool
	showCalled bool
	rmCalled   bool
}

// defForTest constructs a fully-populated CanonicalResourceDef the
// SpecForResource tests reuse. Includes a non-nil MissingDirHint so the
// builder-passes-through path is asserted (the SettingsResource live
// instance leaves it nil; the MCP/Rules instances do not, so both
// branches need coverage).
func defForTest() CanonicalResourceDef {
	return CanonicalResourceDef{
		Kind:           "Demo",
		DirSegment:     "demo",
		SingularRem:    "demo file",
		EnsureScope:    func(_, _, _ string) error { return nil },
		EmptyHint:      func(s string) string { return "empty:" + s },
		MissingDirHint: func(s string) string { return "missing:" + s },
		Use:            "demo",
		Short:          "demo short",
		Long:           "demo long",
		Examples:       []string{"  da demo list", "  da demo list scope"},
		ListShort:      "list demo files",
		ListExamples:   []string{"  da demo list", "  da demo list billing"},
		ListArgsHint:   "list hint",
		ShowShort:      "show one demo file",
		ShowArgsHint:   "show hint",
		RemoveShort:    "remove a demo file",
		RemoveLong:     "remove long body",
		RemoveArgsHint: "remove hint",
	}
}

// TestSpecForResource_CopiesIdentityFields asserts the static-def fields
// (Kind/DirSegment/SingularRem/EnsureScope/EmptyHint/MissingDirHint)
// flow through the builder unchanged. These are the data-layer fields
// RunCanonical{List,Show,Remove} consume — any drift here silently
// breaks the parity-test contract across mcp/settings/rules.
func TestSpecForResource_CopiesIdentityFields(t *testing.T) {
	def := defForTest()
	spec := SpecForResource(def, ResourceRunners{}, nil, nil, nil)

	if spec.Kind != def.Kind || spec.DirSegment != def.DirSegment || spec.SingularRem != def.SingularRem {
		t.Errorf("identity drift: %+v", spec)
	}
	if spec.EnsureScope == nil {
		t.Error("EnsureScope not propagated")
	}
	if got := spec.EmptyHint("s"); got != "empty:s" {
		t.Errorf("EmptyHint = %q", got)
	}
	if got := spec.MissingDirHint("s"); got != "missing:s" {
		t.Errorf("MissingDirHint = %q", got)
	}
}

// TestSpecForResource_CopiesParentCLIFields asserts Use/Short/Long are
// copied verbatim and Examples joins with "\n" (matching the
// CanonicalCmdExampleBlock contract leaves used to call directly).
func TestSpecForResource_CopiesParentCLIFields(t *testing.T) {
	def := defForTest()
	spec := SpecForResource(def, ResourceRunners{}, nil, nil, nil)

	if spec.Use != def.Use || spec.Short != def.Short || spec.Long != def.Long {
		t.Errorf("parent CLI fields drift: Use=%q Short=%q Long=%q", spec.Use, spec.Short, spec.Long)
	}
	wantEx := strings.Join(def.Examples, "\n")
	if spec.Example != wantEx {
		t.Errorf("Example = %q, want %q", spec.Example, wantEx)
	}
}

// TestSpecForResource_BuildsSubCmdStrings asserts the three SubCmdStrings
// triplets are assembled with the canonical Use shape ("list [scope]",
// "show <scope> <name>", "remove <scope> <name>") + the def's per-verb
// Short/Long. RemoveSub.Long must propagate (mcp/rules carry it, the
// generic builder must too).
func TestSpecForResource_BuildsSubCmdStrings(t *testing.T) {
	def := defForTest()
	spec := SpecForResource(def, ResourceRunners{}, nil, nil, nil)

	if spec.ListSub.Use != "list [scope]" || spec.ListSub.Short != def.ListShort {
		t.Errorf("ListSub = %+v", spec.ListSub)
	}
	wantListEx := strings.Join(def.ListExamples, "\n")
	if spec.ListSub.Example != wantListEx {
		t.Errorf("ListSub.Example = %q, want %q", spec.ListSub.Example, wantListEx)
	}
	if spec.ShowSub.Use != "show <scope> <name>" || spec.ShowSub.Short != def.ShowShort {
		t.Errorf("ShowSub = %+v", spec.ShowSub)
	}
	if spec.RemoveSub.Use != "remove <scope> <name>" || spec.RemoveSub.Short != def.RemoveShort {
		t.Errorf("RemoveSub = %+v", spec.RemoveSub)
	}
	if spec.RemoveSub.Long != def.RemoveLong {
		t.Errorf("RemoveSub.Long not propagated: %q", spec.RemoveSub.Long)
	}
}

// TestSpecForResource_PassesThroughArgsValidators asserts the three
// per-verb cobra.PositionalArgs validators are stored on the spec
// unchanged — the leaf binds them from Deps.MaxArgsWithHints (mcp) or
// Deps.MaximumNArgsWithHints (settings/rules) so the builder must not
// substitute its own.
func TestSpecForResource_PassesThroughArgsValidators(t *testing.T) {
	listV := func(_ *cobra.Command, _ []string) error { return errors.New("list-validator") }
	showV := func(_ *cobra.Command, _ []string) error { return errors.New("show-validator") }
	rmV := func(_ *cobra.Command, _ []string) error { return errors.New("remove-validator") }

	spec := SpecForResource(defForTest(), ResourceRunners{}, listV, showV, rmV)

	if err := spec.ListArgs(nil, nil); err == nil || err.Error() != "list-validator" {
		t.Errorf("ListArgs not propagated: %v", err)
	}
	if err := spec.ShowArgs(nil, nil); err == nil || err.Error() != "show-validator" {
		t.Errorf("ShowArgs not propagated: %v", err)
	}
	if err := spec.RemoveArgs(nil, nil); err == nil || err.Error() != "remove-validator" {
		t.Errorf("RemoveArgs not propagated: %v", err)
	}
}

// TestSpecForResource_WiresRunners asserts the per-verb runners on
// ResourceRunners become the spec's ListRun/ShowRun/RemoveRun closures.
// We invoke them directly (not via cobra) because the cobra dispatch is
// exercised by canonical_resource_cmd_test.go — here we only assert the
// builder's field-by-field wiring did not drop or swap runners.
func TestSpecForResource_WiresRunners(t *testing.T) {
	r := &rrMicro{}
	runners := ResourceRunners{
		List:    func(_, _ string) ([]CanonicalFileEntry, error) { return nil, nil },
		Resolve: func(_, _, _ string) (CanonicalFileEntry, error) { return CanonicalFileEntry{}, nil },
		ListRun: func(scope string) error { r.listCalled = true; r.listScope = scope; return nil },
		ShowRun: func(scope, name string) error {
			r.showCalled = true
			r.showScope = scope
			r.showName = name
			return nil
		},
		RemoveRun: func(scope, name string) error { r.rmCalled = true; r.rmScope = scope; r.rmName = name; return nil },
	}
	spec := SpecForResource(defForTest(), runners, nil, nil, nil)

	if spec.List == nil || spec.Resolve == nil {
		t.Fatal("data-layer runners not propagated")
	}
	if err := spec.ListRun("global"); err != nil || !r.listCalled || r.listScope != "global" {
		t.Errorf("ListRun: called=%v scope=%q err=%v", r.listCalled, r.listScope, err)
	}
	if err := spec.ShowRun("g", "n"); err != nil || !r.showCalled || r.showScope != "g" || r.showName != "n" {
		t.Errorf("ShowRun: %+v err=%v", r, err)
	}
	if err := spec.RemoveRun("g", "x"); err != nil || !r.rmCalled || r.rmScope != "g" || r.rmName != "x" {
		t.Errorf("RemoveRun: %+v err=%v", r, err)
	}
}

// TestSpecForResource_NilMissingDirHint asserts the spec carries a nil
// MissingDirHint when the def leaves it nil — matching the live
// SettingsResource which intentionally omits the override so
// RunCanonicalList emits the generic fallback message. Without this
// assertion the missingDirMessage(...) fallback branch in canonfile.go
// is not protected against silent drift.
func TestSpecForResource_NilMissingDirHint(t *testing.T) {
	def := defForTest()
	def.MissingDirHint = nil
	spec := SpecForResource(def, ResourceRunners{}, nil, nil, nil)
	if spec.MissingDirHint != nil {
		t.Errorf("MissingDirHint should remain nil, got %v", spec.MissingDirHint("s"))
	}
}

// TestEntriesFromSpecs_ProjectsEachElement asserts the generic projector
// loops over all input specs and applies the projection function once
// per element. Mirrors the per-leaf list-callback path that used to
// inline this loop.
func TestEntriesFromSpecs_ProjectsEachElement(t *testing.T) {
	type fake struct {
		Scope    string
		BaseName string
		Source   string
	}
	in := []fake{
		{"global", "a.json", "/p/a.json"},
		{"proj", "b.json", "/p/b.json"},
		{"proj", "c.json", "/p/c.json"},
	}
	got := EntriesFromSpecs(in, func(f fake) CanonicalFileEntry {
		return CanonicalFileEntry{Scope: f.Scope, BaseName: f.BaseName, SourcePath: f.Source}
	})
	if len(got) != len(in) {
		t.Fatalf("len = %d, want %d", len(got), len(in))
	}
	for i, f := range in {
		if got[i].Scope != f.Scope || got[i].BaseName != f.BaseName || got[i].SourcePath != f.Source {
			t.Errorf("entry[%d] = %+v, want from %+v", i, got[i], f)
		}
	}
}

// TestEntriesFromSpecs_EmptyInput asserts the projector returns a
// non-nil, zero-length slice for empty input — preserving the
// make([]T, 0) shape so downstream code can range over the result
// without nil-guard branches.
func TestEntriesFromSpecs_EmptyInput(t *testing.T) {
	got := EntriesFromSpecs([]int(nil), func(int) CanonicalFileEntry { return CanonicalFileEntry{} })
	if got == nil {
		t.Error("expected non-nil empty slice for nil input")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestMCPResource_StaticShape asserts the live MCPResource var has the
// fields the mcp leaf expects (Kind/DirSegment/EnsureScope non-nil +
// MissingDirHint present) and the EmptyHint / MissingDirHint closures
// produce the exact pre-refactor message bodies — these are the strings
// the user sees from `da mcp list <scope>` on an empty or missing dir.
// Pinning them here is what protects observable CLI text against silent
// edits to resources.go.
func TestMCPResource_StaticShape(t *testing.T) {
	if MCPResource.Kind != "MCP" || MCPResource.DirSegment != "mcp" {
		t.Errorf("MCPResource identity drift: %+v", MCPResource)
	}
	if MCPResource.EnsureScope == nil || MCPResource.EmptyHint == nil || MCPResource.MissingDirHint == nil {
		t.Error("MCPResource callbacks must be non-nil")
	}
	wantEmpty := "No MCP config files (.json/.yaml/.yml/.toml) under ~/.agents/mcp/global/"
	if got := MCPResource.EmptyHint("global"); got != wantEmpty {
		t.Errorf("MCPResource.EmptyHint = %q, want %q", got, wantEmpty)
	}
	wantMissing := "No ~/.agents/mcp/global/ directory yet (no canonical MCP files for this scope)."
	if got := MCPResource.MissingDirHint("global"); got != wantMissing {
		t.Errorf("MCPResource.MissingDirHint = %q, want %q", got, wantMissing)
	}
}

// TestSettingsResource_NilMissingDirHint pins the intentional omission:
// SettingsResource leaves MissingDirHint nil so RunCanonicalList falls
// back to the generic message format. Changing this would silently flip
// CLI output for the settings family. Also asserts the EmptyHint body
// matches the pre-refactor settings/list.go message verbatim.
func TestSettingsResource_NilMissingDirHint(t *testing.T) {
	if SettingsResource.MissingDirHint != nil {
		t.Error("SettingsResource.MissingDirHint must remain nil — falls back to generic message")
	}
	if SettingsResource.Kind != "Settings" || SettingsResource.DirSegment != "settings" {
		t.Errorf("SettingsResource identity drift: %+v", SettingsResource)
	}
	wantEmpty := "No settings files under ~/.agents/settings/proj/"
	if got := SettingsResource.EmptyHint("proj"); got != wantEmpty {
		t.Errorf("SettingsResource.EmptyHint = %q, want %q", got, wantEmpty)
	}
}

// TestRulesResource_StaticShape asserts the Rule (singular) kind label
// matches the ui.Header rendering — pre-refactor rules/list.go used
// "Rule" not "Rules" and the parity tests assume that exact string —
// plus pins the EmptyHint / MissingDirHint message bodies against the
// pre-refactor rules/list.go strings.
func TestRulesResource_StaticShape(t *testing.T) {
	if RulesResource.Kind != "Rule" {
		t.Errorf("RulesResource.Kind = %q, want %q (singular, matches ui.Header output)", RulesResource.Kind, "Rule")
	}
	if RulesResource.DirSegment != "rules" {
		t.Errorf("RulesResource.DirSegment = %q", RulesResource.DirSegment)
	}
	if RulesResource.MissingDirHint == nil {
		t.Error("RulesResource.MissingDirHint must be non-nil")
	}
	wantEmpty := "No rule files (.mdc/.md/.txt) under ~/.agents/rules/global/"
	if got := RulesResource.EmptyHint("global"); got != wantEmpty {
		t.Errorf("RulesResource.EmptyHint = %q, want %q", got, wantEmpty)
	}
	wantMissing := "No ~/.agents/rules/global/ directory yet (no canonical rule files for this scope)."
	if got := RulesResource.MissingDirHint("global"); got != wantMissing {
		t.Errorf("RulesResource.MissingDirHint = %q, want %q", got, wantMissing)
	}
}
