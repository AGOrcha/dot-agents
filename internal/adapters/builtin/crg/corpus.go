package crg

import (
	"fmt"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Note-field names for the `symbol` note type. Hoisted to consts so the
// ToGraph map keys and any future query/runner code share one spelling
// (avoids S1192 literal duplication and typo drift).
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
// CRG bootstrap writes as a `symbol` note. Both the kg-native adapter and the
// crg-bridge mirror ingest the same Symbol shape so parity comparisons MATCH
// identical note shapes (§11.2).
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

// ToGraph lowers the corpus to SDK notes and edges. The note id is symbolID;
// edge endpoints resolve by qualified name to the first matching symbol id.
// Exported so the crg-bridge mirror ingests the identical note/edge shapes
// (§11.2) — the only difference between the two adapters is the namespace.
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

// Snapshot computes the structured build/status parity snapshot (O6 refinement
// A) for a corpus ingested under adapter at commit. Computed in Go over the
// corpus — never via a SQL-callable view (O6 item G rejected).
func Snapshot(adapter string, c Corpus, commit string) graphstore.ParitySnapshot {
	byKind := map[string]int{}
	byLang := map[string]int{}
	files := map[string]bool{}
	for _, sym := range c.Symbols {
		byKind[sym.Kind]++
		byLang[sym.Language]++
		files[sym.FilePath] = true
	}
	edgeKind := map[string]int{}
	for _, ref := range c.References {
		edgeKind[ref.Kind]++
	}
	return graphstore.ParitySnapshot{
		Adapter:         adapter,
		SchemaDigest:    schemaDigest(c),
		Commit:          commit,
		NodesTotal:      len(c.Symbols),
		NodesByKind:     byKind,
		NodesByLanguage: byLang,
		EdgesByKind:     edgeKind,
		Files:           len(files),
	}
}

// schemaDigest is a stable digest of the corpus's symbol shape — here the
// sorted distinct (kind, language) pairs, sufficient to detect a schema
// change between two ingestions of the same commit.
func schemaDigest(c Corpus) string {
	pairs := map[string]bool{}
	for _, sym := range c.Symbols {
		pairs[sym.Kind+":"+sym.Language] = true
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return fmt.Sprintf("%v", keys)
}

// Diff computes the structured upsert tuples (O6 refinement D) for an
// incremental update from prev to next, keyed by symbol id:
//   - a symbol absent in prev, present in next      → insert
//   - a symbol in both whose content_hash changed   → update
//   - a symbol present in prev, absent in next       → delete
//
// This is the structured replacement for the bridge's parseCRGMutationSummary
// free-text regex (crg.go:504+).
func Diff(prev, next Corpus) []graphstore.UpsertTuple {
	prevByID := indexByID(prev)
	nextByID := indexByID(next)
	var out []graphstore.UpsertTuple
	for id, sym := range nextByID {
		old, existed := prevByID[id]
		switch {
		case !existed:
			out = append(out, tuple(sym, graphstore.OpInsert))
		case old.ContentHash != sym.ContentHash:
			out = append(out, tuple(sym, graphstore.OpUpdate))
		}
	}
	for id, sym := range prevByID {
		if _, stillThere := nextByID[id]; !stillThere {
			out = append(out, tuple(sym, graphstore.OpDelete))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QualifiedName < out[j].QualifiedName })
	return out
}

// indexByID maps each symbol to its stable id.
func indexByID(c Corpus) map[string]Symbol {
	out := make(map[string]Symbol, len(c.Symbols))
	for _, sym := range c.Symbols {
		out[symbolID(sym)] = sym
	}
	return out
}

// tuple lowers a symbol + op to an upsert tuple.
func tuple(s Symbol, op graphstore.UpsertOp) graphstore.UpsertTuple {
	return graphstore.UpsertTuple{
		QualifiedName: s.QualifiedName,
		Kind:          s.Kind,
		FilePath:      s.FilePath,
		LineStart:     s.LineStart,
		Op:            op,
	}
}

// ImpactRadiusRows computes the impact radius of changedIDs over the corpus's
// reference graph up to maxDepth hops (BFS over CALLS/TESTED_BY/IMPORTS). The
// result excludes the seed ids themselves — it is the blast radius — matching
// the bridge's impact-radius tool semantics. The rows are the O6-refinement-C
// node-set oracle the §11.1 impact-radius row compares.
func ImpactRadiusRows(c Corpus, changedIDs []string, maxDepth int) []graphstore.ImpactRow {
	adj, byID := c.adjacency()
	seen := map[string]bool{}
	frontier := append([]string(nil), changedIDs...)
	for _, id := range frontier {
		seen[id] = true
	}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		frontier = expandFrontier(frontier, adj, seen)
	}
	return collectImpactRows(changedIDs, seen, byID)
}

// adjacency builds the id→neighbor adjacency and an id→symbol index from the
// corpus references.
func (c Corpus) adjacency() (map[string][]string, map[string]Symbol) {
	byID := indexByID(c)
	idByName := map[string]string{}
	for _, sym := range c.Symbols {
		if _, seen := idByName[sym.QualifiedName]; !seen {
			idByName[sym.QualifiedName] = symbolID(sym)
		}
	}
	adj := map[string][]string{}
	for _, ref := range c.References {
		from, okF := idByName[ref.From]
		to, okT := idByName[ref.To]
		if okF && okT {
			adj[from] = append(adj[from], to)
		}
	}
	return adj, byID
}

// expandFrontier returns the next BFS frontier, marking newly-seen ids.
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

// collectImpactRows turns the reached set (minus the seeds) into impact rows.
func collectImpactRows(seeds []string, seen map[string]bool, byID map[string]Symbol) []graphstore.ImpactRow {
	seedSet := map[string]bool{}
	for _, id := range seeds {
		seedSet[id] = true
	}
	var rows []graphstore.ImpactRow
	for id := range seen {
		if seedSet[id] {
			continue
		}
		sym := byID[id]
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

// SymbolID exposes the stable symbol id for callers building changed-id query
// inputs against a corpus (e.g. the parity test's impact-radius seeds).
func SymbolID(s Symbol) string { return symbolID(s) }
