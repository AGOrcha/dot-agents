package kg

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// ── Phase 5: Bridge intent mapping ────────────────────────────────────────────

// BridgeIntentMapping maps one bridge intent to one or more KG query intents.
type BridgeIntentMapping struct {
	BridgeIntent string   `json:"bridge_intent" yaml:"bridge_intent"`
	KGIntents    []string `json:"kg_intents" yaml:"kg_intents"`
}

func defaultBridgeMappings() []BridgeIntentMapping {
	return []BridgeIntentMapping{
		{BridgeIntent: "plan_context", KGIntents: []string{"decision_lookup", "synthesis_lookup"}},
		{BridgeIntent: "decision_lookup", KGIntents: []string{"decision_lookup"}},
		{BridgeIntent: "entity_context", KGIntents: []string{"entity_context"}},
		{BridgeIntent: "workflow_memory", KGIntents: []string{"related_notes", "source_lookup"}},
		{BridgeIntent: "contradictions", KGIntents: []string{"contradictions"}},
		{BridgeIntent: "symbol_lookup", KGIntents: []string{"symbol_lookup"}},
		{BridgeIntent: "impact_radius", KGIntents: []string{"impact_radius"}},
		{BridgeIntent: "change_analysis", KGIntents: []string{"change_analysis"}},
		{BridgeIntent: "tests_for", KGIntents: []string{"tests_for"}},
		{BridgeIntent: "callers_of", KGIntents: []string{"callers_of"}},
		{BridgeIntent: "callees_of", KGIntents: []string{"callees_of"}},
		{BridgeIntent: "community_context", KGIntents: []string{"community_context"}},
		{BridgeIntent: "symbol_decisions", KGIntents: []string{"symbol_decisions"}},
		{BridgeIntent: "decision_symbols", KGIntents: []string{"decision_symbols"}},
	}
}

var validBridgeIntents = func() map[string]bool {
	m := make(map[string]bool)
	for _, bm := range defaultBridgeMappings() {
		m[bm.BridgeIntent] = true
	}
	return m
}()

func isValidBridgeIntent(intent string) bool { return validBridgeIntents[intent] }

var codeBridgeIntents = map[string]bool{
	"symbol_lookup":     true,
	"impact_radius":     true,
	"change_analysis":   true,
	"tests_for":         true,
	"callers_of":        true,
	"callees_of":        true,
	"community_context": true,
	"symbol_decisions":  true,
	"decision_symbols":  true,
}

func isCodeBridgeIntent(intent string) bool { return codeBridgeIntents[intent] }

// resolveBridgeQuery fans a bridge intent out to KG queries.
func resolveBridgeQuery(bridgeIntent, query string) ([]GraphQuery, error) {
	for _, bm := range defaultBridgeMappings() {
		if bm.BridgeIntent == bridgeIntent {
			queries := make([]GraphQuery, 0, len(bm.KGIntents))
			for _, kgIntent := range bm.KGIntents {
				queries = append(queries, GraphQuery{
					Intent: kgIntent,
					Query:  query,
					Limit:  10,
				})
			}
			return queries, nil
		}
	}
	return nil, fmt.Errorf("unknown bridge intent %q: valid values are %s", bridgeIntent, strings.Join(sortedKeys(validBridgeIntents), ", "))
}

// mergeBridgeResults merges multiple KG responses into one, deduplicating by note ID.
func mergeBridgeResults(responses []GraphQueryResponse, bridgeIntent string) GraphQueryResponse {
	merged := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        bridgeIntent,
		Provider:      "local-index",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Results:       []GraphQueryResult{},
	}
	seen := make(map[string]bool)
	for _, resp := range responses {
		merged.Query = resp.Query
		for _, r := range resp.Results {
			if !seen[r.ID] {
				seen[r.ID] = true
				merged.Results = append(merged.Results, r)
			}
		}
		merged.Warnings = append(merged.Warnings, resp.Warnings...)
	}
	return merged
}

