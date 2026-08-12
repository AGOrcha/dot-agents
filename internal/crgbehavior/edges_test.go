package crgbehavior

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// lateFailStore fails a read only after the bootstrap's own readback has
// succeeded, so the gate's post-bootstrap readback and derived-view failures
// are both reachable.
type lateFailStore struct {
	inner       *sdk.MemStore
	notesFailAt int
	edgesFailAt int
	notesCalls  int
	edgesCalls  int
}

func (s *lateFailStore) WriteNotes(t sdk.Token, ns string, n []sdk.Note) error {
	return s.inner.WriteNotes(t, ns, n)
}

func (s *lateFailStore) WriteEdges(t sdk.Token, ns string, e []sdk.Edge) error {
	return s.inner.WriteEdges(t, ns, e)
}

func (s *lateFailStore) Notes(t sdk.Token, ns string) ([]sdk.Note, error) {
	s.notesCalls++
	if s.notesCalls == s.notesFailAt {
		return nil, errors.New("notes read failed")
	}
	return s.inner.Notes(t, ns)
}

func (s *lateFailStore) Edges(t sdk.Token, ns string) ([]sdk.Edge, error) {
	s.edgesCalls++
	if s.edgesCalls == s.edgesFailAt {
		return nil, errors.New("edges read failed")
	}
	return s.inner.Edges(t, ns)
}

func TestBootstrapNativeSurfacesLateReadFailures(t *testing.T) {
	// Bootstrap reads the namespace back once; the gate then reads it again for
	// the file map and a third time for the derived views.
	readback := &lateFailStore{inner: sdk.NewMemStore(), notesFailAt: 2}
	if _, err := bootstrapNative(readback, fixtureViews(), "sha"); err == nil ||
		!strings.Contains(err.Error(), "native readback") {
		t.Fatalf("err = %v, want the namespace readback failure", err)
	}
	derived := &lateFailStore{inner: sdk.NewMemStore(), edgesFailAt: 2}
	if _, err := bootstrapNative(derived, fixtureViews(), "sha"); err == nil ||
		!strings.Contains(err.Error(), "derived views") {
		t.Fatalf("err = %v, want the derived-view failure", err)
	}
}

