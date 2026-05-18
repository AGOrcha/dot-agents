// Package graphstore provides the storage interface and types for the unified
// code-structure + knowledge-note graph. It is a Go port of the Python
// code-review-graph storage layer, extended with KG note tables.
package graphstore

// NodeKind enumerates structural node types in the code graph.
const (
	NodeKindFile     = "File"
	NodeKindClass    = "Class"
	NodeKindFunction = "Function"
	NodeKindType     = "Type"
	NodeKindTest     = "Test"
)

// EdgeKind enumerates relationship types between code nodes.
const (
	EdgeKindCalls       = "CALLS"
	EdgeKindImportsFrom = "IMPORTS_FROM"
	EdgeKindInherits    = "INHERITS"
	EdgeKindImplements  = "IMPLEMENTS"
	EdgeKindContains    = "CONTAINS"
	EdgeKindTestedBy    = "TESTED_BY"
	EdgeKindDependsOn   = "DEPENDS_ON"
)

// NodeInfo carries data for inserting/updating a node (parser output shape).
type NodeInfo struct {
	Kind       string
	Name       string
	FilePath   string
	LineStart  int
	LineEnd    int
	Language   string
	ParentName string
	Params     string
	ReturnType string
	Modifiers  string
	IsTest     bool
	Extra      map[string]any
}

// EdgeInfo carries data for inserting/updating an edge (parser output shape).
type EdgeInfo struct {
	Kind     string
	Source   string // qualified name
	Target   string // qualified name
	FilePath string
	Line     int
	Extra    map[string]any
}

// GraphNode is a node as stored and returned from the graph.
type GraphNode struct {
	ID            int64
	Kind          string
	Name          string
	QualifiedName string
	FilePath      string
	LineStart     int
	LineEnd       int
	Language      string
	ParentName    string
	Params        string
	ReturnType    string
	IsTest        bool
	FileHash      string
	Extra         map[string]any
	UpdatedAt     float64
}

// GraphEdge is an edge as stored and returned from the graph.
type GraphEdge struct {
	ID              int64
	Kind            string
	SourceQualified string
	TargetQualified string
	FilePath        string
	Line            int
	Extra           map[string]any
	UpdatedAt       float64
}

// GraphStats aggregates health metrics for the graph.
type GraphStats struct {
	TotalNodes  int
	TotalEdges  int
	NodesByKind map[string]int
	EdgesByKind map[string]int
	Languages   []string
	FilesCount  int
	LastUpdated string
	NotesCount  int
	LinksCount  int
}

// ImpactResult is the output of a GetImpactRadius query.
type ImpactResult struct {
	ChangedNodes  []GraphNode
	ImpactedNodes []GraphNode
	ImpactedFiles []string
	Edges         []GraphEdge
}

// KGNote is a knowledge-graph note record in the warm database layer.
type KGNote struct {
	ID         string // KG note ID (matches frontmatter id)
	Title      string
	NoteType   string // concept, decision, entity, etc.
	Status     string
	Summary    string
	FilePath   string // path to the .md file in KG_HOME
	Version    int
	ArchivedAt string // RFC3339 or empty
	IndexedAt  float64
}

// NoteSymbolLink connects a KG note to a code symbol.
type NoteSymbolLink struct {
	ID            int64
	NoteID        string
	QualifiedName string
	LinkKind      string // "mentions", "implements", "documents", etc.
	CreatedAt     float64
}

