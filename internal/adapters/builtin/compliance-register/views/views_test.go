package views_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	complianceregister "github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register"
	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/compliance-register/views"
	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
	crossnamespace "github.com/AGOrcha/dot-agents/internal/kg/dsl/cross-namespace"
	"github.com/AGOrcha/dot-agents/internal/kg/lockfile"
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
	store := crossnamespace.NewRebuildStore()
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

// TestRunCutoverWiring proves the §10.3 wiring (bug-4): RunCutover ties the
// mechanical CheckCompat gate into the lockfile state machine. A backward-
// compatible CRG bump drives the view to pending-rebuild; an incompatible bump
// (the referenced field's type changes) drives it to dsl-update-required and the
// lockfile then BLOCKS CRG (re)activation.
func TestRunCutoverWiring(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)

	t.Run("compatible_bump_to_pending_rebuild", func(t *testing.T) {
		v, lf := registeredView(t, now)
		status, err := views.RunCutover(lf, v, bumpedCRG(t, "string"), now)
		if err != nil {
			t.Fatalf("RunCutover: %v", err)
		}
		if status != lockfile.StatusPendingRebuild {
			t.Fatalf("status = %s, want pending-rebuild", status)
		}
		if b := lf.ActivationBlockers(crg.Name); len(b) != 0 {
			t.Fatalf("ActivationBlockers = %v, want none", b)
		}
	})

	t.Run("incompatible_bump_blocks_activation", func(t *testing.T) {
		v, lf := registeredView(t, now)
		// symbol.qualified_name string→int: parses but breaks the signature.
		status, err := views.RunCutover(lf, v, bumpedCRG(t, "int"), now)
		if err != nil {
			t.Fatalf("RunCutover: %v", err)
		}
		if status != lockfile.StatusDSLUpdateRequired {
			t.Fatalf("status = %s, want dsl-update-required", status)
		}
		if b := lf.ActivationBlockers(crg.Name); len(b) == 0 {
			t.Fatal("ActivationBlockers = none, want the dependent view (activation must be blocked)")
		}
	})
}

// TestRunCutoverUnregisteredViewErrors covers RunCutover's error path: a view
// that was never registered cannot resolve recompat.
func TestRunCutoverUnregisteredViewErrors(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	v, err := views.ControlsWithChangedFunctionEvidenceView()
	if err != nil {
		t.Fatalf("compile view: %v", err)
	}
	if _, err := views.RunCutover(lockfile.New(), v, bumpedCRG(t, "string"), now); err == nil {
		t.Fatal("RunCutover on an unregistered view: want error")
	}
}

// TestApproximationNoticeFlagsBug2 asserts the BUG-2 caveat is surfaced loudly.
func TestApproximationNoticeFlagsBug2(t *testing.T) {
	if !strings.Contains(views.ApproximationNotice, "KNOWN-INCORRECT") {
		t.Fatalf("ApproximationNotice = %q, want a loud BUG-2 caveat", views.ApproximationNotice)
	}
}

// registeredView compiles the live view and registers it ready in a fresh
// lockfile, depending on the CRG namespace.
func registeredView(t *testing.T, now time.Time) (*crossnamespace.View, *lockfile.Lockfile) {
	t.Helper()
	v, err := views.ControlsWithChangedFunctionEvidenceView()
	if err != nil {
		t.Fatalf("compile view: %v", err)
	}
	lf := lockfile.New()
	lf.RegisterView(v.Consumer(), v.Name(), "sha256:v0",
		[]lockfile.ViewDependency{{Adapter: crg.Name, SchemaDigest: "sha256:d0"}}, now)
	return v, lf
}

// bumpedCRG builds a CRG namespace whose symbol.qualified_name has the given
// type (and an added field), modelling a dependee schema bump.
func bumpedCRG(t *testing.T, qualifiedType string) crossnamespace.Namespace {
	t.Helper()
	info, err := dsl.NewSchemaInfo(
		[]dsl.NoteTypeDecl{{Name: "symbol", Fields: []dsl.FieldDecl{
			{Name: "qualified_name", Type: qualifiedType},
			{Name: "visibility", Type: "string"},
		}}},
		[]dsl.EdgeTypeDecl{{Name: "CALLS", From: "symbol", To: "symbol"}}, 3)
	if err != nil {
		t.Fatalf("bumped crg schema: %v", err)
	}
	return crossnamespace.Namespace{Name: crg.Name, Info: info}
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
