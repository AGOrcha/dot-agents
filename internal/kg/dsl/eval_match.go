package dsl

import "github.com/AGOrcha/dot-agents/internal/adapters/sdk"

// joinKind classifies an edge MATCH by which endpoints are already bound.
type joinKind int

const (
	// joinForward: source (Nodes[0]) bound, end (Nodes[1]) new — extend forward.
	joinForward joinKind = iota
	// joinReverse: end (Nodes[1]) bound, source (Nodes[0]) new — walk INTO the
	// anchor (the §13.2 `(f:finding)-[:affects]->(changed)` shape).
	joinReverse
	// joinBoth: both endpoints already bound — FILTER the row, keeping it only
	// when an edge actually connects the two bound notes (do NOT overwrite).
	joinBoth
)

// applyMatch joins an edge MATCH clause onto the existing rows. The clause's
// pattern is `Nodes[0]-[edge]->Nodes[1]`. Three cases are distinguished by which
// endpoints are already bound (classifyJoin): forward (extend from a bound
// source), reverse (walk into a bound end), and both-bound (filter pre-bound
// pairs by edge existence). OPTIONAL clauses preserve anchor rows with a NULL
// new alias (LEFT JOIN, §5.4.2).
func (ev *evaluator) applyMatch(rows []binding, m MatchClause) ([]binding, error) {
	if m.Edge == nil {
		return ev.applyNodeMatch(rows, m)
	}
	switch ev.classifyJoin(rows, m) {
	case joinReverse:
		return ev.applyReverseMatch(rows, m), nil
	case joinBoth:
		return ev.applyBothBoundMatch(rows, m), nil
	default:
		return ev.applyForwardMatch(rows, m), nil
	}
}

// classifyJoin determines which endpoints of the edge clause are already bound.
func (ev *evaluator) classifyJoin(rows []binding, m MatchClause) joinKind {
	if len(rows) == 0 {
		return joinForward
	}
	_, srcBound := rows[0][m.Nodes[0].Alias]
	_, endBound := rows[0][m.Nodes[1].Alias]
	switch {
	case srcBound && endBound:
		return joinBoth
	case endBound && !srcBound:
		return joinReverse
	default:
		return joinForward
	}
}

// applyForwardMatch extends each row from its bound source (Nodes[0]) to the new
// end alias (Nodes[1]).
func (ev *evaluator) applyForwardMatch(rows []binding, m MatchClause) []binding {
	src, end := m.Nodes[0].Alias, m.Nodes[1].Alias
	endType := m.Nodes[1].Type
	var out []binding
	for _, row := range rows {
		out = ev.extendRow(out, row, m, src, end, endType)
	}
	return out
}

// applyBothBoundMatch filters rows where both endpoints are already bound,
// keeping a row only when an edge of the clause's type connects the bound source
// to the bound end. It NEVER rebinds either alias — it is a pure predicate on
// the existing pair. A variable-length pattern uses reachability within bounds;
// a fixed edge requires a direct edge. OPTIONAL keeps rows that fail (the join
// is satisfied as "no edge required"), matching LEFT JOIN semantics where an
// absent edge does not drop the source row.
func (ev *evaluator) applyBothBoundMatch(rows []binding, m MatchClause) []binding {
	srcAlias, endAlias := m.Nodes[0].Alias, m.Nodes[1].Alias
	var out []binding
	for _, row := range rows {
		if ev.pairConnected(row[srcAlias], row[endAlias], m.Edge) || m.Optional {
			out = append(out, row)
		}
	}
	return out
}

// pairConnected reports whether an edge of the clause's type connects src→end.
// A NULL endpoint is never connected. Fixed edges require a direct hop;
// variable-length edges require reachability within [VarMin, VarMax].
func (ev *evaluator) pairConnected(src, end *sdk.Note, edge *EdgePattern) bool {
	if src == nil || end == nil {
		return false
	}
	if !edge.IsVarLength() {
		for _, to := range ev.outEdges[edge.Type+"|"+src.ID] {
			if to == end.ID {
				return true
			}
		}
		return false
	}
	hops, ok := ev.bfsMinHops(edge.Type, src.ID, edge.VarMax)[end.ID]
	return ok && hops >= edge.VarMin
}

// applyReverseMatch joins a new source alias (Nodes[0]) onto a bound end alias
// (Nodes[1]) by walking edges INTO the anchor. It mirrors the forward join's
// OPTIONAL/required handling.
func (ev *evaluator) applyReverseMatch(rows []binding, m MatchClause) []binding {
	srcType, srcAlias := m.Nodes[0].Type, m.Nodes[0].Alias
	endAlias := m.Nodes[1].Alias
	var out []binding
	for _, row := range rows {
		out = ev.extendReverseRow(out, row, m, srcType, srcAlias, endAlias)
	}
	return out
}

// extendReverseRow appends the rows for one anchor binding in a reverse join:
// each source node whose edge points at the anchor end node binds srcAlias.
func (ev *evaluator) extendReverseRow(out []binding, row binding, m MatchClause, srcType, srcAlias, endAlias string) []binding {
	endNote := row[endAlias]
	if endNote == nil {
		return append(out, row)
	}
	sources := ev.reverseSources(m.Edge.Type, endNote.ID, srcType)
	if len(sources) == 0 && m.Optional {
		return append(out, cloneWith(row, srcAlias, nil))
	}
	for i := range sources {
		n := sources[i]
		out = append(out, cloneWith(row, srcAlias, &n))
	}
	return out
}

