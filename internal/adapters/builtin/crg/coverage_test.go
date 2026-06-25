package crg

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// failStore is a sdk.Store whose writes fail after failAfter successful calls
// and whose reads can be made to fail, so Bootstrap's write and the readback
// error paths are reachable. It satisfies both sdk.Store and crg.StoreReader.
type failStore struct {
	writeCalls int
	failAfter  int
	failReads  bool
}

func (s *failStore) WriteNotes(_ sdk.Token, _ string, _ []sdk.Note) error { return s.maybeFail() }
func (s *failStore) WriteEdges(_ sdk.Token, _ string, _ []sdk.Edge) error { return s.maybeFail() }
func (s *failStore) Notes(_ sdk.Token, _ string) ([]sdk.Note, error)      { return nil, s.readErr() }
func (s *failStore) Edges(_ sdk.Token, _ string) ([]sdk.Edge, error)      { return nil, s.readErr() }
func (s *failStore) maybeFail() error {
	s.writeCalls++
	if s.writeCalls > s.failAfter {
		return errors.New("store: injected write failure")
	}
	return nil
}
func (s *failStore) readErr() error {
	if s.failReads {
		return errors.New("store: injected read failure")
	}
	return nil
}

func TestBootstrap_PropagatesWriteNotesError(t *testing.T) {
	fs := &failStore{failAfter: 0}
	s := sdk.For(Name, fs)
	if _, err := Bootstrap(s, fs, smallCorpus(), nil); err == nil {
		t.Fatal("Bootstrap must propagate a WriteNotes failure")
	}
}

func TestBootstrap_PropagatesWriteEdgesError(t *testing.T) {
	fs := &failStore{failAfter: 1} // notes ok, edges fail
	s := sdk.For(Name, fs)
	if _, err := Bootstrap(s, fs, smallCorpus(), nil); err == nil {
		t.Fatal("Bootstrap must propagate a WriteEdges failure")
	}
}

func TestBootstrap_PropagatesReadbackError(t *testing.T) {
	fs := &failStore{failAfter: 100, failReads: true} // writes ok, readback fails
	s := sdk.For(Name, fs)
	if _, err := Bootstrap(s, fs, smallCorpus(), nil); err == nil {
		t.Fatal("Bootstrap must propagate a readback (snapshot) failure")
	}
}

func TestSnapshotFromStore_ReadEdgesError(t *testing.T) {
	// Notes succeed, edges fail: exercise the second readback error branch.
	fs := &edgeFailStore{}
	if _, err := SnapshotFromStore(Name, fs, Name, "c"); err == nil {
		t.Fatal("SnapshotFromStore must propagate an Edges read failure")
	}
}

func TestDiffFromStore_ReadError(t *testing.T) {
	fs := &failStore{failReads: true}
	if _, err := DiffFromStore(nil, fs, Name); err == nil {
		t.Fatal("DiffFromStore must propagate a read failure")
	}
}

func TestImpactRadiusFromStore_ReadError(t *testing.T) {
	fs := &failStore{failReads: true}
	if _, err := ImpactRadiusFromStore(fs, Name, []string{"x"}, 1); err == nil {
		t.Fatal("ImpactRadiusFromStore must propagate a read failure")
	}
}

// edgeFailStore reads notes fine but fails on Edges, to cover the edges-read
// error branch of readNamespace.
type edgeFailStore struct{}

func (edgeFailStore) Notes(_ sdk.Token, _ string) ([]sdk.Note, error) { return nil, nil }
func (edgeFailStore) Edges(_ sdk.Token, _ string) ([]sdk.Edge, error) {
	return nil, errors.New("store: injected edge read failure")
}

func TestImpactRadius_IdentityWithoutStore(t *testing.T) {
	a := New()
	res, err := a.ImpactRadius(registry.ImpactRequest{ChangedIDs: []string{"x", "y"}})
	if err != nil {
		t.Fatalf("impact radius: %v", err)
	}
	if len(res.IDs) != 2 || res.IDs[0] != "x" || res.IDs[1] != "y" {
		t.Fatalf("identity impact radius = %v, want [x y]", res.IDs)
	}
	// returned slice must be a copy, not alias the input
	in := []string{"a"}
	res, _ = a.ImpactRadius(registry.ImpactRequest{ChangedIDs: in})
	res.IDs[0] = "mutated"
	if in[0] != "a" {
		t.Fatal("ImpactRadius must not alias the caller's slice")
	}
}

func TestSchema_PanicsOnMalformedEmbed(t *testing.T) {
	orig := schemaYAML
	t.Cleanup(func() { schemaYAML = orig })
	schemaYAML = []byte("name: \nversion: \n:::not yaml")
	defer func() {
		if recover() == nil {
			t.Fatal("Schema must panic on a malformed embedded schema")
		}
	}()
	_ = New().Schema()
}

func TestLoadCorpus_Errors(t *testing.T) {
	if _, err := LoadCorpus("/nonexistent/corpus.json"); err == nil {
		t.Fatal("missing corpus file must error")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCorpus(bad); err == nil {
		t.Fatal("malformed corpus JSON must error")
	}
}

func TestPinnedCommits_MissingFileErrors(t *testing.T) {
	if _, err := PinnedCommits("/nonexistent/commits.txt"); err == nil {
		t.Fatal("missing commits file must error")
	}
}

func TestSortedCorpusFiles_MissingDirErrors(t *testing.T) {
	if _, err := SortedCorpusFiles("/nonexistent/dir"); err == nil {
		t.Fatal("missing corpus dir must error")
	}
}

func TestPinnedCommits_SkipsCommentsAndBlanks(t *testing.T) {
	f := filepath.Join(t.TempDir(), "commits.txt")
	if err := os.WriteFile(f, []byte("# header\n\nabc123\n  def456  \n# trailing comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := PinnedCommits(f)
	if err != nil {
		t.Fatalf("pinned commits: %v", err)
	}
	if len(got) != 2 || got[0] != "abc123" || got[1] != "def456" {
		t.Fatalf("commits = %v, want [abc123 def456] (comments/blanks/whitespace stripped)", got)
	}
}

func TestToGraph_SkipsDanglingReference(t *testing.T) {
	c := Corpus{
		Symbols:    []Symbol{{QualifiedName: "a", Kind: "Function", FilePath: "a.go"}},
		References: []Reference{{Kind: "CALLS", From: "a", To: "ghost"}},
	}
	_, edges := c.ToGraph()
	if len(edges) != 0 {
		t.Fatalf("dangling reference (to=ghost) must not become an edge, got %v", edges)
	}
}
