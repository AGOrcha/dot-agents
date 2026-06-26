// Package graphstore is an in-memory node+edge graph that stands in for the
// real KG (the sdd-register adapter's namespace). It deliberately mirrors the
// sdk.Note / sdk.Edge shape (ID/Type/Fields ; Type/From/To) so the prototype
// exercises a REAL graph round-trip — YAML -> nodes+edges -> readback ->
// reconstruct YAML — rather than a struct that mirrors the YAML 1:1.
//
// The whole point of routing through this store is that reconstruction can ONLY
// use what the graph actually holds (node fields + edges). If a YAML field is
// not written as a node field or an edge, it is GONE at readback — which is the
// real D1' loss the mirror-struct experiment hid.
package graphstore

import "sort"

// Node is a typed graph node (the sdk.Note analogue). Fields holds whatever the
// ingest profile chose to persist — the schema-v4 profile persists a SUBSET of
// the YAML fields, which is the loss under test.
type Node struct {
	ID     string
	Type   string
	Fields map[string]any
}

// Edge is a typed directed edge between node ids (the sdk.Edge analogue).
type Edge struct {
	Type string
	From string
	To   string
}

// Store is a tiny graph: id-indexed nodes plus an edge list. Re-ingest MERGES
// nodes (by id) and REWRITES edges by (type, from), modeling the "editing the
// projection updates the graph" path — not a struct re-parse.
type Store struct {
	nodes map[string]*Node
	order []string // insertion order of node ids, for deterministic readback
	edges []Edge
}

// New returns an empty store.
func New() *Store { return &Store{nodes: map[string]*Node{}} }

// PutNode merges a node by id: an existing node's fields are REPLACED with the
// incoming fields (the re-ingest update semantics — last writer wins per node).
func (s *Store) PutNode(n Node) {
	if _, ok := s.nodes[n.ID]; !ok {
		s.order = append(s.order, n.ID)
	}
	cp := Node{ID: n.ID, Type: n.Type, Fields: map[string]any{}}
	for k, v := range n.Fields {
		cp.Fields[k] = v
	}
	s.nodes[n.ID] = &cp
}

// PutEdge appends an edge. Duplicate (type,from,to) edges are ignored so
// re-ingest is idempotent on unchanged structure.
func (s *Store) PutEdge(e Edge) {
	for _, x := range s.edges {
		if x == e {
			return
		}
	}
	s.edges = append(s.edges, e)
}

// RewriteEdgesFrom removes every edge of the given type originating at from,
// then adds the supplied replacements. This models a structural edit: when a
// task's depends_on changes, the old depends_on edges for that task are
// replaced wholesale (edge rewrite), not accumulated.
func (s *Store) RewriteEdgesFrom(edgeType, from string, replacements []Edge) {
	kept := s.edges[:0:0]
	for _, e := range s.edges {
		if e.Type == edgeType && e.From == from {
			continue
		}
		kept = append(kept, e)
	}
	s.edges = kept
	for _, e := range replacements {
		s.PutEdge(e)
	}
}

// Node returns a node by id (readback).
func (s *Store) Node(id string) (*Node, bool) {
	n, ok := s.nodes[id]
	return n, ok
}

// NodesByType returns all nodes of a type, in insertion order (deterministic
// readback — the reconstruction depends on a stable order).
func (s *Store) NodesByType(t string) []*Node {
	var out []*Node
	for _, id := range s.order {
		if n := s.nodes[id]; n.Type == t {
			out = append(out, n)
		}
	}
	return out
}

// OutEdges returns edges of a type originating at from. When edgeType is "",
// all types match. Sorted by To for deterministic reconstruction.
func (s *Store) OutEdges(edgeType, from string) []Edge {
	var out []Edge
	for _, e := range s.edges {
		if e.From == from && (edgeType == "" || e.Type == edgeType) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].To < out[j].To })
	return out
}

// Stats returns node and edge counts for reporting.
func (s *Store) Stats() (nodes, edges int) { return len(s.nodes), len(s.edges) }
