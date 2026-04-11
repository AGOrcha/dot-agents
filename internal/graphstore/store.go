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
	TotalNodes   int
	TotalEdges   int
	NodesByKind  map[string]int
	EdgesByKind  map[string]int
	Languages    []string
	FilesCount   int
	LastUpdated  string
	NotesCount   int
	LinksCount   int
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
	ID          string // KG note ID (matches frontmatter id)
	Title       string
	NoteType    string // concept, decision, entity, etc.
	Status      string
	Summary     string
	FilePath    string // path to the .md file in KG_HOME
	Version     int
	ArchivedAt  string // RFC3339 or empty
	IndexedAt   float64
}

// NoteSymbolLink connects a KG note to a code symbol.
type NoteSymbolLink struct {
	ID            int64
	NoteID        string
	QualifiedName string
	LinkKind      string // "mentions", "implements", "documents", etc.
	CreatedAt     float64
}

// Store is the backend-agnostic interface for all graph operations.
type Store interface {
	// Code graph — write
	UpsertNode(node NodeInfo, fileHash string) (int64, error)
	UpsertEdge(edge EdgeInfo) (int64, error)
	RemoveFileData(filePath string) error
	StoreFileNodesEdges(filePath string, nodes []NodeInfo, edges []EdgeInfo, fileHash string) error
	SetMetadata(key, value string) error
	Commit() error

	// Code graph — read
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

	// KG notes
	UpsertKGNote(note KGNote) error
	GetKGNote(id string) (*KGNote, error)
	SearchKGNotes(query string, limit int) ([]KGNote, error)
	ListArchivedKGNotes() ([]KGNote, error)

	// Note→symbol links
	UpsertNoteSymbolLink(link NoteSymbolLink) (int64, error)
	GetLinksForNote(noteID string) ([]NoteSymbolLink, error)
	GetLinksForSymbol(qualifiedName string) ([]NoteSymbolLink, error)
	DeleteNoteSymbolLink(id int64) error

	// Lifecycle
	Close() error
}
