package crg

import (
	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Note-field names for the `symbol` note type. Hoisted to consts so the
// ToGraph map keys and the readback accessors share one spelling (avoids S1192
// literal duplication and typo drift).
const (
	noteTypeSymbol  = "symbol"
	fieldQualified  = "qualified_name"
	fieldKind       = "kind"
	fieldLanguage   = "language"
	fieldFilePath   = "file_path"
	fieldLineStart  = "line_start"
	fieldContentSum = "content_hash"
)

// Symbol is one ingested code symbol — the normalized Tree-sitter output the
// CRG bootstrap writes as a `symbol` note. It is INGESTION INPUT only: parity
// snapshots are computed from the readback (readback.go), never from this.
type Symbol struct {
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"` // Function | Type (nodes.kind anchor)
	Language      string `json:"language"`
	FilePath      string `json:"file_path"`
	LineStart     int    `json:"line_start"`
	ContentHash   string `json:"content_hash"`
}

// Reference is one typed edge between two symbols (by qualified name). Kind is
// one of CALLS, TESTED_BY, IMPORTS — the edges.kind parity anchor.
type Reference struct {
	Kind string `json:"kind"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Corpus is a snapshot of a repo's symbols + references at one commit — the
// normalized ingestion input. Pinned corpora live under testdata/crg-parity/.
type Corpus struct {
	Commit     string      `json:"commit"`
	Symbols    []Symbol    `json:"symbols"`
	References []Reference `json:"references"`
}

// symbolID is the stable note id for a symbol: qualified name plus file path,
// so two symbols with the same name in different files are distinct nodes.
func symbolID(s Symbol) string {
	return s.QualifiedName + "@" + s.FilePath
}

// SymbolID exposes the stable symbol id for callers building changed-id query
// inputs (e.g. the parity test's impact-radius seeds).
func SymbolID(s Symbol) string { return symbolID(s) }

// ToGraph lowers the corpus to SDK notes and edges for ingestion. The note id
// is symbolID; edge endpoints resolve by qualified name to the first matching
// symbol id, and a reference whose endpoints are not both present is DROPPED
// (it is not a real edge). Because the dropped edges never reach the store, the
// readback-based snapshot will not count them — which is exactly why parity is
// computed from the readback, not from Corpus arithmetic.
func (c Corpus) ToGraph() ([]sdk.Note, []sdk.Edge) {
	idByName := make(map[string]string, len(c.Symbols))
	notes := make([]sdk.Note, 0, len(c.Symbols))
	for _, sym := range c.Symbols {
		id := symbolID(sym)
		if _, seen := idByName[sym.QualifiedName]; !seen {
			idByName[sym.QualifiedName] = id
		}
		notes = append(notes, sdk.Note{
			ID:   id,
			Type: noteTypeSymbol,
			Fields: map[string]any{
				fieldQualified:  sym.QualifiedName,
				fieldKind:       sym.Kind,
				fieldLanguage:   sym.Language,
				fieldFilePath:   sym.FilePath,
				fieldLineStart:  sym.LineStart,
				fieldContentSum: sym.ContentHash,
			},
		})
	}
	edges := make([]sdk.Edge, 0, len(c.References))
	for _, ref := range c.References {
		from, okF := idByName[ref.From]
		to, okT := idByName[ref.To]
		if !okF || !okT {
			continue // dangling reference — not ingested as an edge
		}
		edges = append(edges, sdk.Edge{Type: ref.Kind, From: from, To: to})
	}
	return notes, edges
}

// tuple lowers a symbol + op to an upsert tuple (used by the readback diff).
func tuple(s Symbol, op graphstore.UpsertOp) graphstore.UpsertTuple {
	return graphstore.UpsertTuple{
		QualifiedName: s.QualifiedName,
		Kind:          s.Kind,
		FilePath:      s.FilePath,
		LineStart:     s.LineStart,
		Op:            op,
	}
}

// expandFrontier returns the next BFS frontier, marking newly-seen ids. Shared
// by the readback impact-radius computation.
func expandFrontier(frontier []string, adj map[string][]string, seen map[string]bool) []string {
	var next []string
	for _, id := range frontier {
		for _, nb := range adj[id] {
			if !seen[nb] {
				seen[nb] = true
				next = append(next, nb)
			}
		}
	}
	return next
}