func graphNodeToQueryResult(node graphstore.GraphNode, resultType string) GraphQueryResult {
	summary := node.FilePath
	if summary == "" {
		summary = node.QualifiedName
	}
	if node.LineStart > 0 {
		summary = fmt.Sprintf("%s:%d", summary, node.LineStart)
		if node.LineEnd > node.LineStart {
			summary = fmt.Sprintf("%s-%d", summary, node.LineEnd)
		}
	}
	return GraphQueryResult{
		ID:            node.QualifiedName,
		Type:          resultType,
		Title:         node.QualifiedName,
		Summary:       summary,
		Path:          node.FilePath,
		QualifiedName: node.QualifiedName,
		Kind:          node.Kind,
		FilePath:      node.FilePath,
		LineStart:     node.LineStart,
		LineEnd:       node.LineEnd,
		Language:      node.Language,
	}
}

func graphNodeTypeLabel(node graphstore.GraphNode) string {
	if node.Kind != "" {
		return strings.ToLower(node.Kind)
	}
	if node.IsTest {
		return "test"
	}
	return "symbol"
}

func findCodeNodes(store *graphstore.SQLiteStore, query string, limit int) ([]graphstore.GraphNode, error) {
	if limit <= 0 {
		limit = 10
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	seen := make(map[string]bool)
	nodes := make([]graphstore.GraphNode, 0, limit)
	addNode := func(node *graphstore.GraphNode) {
		if node == nil || node.QualifiedName == "" || seen[node.QualifiedName] {
			return
		}
		seen[node.QualifiedName] = true
		nodes = append(nodes, *node)
	}

	if node, err := store.GetNode(q); err == nil && node != nil {
		addNode(node)
	}
	if fileNodes, err := store.GetNodesByFile(q); err == nil {
		for i := range fileNodes {
			addNode(&fileNodes[i])
		}
	}
	if len(nodes) < limit {
		matches, err := store.SearchNodes(q, limit)
		if err != nil {
			return nil, err
		}
		for i := range matches {
			addNode(&matches[i])
		}
	}
	return nodes, nil
}

func collectGraphResults(nodes []graphstore.GraphNode, resultType string, limit int) []GraphQueryResult {
	results := make([]GraphQueryResult, 0, len(nodes))
	seen := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		id := node.QualifiedName
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		nodeType := resultType
		if nodeType == "" {
			nodeType = graphNodeTypeLabel(node)
		}
		results = append(results, graphNodeToQueryResult(node, nodeType))
		if limit > 0 && len(results) >= limit {
			break
		}
	}
	return results
}

func collectNeighborResults(store *graphstore.SQLiteStore, nodes []graphstore.GraphNode, edgeKind string, inbound bool, limit int) ([]GraphQueryResult, error) {
	seen := make(map[string]bool)
	var results []GraphQueryResult
	for _, node := range nodes {
		edges, err := neighborEdges(store, node.QualifiedName, inbound)
		if err != nil {
			return nil, err
		}
		done, err := appendNeighborMatches(store, edges, edgeKind, inbound, seen, &results, limit)
		if err != nil {
			return nil, err
		}
		if done {
			return results, nil
		}
	}
	return results, nil
}

// neighborEdges returns the inbound or outbound edges for qn.
func neighborEdges(store *graphstore.SQLiteStore, qn string, inbound bool) ([]graphstore.GraphEdge, error) {
	if inbound {
		return store.GetEdgesByTarget(qn)
	}
	return store.GetEdgesBySource(qn)
}

// appendNeighborMatches walks edges, resolves the neighbor on the opposite
// end, and appends a result row when the edge/neighbor pass the kind filter.
// Returns done=true once limit has been reached.
func appendNeighborMatches(
	store *graphstore.SQLiteStore,
	edges []graphstore.GraphEdge,
	edgeKind string,
	inbound bool,
	seen map[string]bool,
	results *[]GraphQueryResult,
	limit int,
) (bool, error) {
	for _, edge := range edges {
		if edgeKind != "" && edge.Kind != edgeKind {
			continue
		}
		neighborQN := neighborQualifiedName(edge, inbound)
		if seen[neighborQN] {
			continue
		}
		neighbor, ok := loadNeighborNode(store, neighborQN)
		if !ok {
			continue
		}
		if shouldSkipNeighborForKind(edgeKind, neighbor) {
			continue
		}
		seen[neighborQN] = true
		*results = append(*results, graphNodeToQueryResult(*neighbor, graphNodeTypeLabel(*neighbor)))
		if limit > 0 && len(*results) >= limit {
			return true, nil
		}
	}
	return false, nil
}

