// Package sdk is the `da-adapter-sdk` Go surface that bootstrap skills use
// exclusively to write into scoped KG storage
// (graph-backend-adapter-contract §8.4).
//
// The contract rule the SDK enforces (§8.2): bootstrap skills and named
// queries MUST NOT open direct DB connections. Every storage operation goes
// through the SDK, which attaches a short-lived namespace token scoped to the
// operation. The storage layer validates every namespace referenced by an
// operation against the token's authorized set, and rejects anything outside
// it before any row is touched.
//
// This package ships the SDK surface (§8.4.1) and an in-memory store backing
// it. The in-memory store is the substrate the TTRPG dogfood
// (.agents/sandbox/ttrpg-adapter/) runs its hard test against without standing
// up SQLite/Postgres: it is a faithful model of the namespace-token contract,
// not a production backend. A production SDK swaps the Store implementation
// for one backed by the gcc role-segregated graphstore (§2.7) — the SDK
// surface (this file) does not change.
package sdk

import (
	"fmt"
	"sort"
)

// Note is a single note written to a namespace. The SDK is schema-agnostic at
// this layer: schema validation (§4) is the registry's job and runs before a
// bootstrap calls WriteNotes. ID and Type are required; Fields carries the
// declared note-type fields.
type Note struct {
	ID     string
	Type   string
	Fields map[string]any
}

// Edge is a single typed edge between two notes (by id).
type Edge struct {
	Type string
	From string
	To   string
}

// Mode is a namespace access mode (§8.2): read or write.
type Mode string

const (
	// ModeRead authorizes reads against a namespace.
	ModeRead Mode = "read"
	// ModeWrite authorizes writes against a namespace.
	ModeWrite Mode = "write"
)

// Grant is one (namespace, mode) pair in a token's authorized set (§8.2).
type Grant struct {
	Namespace string
	Mode      Mode
}

// Token is the namespace token of §8.2: a set of authorized (namespace, mode)
// pairs derived from the executing adapter's own namespace plus any
// cross-namespace dependencies declared in the operation being authorized.
//
// In a production backend the token is signed and validated at the storage
// layer (§8.2.1 N13). Here the SDK constructs and checks it in-process; the
// authorization model — every referenced namespace must be in the authorized
// set — is identical, which is what the dogfood needs to exercise.
type Token struct {
	PrimaryAdapter string
	Authorized     []Grant
	IssuedFor      string
}

// authorizes reports whether the token grants the given mode on ns.
func (t Token) authorizes(ns string, mode Mode) bool {
	for _, g := range t.Authorized {
		if g.Namespace == ns && g.Mode == mode {
			return true
		}
	}
	return false
}

// OwnReadToken derives the token for an adapter reading its own namespace
// (§8.2 derivation rule: own queries grant {adapter, read}). N1/N5.
func OwnReadToken(adapter, operation string) Token {
	return Token{
		PrimaryAdapter: adapter,
		IssuedFor:      operation,
		Authorized:     []Grant{{Namespace: adapter, Mode: ModeRead}},
	}
}

// BootstrapToken derives the token for an adapter's bootstrap skill (§8.2
// derivation rule: bootstrap grants {adapter, write}). N2.
func BootstrapToken(adapter string) Token {
	return Token{
		PrimaryAdapter: adapter,
		IssuedFor:      "bootstrap",
		Authorized:     []Grant{{Namespace: adapter, Mode: ModeWrite}},
	}
}

// ViewToken derives the token for a materialized view that declares reads_from
// (§8.2 derivation rule: grants {adapter, write} plus {dep, read} for each
// dependency). N3/N4.
func ViewToken(adapter, view string, readsFrom []string) Token {
	auth := []Grant{{Namespace: adapter, Mode: ModeWrite}}
	for _, dep := range readsFrom {
		auth = append(auth, Grant{Namespace: dep, Mode: ModeRead})
	}
	return Token{PrimaryAdapter: adapter, IssuedFor: view, Authorized: auth}
}

// Store is the storage substrate the SDK writes into. It is the only thing the
// SDK touches; a production SDK injects a gcc-backed Store (§2.7) and the SDK
// surface is unchanged. Every method takes the operation token and rejects
// out-of-namespace access (§8.2 storage-layer enforcement).
type Store interface {
	// WriteNotes bulk-writes notes to ns. token must grant {ns, write}.
	WriteNotes(token Token, ns string, notes []Note) error
	// WriteEdges bulk-writes edges to ns. token must grant {ns, write}.
	WriteEdges(token Token, ns string, edges []Edge) error
	// Notes returns all notes in ns. token must grant {ns, read}.
	Notes(token Token, ns string) ([]Note, error)
	// Edges returns all edges in ns. token must grant {ns, read}.
	Edges(token Token, ns string) ([]Edge, error)
}

