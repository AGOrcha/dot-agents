package crg

import (
	"fmt"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// StoreReader is the read-only slice of the Store seam the parity surfaces are
// computed from. Both adapters' parity snapshots are derived by READING BACK
// the actually-ingested notes/edges from a namespace through this seam — never
// from the input corpus. That is what makes the parity test verify equivalence
// rather than guarantee it by construction: if an adapter drops a dangling
// edge on write, or writes a different shape, the readback reflects it and the
// comparison can fail. sdk.MemStore satisfies this interface.
type StoreReader interface {
	Notes(token sdk.Token, ns string) ([]sdk.Note, error)
	Edges(token sdk.Token, ns string) ([]sdk.Edge, error)
}

// readNamespace reads the notes and edges actually persisted in ns, using an
// own-read token for ns (§8.2: an adapter may read its own namespace back).
func readNamespace(store StoreReader, ns string) ([]sdk.Note, []sdk.Edge, error) {
	tok := sdk.OwnReadToken(ns, "parity-snapshot")
	notes, err := store.Notes(tok, ns)
	if err != nil {
		return nil, nil, fmt.Errorf("crg: read notes from %q: %w", ns, err)
	}
	edges, err := store.Edges(tok, ns)
	if err != nil {
		return nil, nil, fmt.Errorf("crg: read edges from %q: %w", ns, err)
	}
	return notes, edges, nil
}

// SnapshotFromStore builds the structured build/status parity snapshot (O6
// refinement A) by READING BACK what was actually ingested into ns through the
// Store seam — the persisted notes and edges, not the input corpus. EdgesByKind
// counts only edges that were actually written (dangling references dropped at
// write time do NOT appear), so the snapshot reflects storage reality.
func SnapshotFromStore(adapter string, store StoreReader, ns, commit string) (graphstore.ParitySnapshot, error) {
	notes, edges, err := readNamespace(store, ns)
	if err != nil {
		return graphstore.ParitySnapshot{}, err
	}
	return snapshotFromGraph(adapter, notes, edges, commit), nil
}

// snapshotFromGraph computes the snapshot anchor columns from persisted notes
// and edges (the readback). It is pure over its inputs.
func snapshotFromGraph(adapter string, notes []sdk.Note, edges []sdk.Edge, commit string) graphstore.ParitySnapshot {
	byKind := map[string]int{}
	byLang := map[string]int{}
	files := map[string]bool{}
	for _, n := range notes {
		byKind[fieldString(n, fieldKind)]++
		byLang[fieldString(n, fieldLanguage)]++
		files[fieldString(n, fieldFilePath)] = true
	}
	edgeKind := map[string]int{}
	for _, e := range edges {
		edgeKind[e.Type]++
	}
	return graphstore.ParitySnapshot{
		Adapter:         adapter,
		SchemaDigest:    digestFromNotes(notes),
		Commit:          commit,
		NodesTotal:      len(notes),
		NodesByKind:     byKind,
		NodesByLanguage: byLang,
		EdgesByKind:     edgeKind,
		Files:           len(files),
	}
}

// fieldString reads a string field off a persisted note, empty if absent.
func fieldString(n sdk.Note, key string) string {
	if v, ok := n.Fields[key].(string); ok {
		return v
	}
	return ""
}

// digestFromNotes is the schema digest computed from persisted notes: the
// sorted distinct (kind, language) pairs actually present in storage.
func digestFromNotes(notes []sdk.Note) string {
	pairs := map[string]bool{}
	for _, n := range notes {
		pairs[fieldString(n, fieldKind)+":"+fieldString(n, fieldLanguage)] = true
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Sprintf("%v", keys)
}

// symbolFromNote reconstructs a Symbol from a persisted note (the readback
// inverse of ToGraph). Used by the store-backed upsert and impact oracles.
func symbolFromNote(n sdk.Note) Symbol {
	line, _ := n.Fields[fieldLineStart].(int)
	return Symbol{
		QualifiedName: fieldString(n, fieldQualified),
		Kind:          fieldString(n, fieldKind),
		Language:      fieldString(n, fieldLanguage),
		FilePath:      fieldString(n, fieldFilePath),
		LineStart:     line,
		ContentHash:   fieldString(n, fieldContentSum),
	}
}

// DiffFromStore computes the structured upsert tuples (O6 refinement D) between
// two persisted namespace states read back through the Store seam. prevNotes is
// the previous commit's persisted notes; the current state is read from ns.
// This compares STORAGE state to STORAGE state, not corpus to corpus.
func DiffFromStore(prevNotes []sdk.Note, store StoreReader, ns string) ([]graphstore.UpsertTuple, error) {
	curNotes, _, err := readNamespace(store, ns)
	if err != nil {
		return nil, err
	}
	return diffNotes(prevNotes, curNotes), nil
}

// diffNotes computes insert/update/delete upsert tuples between two persisted
// note sets, keyed by note id.
func diffNotes(prev, cur []sdk.Note) []graphstore.UpsertTuple {
	prevByID := notesByID(prev)
	curByID := notesByID(cur)
	var out []graphstore.UpsertTuple
	for id, n := range curByID {
		old, existed := prevByID[id]
		switch {
		case !existed:
			out = append(out, tuple(symbolFromNote(n), graphstore.OpInsert))
		case fieldString(old, fieldContentSum) != fieldString(n, fieldContentSum):
			out = append(out, tuple(symbolFromNote(n), graphstore.OpUpdate))
		}
	}
	for id, n := range prevByID {
		if _, stillThere := curByID[id]; !stillThere {
			out = append(out, tuple(symbolFromNote(n), graphstore.OpDelete))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QualifiedName < out[j].QualifiedName })
	return out
}

// notesByID indexes persisted notes by their note id.
func notesByID(notes []sdk.Note) map[string]sdk.Note {
	out := make(map[string]sdk.Note, len(notes))
	for _, n := range notes {
		out[n.ID] = n
	}
	return out
}

// ImpactRadiusFromStore computes the impact radius of changedIDs over the
// persisted edge graph read back from ns, up to maxDepth hops. It expands over
// the edges ACTUALLY WRITTEN to storage (dangling references already dropped),
// so the impact-radius parity row compares storage-derived node sets.
func ImpactRadiusFromStore(store StoreReader, ns string, changedIDs []string, maxDepth int) ([]graphstore.ImpactRow, error) {
	notes, edges, err := readNamespace(store, ns)
	if err != nil {
		return nil, err
	}
	return impactFromGraph(notes, edges, changedIDs, maxDepth), nil
}

// impactFromGraph runs the BFS impact radius over persisted notes/edges.
func impactFromGraph(notes []sdk.Note, edges []sdk.Edge, changedIDs []string, maxDepth int) []graphstore.ImpactRow {
	byID := notesByID(notes)
	adj := map[string][]string{}
	for _, e := range edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	seen := map[string]bool{}
	frontier := append([]string(nil), changedIDs...)
	for _, id := range frontier {
		seen[id] = true
	}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		frontier = expandFrontier(frontier, adj, seen)
	}
	return collectImpactRowsFromNotes(changedIDs, seen, byID)
}

// collectImpactRowsFromNotes turns the reached set (minus seeds) into impact
// rows using persisted note fields.
func collectImpactRowsFromNotes(seeds []string, seen map[string]bool, byID map[string]sdk.Note) []graphstore.ImpactRow {
	seedSet := map[string]bool{}
	for _, id := range seeds {
		seedSet[id] = true
	}
	var rows []graphstore.ImpactRow
	for id := range seen {
		if seedSet[id] {
			continue
		}
		sym := symbolFromNote(byID[id])
		rows = append(rows, graphstore.ImpactRow{
			NodeID:        id,
			Kind:          sym.Kind,
			QualifiedName: sym.QualifiedName,
			FilePath:      sym.FilePath,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].NodeID < rows[j].NodeID })
	return rows
}