func neighborQualifiedName(edge graphstore.GraphEdge, inbound bool) string {
	if inbound {
		return edge.SourceQualified
	}
	return edge.TargetQualified
}

func loadNeighborNode(store *graphstore.SQLiteStore, neighborQN string) (*graphstore.GraphNode, bool) {
	neighbor, err := store.GetNode(neighborQN)
	return neighbor, err == nil && neighbor != nil
}

// shouldSkipNeighborForKind returns true if the neighbor should be excluded based on edge kind.
func shouldSkipNeighborForKind(edgeKind string, neighbor *graphstore.GraphNode) bool {
	if edgeKind == graphstore.EdgeKindTestedBy && !neighbor.IsTest && neighbor.Kind != graphstore.NodeKindTest {
		return true
	}
	return false
}

func collectSymbolDecisionResults(store *graphstore.SQLiteStore, nodes []graphstore.GraphNode, limit int) ([]GraphQueryResult, error) {
	seen := make(map[string]bool)
	var results []GraphQueryResult
	for _, node := range nodes {
		links, err := store.GetLinksForSymbol(node.QualifiedName)
		if err != nil {
			return nil, err
		}
		if appendDecisionLinkMatches(store, node.QualifiedName, links, seen, &results, limit) {
			return results, nil
		}
	}
	return results, nil
}

// appendDecisionLinkMatches walks the note links for a symbol, materializes
// the matching decision/synthesis/concept notes, and appends them to results.
// Returns true once limit has been reached.
func appendDecisionLinkMatches(
	store *graphstore.SQLiteStore,
	symbolQN string,
	links []graphstore.NoteSymbolLink,
	seen map[string]bool,
	results *[]GraphQueryResult,
	limit int,
) bool {
	for _, link := range links {
		if seen[link.NoteID] {
			continue
		}
		note, err := store.GetKGNote(link.NoteID)
		if err != nil || note == nil {
			continue
		}
		if !isDecisionNoteType(note.NoteType) {
			continue
		}
		seen[link.NoteID] = true
		*results = append(*results, GraphQueryResult{
			ID:         note.ID,
			Type:       note.NoteType,
			Title:      note.Title,
			Summary:    note.Summary,
			Path:       note.FilePath,
			SourceRefs: []string{symbolQN},
		})
		if limit > 0 && len(*results) >= limit {
			return true
		}
	}
	return false
}

// isDecisionNoteType reports whether note belongs to one of the high-signal
// note categories used by the decision lookup intents.
func isDecisionNoteType(noteType string) bool {
	switch noteType {
	case "decision", "synthesis", "concept":
		return true
	}
	return false
}

func collectDecisionSymbolResults(store *graphstore.SQLiteStore, query string, limit int) ([]GraphQueryResult, error) {
	candidates, err := decisionNoteCandidates(store, query, limit)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var results []GraphQueryResult
	for _, note := range candidates {
		links, err := store.GetLinksForNote(note.ID)
		if err != nil {
			return nil, err
		}
		if appendDecisionSymbolMatches(store, note, links, seen, &results, limit) {
			return results, nil
		}
	}
	return results, nil
}

// decisionNoteCandidates resolves the candidate set of decision/synthesis/
// concept notes for collectDecisionSymbolResults, preferring an exact ID
// match over a fuzzy search.
func decisionNoteCandidates(store *graphstore.SQLiteStore, query string, limit int) ([]graphstore.KGNote, error) {
	var candidates []graphstore.KGNote
	if note, err := store.GetKGNote(strings.TrimSpace(query)); err == nil && note != nil && isDecisionNoteType(note.NoteType) {
		candidates = append(candidates, *note)
	}
	if len(candidates) > 0 {
		return candidates, nil
	}
	notes, err := store.SearchKGNotes(query, limit)
	if err != nil {
		return nil, err
	}
	for _, note := range notes {
		if isDecisionNoteType(note.NoteType) {
			candidates = append(candidates, note)
		}
	}
	return candidates, nil
}

