package graphstore

import "sync"

// Path A lazy/cheap ephemeral open (spec graphstore-concurrency-contract,
// decision C-Hybrid): `da` is many short-lived processes. A process that
// never touches the graph must not pay the store-open cost (SQLite file
// open + WAL/PRAGMA + schema init, or a Postgres pool dial + ping). A
// LazyStore defers the real provider open until the first method call, so
// "open the store only when a command actually needs the graph" is a
// property of the provider — not something every caller must remember.
//
// LazyStore IS a Store (it satisfies the unchanged contract), so it is a
// transparent provider variant: callers/Deps bind to Store exactly as
// before, and the future A->B daemon swap stays invisible. The open
// happens at most once (sync.Once); the first call that triggers a failed
// open returns that error and every subsequent call returns it too — the
// handle does not silently degrade to a half-open store.

// NewLazyStore wraps a provider-open thunk in a Store whose backend is
// opened lazily on first use. open is the cheap, late, per-process
// acquisition (e.g. func() (Store, error) { return OpenSQLite(path) }).
// Acquisition stays explicit and cheap (CONTRACT.md guarantee #4): the
// returned value is constructed with zero I/O.
func NewLazyStore(open func() (Store, error)) Store {
	return &lazyStore{open: open}
}

// lazyStore defers open until the first contract method is invoked. It is
// single-goroutine within a process like every Store handle (CONTRACT.md
// guarantee #3); the sync.Once only guarantees the open thunk runs once if
// a caller does add their own synchronization, it does not make the opened
// backend concurrently usable.
type lazyStore struct {
	open  func() (Store, error)
	once  sync.Once
	store Store
	err   error
}

// resolve performs the one-time late open and returns the backing Store or
// the (sticky) open error.
func (l *lazyStore) resolve() (Store, error) {
	l.once.Do(func() {
		l.store, l.err = l.open()
	})
	return l.store, l.err
}

// --- CodeGraphReader ---

func (l *lazyStore) GetNode(q string) (*GraphNode, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetNode(q)
}

func (l *lazyStore) GetNodesByFile(p string) ([]GraphNode, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetNodesByFile(p)
}

func (l *lazyStore) GetEdgesBySource(q string) ([]GraphEdge, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetEdgesBySource(q)
}

func (l *lazyStore) GetEdgesByTarget(q string) ([]GraphEdge, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetEdgesByTarget(q)
}

func (l *lazyStore) GetEdgesAmong(qs []string) ([]GraphEdge, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetEdgesAmong(qs)
}

func (l *lazyStore) GetAllFiles() ([]string, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetAllFiles()
}

func (l *lazyStore) SearchNodes(q string, limit int) ([]GraphNode, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.SearchNodes(q, limit)
}

func (l *lazyStore) GetMetadata(k string) (string, error) {
	s, err := l.resolve()
	if err != nil {
		return "", err
	}
	return s.GetMetadata(k)
}

func (l *lazyStore) GetStats() (GraphStats, error) {
	s, err := l.resolve()
	if err != nil {
		return GraphStats{}, err
	}
	return s.GetStats()
}

func (l *lazyStore) GetImpactRadius(files []string, maxDepth, maxNodes int) (ImpactResult, error) {
	s, err := l.resolve()
	if err != nil {
		return ImpactResult{}, err
	}
	return s.GetImpactRadius(files, maxDepth, maxNodes)
}

// --- CodeGraphWriter ---

func (l *lazyStore) UpsertNode(n NodeInfo, fileHash string) (int64, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.UpsertNode(n, fileHash)
}

func (l *lazyStore) UpsertEdge(e EdgeInfo) (int64, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.UpsertEdge(e)
}

func (l *lazyStore) RemoveFileData(p string) error {
	s, err := l.resolve()
	if err != nil {
		return err
	}
	return s.RemoveFileData(p)
}

func (l *lazyStore) StoreFileNodesEdges(p string, n []NodeInfo, e []EdgeInfo, fileHash string) error {
	s, err := l.resolve()
	if err != nil {
		return err
	}
	return s.StoreFileNodesEdges(p, n, e, fileHash)
}

func (l *lazyStore) SetMetadata(k, v string) error {
	s, err := l.resolve()
	if err != nil {
		return err
	}
	return s.SetMetadata(k, v)
}

func (l *lazyStore) Commit() error {
	s, err := l.resolve()
	if err != nil {
		return err
	}
	return s.Commit()
}

// --- KGNoteStore ---

func (l *lazyStore) UpsertKGNote(n KGNote) error {
	s, err := l.resolve()
	if err != nil {
		return err
	}
	return s.UpsertKGNote(n)
}

func (l *lazyStore) GetKGNote(id string) (*KGNote, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetKGNote(id)
}

