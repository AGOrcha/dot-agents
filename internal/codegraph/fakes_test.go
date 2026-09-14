package codegraph

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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

	files    []string
	filesErr error
	nodes    map[string][]graphstore.GraphNode
	nodesErr error
	edges    map[string][]graphstore.GraphEdge
	edgesErr error
	// edgeSources records each GetEdgesBySource query, so a test can assert
	// the read layer collapses duplicate sources instead of re-querying.
	edgeSources []string
	stats       graphstore.GraphStats
	statsErr    error
	impact      graphstore.ImpactResult
	impactErr   error
	removeErr   error
	writeErr    error
	metaErr     error
	meta        map[string]string
	// The derived-view role: the persisted tables every query path and the
	// post-process pass read back.
	allNodes       []graphstore.GraphNode
	allNodesErr    error
	allEdges       []graphstore.GraphEdge
	allEdgesErr    error
	flows          []graphstore.FlowRow
	flowsErr       error
	memberships    []graphstore.FlowMembershipRow
	membershipsErr error
	communities    []graphstore.CommunityRow
	communitiesErr error
	risk           []graphstore.RiskIndexRow
	riskErr        error
	// The post-process worklist and its injectable failures.
	pendingSignatures []graphstore.GraphNode
	signatureErr      error
	ftsIndexed        int
	ftsErr            error
	edgesByTarget     map[string][]graphstore.GraphEdge
}

func (f *fakeStore) GetAllFiles() ([]string, error) { return f.files, f.filesErr }

func (f *fakeStore) GetNodesByFile(path string) ([]graphstore.GraphNode, error) {
	return f.nodes[path], f.nodesErr
}

func (f *fakeStore) GetEdgesBySource(qualified string) ([]graphstore.GraphEdge, error) {
	f.edgeSources = append(f.edgeSources, qualified)
	return f.edges[qualified], f.edgesErr
}

func (f *fakeStore) GetStats() (graphstore.GraphStats, error) { return f.stats, f.statsErr }

// GetEdgesByTarget backs the dependent expansion an incremental update runs.
func (f *fakeStore) GetEdgesByTarget(qualified string) ([]graphstore.GraphEdge, error) {
	return f.edgesByTarget[qualified], f.edgesErr
}

func (f *fakeStore) GetImpactRadius([]string, int, int) (graphstore.ImpactResult, error) {
	return f.impact, f.impactErr
}

func (f *fakeStore) RemoveFileData(string) error { return f.removeErr }

func (f *fakeStore) StoreFileNodesEdges(string, []graphstore.NodeInfo, []graphstore.EdgeInfo, string) error {
	return f.writeErr
}

func (f *fakeStore) SetMetadata(string, string) error { return f.metaErr }

func (f *fakeStore) Close() error { return nil }

func (f *fakeStore) GetMetadata(key string) (string, error) { return f.meta[key], f.metaErr }

func (f *fakeStore) Commit() error { return f.writeErr }

func (f *fakeStore) ReadAllNodes() ([]graphstore.GraphNode, error) {
	return f.allNodes, f.allNodesErr
}

func (f *fakeStore) ReadAllEdges() ([]graphstore.GraphEdge, error) {
	return f.allEdges, f.allEdgesErr
}

func (f *fakeStore) ReadFlows() ([]graphstore.FlowRow, error) { return f.flows, f.flowsErr }

func (f *fakeStore) ReadFlowMemberships() ([]graphstore.FlowMembershipRow, error) {
	return f.memberships, f.membershipsErr
}

func (f *fakeStore) ReadCommunities() ([]graphstore.CommunityRow, error) {
	return f.communities, f.communitiesErr
}

func (f *fakeStore) ReadRiskIndex() ([]graphstore.RiskIndexRow, error) { return f.risk, f.riskErr }

func (f *fakeStore) ReadNodesByID(ids []int64) ([]graphstore.GraphNode, error) {
	var out []graphstore.GraphNode
	for _, node := range f.allNodes {
		for _, id := range ids {
			if node.ID == id {
				out = append(out, node)
				break
			}
		}
	}
	return out, f.allNodesErr
}

func (f *fakeStore) ReadNodesByCommunity(id int64) ([]graphstore.GraphNode, error) {
	var out []graphstore.GraphNode
	for _, node := range f.allNodes {
		if node.CommunityID == id {
			out = append(out, node)
		}
	}
	return out, f.allNodesErr
}

// The post-process writers. fakeStore embeds graphstore.Store as a NIL
// interface, so any method the engine calls and the fake does not override
// SEGFAULTS rather than failing to compile — every write the lifecycle
// performs therefore needs a body here, not only the ones a given test
// cares about.
func (f *fakeStore) NodesWithoutSignature() ([]graphstore.GraphNode, error) {
	return f.pendingSignatures, f.signatureErr
}

func (f *fakeStore) SetNodeSignature(int64, string) error { return f.signatureErr }

func (f *fakeStore) SetNodeCommunity(int64, int64) error { return f.writeErr }

func (f *fakeStore) RebuildFTS() (int, error) { return f.ftsIndexed, f.ftsErr }

func (f *fakeStore) ReplaceFlows(flows []graphstore.FlowRow, _ [][]int64) (int, error) {
	f.flows = flows
	return len(flows), f.writeErr
}

func (f *fakeStore) ReplaceCommunities(communities []graphstore.CommunityRow, _ [][]string) (int, error) {
	f.communities = communities
	return len(communities), f.writeErr
}

func (f *fakeStore) ReplaceCommunitySummaries(rows []graphstore.CommunitySummaryRow) (int, error) {
	return len(rows), f.writeErr
}

func (f *fakeStore) ReplaceFlowSnapshots(rows []graphstore.FlowSnapshotRow) (int, error) {
	return len(rows), f.writeErr
}

func (f *fakeStore) ReplaceRiskIndex(rows []graphstore.RiskIndexRow) (int, error) {
	f.risk = rows
	return len(rows), f.writeErr
}

func (f *fakeStore) ReadCommunitySummaries() ([]graphstore.CommunitySummaryRow, error) {
	return nil, f.communitiesErr
}

func (f *fakeStore) ReadFlowSnapshots() ([]graphstore.FlowSnapshotRow, error) {
	return nil, f.flowsErr
}

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