// appendDecisionSymbolMatches walks the symbol links for a decision note and
// appends a result row per linked symbol (resolving warm-store metadata when
// available). Returns true once limit has been reached.
func appendDecisionSymbolMatches(
	store *graphstore.SQLiteStore,
	note graphstore.KGNote,
	links []graphstore.NoteSymbolLink,
	seen map[string]bool,
	results *[]GraphQueryResult,
	limit int,
) bool {
	for _, link := range links {
		if seen[link.QualifiedName] {
			continue
		}
		seen[link.QualifiedName] = true
		summary := fmt.Sprintf("%s via %s", note.Title, link.LinkKind)
		node, err := store.GetNode(link.QualifiedName)
		if err != nil || node == nil {
			*results = append(*results, GraphQueryResult{
				ID:      link.QualifiedName,
				Type:    "symbol",
				Title:   link.QualifiedName,
				Summary: summary,
			})
		} else {
			*results = append(*results, graphNodeToQueryResult(*node, graphNodeTypeLabel(*node)))
			(*results)[len(*results)-1].Summary = summary
		}
		if limit > 0 && len(*results) >= limit {
			return true
		}
	}
	return false
}

func collectChangeAnalysisResults(query string, limit int) (GraphQueryResponse, error) {
	resp := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        "change_analysis",
		Query:         query,
		Provider:      "crg",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	bridge, err := graphstore.NewCRGBridge(crgRepoRoot())
	if err != nil {
		resp.Provider = "crg-unavailable"
		resp.Warnings = append(resp.Warnings, err.Error())
		return resp, nil
	}
	report, err := bridge.DetectChanges(graphstore.DetectChangesOptions{})
	if err != nil {
		return resp, err
	}
	matches := caseInsensitiveMatcher(query)
	if appendChangedFunctionMatches(&resp, report.ChangedFunctions, matches, limit) {
		return resp, nil
	}
	if appendTestGapMatches(&resp, report.TestGaps, matches, limit) {
		return resp, nil
	}
	if appendReviewPriorityMatches(&resp, report.ReviewPriorities, matches, limit) {
		return resp, nil
	}
	if resp.Results == nil {
		resp.Results = []GraphQueryResult{}
	}
	return resp, nil
}

// caseInsensitiveMatcher returns a predicate that matches when query is
// empty or a case-insensitive substring of any provided text.
func caseInsensitiveMatcher(query string) func(...string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	return func(texts ...string) bool {
		if q == "" {
			return true
		}
		for _, text := range texts {
			if strings.Contains(strings.ToLower(text), q) {
				return true
			}
		}
		return false
	}
}

// appendChangedFunctionMatches projects matching changed-function entries
// onto resp.Results and reports whether the limit has been reached.
func appendChangedFunctionMatches(resp *GraphQueryResponse, fns []graphstore.CRGChangedNode, matches func(...string) bool, limit int) bool {
	for _, fn := range fns {
		if !matches(fn.Name, fn.QualifiedName, fn.FilePath) {
			continue
		}
		resp.Results = append(resp.Results, GraphQueryResult{
			ID:            fn.QualifiedName,
			Type:          "changed_function",
			Title:         fn.QualifiedName,
			Summary:       fn.FilePath,
			Path:          fn.FilePath,
			QualifiedName: fn.QualifiedName,
			FilePath:      fn.FilePath,
			RiskScore:     fn.RiskScore,
		})
		if limit > 0 && len(resp.Results) >= limit {
			return true
		}
	}
	return false
}

// appendTestGapMatches projects matching test-gap entries onto resp.Results.
func appendTestGapMatches(resp *GraphQueryResponse, gaps []graphstore.CRGTestGap, matches func(...string) bool, limit int) bool {
	for _, gap := range gaps {
		if !matches(gap.QualifiedName, gap.FilePath) {
			continue
		}
		resp.Results = append(resp.Results, GraphQueryResult{
			ID:            gap.QualifiedName,
			Type:          "test_gap",
			Title:         gap.QualifiedName,
			Summary:       gap.FilePath,
			Path:          gap.FilePath,
			QualifiedName: gap.QualifiedName,
			FilePath:      gap.FilePath,
			TestCoverage:  "missing",
		})
		if limit > 0 && len(resp.Results) >= limit {
			return true
		}
	}
	return false
}

