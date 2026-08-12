package codegraph

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// errFake is the sentinel every injected store or derivation failure returns.
var errFake = errors.New("codegraph test: injected failure")

// fakeStore is a graphstore.Store whose result and failure per method is set by
// the test. The contract interface is embedded rather than implemented in full,
// so only the handful of methods the engine actually calls need bodies and any
// unexpected call fails loudly on the nil embedded value instead of silently
// returning a zero result.
type fakeStore struct {
	graphstore.Store

	files     []string
	filesErr  error
	nodes     map[string][]graphstore.GraphNode
	nodesErr  error
	edges     map[string][]graphstore.GraphEdge
	edgesErr  error
	stats     graphstore.GraphStats
	statsErr  error
	impact    graphstore.ImpactResult
	impactErr error
	removeErr error
	writeErr  error
	metaErr   error
}

func (f *fakeStore) GetAllFiles() ([]string, error) { return f.files, f.filesErr }

func (f *fakeStore) GetNodesByFile(path string) ([]graphstore.GraphNode, error) {
	return f.nodes[path], f.nodesErr
}

func (f *fakeStore) GetEdgesBySource(qualified string) ([]graphstore.GraphEdge, error) {
	return f.edges[qualified], f.edgesErr
}

func (f *fakeStore) GetStats() (graphstore.GraphStats, error) { return f.stats, f.statsErr }

func (f *fakeStore) GetImpactRadius([]string, int, int) (graphstore.ImpactResult, error) {
	return f.impact, f.impactErr
}

func (f *fakeStore) RemoveFileData(string) error { return f.removeErr }

func (f *fakeStore) StoreFileNodesEdges(string, []graphstore.NodeInfo, []graphstore.EdgeInfo, string) error {
	return f.writeErr
}

func (f *fakeStore) SetMetadata(string, string) error { return f.metaErr }

func (f *fakeStore) Close() error { return nil }

// engineWithStore returns an engine rooted at root whose persistence is the
// given fake, with a stubbed diff so no test reaches git.
func engineWithStore(t *testing.T, root string, store graphstore.Store) *Engine {
	t.Helper()
	e := Open(root)
	e.store = store
	e.changedFiles = func(string, string) ([]string, error) { return []string{"lib/lib.go"}, nil }
	return e
}

// unopenableEngine returns an engine whose database can never be opened: the
// parent of its db path is a regular file, so the store's directory creation
// fails. It exercises the write-path open failure.
func unopenableEngine(t *testing.T) *Engine {
	t.Helper()
	root := writeFixture(t)
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	e := Open(root)
	e.dbPath = filepath.Join(blocker, "code-graph.db")
	e.changedFiles = func(string, string) ([]string, error) { return []string{"lib/lib.go"}, nil }
	t.Cleanup(func() { _ = e.Close() })
	return e
}

// unreadableEngine returns an engine whose db path exists (so the graph reads
// as built) but is a directory, so every lazy open of it fails. It exercises
// the read-path open failure every query degrades through.
func unreadableEngine(t *testing.T) *Engine {
	t.Helper()
	e := Open(writeFixture(t))
	e.dbPath = t.TempDir()
	e.changedFiles = func(string, string) ([]string, error) { return []string{"lib/lib.go"}, nil }
	t.Cleanup(func() { _ = e.Close() })
	return e
}

// writeExtra adds one more source file to an existing fixture root.
func writeExtra(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// failCRGDerivations points every crg derivation seam at a failing stub for the
// duration of the test. Production binds the real derivations; the in-process
// namespace projection cannot fail on its own, so the seam is the only way to
// reach the engine's readback-failure arms.
func failCRGDerivations(t *testing.T) {
	t.Helper()
	flows, memberships := flowsFromStore, flowMembershipsFromStore
	communities, risk, post := communitiesFromStore, riskIndexFromStore, postprocessFromStore
	t.Cleanup(func() {
		flowsFromStore, flowMembershipsFromStore = flows, memberships
		communitiesFromStore, riskIndexFromStore, postprocessFromStore = communities, risk, post
	})
	flowsFromStore = func(crg.StoreReader, string) ([]crg.Flow, error) { return nil, errFake }
	flowMembershipsFromStore = func(crg.StoreReader, string) ([]crg.FlowMembership, error) {
		return nil, errFake
	}
	communitiesFromStore = func(crg.StoreReader, string) (map[string]string, error) { return nil, errFake }
	riskIndexFromStore = func(crg.StoreReader, string) (map[string]float64, error) { return nil, errFake }
	postprocessFromStore = func(crg.StoreReader, string) (crg.Postprocess, error) {
		return crg.Postprocess{}, errFake
	}
}
