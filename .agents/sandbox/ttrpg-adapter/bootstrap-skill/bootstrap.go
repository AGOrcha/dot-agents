// Package bootstrap is the TTRPG campaign bootstrap skill's Go core
// (graph-backend-adapter-contract §8.4, §13.3). It parses a corpus of session
// logs and writes the resulting notes/edges into scoped KG storage using the
// `da-adapter-sdk` EXCLUSIVELY — it never opens a DB connection itself (§8.2:
// direct DB connections are forbidden by contract).
//
// In production this code is the executable payload of the Tier 2 OCI package
// `ttrpg-campaign-bootstrap` (§9), invoked by `da` inside the skill-execution
// sandbox with an SDK handle bound to the ttrpg namespace. The same Run entry
// point drives the dogfood hard test (ttrpg_dogfood_test.go), which is why the
// parsing/upsert logic lives in an importable package rather than a main.
package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
)

// sessionLog is the on-disk shape of one corpus file (corpus/README.md).
type sessionLog struct {
	Session struct {
		Number   int    `json:"number"`
		Title    string `json:"title"`
		PlayedAt string `json:"played_at"`
	} `json:"session"`
	Notes []rawNote `json:"notes"`
	Edges []rawEdge `json:"edges"`
}

type rawNote struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Fields map[string]any `json:"fields"`
}

type rawEdge struct {
	Type string `json:"type"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Result is what Run reports: the distinct note/edge counts that landed in the
// graph. The hard test asserts these against oracle.yaml.
type Result struct {
	SessionsParsed int
	NoteCount      int            // distinct note ids
	EdgeCount      int            // distinct (type, from, to) edges
	NotesByType    map[string]int // distinct ids per note type
	EdgesByType    map[string]int // distinct edges per edge type
}

// Run parses every session-*.json under corpusDir and writes the campaign into
// the adapter namespace via the SDK. Notes are upserted idempotently by id
// (last writer wins on fields), modeling persistent campaign entities that
// evolve across sessions. Each session becomes a `session` note, and every
// note/edge a session introduces is linked back to it via a `documents` edge
// (provenance anchor, §7.3 derivation source).
func Run(s *sdk.SDK, corpusDir string) (Result, error) {
	files, err := filepath.Glob(filepath.Join(corpusDir, "session-*.json"))
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: glob corpus: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return Result{}, fmt.Errorf("bootstrap: no session logs under %s", corpusDir)
	}

	// Upsert by id: notes keyed by id (last writer wins); edges deduped by the
	// (type, from, to) triple. session notes + documents edges are synthesized.
	notes := map[string]sdk.Note{}
	edges := map[string]sdk.Edge{}

	for _, f := range files {
		log, err := parseSession(f)
		if err != nil {
			return Result{}, err
		}
		sessionID := fmt.Sprintf("session:%d", log.Session.Number)
		notes[sessionID] = sdk.Note{
			ID:   sessionID,
			Type: "session",
			Fields: map[string]any{
				"number":    log.Session.Number,
				"title":     log.Session.Title,
				"played_at": log.Session.PlayedAt,
			},
		}
		for _, n := range log.Notes {
			notes[n.ID] = sdk.Note{ID: n.ID, Type: n.Type, Fields: n.Fields}
			// Provenance: this session documents every event it introduces.
			if n.Type == "event" {
				e := sdk.Edge{Type: "documents", From: sessionID, To: n.ID}
				edges[edgeKey(e)] = e
			}
		}
		for _, e := range log.Edges {
			edge := sdk.Edge{Type: e.Type, From: e.From, To: e.To}
			edges[edgeKey(edge)] = edge
		}
		s.DeclarePredicateFired("session.recorded", map[string]any{"number": log.Session.Number})
	}

	noteSlice := sortedNotes(notes)
	edgeSlice := sortedEdges(edges)

	if err := s.WriteNotes(noteSlice); err != nil {
		return Result{}, fmt.Errorf("bootstrap: write notes: %w", err)
	}
	if err := s.WriteEdges(edgeSlice); err != nil {
		return Result{}, fmt.Errorf("bootstrap: write edges: %w", err)
	}

	return Result{
		SessionsParsed: len(files),
		NoteCount:      len(noteSlice),
		EdgeCount:      len(edgeSlice),
		NotesByType:    countByType(noteSlice),
		EdgesByType:    countEdgesByType(edgeSlice),
	}, nil
}

func parseSession(path string) (sessionLog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionLog{}, fmt.Errorf("bootstrap: read %s: %w", path, err)
	}
	var log sessionLog
	if err := json.Unmarshal(data, &log); err != nil {
		return sessionLog{}, fmt.Errorf("bootstrap: parse %s: %w", path, err)
	}
	if log.Session.Number == 0 {
		return sessionLog{}, fmt.Errorf("bootstrap: %s has no session.number", path)
	}
	return log, nil
}

func edgeKey(e sdk.Edge) string { return e.Type + "\x00" + e.From + "\x00" + e.To }

func sortedNotes(m map[string]sdk.Note) []sdk.Note {
	out := make([]sdk.Note, 0, len(m))
	for _, n := range m {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedEdges(m map[string]sdk.Edge) []sdk.Edge {
	out := make([]sdk.Edge, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return edgeKey(out[i]) < edgeKey(out[j]) })
	return out
}

func countByType(notes []sdk.Note) map[string]int {
	out := map[string]int{}
	for _, n := range notes {
		out[n.Type]++
	}
	return out
}

func countEdgesByType(edges []sdk.Edge) map[string]int {
	out := map[string]int{}
	for _, e := range edges {
		out[e.Type]++
	}
	return out
}