// appendReviewPriorityMatches projects matching review-priority entries
// onto resp.Results.
func appendReviewPriorityMatches(resp *GraphQueryResponse, priorities []graphstore.CRGPriority, matches func(...string) bool, limit int) bool {
	for _, priority := range priorities {
		if !matches(priority.QualifiedName, priority.Reason) {
			continue
		}
		resp.Results = append(resp.Results, GraphQueryResult{
			ID:            priority.QualifiedName,
			Type:          "review_priority",
			Title:         priority.QualifiedName,
			Summary:       priority.Reason,
			QualifiedName: priority.QualifiedName,
			RiskScore:     priority.RiskScore,
		})
		if limit > 0 && len(resp.Results) >= limit {
			return true
		}
	}
	return false
}

func collectCommunityContextResults(query string, limit int) (GraphQueryResponse, error) {
	resp := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        "community_context",
		Query:         query,
		Provider:      "crg",
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
	bridge, err := graphstore.NewCRGBridge(crgRepoRoot())
	if err != nil {
		resp.Provider = "crg-unavailable"
		resp.Warnings = append(resp.Warnings, err.Error())
		return resp, nil
	}
	communities, err := bridge.ListCommunities(0, "size")
	if err != nil {
		return resp, err
	}
	matches := caseInsensitiveMatcher(query)
	for _, community := range communities.Communities {
		if !matches(community.Name, community.Description, strings.Join(community.Members, " ")) {
			continue
		}
		resp.Results = append(resp.Results, GraphQueryResult{
			ID:         fmt.Sprintf("community:%d", community.ID),
			Type:       "community",
			Title:      community.Name,
			Summary:    fmt.Sprintf("size=%d cohesion=%.2f %s", community.Size, community.Cohesion, community.Description),
			SourceRefs: community.Members,
		})
		if limit > 0 && len(resp.Results) >= limit {
			break
		}
	}
	if resp.Results == nil {
		resp.Results = []GraphQueryResult{}
	}
	return resp, nil
}

func collectCodeBridgeResults(kgHomeDir, bridgeIntent, query string, limit int) (GraphQueryResponse, error) {
	if bridgeIntent == "change_analysis" {
		return collectChangeAnalysisResults(query, limit)
	}
	if bridgeIntent == "community_context" {
		return collectCommunityContextResults(query, limit)
	}
	store, err := openKGStore(kgHomeDir)
	if err != nil {
		return GraphQueryResponse{}, fmt.Errorf("open graph store: %w", err)
	}
	defer store.Close()

	resp := GraphQueryResponse{
		SchemaVersion: 1,
		Intent:        bridgeIntent,
		Query:         query,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Provider:      "warm-graphstore",
	}
	if err := dispatchWarmStoreBridgeIntent(store, &resp, bridgeIntent, query, limit); err != nil {
		return resp, err
	}
	if resp.Results == nil {
		resp.Results = []GraphQueryResult{}
	}
	annotateBridgeSparsity(store, &resp)
	return resp, nil
}

// dispatchWarmStoreBridgeIntent routes a warm-store bridge query to the
// matching collector, populating resp.Results / resp.Warnings on success.
func dispatchWarmStoreBridgeIntent(store *graphstore.SQLiteStore, resp *GraphQueryResponse, bridgeIntent, query string, limit int) error {
	switch bridgeIntent {
	case "symbol_lookup":
		return runSymbolLookup(store, resp, query, limit)
	case "impact_radius":
		return runImpactRadius(store, resp, query, limit)
	case "tests_for":
		return runTestsFor(store, resp, query, limit)
	case "callers_of":
		return runNeighbors(store, resp, query, graphstore.EdgeKindCalls, true, limit)
	case "callees_of":
		return runNeighbors(store, resp, query, graphstore.EdgeKindCalls, false, limit)
	case "symbol_decisions":
		return runSymbolDecisions(store, resp, query, limit)
	case "decision_symbols":
		results, err := collectDecisionSymbolResults(store, query, limit)
		if err != nil {
			return err
		}
		resp.Results = results
		return nil
	default:
		return fmt.Errorf("unknown code bridge intent %q: valid values are %s", bridgeIntent, strings.Join(sortedKeys(codeBridgeIntents), ", "))
	}
}