func TestCommunityDivergenceOnDifferentKeySets(t *testing.T) {
	views := fixtureViews()
	// The bridge assigned no community to one of the changed symbols.
	delete(views.Communities, idLone)
	cfg := fixtureConfig(Task{Commit: "7777777777777777", ChangedFiles: []string{"a.go", "b.go"}})
	report, err := Run(cfg, views, &fakeImpact{changed: []string{idEntry, idStep, idWidget, idLone}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	s := surfaceNamed(t, report, SurfaceCommunities)
	if s.Pass || !strings.Contains(strings.Join(s.Detail, " "), "do not cover the same") {
		t.Fatalf("a missing community assignment must be reported as a coverage divergence: %+v", s)
	}
}

func TestRunRejectsUnnormalizedOrEmptyGraphs(t *testing.T) {
	if _, err := Run(fixtureConfig(), BridgeViews{}, agreeingImpact()); err == nil ||
		!strings.Contains(err.Error(), "no symbols") {
		t.Fatalf("err = %v, want the empty-graph guard", err)
	}
	// Both spellings must trip the guard: a graph built under a POSIX root is
	// still un-normalized when the gate runs on Windows, where
	// filepath.IsAbs("/abs/repo/a.go") is false.
	for _, root := range []string{"/abs/repo/", `C:\abs\repo\`} {
		absolute := fixtureViews()
		for i := range absolute.Symbols {
			absolute.Symbols[i].FilePath = root + absolute.Symbols[i].FilePath
		}
		_, err := Run(fixtureConfig(), absolute, agreeingImpact())
		if err == nil || !strings.Contains(err.Error(), "still absolute") {
			t.Fatalf("root %q: err = %v, want the root-mismatch guard (it would otherwise read as total divergence)", root, err)
		}
	}
}

func TestLooksAbsoluteAcceptsBothPlatformSpellings(t *testing.T) {
	absolute := []string{"/abs/repo/a.go", `C:\abs\repo\a.go`, "c:/abs/repo/a.go"}
	for _, p := range absolute {
		if !looksAbsolute(p) {
			t.Fatalf("%q must be recognized as absolute on any host", p)
		}
	}
	relative := []string{"pkg/a.go", `pkg\a.go`, "C:relative.go", "1:/notadrive.go", "ab"}
	for _, p := range relative {
		if looksAbsolute(p) {
			t.Fatalf("%q is not an absolute path", p)
		}
	}
}

func TestScoredBySidesRequiresBothSides(t *testing.T) {
	a := map[string]float64{"x": 1, "y": 2}
	b := map[string]float64{"y": 5, "z": 6}
	got := scoredBySides(a, b, []string{"x", "y", "z"})
	if strings.Join(got, ",") != "y" {
		t.Fatalf("scoredBySides = %v, want only the symbols both sides score", got)
	}
}

func TestTokensForHandlesUnqualifiedTokens(t *testing.T) {
	got := tokensFor([]string{"a.go::Entry", "bare", "b.go::Other"}, []string{"Entry", "bare"})
	if strings.Join(got, ",") != "a.go::Entry,bare" {
		t.Fatalf("tokensFor = %v, want the qualified and unqualified matches", got)
	}
}

func TestCapDetailBoundsTheDiff(t *testing.T) {
	var many []string
	for i := 0; i < maxDetailLines+5; i++ {
		many = append(many, fmt.Sprintf("difference %02d", i))
	}
	got := capDetail(many)
	if len(got) != maxDetailLines+1 {
		t.Fatalf("capDetail kept %d lines, want %d plus an overflow note", len(got), maxDetailLines)
	}
	if !strings.Contains(got[len(got)-1], "and 5 more") {
		t.Fatalf("the overflow note must state how much was elided, got %q", got[len(got)-1])
	}
}

func TestShortAbbreviatesOnlyLongShas(t *testing.T) {
	if short("abc") != "abc" || short("0123456789") != "01234567" {
		t.Fatal("short must abbreviate long shas and pass short ones through")
	}
}

func TestKeyFlowsByEntryPointDropsFlowsWithoutAnEntry(t *testing.T) {
	rows := keyFlowsByEntryPoint(map[int64][]crg.FlowMembership{
		1: {{MemberID: "a", Position: 1}, {MemberID: "b", Position: 2}},
		2: {{MemberID: "z", Position: 0}, {MemberID: "y", Position: 1}},
		3: {{MemberID: "c", Position: 0}},
	})
	if len(rows) != 3 {
		t.Fatalf("a flow with no position-0 member cannot be keyed and must be dropped, got %+v", rows)
	}
	if rows[0].FlowID != "c" || rows[1].FlowID != "z" || rows[1].Position != 0 {
		t.Fatalf("rows must be ordered by (flow id, position), got %+v", rows)
	}
}

func TestReadFromStoreReportsAnUnusableDriver(t *testing.T) {
	_, err := readFromStore("no-such-driver", filepath.Join(t.TempDir(), "graph.db"), repoPrefix)
	if err == nil || !strings.Contains(err.Error(), "open bridge graph") {
		t.Fatalf("err = %v, want a wrapped open failure", err)
	}
}

func TestWriteJSONReportsEncodingAndWriteFailures(t *testing.T) {
	dir := t.TempDir()
	if err := writeJSON(filepath.Join(dir, "x.json"), make(chan int)); err == nil {
		t.Fatal("an unencodable value must be an error")
	}
	if err := (Manifest{SchemaVersion: ManifestSchemaVersion, Tasks: []Task{{Commit: "a"}}}).Save(dir); err == nil {
		t.Fatal("writing over an existing directory must fail")
	}
}

func TestRunLiveSurfacesAViewsFailure(t *testing.T) {
	repo := fakeCRGRepo(t, "#!/bin/sh\nexit 0\n")
	graph := filepath.Join(repo, ".code-review-graph")
	if err := os.MkdirAll(graph, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(graph, "graph.db"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunLive(Config{}, repo); err == nil {
		t.Fatal("an unreadable legacy graph must fail the run")
	}
}
