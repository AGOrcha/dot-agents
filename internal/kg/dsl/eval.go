package dsl

import (
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// Eval executes a parsed query against one namespace's notes+edges and the
// caller's params, producing result rows. This is the runtime that makes the
// hand-written dogfood runners (the t3 #168 SDK Query stand-ins) expressible
// declaratively: a compiled *Query plus an sdk.NamespaceView yields the same
// rows the bespoke Go runners did.
//
// Eval implements the v1 semantics the conformance catalog pins: ref-join
// resolution (§5.4.1), OPTIONAL+WHERE lowering (§5.4.2), variable-length BFS
// with hop_count (§5.1), and the stale-tag selectors (§7.3). It operates over
// the in-memory NamespaceView the SDK hands a query runner, so it needs no SQL
// backend — the lowering rules are honored by construction, not by emitting SQL.
func Eval(q *Query, view sdk.NamespaceView, params map[string]any) ([]sdk.Row, error) {
	ev := &evaluator{q: q, params: params, byID: indexNotes(view.Notes)}
	ev.indexEdges(view.Edges)
	bindings, err := ev.buildBindings()
	if err != nil {
		return nil, err
	}
	return ev.project(bindings)
}

// evaluator holds the per-call evaluation state: the parsed query, the bound
// params, and note/edge indexes built once per Eval.
type evaluator struct {
	q      *Query
	params map[string]any
	byID   map[string]sdk.Note
	// outEdges maps "type|from" → []to for forward edge traversal.
	outEdges map[string][]string
	// inEdges maps "type|to" → []from for reverse traversal.
	inEdges map[string][]string
	// hopCount carries the variable-length hop count for the current binding
	// row (RETURN hop_count reads it).
	hopCount map[string]int
}

func indexNotes(notes []sdk.Note) map[string]sdk.Note {
	m := make(map[string]sdk.Note, len(notes))
	for _, n := range notes {
		m[n.ID] = n
	}
	return m
}

// indexEdges builds the forward/reverse adjacency maps keyed by "type|node".
func (ev *evaluator) indexEdges(edges []sdk.Edge) {
	ev.outEdges = map[string][]string{}
	ev.inEdges = map[string][]string{}
	for _, e := range edges {
		ev.outEdges[e.Type+"|"+e.From] = append(ev.outEdges[e.Type+"|"+e.From], e.To)
		ev.inEdges[e.Type+"|"+e.To] = append(ev.inEdges[e.Type+"|"+e.To], e.From)
	}
}

// binding is one row of alias→note assignments under construction. Missing keys
// model unmatched OPTIONAL aliases (NULL).
type binding map[string]*sdk.Note

// buildBindings produces the joined alias rows after applying the MATCH chain
// and the WHERE filter (with §5.4.2 lowering folded into the join step).
func (ev *evaluator) buildBindings() ([]binding, error) {
	ev.hopCount = map[string]int{}
	if len(ev.q.Matches) == 0 {
		return []binding{{}}, nil // RETURN-only query (e.g. the none adapter)
	}
	rows := ev.seedRows(ev.q.Matches[0])
	for _, m := range ev.q.Matches[1:] {
		next, err := ev.applyMatch(rows, m)
		if err != nil {
			return nil, err
		}
		rows = next
	}
	return ev.applyWhere(rows)
}

// seedRows produces the initial bindings from the first MATCH clause.
func (ev *evaluator) seedRows(m MatchClause) []binding {
	notes := ev.notesOfTypeSlice(m.Nodes[0].Type)
	rows := make([]binding, 0, len(notes))
	for i := range notes {
		n := notes[i]
		rows = append(rows, binding{m.Nodes[0].Alias: &n})
	}
	if m.Edge != nil {
		out, _ := ev.applyMatch(rows, m)
		return out
	}
	return rows
}

// notesOfTypeSlice returns all notes of a given type as a fresh slice (so each
// binding holds a distinct pointer).
func (ev *evaluator) notesOfTypeSlice(typ string) []sdk.Note {
	var out []sdk.Note
	for _, n := range ev.byID {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

// noteByID returns a copy of the note with id, if present.
func (ev *evaluator) noteByID(id string) (sdk.Note, bool) {
	n, ok := ev.byID[id]
	return n, ok
}