// runSymbolLookup populates resp with raw symbol matches for query.
func runSymbolLookup(store *graphstore.SQLiteStore, resp *GraphQueryResponse, query string, limit int) error {
	nodes, err := findCodeNodes(store, query, limit)
	if err != nil {
		return err
	}
	resp.Results = collectGraphResults(nodes, "", limit)
	return nil
}

// runImpactRadius runs the impact-radius lookup and projects both changed
// and impacted nodes onto resp, attaching warning metadata when relevant.
func runImpactRadius(store *graphstore.SQLiteStore, resp *GraphQueryResponse, query string, limit int) error {
	nodes, err := findCodeNodes(store, query, limit)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		resp.Warnings = append(resp.Warnings, "no matching code symbols found")
		return nil
	}
	files := uniqueFilePaths(nodes)
	impact, err := store.GetImpactRadius(files, 2, limit)
	if err != nil {
		return err
	}
	resp.Results = collectGraphResults(impact.ChangedNodes, "changed_symbol", limit)
	if limit <= 0 || len(resp.Results) < limit {
		resp.Results = append(resp.Results, collectGraphResults(impact.ImpactedNodes, "impacted_symbol", limit-len(resp.Results))...)
	}
	if len(impact.ImpactedFiles) > 0 {
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("impact radius spans %d files", len(impact.ImpactedFiles)))
	}
	return nil
}

// uniqueFilePaths returns the deduplicated, non-empty FilePath values of
// the given nodes.
func uniqueFilePaths(nodes []graphstore.GraphNode) []string {
	files := make([]string, 0, len(nodes))
	seen := make(map[string]bool)
	for _, node := range nodes {
		if node.FilePath == "" || seen[node.FilePath] {
			continue
		}
		seen[node.FilePath] = true
		files = append(files, node.FilePath)
	}
	return files
}

// runTestsFor runs the tests_for intent: prefers inbound tested-by edges,
// then falls back to outbound when no results were found.
func runTestsFor(store *graphstore.SQLiteStore, resp *GraphQueryResponse, query string, limit int) error {
	nodes, err := findCodeNodes(store, query, limit)
	if err != nil {
		return err
	}
	results, err := collectNeighborResults(store, nodes, graphstore.EdgeKindTestedBy, true, limit)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		results, err = collectNeighborResults(store, nodes, graphstore.EdgeKindTestedBy, false, limit)
		if err != nil {
			return err
		}
	}
	resp.Results = results
	return nil
}

// runNeighbors handles callers_of / callees_of by walking call edges in the
// requested direction.
func runNeighbors(store *graphstore.SQLiteStore, resp *GraphQueryResponse, query, edgeKind string, inbound bool, limit int) error {
	nodes, err := findCodeNodes(store, query, limit)
	if err != nil {
		return err
	}
	results, err := collectNeighborResults(store, nodes, edgeKind, inbound, limit)
	if err != nil {
		return err
	}
	resp.Results = results
	return nil
}

// runSymbolDecisions handles the symbol_decisions intent.
func runSymbolDecisions(store *graphstore.SQLiteStore, resp *GraphQueryResponse, query string, limit int) error {
	nodes, err := findCodeNodes(store, query, limit)
	if err != nil {
		return err
	}
	results, err := collectSymbolDecisionResults(store, nodes, limit)
	if err != nil {
		return err
	}
	resp.Results = results
	return nil
}

// annotateBridgeSparsity attaches the 0–100 sparsity score and the
// bridge-sparse warning when both the result set and warm store are empty.
func annotateBridgeSparsity(store *graphstore.SQLiteStore, resp *GraphQueryResponse) {
	nodeCount := store.CountNodes()
	score := computeSparsityScore(len(resp.Results), nodeCount)
	resp.SparsityScore = &score
	if len(resp.Results) == 0 && nodeCount == 0 {
		resp.Warnings = append(resp.Warnings,
			fmt.Sprintf("[bridge-sparse] warm store has %d nodes — run 'dot-agents kg build' then 'dot-agents kg warm --include-code' to populate code-lane", nodeCount))
	}
}