// SDK is the `da-adapter-sdk` surface (§8.4.1). A bootstrap skill is
// constructed with For(adapter, store) and calls only these methods; it never
// sees a Store, a Token, or a namespace string it did not declare. The SDK
// derives the token for each operation from the adapter's identity and the
// operation kind — the bootstrap cannot widen its own authority.
type SDK struct {
	adapter string
	store   Store
	// predicatesFired records env-predicate fire calls (§8.4.1
	// declare_predicate_fired) so the dogfood can assert on them without a
	// live driver. Production wires this to the scoped-KG driver bus.
	predicatesFired []FiredPredicate
}

// FiredPredicate records a declare_predicate_fired call (§8.4.1).
type FiredPredicate struct {
	Predicate string
	Args      map[string]any
}

// For constructs an SDK bound to a single adapter's namespace and store. This
// is the only constructor a bootstrap skill uses; it cannot reach storage any
// other way (§8.2: direct DB connections are forbidden by contract).
func For(adapter string, store Store) *SDK {
	return &SDK{adapter: adapter, store: store}
}

// Adapter returns the namespace this SDK is bound to.
func (s *SDK) Adapter() string { return s.adapter }

// WriteNotes bulk-writes notes to the adapter's own namespace (§8.4.1). The
// SDK attaches a {adapter, write} bootstrap token; the store rejects any
// attempt to write elsewhere.
func (s *SDK) WriteNotes(notes []Note) error {
	if err := validateNotes(notes); err != nil {
		return err
	}
	return s.store.WriteNotes(BootstrapToken(s.adapter), s.adapter, notes)
}

// WriteEdges bulk-writes edges to the adapter's own namespace (§8.4.1).
func (s *SDK) WriteEdges(edges []Edge) error {
	if err := validateEdges(edges); err != nil {
		return err
	}
	return s.store.WriteEdges(BootstrapToken(s.adapter), s.adapter, edges)
}

// Query executes a read-only operation within the adapter's own namespace and
// returns the matching notes (§8.4.1 sdk.query). The runner is a caller-
// supplied function that operates on the namespace's notes and edges; in v1
// the named-query DSL compiler (internal/kg/dsl, a sibling task) would produce
// these runners from queries.yaml. Until that lands, the dogfood supplies
// runners directly — the SDK's job here is the token-scoped read boundary, not
// DSL compilation. The runner receives ONLY this adapter's namespace data:
// cross-namespace reads are not reachable from a named query (§8.3).
func (s *SDK) Query(runner QueryRunner) ([]Row, error) {
	token := OwnReadToken(s.adapter, "query")
	notes, err := s.store.Notes(token, s.adapter)
	if err != nil {
		return nil, err
	}
	edges, err := s.store.Edges(token, s.adapter)
	if err != nil {
		return nil, err
	}
	return runner(NamespaceView{Notes: notes, Edges: edges}), nil
}

// MaterializeView computes and persists a view (§8.4.1 sdk.materialize_view).
// The SDK derives a multi-namespace token from readsFrom: {adapter, write}
// plus {dep, read} for each dependency. This is the ONLY surface that may read
// cross-namespace (§8.3). readNotes lets the runner pull a dependency's notes;
// the store rejects any namespace not in readsFrom.
func (s *SDK) MaterializeView(name string, readsFrom []string, runner ViewRunner) error {
	token := ViewToken(s.adapter, name, readsFrom)
	read := func(ns string) ([]Note, error) {
		return s.store.Notes(token, ns)
	}
	notes, err := runner(read)
	if err != nil {
		return err
	}
	return s.store.WriteNotes(token, s.adapter, notes)
}

// DeclarePredicateFired fires an env predicate (§8.4.1
// sdk.declare_predicate_fired). The SDK records it; production routes it to the
// scoped-KG driver bus.
func (s *SDK) DeclarePredicateFired(predicate string, args map[string]any) {
	s.predicatesFired = append(s.predicatesFired, FiredPredicate{Predicate: predicate, Args: args})
}

