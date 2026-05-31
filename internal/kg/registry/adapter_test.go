package registry

import (
	"reflect"
	"testing"
)

// fakeAdapter is a minimal Adapter for registry tests.
type fakeAdapter struct {
	name    string
	version string
}

func (f fakeAdapter) Name() string { return f.name }
func (f fakeAdapter) Schema() Schema {
	return Schema{Name: f.name, Version: f.version, ImpactRadius: ImpactRadius{Query: "RETURN $changed_ids AS id"}}
}
func (f fakeAdapter) ImpactRadius(req ImpactRequest) (ImpactResult, error) {
	return ImpactResult{IDs: req.ChangedIDs}, nil
}

func TestRegisterAndNames(t *testing.T) {
	reg := New()
	if err := reg.Register(fakeAdapter{name: "alpha", version: "1.0.0"}); err != nil {
		t.Fatalf("Register alpha: %v", err)
	}
	if err := reg.Register(fakeAdapter{name: "beta", version: "2.0.0"}); err != nil {
		t.Fatalf("Register beta: %v", err)
	}
	got := reg.Names()
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestRegisterErrors(t *testing.T) {
	reg := New()
	if err := reg.Register(nil); err == nil {
		t.Fatal("Register(nil) want error")
	}
	if err := reg.Register(fakeAdapter{name: "", version: "1.0.0"}); err == nil {
		t.Fatal("Register empty-name want error")
	}
	if err := reg.Register(fakeAdapter{name: "dup", version: "1.0.0"}); err != nil {
		t.Fatalf("Register dup: %v", err)
	}
	if err := reg.Register(fakeAdapter{name: "dup", version: "1.1.0"}); err == nil {
		t.Fatal("Register duplicate want error")
	}
}

func TestResolve(t *testing.T) {
	reg := New()
	if err := reg.Register(fakeAdapter{name: "none", version: "1.0.0"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{name: "builtin caret satisfied", ref: "dotagents-builtin:graph/none@^1.0"},
		{name: "no constraint", ref: "none"},
		{name: "exact satisfied", ref: "none@1.0.0"},
		{name: "exact unsatisfied", ref: "none@1.0.1", wantErr: true},
		{name: "caret unsatisfied major", ref: "none@^2.0", wantErr: true},
		{name: "unknown adapter", ref: "missing", wantErr: true},
		{name: "bad ref", ref: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := reg.Resolve(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Resolve(%q) want error", tt.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve(%q) unexpected error: %v", tt.ref, err)
			}
			if a.Name() != "none" {
				t.Fatalf("Resolve(%q).Name() = %q, want none", tt.ref, a.Name())
			}
		})
	}
}

func TestResolveInvalidRegisteredVersion(t *testing.T) {
	reg := New()
	if err := reg.Register(fakeAdapter{name: "bad", version: "not-a-version"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := reg.Resolve("bad@^1.0"); err == nil {
		t.Fatal("Resolve with unparseable registered version want error")
	}
}

func TestRegisterNamesEmpty(t *testing.T) {
	if got := New().Names(); len(got) != 0 {
		t.Fatalf("empty registry Names() = %v, want empty", got)
	}
}
