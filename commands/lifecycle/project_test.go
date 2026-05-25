package lifecycle

import (
	"errors"
	"reflect"
	"testing"
)

// ---------- ImportCandidate / ImportOutput shapes ----------

func TestImportCandidate_ZeroValueIsUsable(t *testing.T) {
	c := ImportCandidate{}
	if c.Project != "" || c.SourceRoot != "" || c.SourcePath != "" || c.DestRel != "" {
		t.Errorf("zero value should be empty strings, got %+v", c)
	}
}

func TestImportCandidate_FieldsAssign(t *testing.T) {
	c := ImportCandidate{
		Project:    "proj",
		SourceRoot: "/src",
		SourcePath: "/src/AGENTS.md",
		DestRel:    "rules/proj/agents.md",
	}
	if c.Project != "proj" || c.SourceRoot != "/src" || c.SourcePath != "/src/AGENTS.md" || c.DestRel != "rules/proj/agents.md" {
		t.Errorf("field assignment mismatch: %+v", c)
	}
}

func TestImportOutput_FieldsAssign(t *testing.T) {
	o := ImportOutput{
		DestRel: "hooks/proj/post-save/HOOK.yaml",
		Content: []byte("name: x\n"),
		Origin:  "github",
	}
	if o.DestRel != "hooks/proj/post-save/HOOK.yaml" {
		t.Errorf("DestRel mismatch: %q", o.DestRel)
	}
	if string(o.Content) != "name: x\n" {
		t.Errorf("Content mismatch: %q", string(o.Content))
	}
	if o.Origin != "github" {
		t.Errorf("Origin mismatch: %q", o.Origin)
	}
}

// ---------- CanonicalImportOutputs seam ----------

// The default CanonicalImportOutputs (when commands/import.go's init
// has not run, e.g. in a pure lifecycle test binary) returns
// (nil, false, nil) so callers know to fall back to legacy restore.
func TestCanonicalImportOutputs_DefaultIsNotHandled(t *testing.T) {
	// Save and restore the package-level seam so other tests are
	// unaffected (this test only verifies the default shape).
	saved := CanonicalImportOutputs
	defer func() { CanonicalImportOutputs = saved }()

	CanonicalImportOutputs = func(c ImportCandidate) ([]ImportOutput, bool, error) {
		return nil, false, nil
	}

	outputs, ok, err := CanonicalImportOutputs(ImportCandidate{Project: "p"})
	if err != nil {
		t.Fatalf("default seam should not error: %v", err)
	}
	if ok {
		t.Error("default seam should report handled=false")
	}
	if outputs != nil {
		t.Errorf("default seam should return nil outputs, got %v", outputs)
	}
}

// CanonicalImportOutputs forwards arguments to the wired implementation
// and surfaces both error and outputs unchanged.
func TestCanonicalImportOutputs_ForwardsToWiredImpl(t *testing.T) {
	saved := CanonicalImportOutputs
	defer func() { CanonicalImportOutputs = saved }()

	wantOutputs := []ImportOutput{
		{DestRel: "rules/proj/agents.md", Content: []byte("x"), Origin: "cursor"},
	}
	wantErr := errors.New("boom")

	var seen ImportCandidate
	CanonicalImportOutputs = func(c ImportCandidate) ([]ImportOutput, bool, error) {
		seen = c
		return wantOutputs, true, wantErr
	}

	got, ok, err := CanonicalImportOutputs(ImportCandidate{
		Project:    "proj",
		SourceRoot: "/root",
		SourcePath: "/root/x.md",
	})
	if !ok {
		t.Error("expected handled=true")
	}
	if err != wantErr {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}
	if !reflect.DeepEqual(got, wantOutputs) {
		t.Errorf("outputs mismatch: got %+v, want %+v", got, wantOutputs)
	}
	if seen.Project != "proj" || seen.SourceRoot != "/root" || seen.SourcePath != "/root/x.md" {
		t.Errorf("candidate forwarding mismatch: %+v", seen)
	}
}
