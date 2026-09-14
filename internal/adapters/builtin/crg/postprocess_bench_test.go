package crg

import (
	"fmt"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Measure-first baselines for the derived-view computations. Each benchmark
// runs a stage over a synthetic graph of the given size, passed as the same
// []GraphNode / []GraphEdge slices the lifecycle loads once and reuses — so
// what is measured is the algorithm, not the store readback.
//
// The synthetic graph is shaped to make every stage do real work: a forest of
// small CALLS binary trees whose roots are flow entry points, cross-block
// IMPORTS_FROM edges, CONTAINS edges from a File node per block (which is also
// what exercises the entry-point detector's File-source filter), and a few
// TESTED_BY edges so the coverage terms are fractions rather than constants.
// Nodes are spread over one directory per block, which is what gives community
// detection more than one group to find.

const benchBlockSize = 16

// benchScales is the node-count sweep the derivations parametrize over.
var benchScales = []struct {
	name string
	n    int
}{
	{"N=100", 100},
	{"N=500", 500},
	{"N=2000", 2000},
}

// benchGraph builds a synthetic graph of n code nodes plus one File node per
// block.
func benchGraph(n int) ([]graphstore.GraphNode, []graphstore.GraphEdge) {
	var nodes []graphstore.GraphNode
	var edges []graphstore.GraphEdge
	nextNodeID := int64(0)
	nextEdgeID := int64(0)
	addEdge := func(kind, source, target, file string) {
		nextEdgeID++
		edges = append(edges, graphstore.GraphEdge{
			ID: nextEdgeID, Kind: kind, SourceQualified: source,
			TargetQualified: target, FilePath: file, Confidence: 1.0,
			ConfidenceTier: "EXTRACTED",
		})
	}

	// One File node per block, so blocks land in distinct directories.
	fileOf := func(i int) string {
		return fmt.Sprintf("repo/blk%d/f.go", i/benchBlockSize)
	}
	for block := 0; block*benchBlockSize < n; block++ {
		path := fileOf(block * benchBlockSize)
		nextNodeID++
		nodes = append(nodes, graphstore.GraphNode{
			ID: nextNodeID, Kind: graphstore.NodeKindFile, Name: path,
			QualifiedName: path, FilePath: path, Language: "go",
		})
	}

	qualified := make([]string, n)
	for i := range n {
		path := fileOf(i)
		name := fmt.Sprintf("sym%d", i)
		qualified[i] = path + "::" + name
		nextNodeID++
		nodes = append(nodes, graphstore.GraphNode{
			ID: nextNodeID, Kind: graphstore.NodeKindFunction, Name: name,
			QualifiedName: qualified[i], FilePath: path, Language: "go",
			Params: "()", Signature: "def " + name + "(())",
		})
		addEdge(graphstore.EdgeKindContains, path, qualified[i], path)
	}

	for i := range n {
		local := i % benchBlockSize
		if local == 0 {
			continue // block root: a flow entry point, nothing calls it
		}
		parent := i - local + (local-1)/2
		addEdge(graphstore.EdgeKindCalls, qualified[parent], qualified[i], fileOf(parent))
	}
	for i := 0; i+benchBlockSize < n; i += benchBlockSize {
		addEdge(graphstore.EdgeKindImportsFrom, fileOf(i), fileOf(i+benchBlockSize), fileOf(i))
		if i+2 < n {
			addEdge(graphstore.EdgeKindTestedBy, qualified[i+1], qualified[i+2], fileOf(i+1))
		}
	}
	return nodes, edges
}

func BenchmarkTraceFlows(b *testing.B) {
	for _, sc := range benchScales {
		nodes, edges := benchGraph(sc.n)
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if flows, _ := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false); len(flows) == 0 {
					b.Fatal("benchmark graph produced no flows")
				}
			}
		})
	}
}

func BenchmarkDetectCommunities(b *testing.B) {
	for _, sc := range benchScales {
		nodes, edges := benchGraph(sc.n)
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if rows, _ := DetectCommunities(nodes, edges, DefaultCommunityMinSize); len(rows) == 0 {
					b.Fatal("benchmark graph produced no communities")
				}
			}
		})
	}
}

func BenchmarkComputeSummaries(b *testing.B) {
	for _, sc := range benchScales {
		nodes, edges := benchGraph(sc.n)
		// Summaries are keyed by stored flow and community ids, so the
		// benchmark assigns them the way the store would.
		flows, paths := TraceFlows(nodes, edges, DefaultFlowMaxDepth, false)
		for i := range flows {
			flows[i].ID = int64(i + 1)
			flows[i].PathJSON = benchPathJSON(paths[i])
		}
		communities, members := DetectCommunities(nodes, edges, DefaultCommunityMinSize)
		assigned := assignCommunityIDs(nodes, communities, members)
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				s := ComputeSummaries(assigned, edges, flows, communities, "stamp")
				if len(s.RiskIndex) == 0 {
					b.Fatal("benchmark graph produced no risk rows")
				}
			}
		})
	}
}

func BenchmarkResolveBareCallTargets(b *testing.B) {
	for _, sc := range benchScales {
		nodes, edges := benchGraph(sc.n)
		// Give every block root one bare call, which is the shape the pass
		// actually has to decide on.
		bare := append([]graphstore.GraphEdge(nil), edges...)
		nextID := int64(len(edges))
		for i := 0; i < sc.n; i += benchBlockSize {
			nextID++
			bare = append(bare, graphstore.GraphEdge{
				ID: nextID, Kind: graphstore.EdgeKindCalls,
				SourceQualified: nodes[len(nodes)-1].QualifiedName,
				TargetQualified: fmt.Sprintf("sym%d", i),
				FilePath:        nodes[len(nodes)-1].FilePath,
			})
		}
		b.Run(sc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				ResolveBareCallTargets(nodes, bare)
			}
		})
	}
}

// benchPathJSON renders a node-id path the way the store stores it.
func benchPathJSON(path []int64) string {
	out := "["
	for i, id := range path {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprint(id)
	}
	return out + "]"
}

// assignCommunityIDs mirrors what the store's ReplaceCommunities does to
// nodes.community_id, so ComputeSummaries sees the input it would see in
// production. Community ids are 1-based, matching AUTOINCREMENT.
func assignCommunityIDs(
	nodes []graphstore.GraphNode,
	communities []graphstore.CommunityRow,
	members [][]string,
) []graphstore.GraphNode {
	idByQN := make(map[string]int64)
	for i := range communities {
		communities[i].ID = int64(i + 1)
		for _, qn := range members[i] {
			idByQN[qn] = communities[i].ID
		}
	}
	out := append([]graphstore.GraphNode(nil), nodes...)
	for i := range out {
		out[i].CommunityID = idByQN[out[i].QualifiedName]
	}
	return out
}