// The graphstore contract is segregated into cohesive ROLE interfaces,
// grouped by how callers actually use the store, then composed into the
// whole-store Store. This is an Interface-Segregation split: a caller or
// the injected Deps handle should depend on the NARROWEST role it uses, so
// a test fake stubs only that role's handful of methods instead of all 28.
//
// Provider guarantees apply uniformly to every role (a role is a view of
// the same provider, not a different backend):
//
//   - Bounds. Where an operation accepts maxNodes/maxDepth/limit
//     arguments (e.g. SearchNodes, GetImpactRadius on CodeGraphReader),
//     the provider treats them as the caller's requested ceiling. The
//     contract's intent is a hard, uniform cap across the native and CRG
//     paths plus a request timeout; enforcing that uniformly is the
//     provider's responsibility (Path A, delivered by gcc2 — not yet
//     enforced by every concrete store at the time this contract is
//     published).
//   - Request timeout. Long-running graph traversals are bounded by a
//     provider-owned request timeout; callers do not implement their own
//     deadline around store calls.
//   - Concurrency ownership. A store handle is single-goroutine within a
//     process: callers must not share one handle across goroutines
//     without their own synchronization. Cross-process safety and write
//     serialization (SQLite's single-writer/WAL behavior, a connection
//     pool, or a future broker/daemon) are the PROVIDER's job, not the
//     caller's and not the Deps singleton's. The Deps singleton is only a
//     holder of a contract-typed handle (see Handle); it is explicitly
//     NOT the concurrency story — the provider behind the contract is.
//   - Lifecycle. Acquiring and releasing a handle is explicit and cheap.
//     Callers obtain a store, use it, and Close it (the Closer role);
//     they never manage backend connections, pools, or subprocess
//     workers directly.
//
// The roles are derived from existing concrete-store usage and the change
// is additive: *SQLiteStore and *PostgresStore already satisfy every role
// and the composed Store (see the compile-time assertions below).
// Publishing this changes no behavior; gcc2 implements the Path A
// enforcement internals and gcc3 binds all callers + the Deps singleton
// to the narrowest role each needs.

// CodeGraphReader is the read-only view of the code-structure graph:
// node/edge lookups, file enumeration, search, metadata reads, aggregate
// stats, and bounded impact-radius traversal. Read-mostly callers (status,
// review, impact, orient flows) should depend on this role alone.
type CodeGraphReader interface {
	GetNode(qualifiedName string) (*GraphNode, error)
	GetNodesByFile(filePath string) ([]GraphNode, error)
	GetEdgesBySource(qualifiedName string) ([]GraphEdge, error)
	GetEdgesByTarget(qualifiedName string) ([]GraphEdge, error)
	GetEdgesAmong(qualifiedNames []string) ([]GraphEdge, error)
	GetAllFiles() ([]string, error)
	SearchNodes(query string, limit int) ([]GraphNode, error)
	GetMetadata(key string) (string, error)
	GetStats() (GraphStats, error)
	GetImpactRadius(changedFiles []string, maxDepth, maxNodes int) (ImpactResult, error)
}

// CodeGraphWriter is the mutation view of the code-structure graph:
// node/edge upserts, per-file replace, metadata writes, and Commit. Only
// the build/update pipeline (graph build, precommit refresh) depends on
// this role.
type CodeGraphWriter interface {
	UpsertNode(node NodeInfo, fileHash string) (int64, error)
	UpsertEdge(edge EdgeInfo) (int64, error)
	RemoveFileData(filePath string) error
	StoreFileNodesEdges(filePath string, nodes []NodeInfo, edges []EdgeInfo, fileHash string) error
	SetMetadata(key, value string) error
	Commit() error
}

// KGNoteStore is the knowledge-graph note view: upsert/get/search a note
// and list archived notes. KG curation/sync callers depend on this role
// (often paired with NoteSymbolLinkStore).
type KGNoteStore interface {
	UpsertKGNote(note KGNote) error
	GetKGNote(id string) (*KGNote, error)
	SearchKGNotes(query string, limit int) ([]KGNote, error)
	ListArchivedKGNotes() ([]KGNote, error)
}

// NoteSymbolLinkStore is the note↔code-symbol link view: upsert, the two
// directional lookups, and delete. The warm-link sync flow depends on this
// role.
type NoteSymbolLinkStore interface {
	UpsertNoteSymbolLink(link NoteSymbolLink) (int64, error)
	GetLinksForNote(noteID string) ([]NoteSymbolLink, error)
	GetLinksForSymbol(qualifiedName string) ([]NoteSymbolLink, error)
	DeleteNoteSymbolLink(id int64) error
}

