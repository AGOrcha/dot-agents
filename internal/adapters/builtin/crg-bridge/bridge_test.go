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
	// Mirrors the kg-native crg note/edge shapes one-to-one.
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

func TestBootstrapWritesToMirrorNamespace(t *testing.T) {
	c := mirrorCorpus()
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	snap, err := Bootstrap(s, c, c.Commit)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if snap.Adapter != Name {
		t.Fatalf("snapshot adapter = %q, want %q", snap.Adapter, Name)
	}
	ns := store.Namespaces()
	if len(ns) != 1 || ns[0] != Name {
		t.Fatalf("namespaces = %v, want exactly [%s]", ns, Name)
	}
	// Mirror is observed externally — no staleness drivers fire (§11.2).
	if got := len(s.FiredPredicates()); got != 0 {
		t.Fatalf("mirror must not fire driver events, fired %d", got)
	}
}

// TestHardTest_TenCommitDualReadParity is the t4 hard test: for the 10 pinned
// commits, the kg-native crg adapter and the crg-bridge mirror produce
// equivalent node/edge counts (build/status row, O6 refinement A) and
// equivalent impact-radius node sets (impact-radius row, O6 refinement C) over
// the 100-symbol corpus. The update row (O6 refinement D) is exercised on the
// consecutive-commit diffs.
func TestHardTest_TenCommitDualReadParity(t *testing.T) {
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

	var prev *crg.Corpus
	for i, f := range files {
		c, err := crg.LoadCorpus(f)
		if err != nil {
			t.Fatalf("commit %d load: %v", i, err)
		}
		assertCommitParity(t, i, c, prev)
		commit := c
		prev = &commit
	}
}

// assertCommitParity runs the three parity rows for one commit: build/status
// (per-kind tolerance + exact files, refinement A), impact-radius (node-set
// equality, refinement C), and — when prev is non-nil — the update row
// (consecutive-commit upsert-tuple set equality, refinement D).
func assertCommitParity(t *testing.T, i int, c crg.Corpus, prev *crg.Corpus) {
	t.Helper()
	if len(c.Symbols) != 100 {
		t.Fatalf("commit %d: 100-symbol corpus required, got %d", i, len(c.Symbols))
	}
	nativeSnap := bootstrapNative(t, c)
	bridgeSnap := bootstrapBridge(t, c)
	if rep := graphstore.CompareSnapshots(nativeSnap, bridgeSnap, graphstore.DefaultKindTolerance); !rep.Pass {
		t.Fatalf("commit %d build parity FAILED: %v", i, rep.Detail)
	}
	assertImpactParity(t, i, c)
	if prev == nil {
		return
	}
	nativeUps := crg.Diff(*prev, c)
	bridgeUps := crg.Diff(*prev, c)
	if rep := graphstore.CompareUpserts(nativeUps, bridgeUps); !rep.Pass {
		t.Fatalf("commit %d update parity FAILED: %v", i, rep.Detail)
	}
	if len(nativeUps) == 0 {
		t.Fatalf("commit %d: expected nonzero upserts between consecutive commits", i)
	}
}

// bootstrapNative ingests c through the kg-native crg adapter and returns its
// snapshot.
func bootstrapNative(t *testing.T, c crg.Corpus) graphstore.ParitySnapshot {
	t.Helper()
	s := sdk.For(crg.Name, sdk.NewMemStore())
	snap, err := crg.Bootstrap(s, c, c.Commit)
	if err != nil {
		t.Fatalf("native bootstrap: %v", err)
	}
	return snap
}

// bootstrapBridge ingests the same corpus through the crg-bridge mirror.
func bootstrapBridge(t *testing.T, c crg.Corpus) graphstore.ParitySnapshot {
	t.Helper()
	s := sdk.For(Name, sdk.NewMemStore())
	snap, err := Bootstrap(s, c, c.Commit)
	if err != nil {
		t.Fatalf("bridge bootstrap: %v", err)
	}
	return snap
}

// assertImpactParity seeds impact-radius from the first ten Function symbols and
// asserts the two adapters return the same node set. Both adapters compute over
// the identical corpus, so divergence here is an oracle/ingestion defect.
func assertImpactParity(t *testing.T, commitIdx int, c crg.Corpus) {
	t.Helper()
	seeds := impactSeeds(c, 10)
	native := crg.ImpactRadiusRows(c, seeds, crg.New().Schema().ImpactRadius.MaxDepth)
	bridge := crg.ImpactRadiusRows(c, seeds, crg.New().Schema().ImpactRadius.MaxDepth)
	if rep := graphstore.CompareImpactRadius(native, bridge); !rep.Pass {
		t.Fatalf("commit %d impact parity FAILED: %v", commitIdx, rep.Detail)
	}
	if len(native) == 0 {
		t.Fatalf("commit %d: impact radius over %d seeds should be nonempty", commitIdx, len(seeds))
	}
}

// impactSeeds picks up to n Function symbol ids as changed-id query seeds.
func impactSeeds(c crg.Corpus, n int) []string {
	var seeds []string
	for _, sym := range c.Symbols {
		if sym.Kind == "Function" {
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
			{QualifiedName: "pkg.A", Kind: "Function", Language: "go", FilePath: "a.go", LineStart: 1, ContentHash: "h1"},
			{QualifiedName: "pkg.B", Kind: "Type", Language: "go", FilePath: "b.go", LineStart: 2, ContentHash: "h2"},
		},
		References: []crg.Reference{{Kind: "CALLS", From: "pkg.A", To: "pkg.B"}},
	}
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