// reverseSources returns the source-typed nodes whose edge of edgeType points
// at endID (the in-neighbors of the anchor).
func (ev *evaluator) reverseSources(edgeType, endID, srcType string) []sdk.Note {
	var out []sdk.Note
	for _, from := range ev.inEdges[edgeType+"|"+endID] {
		if n, ok := ev.noteByID(from); ok && (srcType == "" || n.Type == srcType) {
			out = append(out, n)
		}
	}
	return out
}

// applyNodeMatch handles a bare (edgeless) additional MATCH: a cross-product
// against all notes of the clause's type. Used when a query MATCHes a second
// independent node set (e.g. the §13.5 concept clause).
func (ev *evaluator) applyNodeMatch(rows []binding, m MatchClause) ([]binding, error) {
	alias, typ := m.Nodes[0].Alias, m.Nodes[0].Type
	notes := ev.notesOfTypeSlice(typ)
	var out []binding
	for _, row := range rows {
		for i := range notes {
			n := notes[i]
			out = append(out, cloneWith(row, alias, &n))
		}
	}
	return out, nil
}

// extendRow appends the joined rows for one source binding. For variable-length
// edges it runs a BFS recording hop_count; for fixed edges it joins each
// neighbor. OPTIONAL clauses with no neighbor keep the source row with a NULL
// end alias.
func (ev *evaluator) extendRow(out []binding, row binding, m MatchClause, src, end, endType string) []binding {
	srcNote := row[src]
	if srcNote == nil {
		return append(out, row) // source already NULL; nothing to join
	}
	neighbors := ev.neighbors(m.Edge, srcNote.ID, endType)
	if len(neighbors) == 0 && m.Optional {
		return append(out, cloneWith(row, end, nil))
	}
	for _, hit := range neighbors {
		n := hit.note
		joined := cloneWith(row, end, &n)
		if m.Edge.IsVarLength() {
			ev.hopCount[bindingKey(joined)] = hit.hops
		}
		out = append(out, joined)
	}
	return out
}

// neighborHit is one reachable end node and the hop count to reach it.
type neighborHit struct {
	note sdk.Note
	hops int
}

// neighbors returns the end-typed nodes reachable from srcID via the edge
// pattern. Fixed edges yield direct out-neighbors at hop 1; variable-length
// edges yield BFS-reachable nodes with their minimum hop count (§5.1, no
// paths-as-objects — only end node + hop_count).
func (ev *evaluator) neighbors(edge *EdgePattern, srcID, endType string) []neighborHit {
	if !edge.IsVarLength() {
		return ev.fixedNeighbors(edge.Type, srcID, endType)
	}
	return ev.varNeighbors(edge, srcID, endType)
}

// fixedNeighbors returns direct out-neighbors of the declared end type.
func (ev *evaluator) fixedNeighbors(edgeType, srcID, endType string) []neighborHit {
	var out []neighborHit
	for _, to := range ev.outEdges[edgeType+"|"+srcID] {
		if n, ok := ev.noteByID(to); ok && (endType == "" || n.Type == endType) {
			out = append(out, neighborHit{note: n, hops: 1})
		}
	}
	return out
}

// varNeighbors runs a bounded BFS over the edge type, returning each reachable
// end-typed node with its minimum hop count within [VarMin, VarMax].
func (ev *evaluator) varNeighbors(edge *EdgePattern, srcID, endType string) []neighborHit {
	best := ev.bfsMinHops(edge.Type, srcID, edge.VarMax)
	var out []neighborHit
	for id, hops := range best {
		if hops < edge.VarMin {
			continue
		}
		if n, ok := ev.noteByID(id); ok && (endType == "" || n.Type == endType) {
			out = append(out, neighborHit{note: n, hops: hops})
		}
	}
	return out
}

// bfsMinHops computes the minimum hop count from start to every node reachable
// within maxHops along the given edge type.
func (ev *evaluator) bfsMinHops(edgeType, start string, maxHops int) map[string]int {
	type qn struct {
		id  string
		hop int
	}
	best := map[string]int{}
	queue := []qn{{start, 0}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.hop >= maxHops {
			continue
		}
		for _, nb := range ev.outEdges[edgeType+"|"+cur.id] {
			h := cur.hop + 1
			if prev, ok := best[nb]; !ok || h < prev {
				best[nb] = h
				queue = append(queue, qn{nb, h})
			}
		}
	}
	return best
}

// cloneWith returns a copy of row with alias set to note (note may be nil for a
// NULL/optional binding).
func cloneWith(row binding, alias string, note *sdk.Note) binding {
	out := make(binding, len(row)+1)
	for k, v := range row {
		out[k] = v
	}
	out[alias] = note
	return out
}

// bindingKey produces a stable identity for a binding so hopCount can be keyed
// per joined row. It concatenates each non-nil alias→id pair in alias order.
func bindingKey(b binding) string {
	keys := sortedAliases(b)
	var s string
	for _, a := range keys {
		if b[a] != nil {
			s += a + "=" + b[a].ID + ";"
		}
	}
	return s
}
