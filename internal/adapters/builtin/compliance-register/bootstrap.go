package complianceregister

import (
	"encoding/json"
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// errUnknownQuery is the error returned by RunNamed for an unregistered query.
func errUnknownQuery(name string) error {
	return fmt.Errorf("compliance-register: no named query %q", name)
}

// corpusFile is the JSON shape of a testdata corpus: a flat note list and edge
// list the stub bootstrap writes into the adapter namespace via the SDK.
type corpusFile struct {
	Notes []corpusNote `json:"notes"`
	Edges []corpusEdge `json:"edges"`
}

type corpusNote struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Fields map[string]any `json:"fields"`
}

type corpusEdge struct {
	Type string `json:"type"`
	From string `json:"from"`
	To   string `json:"to"`
}

// Bootstrap is the stub bootstrap (§13.2): it parses a hand-authored corpus and
// writes it into the adapter's own namespace through the SDK. It is read-only
// with respect to schema (it never mutates declarations) and uses the SDK
// exclusively (§8.2: no direct DB connections). It returns the SDK so callers
// can issue token-scoped reads against the populated namespace.
func (a *Adapter) Bootstrap(store sdk.Store, corpusJSON []byte) (*sdk.SDK, error) {
	var corpus corpusFile
	if err := json.Unmarshal(corpusJSON, &corpus); err != nil {
		return nil, fmt.Errorf("compliance-register bootstrap: parse corpus: %w", err)
	}
	s := sdk.For(a.Name(), store)
	if err := s.WriteNotes(toSDKNotes(corpus.Notes)); err != nil {
		return nil, fmt.Errorf("compliance-register bootstrap: write notes: %w", err)
	}
	if err := s.WriteEdges(toSDKEdges(corpus.Edges)); err != nil {
		return nil, fmt.Errorf("compliance-register bootstrap: write edges: %w", err)
	}
	return s, nil
}

// LoadView reads the adapter namespace back through the SDK as a NamespaceView
// the DSL evaluator runs over. The read is token-scoped (own-namespace read).
func (a *Adapter) LoadView(s *sdk.SDK) (sdk.NamespaceView, error) {
	var view sdk.NamespaceView
	_, err := s.Query(func(v sdk.NamespaceView) []sdk.Row {
		view = v
		return nil
	})
	return view, err
}

// FireEnvTrigger applies an environmental trigger (§7) to the corpus view and
// returns a new view in which the lifecycle is COMPLETE: matching notes carry
// the §7.3 environmental stale payload AND the §7.3 env→derivation propagation
// has already run, so controls deriving from an environmentally-invalidated
// policy carry their derivation-stale tag too. This is the whole driver
// lifecycle in one call — a caller that fires `policy.review_due` and then runs
// `policy_review_due_impact` gets the surfaced controls without a separate
// propagation step. ApplyEnvTrigger + ApplyDerivation remain exported for
// callers that need the stages individually, but the driver path does not
// require a manual second call.
func (a *Adapter) FireEnvTrigger(view sdk.NamespaceView, trig dsl.EnvTrigger) (sdk.NamespaceView, error) {
	tagged, err := dsl.ApplyEnvTrigger(a.envPred, view.Notes, trig)
	if err != nil {
		return sdk.NamespaceView{}, err
	}
	env := mergeTagged(view, tagged)
	return a.ApplyDerivation(env), nil
}

// toSDKNotes converts corpus notes to SDK notes.
func toSDKNotes(notes []corpusNote) []sdk.Note {
	out := make([]sdk.Note, 0, len(notes))
	for _, n := range notes {
		out = append(out, sdk.Note{ID: n.ID, Type: n.Type, Fields: n.Fields})
	}
	return out
}

// toSDKEdges converts corpus edges to SDK edges.
func toSDKEdges(edges []corpusEdge) []sdk.Edge {
	out := make([]sdk.Edge, 0, len(edges))
	for _, e := range edges {
		out = append(out, sdk.Edge{Type: e.Type, From: e.From, To: e.To})
	}
	return out
}

// mergeTagged returns a view where the tagged notes replace their originals (by
// id), preserving every other note and all edges.
func mergeTagged(view sdk.NamespaceView, tagged []sdk.Note) sdk.NamespaceView {
	byID := make(map[string]sdk.Note, len(tagged))
	for _, n := range tagged {
		byID[n.ID] = n
	}
	notes := make([]sdk.Note, 0, len(view.Notes))
	for _, n := range view.Notes {
		if updated, ok := byID[n.ID]; ok {
			notes = append(notes, updated)
			continue
		}
		notes = append(notes, n)
	}
	return sdk.NamespaceView{Notes: notes, Edges: view.Edges}
}
