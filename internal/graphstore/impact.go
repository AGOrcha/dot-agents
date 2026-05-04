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
	visited := map[string]bool{}
	frontier := make([]string, 0, len(seeds))
	for q := range seeds {
		frontier = append(frontier, q)
	}

	impacted := map[string]bool{}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, qn := range frontier {
			visited[qn] = true
			for _, neighbor := range fwd[qn] {
				if !visited[neighbor] {
					next = append(next, neighbor)
					impacted[neighbor] = true
				}
			}
			for _, pred := range rev[qn] {
				if !visited[pred] {
					next = append(next, pred)
					impacted[pred] = true
				}
			}
		}
		if len(visited)+len(next) > maxNodes {
			break
		}
		frontier = next
	}

	var changedNodes []GraphNode
	for qn := range seeds {
		n, err := store.GetNode(qn)
		if err != nil || n == nil {
			continue
		}
		changedNodes = append(changedNodes, *n)
	}

	var impactedNodes []GraphNode
	for qn := range impacted {
		if seeds[qn] {
			continue
		}
		n, err := store.GetNode(qn)
		if err != nil || n == nil {
			continue
		}
		impactedNodes = append(impactedNodes, *n)
	}

	impactedFiles := map[string]bool{}
	for _, n := range impactedNodes {
		impactedFiles[n.FilePath] = true
	}
	var files []string
	for f := range impactedFiles {
		files = append(files, f)
	}

	all := make([]string, 0, len(seeds)+len(impacted))
	for q := range seeds {
		all = append(all, q)
	}
	for q := range impacted {
		all = append(all, q)
	}
	edges, err := store.GetEdgesAmong(all)
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