// computeSparsityScore returns a 0–100 sparsity score for bridge results.
// 0 means well-evidenced; 100 means no results and store is empty.
func computeSparsityScore(resultCount, storeNodeCount int) int {
	if storeNodeCount == 0 {
		return 100
	}
	if resultCount == 0 {
		return 75 // store has data but query found nothing
	}
	return 0 // found results — treat as evidenced
}

// ── Phase 5: KGAdapter interface ──────────────────────────────────────────────

// KGAdapter is the interface for pluggable graph query backends.
type KGAdapter interface {
	Name() string
	Query(query GraphQuery) (GraphQueryResponse, error)
	Health() (KGAdapterHealth, error)
	Available() bool
}

// KGAdapterHealth reports status for one adapter.
type KGAdapterHealth struct {
	AdapterName     string   `json:"adapter_name"`
	Available       bool     `json:"available"`
	LastQueryTime   string   `json:"last_query_time,omitempty"`
	LastQueryStatus string   `json:"last_query_status,omitempty"`
	NoteCount       int      `json:"note_count"`
	Warnings        []string `json:"warnings,omitempty"`
}

// LocalFileAdapter wraps the Phase 3 index-based search as a KGAdapter.
type LocalFileAdapter struct {
	kgHome        string
	lastQueryTime string
	lastStatus    string
}

func NewLocalFileAdapter(kgHome string) *LocalFileAdapter {
	return &LocalFileAdapter{kgHome: kgHome}
}

func (a *LocalFileAdapter) Name() string { return "local-file" }

func (a *LocalFileAdapter) Available() bool {
	_, err := os.Stat(filepath.Join(a.kgHome, "self", "config.yaml"))
	return err == nil
}

func (a *LocalFileAdapter) Query(query GraphQuery) (GraphQueryResponse, error) {
	resp, err := executeQuery(a.kgHome, query)
	a.lastQueryTime = time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		a.lastStatus = "error"
	} else {
		a.lastStatus = "ok"
	}
	return resp, err
}

func (a *LocalFileAdapter) Health() (KGAdapterHealth, error) {
	h := KGAdapterHealth{
		AdapterName:     a.Name(),
		Available:       a.Available(),
		LastQueryTime:   a.lastQueryTime,
		LastQueryStatus: a.lastStatus,
	}
	if !h.Available {
		h.Warnings = append(h.Warnings, "graph not initialized")
		return h, nil
	}
	// Count notes
	_ = walkNoteFiles(a.kgHome, func(_ string, _ fs.DirEntry) error {
		h.NoteCount++
		return nil
	})
	return h, nil
}

// collectAdapterHealth gathers health from all adapters and writes to ops/adapters/.
func collectAdapterHealth(kgHomeDir string, adapters []KGAdapter) []KGAdapterHealth {
	healthList := make([]KGAdapterHealth, 0, len(adapters))
	for _, adapter := range adapters {
		h, _ := adapter.Health()
		healthList = append(healthList, h)
	}
	// Write to ops/adapters/adapter-health.json
	adapterHealthPath := filepath.Join(kgHomeDir, "ops", "adapters", "adapter-health.json")
	if err := os.MkdirAll(filepath.Dir(adapterHealthPath), 0755); err == nil {
		if data, err := json.MarshalIndent(healthList, "", "  "); err == nil {
			_ = os.WriteFile(adapterHealthPath, data, 0644)
		}
	}
	return healthList
}

// ── Phase 5: Bridge endpoint ──────────────────────────────────────────────────

// executeBridgeQuery resolves a bridge intent, executes KG queries, merges results.
func executeBridgeQuery(kgHomeDir, bridgeIntent, query string) (GraphQueryResponse, error) {
	if isCodeBridgeIntent(bridgeIntent) {
		return collectCodeBridgeResults(kgHomeDir, bridgeIntent, query, 10)
	}
	queries, err := resolveBridgeQuery(bridgeIntent, query)
	if err != nil {
		return GraphQueryResponse{}, err
	}
	adapter := NewLocalFileAdapter(kgHomeDir)
	if !adapter.Available() {
		return GraphQueryResponse{}, fmt.Errorf("KG not initialized at %s: run 'dot-agents kg setup' first", kgHomeDir)
	}
	var responses []GraphQueryResponse
	for _, q := range queries {
		resp, _ := adapter.Query(q) // collect even on partial error
		responses = append(responses, resp)
	}
	merged := mergeBridgeResults(responses, bridgeIntent)
	merged.Provider = adapter.Name()
	// Update adapter health
	collectAdapterHealth(kgHomeDir, []KGAdapter{adapter})
	return merged, nil
}