// Closer is the acquire/release view (idiomatic single-method interface,
// io.Closer shape). Callers that own a handle's lifetime depend on this
// role to release it; callers handed a borrowed handle should NOT depend
// on it (they must not Close what they do not own).
type Closer interface {
	Close() error
}

// Store is the published, backend-agnostic whole-store contract: every
// role composed. Existing whole-store callers and the Deps handle bind to
// this; callers that use only one concern should instead depend on the
// matching role above. It is the single stable surface that makes the
// ephemeral→pooled→daemon evolution (spec graphstore-concurrency-contract,
// decision C-Hybrid) a transparent provider swap with no caller-visible
// change.
type Store interface {
	CodeGraphReader
	CodeGraphWriter
	KGNoteStore
	NoteSymbolLinkStore
	Closer
}

// Compile-time assertions that the existing concrete stores satisfy the
// composed contract AND every segregated role. These pin the interfaces to
// real implementations so the contract cannot drift away from what callers
// actually run, and so the additive nature of this change is verified by
// the compiler (gcc1 changes no behavior — it only publishes, segregates,
// and documents the surface). Asserting each role explicitly guarantees a
// caller may safely narrow to any single role.
var (
	_ Store = (*SQLiteStore)(nil)
	_ Store = (*PostgresStore)(nil)

	_ CodeGraphReader     = (*SQLiteStore)(nil)
	_ CodeGraphWriter     = (*SQLiteStore)(nil)
	_ KGNoteStore         = (*SQLiteStore)(nil)
	_ NoteSymbolLinkStore = (*SQLiteStore)(nil)
	_ Closer              = (*SQLiteStore)(nil)

	_ CodeGraphReader     = (*PostgresStore)(nil)
	_ CodeGraphWriter     = (*PostgresStore)(nil)
	_ KGNoteStore         = (*PostgresStore)(nil)
	_ NoteSymbolLinkStore = (*PostgresStore)(nil)
	_ Closer              = (*PostgresStore)(nil)
)

// Handle is the contract-typed boundary the dependency-injection singleton
// (the package-level `deps` in commands/* — di-refactor OD-1) holds. It
// deliberately exposes ONLY a contract-typed Store accessor: the singleton
// is justified solely because it carries a Store whose provider owns
// pooling and serialization. The singleton is NOT the concurrency story
// and must never reach a concrete backend; it reads the graph exclusively
// through Store().
//
// Defined here (write scope: internal/graphstore) so the contract and its
// DI boundary are published together as one reviewable artifact. Binding
// the actual command-package Deps structs to this handle is gcc3 (refactor
// all callers) and is intentionally deferred — this type pins the shape
// gcc3 will adopt without changing any caller now.
type Handle struct {
	store Store
}

// NewHandle wraps a contract-typed Store for the DI singleton to hold.
// Acquisition is cheap and explicit; the provider behind store owns all
// connection/pool/serialization concerns.
func NewHandle(store Store) Handle { return Handle{store: store} }

// Store returns the whole-store contract-typed handle. Callers bind to
// this interface, never to a concrete backend. Returns nil if the handle
// is unset, letting callers fall back to their existing direct-open path
// until gcc3 wires this end-to-end.
//
// To narrow to a single role, do NOT add a per-role accessor — a Store
// already satisfies every role (it embeds them). Declare the dependency
// as the narrow role type and assign it from Store():
//
//	var r CodeGraphReader = h.Store() // a Store IS a CodeGraphReader
//
// This is the idiomatic "accept interfaces" Go: the call site documents
// exactly the role it needs, a test fake stubs only that role, and there
// is exactly one nil-safe accessor (no duplicated narrowing bodies). The
// nil-safety is inherited: an unset handle's Store() is nil, so the
// narrowed interface value is nil too.
func (h Handle) Store() Store { return h.store }
