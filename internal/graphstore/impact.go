package graphstore

// impactStoreView is the minimum slice of Store needed by
// computeImpactRadius — node lookup + edges-among query. Both
// SQLiteStore and PostgresStore satisfy this implicitly.
type impactStoreView interface {
	GetNode(qualifiedName string) (*GraphNode, error)
	GetEdgesAmong(qualifiedNames []string) ([]GraphEdge, error)
}

// computeImpactRadius runs the shared BFS + node-resolution body of
// (*SQLiteStore).GetImpactRadius and (*PostgresStore).GetImpactRadius.
// Each backend pre-loads the seed set and fwd/rev adjacency maps from
// its own driver-specific edge query, then delegates the
// driver-agnostic graph-traversal + result-assembly here.
//
// maxDepth bounds BFS hops; maxNodes caps the visited-frontier so
// extremely connected graphs don't fan out indefinitely.
func computeImpactRadius(
	seeds map[string]bool,
	fwd, rev map[string][]string,
	maxDepth, maxNodes int,
	store impactStoreView,
) (ImpactResult, error) {
	impacted := bfsImpacted(seeds, fwd, rev, maxDepth, maxNodes)

	changedNodes := resolveImpactNodes(seeds, nil, store)
	impactedNodes := resolveImpactNodes(impacted, seeds, store)

	files := uniqueImpactFiles(impactedNodes)

	edges, err := store.GetEdgesAmong(allQualifiedNames(seeds, impacted))
	if err != nil {
		return ImpactResult{}, err
	}

	return ImpactResult{
		ChangedNodes:  changedNodes,
		ImpactedNodes: impactedNodes,
		ImpactedFiles: files,
		Edges:         edges,
	}, nil
}

// bfsImpacted walks fwd/rev adjacency from the seed set, expanding hop-by-hop
// until maxDepth hops or the maxNodes cap is reached. Seeds themselves are
// not included in the returned set.
func bfsImpacted(seeds map[string]bool, fwd, rev map[string][]string, maxDepth, maxNodes int) map[string]bool {
	visited := map[string]bool{}
	frontier := make([]string, 0, len(seeds))
	for q := range seeds {
		frontier = append(frontier, q)
	}
	impacted := map[string]bool{}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		next := expandFrontier(frontier, fwd, rev, visited, impacted)
		if len(visited)+len(next) > maxNodes {
			break
		}
		frontier = next
	}
	return impacted
}

// expandFrontier marks the current frontier visited and returns the next
// frontier of unvisited neighbors (in either direction), recording each
// such neighbor in the impacted set.
func expandFrontier(frontier []string, fwd, rev map[string][]string, visited, impacted map[string]bool) []string {
	var next []string
	for _, qn := range frontier {
		visited[qn] = true
		next = appendUnvisited(next, fwd[qn], visited, impacted)
		next = appendUnvisited(next, rev[qn], visited, impacted)
	}
	return next
}

// appendUnvisited extends next with neighbors not yet visited, recording each
// in impacted.
func appendUnvisited(next, neighbors []string, visited, impacted map[string]bool) []string {
	for _, n := range neighbors {
		if !visited[n] {
			next = append(next, n)
			impacted[n] = true
		}
	}
	return next
}

// resolveImpactNodes loads node records for each qualified name in qns,
// skipping any that match the exclude set or fail to load.
func resolveImpactNodes(qns, exclude map[string]bool, store impactStoreView) []GraphNode {
	var nodes []GraphNode
	for qn := range qns {
		if exclude[qn] {
			continue
		}
		n, err := store.GetNode(qn)
		if err != nil || n == nil {
			continue
		}
		nodes = append(nodes, *n)
	}
	return nodes
}

// uniqueImpactFiles returns the deduplicated FilePath values of the given
// nodes.
func uniqueImpactFiles(nodes []GraphNode) []string {
	seen := map[string]bool{}
	for _, n := range nodes {
		seen[n.FilePath] = true
	}
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	return files
}

// edgeRowIterator is the minimal row-iterator both backends' edge query
// returns: database/sql's *sql.Rows and pgx v5's pgx.Rows each satisfy it
// (Next/Scan/Err). It lets the edge-adjacency build have one
// implementation instead of being copied per backend.
type edgeRowIterator interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// buildEdgeAdjacency consumes the rows of
// "SELECT source_qualified, target_qualified FROM edges" into forward and
// reverse adjacency maps used as input to computeImpactRadius. The caller
// owns closing rows; this only iterates and surfaces any iteration error.
func buildEdgeAdjacency(rows edgeRowIterator) (fwd, rev map[string][]string, err error) {
	fwd = map[string][]string{}
	rev = map[string][]string{}
	for rows.Next() {
		var src, tgt string
		if err = rows.Scan(&src, &tgt); err != nil {
			return nil, nil, err
		}
		fwd[src] = append(fwd[src], tgt)
		rev[tgt] = append(rev[tgt], src)
	}
	return fwd, rev, rows.Err()
}

// allQualifiedNames returns the union of seed and impacted qualified names.
func allQualifiedNames(seeds, impacted map[string]bool) []string {
	all := make([]string, 0, len(seeds)+len(impacted))
	for q := range seeds {
		all = append(all, q)
	}
	for q := range impacted {
		all = append(all, q)
	}
	return all
}