// ── Phase 5: Bridge contract ──────────────────────────────────────────────────

// writeBridgeContract writes KG_HOME/self/schema/bridge-contract.yaml.
func writeBridgeContract(kgHomeDir string) error {
	schemaDir := filepath.Join(kgHomeDir, "self", "schema")
	if err := os.MkdirAll(schemaDir, 0755); err != nil {
		return err
	}
	mappings := defaultBridgeMappings()
	intents := make([]string, 0, len(mappings))
	for _, m := range mappings {
		intents = append(intents, m.BridgeIntent)
	}
	type contract struct {
		SchemaVersion    int                   `yaml:"schema_version"`
		SupportedIntents []string              `yaml:"supported_intents"`
		IntentMappings   []BridgeIntentMapping `yaml:"intent_mappings"`
		ResponseVersion  int                   `yaml:"response_version"`
		Adapters         []string              `yaml:"adapters"`
	}
	c := contract{
		SchemaVersion:    1,
		SupportedIntents: intents,
		IntentMappings:   mappings,
		ResponseVersion:  1,
		Adapters:         []string{"local-file"},
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(schemaDir, "bridge-contract.yaml"), data, 0644)
}

// ── kg bridge subcommands ─────────────────────────────────────────────────────

func runKGBridgeQuery(deps Deps, cmd *cobra.Command, args []string) error {
	home := kgHome()
	if _, err := os.Stat(kgConfigPath()); os.IsNotExist(err) {
		return fmt.Errorf("knowledge graph not initialized: run 'dot-agents kg setup' first")
	}
	intent, _ := cmd.Flags().GetString("intent")
	if intent == "" {
		return fmt.Errorf("--intent is required: valid values are %s", strings.Join(sortedKeys(validBridgeIntents), ", "))
	}
	query := strings.Join(args, " ")

	resp, err := executeBridgeQuery(home, intent, query)
	if err != nil {
		return err
	}
	if deps.Flags.JSON {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header(fmt.Sprintf("Bridge Query: %s  [%s]", intent, query))
	if len(resp.Results) == 0 {
		ui.Info("No results found.")
	} else {
		for _, r := range resp.Results {
			ui.Bullet("found", fmt.Sprintf("[%s] %s — %s", r.Type, r.Title, summarize(r.Summary, 60)))
		}
	}
	for _, w := range resp.Warnings {
		ui.Warn(w)
	}
	return nil
}

func runKGBridgeHealth(deps Deps, cmd *cobra.Command, _ []string) error {
	home := kgHome()
	adapter := NewLocalFileAdapter(home)
	adapters := []KGAdapter{adapter}
	healthList := collectAdapterHealth(home, adapters)

	if deps.Flags.JSON {
		data, _ := json.MarshalIndent(healthList, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header("KG Bridge Health")
	for _, h := range healthList {
		status := ui.ColorText(ui.Green, "available")
		if !h.Available {
			status = ui.ColorText(ui.Red, "unavailable")
		}
		ui.Info(fmt.Sprintf("  Adapter: %s  [%s]", h.AdapterName, status))
		ui.Info(fmt.Sprintf("  Notes: %d", h.NoteCount))
		if h.LastQueryTime != "" {
			ui.Info(fmt.Sprintf("  Last query: %s  status=%s", h.LastQueryTime, h.LastQueryStatus))
		}
		for _, w := range h.Warnings {
			ui.Warn(w)
		}
	}
	return nil
}

func runKGBridgeMapping(deps Deps, _ *cobra.Command, _ []string) error {
	mappings := defaultBridgeMappings()
	if deps.Flags.JSON {
		data, _ := json.MarshalIndent(mappings, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	ui.Header("Bridge Intent Mapping")
	for _, m := range mappings {
		ui.Info(fmt.Sprintf("  %-20s → %s", m.BridgeIntent, strings.Join(m.KGIntents, " + ")))
	}
	return nil
}