func (l *lazyStore) SearchKGNotes(q string, limit int) ([]KGNote, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.SearchKGNotes(q, limit)
}

func (l *lazyStore) ListArchivedKGNotes() ([]KGNote, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ListArchivedKGNotes()
}

// --- NoteSymbolLinkStore ---

func (l *lazyStore) UpsertNoteSymbolLink(link NoteSymbolLink) (int64, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.UpsertNoteSymbolLink(link)
}

func (l *lazyStore) GetLinksForNote(noteID string) ([]NoteSymbolLink, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetLinksForNote(noteID)
}

func (l *lazyStore) GetLinksForSymbol(q string) ([]NoteSymbolLink, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.GetLinksForSymbol(q)
}

func (l *lazyStore) DeleteNoteSymbolLink(id int64) error {
	s, err := l.resolve()
	if err != nil {
		return err
	}
	return s.DeleteNoteSymbolLink(id)
}

// --- CodeGraphDerived ---

func (l *lazyStore) ReadFlows() ([]FlowRow, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadFlows()
}

func (l *lazyStore) ReadFlowMemberships() ([]FlowMembershipRow, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadFlowMemberships()
}

func (l *lazyStore) ReadCommunities() ([]CommunityRow, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadCommunities()
}

func (l *lazyStore) ReadCommunitySummaries() ([]CommunitySummaryRow, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadCommunitySummaries()
}

func (l *lazyStore) ReadFlowSnapshots() ([]FlowSnapshotRow, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadFlowSnapshots()
}

func (l *lazyStore) ReadRiskIndex() ([]RiskIndexRow, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadRiskIndex()
}

func (l *lazyStore) SearchNodesFTS(query string, limit int) ([]int64, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.SearchNodesFTS(query, limit)
}

func (l *lazyStore) SearchNodesFTSWords(words []string, limit int) ([]int64, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.SearchNodesFTSWords(words, limit)
}

func (l *lazyStore) CountNodesFTSWords(words []string) (int, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.CountNodesFTSWords(words)
}

func (l *lazyStore) ReplaceFlows(flows []FlowRow, paths [][]int64) (int, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.ReplaceFlows(flows, paths)
}

func (l *lazyStore) ReplaceCommunities(communities []CommunityRow, members [][]string) (int, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.ReplaceCommunities(communities, members)
}

func (l *lazyStore) ReplaceCommunitySummaries(rows []CommunitySummaryRow) (int, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.ReplaceCommunitySummaries(rows)
}

func (l *lazyStore) ReplaceFlowSnapshots(rows []FlowSnapshotRow) (int, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.ReplaceFlowSnapshots(rows)
}

func (l *lazyStore) ReplaceRiskIndex(rows []RiskIndexRow) (int, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.ReplaceRiskIndex(rows)
}

func (l *lazyStore) ApplyEdgeRewrites(rewrites []EdgeRewrite) (int, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.ApplyEdgeRewrites(rewrites)
}

func (l *lazyStore) RebuildFTS() (int, error) {
	s, err := l.resolve()
	if err != nil {
		return 0, err
	}
	return s.RebuildFTS()
}

func (l *lazyStore) SetNodeSignature(id int64, signature string) error {
	s, err := l.resolve()
	if err != nil {
		return err
	}
	return s.SetNodeSignature(id, signature)
}

func (l *lazyStore) SetNodeCommunity(id, communityID int64) error {
	s, err := l.resolve()
	if err != nil {
		return err
	}
	return s.SetNodeCommunity(id, communityID)
}

func (l *lazyStore) NodesWithoutSignature() ([]GraphNode, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.NodesWithoutSignature()
}

func (l *lazyStore) ReadNodesByKind(kinds []string) ([]GraphNode, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadNodesByKind(kinds)
}

func (l *lazyStore) ReadNodesByID(ids []int64) ([]GraphNode, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadNodesByID(ids)
}

func (l *lazyStore) ReadNodesByCommunity(communityID int64) ([]GraphNode, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadNodesByCommunity(communityID)
}

func (l *lazyStore) ReadAllNodes() ([]GraphNode, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadAllNodes()
}

func (l *lazyStore) ReadAllEdges() ([]GraphEdge, error) {
	s, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return s.ReadAllEdges()
}

// --- Closer ---

// Close releases the backend only if it was ever opened. A LazyStore that
// was never used closes with no error and never triggers a late open just
// to close it (acquire/release stays cheap — CONTRACT.md guarantee #4).
func (l *lazyStore) Close() error {
	if l.store == nil {
		return nil
	}
	return l.store.Close()
}

// lazyStore satisfies the whole composed contract (and therefore every
// role). This pins the transparent-variant property at compile time.
var _ Store = (*lazyStore)(nil)
