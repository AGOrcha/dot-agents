package crgbridge

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

const kindFn = "Function"

func TestSchema_MirrorsCRGAndIsMigrationOnly(t *testing.T) {
	b := New().Schema()
	if !b.MigrationOnly {
		t.Fatal("crg-bridge MUST be migration_only (§11.2)")
	}
	if !New().MigrationOnly() {
		t.Fatal("MigrationOnly() accessor should report true")
	}
	if len(b.StalenessDrivers) != 0 {
		t.Fatalf("crg-bridge mirror has no drivers (§11.2), got %v", b.StalenessDrivers)
	}
	if b.ImpactRadius.MaxDepth != 0 {
		t.Fatalf("mirror impact_radius max_depth must be 0 (read target only), got %d", b.ImpactRadius.MaxDepth)
	}
	native := crg.New().Schema()
	if len(b.NoteTypes) != len(native.NoteTypes) || len(b.EdgeTypes) != len(native.EdgeTypes) {
		t.Fatalf("mirror schema must match crg note/edge type counts (§11.2): bridge=%d/%d crg=%d/%d",
			len(b.NoteTypes), len(b.EdgeTypes), len(native.NoteTypes), len(native.EdgeTypes))
	}
}

func TestRegisterAndResolveBridge(t *testing.T) {
	reg := registry.New()
	if err := Register(reg); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := reg.Resolve("dotagents-builtin:graph/crg-bridge@^0.1"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// legacySeed simulates the external Python CRG bridge having already written its
// state into the kg_crg-bridge namespace. It writes through a bootstrap token
// for the bridge namespace — the EXTERNAL process, not the adapter (the adapter
// has no write path). The adapter only reads this back via MirrorSnapshot.
//
// drop, when non-empty, omits all symbols of that kind from the bridge write —
// a deliberately divergent legacy state used to prove the parity test CATCHES a
// real bridge-vs-kg-native divergence (it is not tautological).
func legacySeed(t *testing.T, store *sdk.MemStore, c crg.Corpus, drop string) {
	t.Helper()
	notes, edges := c.ToGraph()
	if drop != "" {
		notes = filterOutKind(notes, drop)
	}
	tok := sdk.BootstrapToken(Name) // {crg-bridge, write} — the external process's grant
	if err := store.WriteNotes(tok, Name, notes); err != nil {
		t.Fatalf("legacy seed notes: %v", err)
	}
	if err := store.WriteEdges(tok, Name, edges); err != nil {
		t.Fatalf("legacy seed edges: %v", err)
	}
}

// filterOutKind drops every note of the given kind (for the divergence test).
func filterOutKind(notes []sdk.Note, kind string) []sdk.Note {
	out := notes[:0:0]
	for _, n := range notes {
		if n.Fields["kind"] != kind {
			out = append(out, n)
		}
	}
	return out
}

func TestMirrorSnapshot_IsReadOnlyReadback(t *testing.T) {
	c := mirrorCorpus()
	store := sdk.NewMemStore()
	legacySeed(t, store, c, "")
	snap, err := MirrorSnapshot(store, c.Commit)
	if err != nil {
		t.Fatalf("mirror snapshot: %v", err)
	}
	if snap.Adapter != Name {
		t.Fatalf("snapshot adapter = %q, want %q", snap.Adapter, Name)
	}
	if snap.NodesTotal != len(c.Symbols) {
		t.Fatalf("readback nodes = %d, want %d", snap.NodesTotal, len(c.Symbols))
	}
	// The adapter exposes no write path: only the legacy seed populated the
	// namespace, and MirrorSnapshot only read it.
	if ns := store.Namespaces(); len(ns) != 1 || ns[0] != Name {
		t.Fatalf("namespaces = %v, want exactly [%s]", ns, Name)
	}
}

// TestHardTest_TenCommitDualReadParity is the t4 hard test. For the 10 pinned
// commits it ingests the corpus through the kg-native crg adapter (writing
// kg_crg.*) AND seeds the legacy bridge state (kg_crg-bridge.*) INDEPENDENTLY,
// then compares the two by READING BOTH NAMESPACES BACK from the Store seam:
//   - build/status: per-kind tolerance + exact files (refinement A)
//   - impact-radius: node-set equality over a seeded query corpus (refinement C)
//   - update: consecutive-commit upsert-tuple set equality (refinement D)
//
// Because both sides are read back from storage (not from corpus arithmetic), a
// real divergence is detectable — proven by TestHardTest_CatchesDivergence.
func TestHardTest_TenCommitDualReadParity(t *testing.T) {
	files := loadParityFiles(t)
	var prevNative, prevBridge []sdk.Note
	for i, f := range files {
		c, err := crg.LoadCorpus(f)
		if err != nil {
			t.Fatalf("commit %d load: %v", i, err)
		}
		if len(c.Symbols) != 100 {
			t.Fatalf("commit %d: 100-symbol corpus required, got %d", i, len(c.Symbols))
		}
		nb, bb := assertCommitParity(t, i, c, prevNative, prevBridge, "")
		prevNative, prevBridge = nb, bb
	}
}

// TestHardTest_CatchesDivergence proves the readback-based parity is NOT
// tautological: when the legacy bridge drops every Type symbol, the build-row
// comparison must FAIL (the kg-native side still has them).
func TestHardTest_CatchesDivergence(t *testing.T) {
	files := loadParityFiles(t)
	c, err := crg.LoadCorpus(files[0])
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	nativeStore := sdk.NewMemStore()
	ns := sdk.For(crg.Name, nativeStore)
	if _, err := crg.Bootstrap(ns, nativeStore, c, nil); err != nil {
		t.Fatalf("native bootstrap: %v", err)
	}
	nativeSnap, err := crg.SnapshotFromStore(crg.Name, nativeStore, crg.Name, c.Commit)
	if err != nil {
		t.Fatalf("native snapshot: %v", err)
	}

	bridgeStore := sdk.NewMemStore()
	legacySeed(t, bridgeStore, c, "Type") // drop every Type — a real divergence
	bridgeSnap, err := MirrorSnapshot(bridgeStore, c.Commit)
	if err != nil {
		t.Fatalf("mirror snapshot: %v", err)
	}

	rep := graphstore.CompareSnapshots(nativeSnap, bridgeSnap, graphstore.DefaultKindTolerance)
	if rep.Pass {
		t.Fatal("dropping every Type symbol from the bridge MUST fail parity — readback test is tautological otherwise")
	}
}

// assertCommitParity ingests c into both namespaces (independently), reads both
// back, and asserts the three parity rows. Returns the two namespaces' persisted
// notes so the next commit's update row diffs storage-vs-storage.
func assertCommitParity(t *testing.T, i int, c crg.Corpus, prevNative, prevBridge []sdk.Note, drop string) (nativeNotes, bridgeNotes []sdk.Note) {
	t.Helper()
	nativeStore := sdk.NewMemStore()
	ns := sdk.For(crg.Name, nativeStore)
	if _, err := crg.Bootstrap(ns, nativeStore, c, prevNative); err != nil {
		t.Fatalf("commit %d native bootstrap: %v", i, err)
	}
	bridgeStore := sdk.NewMemStore()
	legacySeed(t, bridgeStore, c, drop)

	nativeSnap := snapshotOf(t, crg.Name, nativeStore, c.Commit)
	bridgeSnap, err := MirrorSnapshot(bridgeStore, c.Commit)
	if err != nil {
		t.Fatalf("commit %d mirror snapshot: %v", i, err)
	}
	if rep := graphstore.CompareSnapshots(nativeSnap, bridgeSnap, graphstore.DefaultKindTolerance); !rep.Pass {
		t.Fatalf("commit %d build parity FAILED: %v", i, rep.Detail)
	}
	assertImpactParity(t, i, c, nativeStore, bridgeStore)
	assertUpdateParity(t, i, prevNative, prevBridge, nativeStore, bridgeStore)
	return readNotes(t, nativeStore, crg.Name), readNotes(t, bridgeStore, Name)
}

// assertImpactParity reads back both namespaces' edge graphs and asserts the
// impact-radius node sets match for a seeded query (refinement C).
func assertImpactParity(t *testing.T, i int, c crg.Corpus, nativeStore, bridgeStore *sdk.MemStore) {
	t.Helper()
	seeds := impactSeeds(c, 10)
	depth := crg.New().Schema().ImpactRadius.MaxDepth
	native, err := crg.ImpactRadiusFromStore(nativeStore, crg.Name, seeds, depth)
	if err != nil {
		t.Fatalf("commit %d native impact: %v", i, err)
	}
	bridge, err := crg.ImpactRadiusFromStore(bridgeStore, Name, seeds, depth)
	if err != nil {
		t.Fatalf("commit %d bridge impact: %v", i, err)
	}
	if rep := graphstore.CompareImpactRadius(native, bridge); !rep.Pass {
		t.Fatalf("commit %d impact parity FAILED: %v", i, rep.Detail)
	}
	if len(native) == 0 {
		t.Fatalf("commit %d: impact radius over %d seeds should be nonempty", i, len(seeds))
	}
}

// assertUpdateParity, for commits after the first, diffs each namespace's
// current persisted state against its prior persisted state and asserts the
// upsert-tuple sets agree (refinement D) — storage-vs-storage, not corpus.
func assertUpdateParity(t *testing.T, i int, prevNative, prevBridge []sdk.Note, nativeStore, bridgeStore *sdk.MemStore) {
	t.Helper()
	if prevNative == nil {
		return
	}
	nativeUps, err := crg.DiffFromStore(prevNative, nativeStore, crg.Name)
	if err != nil {
		t.Fatalf("commit %d native diff: %v", i, err)
	}
	bridgeUps, err := crg.DiffFromStore(prevBridge, bridgeStore, Name)
	if err != nil {
		t.Fatalf("commit %d bridge diff: %v", i, err)
	}
	if rep := graphstore.CompareUpserts(nativeUps, bridgeUps); !rep.Pass {
		t.Fatalf("commit %d update parity FAILED: %v", i, rep.Detail)
	}
	if len(nativeUps) == 0 {
		t.Fatalf("commit %d: expected nonzero upserts between consecutive commits", i)
	}
}

// snapshotOf reads a namespace back and builds its parity snapshot.
func snapshotOf(t *testing.T, adapter string, store *sdk.MemStore, commit string) graphstore.ParitySnapshot {
	t.Helper()
	snap, err := crg.SnapshotFromStore(adapter, store, adapter, commit)
	if err != nil {
		t.Fatalf("snapshot %s: %v", adapter, err)
	}
	return snap
}

// readNotes reads a namespace's persisted notes back for the next commit's diff.
func readNotes(t *testing.T, store *sdk.MemStore, ns string) []sdk.Note {
	t.Helper()
	notes, err := store.Notes(sdk.OwnReadToken(ns, "readback"), ns)
	if err != nil {
		t.Fatalf("readback notes %s: %v", ns, err)
	}
	return notes
}

// impactSeeds picks up to n Function symbol ids as changed-id query seeds.
func impactSeeds(c crg.Corpus, n int) []string {
	var seeds []string
	for _, sym := range c.Symbols {
		if sym.Kind == kindFn {
			seeds = append(seeds, crg.SymbolID(sym))
		}
		if len(seeds) == n {
			break
		}
	}
	return seeds
}

func mirrorCorpus() crg.Corpus {
	return crg.Corpus{
		Commit: "cafef00d",
		Symbols: []crg.Symbol{
			{QualifiedName: "pkg.A", Kind: kindFn, Language: "go", FilePath: "a.go", LineStart: 1, ContentHash: "h1"},
			{QualifiedName: "pkg.B", Kind: "Type", Language: "go", FilePath: "b.go", LineStart: 2, ContentHash: "h2"},
		},
		References: []crg.Reference{{Kind: "CALLS", From: "pkg.A", To: "pkg.B"}},
	}
}

func loadParityFiles(t *testing.T) []string {
	t.Helper()
	dir := parityDir(t)
	commits, err := crg.PinnedCommits(filepath.Join(dir, "commits.txt"))
	if err != nil {
		t.Fatalf("pinned commits: %v", err)
	}
	files, err := crg.SortedCorpusFiles(filepath.Join(dir, "corpus"))
	if err != nil {
		t.Fatalf("corpus files: %v", err)
	}
	if len(files) != 10 || len(commits) != 10 {
		t.Fatalf("hard test requires 10 pinned commits, got files=%d commits=%d", len(files), len(commits))
	}
	return files
}

func parityDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	// internal/adapters/builtin/crg-bridge/bridge_test.go → repo root is 4 dirs up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	return filepath.Join(root, "testdata", "crg-parity")
}
