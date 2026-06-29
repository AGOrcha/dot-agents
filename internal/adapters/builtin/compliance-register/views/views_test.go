package views_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	complianceregister "github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register"
	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register/views"
	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	crossnamespace "github.com/AGOrcha/dot-agents/internal/kg/dsl/cross-namespace"
)

// corpus is the testdata fixture shape: a flat note + edge list per namespace.
type corpus struct {
	Notes []struct {
		ID     string         `json:"id"`
		Type   string         `json:"type"`
		Fields map[string]any `json:"fields"`
	} `json:"notes"`
	Edges []struct {
		Type string `json:"type"`
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"edges"`
}

// dataPath resolves a testdata/cross-adapter fixture relative to the repo root.
func dataPath(t *testing.T, name string) string {
	t.Helper()
	// Package lives at internal/adapters/builtin/compliance-register/views;
	// the repo root is five levels up.
	return filepath.Join("..", "..", "..", "..", "..", "testdata", "cross-adapter", name)
}

// seed writes a fixture file into the given namespace through the SDK.
func seed(t *testing.T, store sdk.Store, ns, file string) {
	t.Helper()
	data, err := os.ReadFile(dataPath(t, file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var c corpus
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	s := sdk.For(ns, store)
	notes := make([]sdk.Note, 0, len(c.Notes))
	for _, n := range c.Notes {
		notes = append(notes, sdk.Note{ID: n.ID, Type: n.Type, Fields: n.Fields})
	}
	if err := s.WriteNotes(notes); err != nil {
		t.Fatalf("write notes %s: %v", file, err)
	}
	edges := make([]sdk.Edge, 0, len(c.Edges))
	for _, e := range c.Edges {
		edges = append(edges, sdk.Edge{Type: e.Type, From: e.From, To: e.To})
	}
	if err := s.WriteEdges(edges); err != nil {
		t.Fatalf("write edges %s: %v", file, err)
	}
}

// TestViewCompiles proves the §8.3 view compiles from the LIVE compliance and
// CRG adapter schemas, declares CRG as its only dependency, and derives the
// N3 multi-namespace token (write on compliance + read on crg).
func TestViewCompiles(t *testing.T) {
	v, err := views.ControlsWithChangedFunctionEvidenceView()
	if err != nil {
		t.Fatalf("compile view: %v", err)
	}
	if v.Name() != views.ControlsWithChangedFunctionEvidence {
		t.Errorf("name = %q, want %q", v.Name(), views.ControlsWithChangedFunctionEvidence)
	}
	if v.Consumer() != complianceregister.Name {
		t.Errorf("consumer = %q, want %q", v.Consumer(), complianceregister.Name)
	}
	if got := v.Deps(); !reflect.DeepEqual(got, []string{crg.Name}) {
		t.Errorf("deps = %v, want [%q]", got, crg.Name)
	}
	wantTouched := []string{complianceregister.Name, crg.Name}
	sort.Strings(wantTouched)
	if got := v.TouchedNamespaces(); !reflect.DeepEqual(got, wantTouched) {
		t.Errorf("touched = %v, want %v", got, wantTouched)
	}
	assertToken(t, v.Token(), map[string]sdk.Mode{
		complianceregister.Name: sdk.ModeWrite,
		crg.Name:                sdk.ModeRead,
	})
}

// TestViewMaterializesFromCRG is the §8.3 hard test against the real adapters
// and testdata: the view surfaces exactly the controls whose cited evidence
// references a source-stale CRG symbol.
func TestViewMaterializesFromCRG(t *testing.T) {
	store := sdk.NewMemStore()
	seed(t, store, complianceregister.Name, "compliance.json")
	seed(t, store, crg.Name, "crg.json")

	v, err := views.ControlsWithChangedFunctionEvidenceView()
	if err != nil {
		t.Fatalf("compile view: %v", err)
	}
	rows, err := v.Materialize(store)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	assertRows(t, rows, [][3]string{
		{"AC-2-MFA", "ev-mfa", "auth.Login"},
		{"DR-1-RETENTION", "ev-retain", "data.Retain"},
	})
}

// TestViewCompatAgainstLiveCRG proves the mechanical cutover gate (§10.3)
// reports the view compatible with the live CRG schema it was built against.
func TestViewCompatAgainstLiveCRG(t *testing.T) {
	v, err := views.ControlsWithChangedFunctionEvidenceView()
	if err != nil {
		t.Fatalf("compile view: %v", err)
	}
	state, cerr := v.CheckCompat([]crossnamespace.Namespace{views.CRGNamespace()})
	if state != crossnamespace.CompatOK || cerr != nil {
		t.Fatalf("CheckCompat = (%s, %v), want (compatible, nil)", state, cerr)
	}
}

func assertToken(t *testing.T, tok sdk.Token, want map[string]sdk.Mode) {
	t.Helper()
	got := map[string]sdk.Mode{}
	for _, g := range tok.Authorized {
		got[g.Namespace] = g.Mode
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("token grants = %v, want %v", got, want)
	}
}

func assertRows(t *testing.T, rows []sdk.Row, want [][3]string) {
	t.Helper()
	got := make([][3]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, [3]string{s(r["control_id"]), s(r["evidence_id"]), s(r["function"])})
	}
	sortTriples(got)
	w := append([][3]string(nil), want...)
	sortTriples(w)
	if !reflect.DeepEqual(got, w) {
		t.Fatalf("rows = %v, want %v", got, w)
	}
}

func sortTriples(x [][3]string) {
	sort.Slice(x, func(i, j int) bool {
		for k := 0; k < 3; k++ {
			if x[i][k] != x[j][k] {
				return x[i][k] < x[j][k]
			}
		}
		return false
	})
}

func s(v any) string {
	out, _ := v.(string)
	return out
}