// FiredPredicates returns the predicates fired through this SDK, in call order.
func (s *SDK) FiredPredicates() []FiredPredicate { return s.predicatesFired }

// QueryRunner runs a named query over a single namespace's data and returns
// rows. It is the in-process stand-in for a compiled DSL query.
type QueryRunner func(NamespaceView) []Row

// ViewRunner computes a materialized view's notes, pulling dependency notes via
// read (which is token-checked at the storage layer).
type ViewRunner func(read func(ns string) ([]Note, error)) ([]Note, error)

// NamespaceView is the read-only data a QueryRunner sees.
type NamespaceView struct {
	Notes []Note
	Edges []Edge
}

// NotesByType returns the namespace's notes of a given type.
func (v NamespaceView) NotesByType(t string) []Note {
	var out []Note
	for _, n := range v.Notes {
		if n.Type == t {
			out = append(out, n)
		}
	}
	return out
}

// EdgesByType returns the namespace's edges of a given type.
func (v NamespaceView) EdgesByType(t string) []Edge {
	var out []Edge
	for _, e := range v.Edges {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// Row is a single named-query result row: an ordered set of column values
// keyed by the RETURN column name.
type Row map[string]any

func validateNotes(notes []Note) error {
	for i, n := range notes {
		if n.ID == "" {
			return fmt.Errorf("sdk: note[%d] has empty id", i)
		}
		if n.Type == "" {
			return fmt.Errorf("sdk: note %q has empty type", n.ID)
		}
	}
	return nil
}

func validateEdges(edges []Edge) error {
	for i, e := range edges {
		if e.Type == "" {
			return fmt.Errorf("sdk: edge[%d] has empty type", i)
		}
		if e.From == "" || e.To == "" {
			return fmt.Errorf("sdk: edge[%d] (%s) missing from/to", i, e.Type)
		}
	}
	return nil
}

// MemStore is an in-memory Store for tests and the dogfood hard test. It
// enforces the §8.2 namespace-token contract: every operation's namespace and
// mode must be authorized by the supplied token, else the operation is
// rejected before touching storage. The zero value is not usable; use
// NewMemStore.
type MemStore struct {
	notes map[string][]Note
	edges map[string][]Edge
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{notes: map[string][]Note{}, edges: map[string][]Edge{}}
}

// WriteNotes appends notes to ns after token authorization (§8.2 N8: a write to
// a namespace the token does not grant write on is rejected at the storage
// layer regardless of token contents).
func (m *MemStore) WriteNotes(token Token, ns string, notes []Note) error {
	if !token.authorizes(ns, ModeWrite) {
		return fmt.Errorf("storage: token issued_for %q does not authorize write to namespace %q", token.IssuedFor, ns)
	}
	m.notes[ns] = append(m.notes[ns], notes...)
	return nil
}

// WriteEdges appends edges to ns after token authorization.
func (m *MemStore) WriteEdges(token Token, ns string, edges []Edge) error {
	if !token.authorizes(ns, ModeWrite) {
		return fmt.Errorf("storage: token issued_for %q does not authorize write to namespace %q", token.IssuedFor, ns)
	}
	m.edges[ns] = append(m.edges[ns], edges...)
	return nil
}

// Notes returns ns's notes after token authorization (§8.2 N9: defense in
// depth — even a valid token is checked per-namespace).
func (m *MemStore) Notes(token Token, ns string) ([]Note, error) {
	if !token.authorizes(ns, ModeRead) {
		return nil, fmt.Errorf("storage: token issued_for %q does not authorize read of namespace %q", token.IssuedFor, ns)
	}
	out := make([]Note, len(m.notes[ns]))
	copy(out, m.notes[ns])
	return out, nil
}

// Edges returns ns's edges after token authorization.
func (m *MemStore) Edges(token Token, ns string) ([]Edge, error) {
	if !token.authorizes(ns, ModeRead) {
		return nil, fmt.Errorf("storage: token issued_for %q does not authorize read of namespace %q", token.IssuedFor, ns)
	}
	out := make([]Edge, len(m.edges[ns]))
	copy(out, m.edges[ns])
	return out, nil
}

// Namespaces returns the namespaces with any data, sorted. Test/inspection aid.
func (m *MemStore) Namespaces() []string {
	seen := map[string]bool{}
	for ns := range m.notes {
		seen[ns] = true
	}
	for ns := range m.edges {
		seen[ns] = true
	}
	out := make([]string, 0, len(seen))
	for ns := range seen {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}
