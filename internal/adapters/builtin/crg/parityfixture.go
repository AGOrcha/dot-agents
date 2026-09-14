package crg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// LoadCorpus reads a pinned parity corpus from a testdata JSON file
// (testdata/crg-parity/corpus/<commit>.json). The file is the normalized
// Tree-sitter ingestion output for one commit — the same fixture both adapters
// ingest so their build/update/impact parity surfaces are directly
// comparable.
//
// The corpus is the INGESTION-side fixture. The derived views are pinned
// separately and far more strictly by the release contract; see
// LoadReleaseGraph.
func LoadCorpus(path string) (Corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Corpus{}, fmt.Errorf("crg: read corpus %s: %w", path, err)
	}
	var c Corpus
	if err := json.Unmarshal(data, &c); err != nil {
		return Corpus{}, fmt.Errorf("crg: parse corpus %s: %w", path, err)
	}
	return c, nil
}

// PinnedCommits reads the ordered list of pinned commit ids from
// testdata/crg-parity/commits.txt (one id per line; blank lines and #
// comments ignored).
func PinnedCommits(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("crg: read commits %s: %w", path, err)
	}
	var commits []string
	for _, line := range splitLines(string(data)) {
		if line == "" || line[0] == '#' {
			continue // blank lines and # comments are ignored
		}
		commits = append(commits, line)
	}
	return commits, nil
}

// splitLines splits on \n and trims trailing \r and surrounding spaces.
func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := s[start:i]
			line = trimSpace(line)
			out = append(out, line)
			start = i + 1
		}
	}
	return out
}

// trimSpace trims ASCII spaces, tabs, and carriage returns from both ends.
func trimSpace(s string) string {
	isSpace := func(b byte) bool { return b == ' ' || b == '\t' || b == '\r' }
	i, j := 0, len(s)
	for i < j && isSpace(s[i]) {
		i++
	}
	for j > i && isSpace(s[j-1]) {
		j--
	}
	return s[i:j]
}

// SortedCorpusFiles lists the per-commit corpus JSON files in commit order.
func SortedCorpusFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("crg: read corpus dir %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// ---------------------------------------------------------------------------
// Release contract fixture (testdata/crg-release/<version>/graph.json)
// ---------------------------------------------------------------------------

// repoRootPlaceholder is the token the release-contract generator substitutes
// for the fixture repository's absolute path. Every path-bearing field in
// graph.json carries it, because upstream's node identity IS the absolute
// file path — recording the generator's real path would make the fixture
// machine-specific and useless in CI.
const repoRootPlaceholder = "${REPO_ROOT}"

// ReleaseGraph is the decoded release-contract graph fixture: upstream
// code-review-graph's exact nodes, edges and derived rows for the fixture
// repository, captured from a real install.
//
// This is the parity ORACLE for the derived-view computations. The rows are
// compared field for field, which is why the loader preserves upstream's
// exact value shapes — notably KeySymbols / CriticalPath / PathJSON as raw
// JSON TEXT, since Python's and Go's encoders disagree on both separators and
// escaping.
//
// Three columns are absent by design: flows.created_at, flows.updated_at and
// risk_index.last_computed are wall-clock stamps, so the generator omits them
// rather than recording an unmatchable value. A test must not assert on them.
type ReleaseGraph struct {
	Nodes              []graphstore.GraphNode
	Edges              []graphstore.GraphEdge
	Flows              []graphstore.FlowRow
	FlowMemberships    []graphstore.FlowMembershipRow
	Communities        []graphstore.CommunityRow
	CommunitySummaries []graphstore.CommunitySummaryRow
	FlowSnapshots      []graphstore.FlowSnapshotRow
	RiskIndex          []graphstore.RiskIndexRow
	FTS                []ReleaseFTSRow
}

// ReleaseFTSRow is one expected nodes_fts row: the four indexed columns plus
// the node id they project from. nodes_fts is an external-content FTS5 table,
// so these are not separately stored values but the nodes columns the index
// exposes to a MATCH — which is exactly what makes them assertable.
type ReleaseFTSRow struct {
	NodeID        int64
	Name          string
	QualifiedName string
	FilePath      string
	Signature     string
}

// releaseGraphJSON mirrors graph.json's on-disk shape. It is kept separate
// from ReleaseGraph because the fixture uses upstream's SQL column names and
// SQLite's nullable / 0-1 encodings, neither of which the Go row structs use.
type releaseGraphJSON struct {
	Nodes []struct {
		ID            int64   `json:"id"`
		Kind          string  `json:"kind"`
		Name          string  `json:"name"`
		QualifiedName string  `json:"qualified_name"`
		FilePath      string  `json:"file_path"`
		LineStart     int     `json:"line_start"`
		LineEnd       int     `json:"line_end"`
		Language      *string `json:"language"`
		ParentName    *string `json:"parent_name"`
		Params        *string `json:"params"`
		ReturnType    *string `json:"return_type"`
		Modifiers     *string `json:"modifiers"`
		IsTest        int     `json:"is_test"`
		Signature     *string `json:"signature"`
		CommunityID   *int64  `json:"community_id"`
	} `json:"nodes"`
	Edges []struct {
		ID              int64   `json:"id"`
		Kind            string  `json:"kind"`
		SourceQualified string  `json:"source_qualified"`
		TargetQualified string  `json:"target_qualified"`
		FilePath        string  `json:"file_path"`
		Line            int     `json:"line"`
		Confidence      float64 `json:"confidence"`
		ConfidenceTier  string  `json:"confidence_tier"`
	} `json:"edges"`
	Flows []struct {
		ID           int64   `json:"id"`
		Name         string  `json:"name"`
		EntryPointID int64   `json:"entry_point_id"`
		Depth        int     `json:"depth"`
		NodeCount    int     `json:"node_count"`
		FileCount    int     `json:"file_count"`
		Criticality  float64 `json:"criticality"`
		PathJSON     string  `json:"path_json"`
	} `json:"flows"`
	FlowMemberships []struct {
		FlowID   int64 `json:"flow_id"`
		NodeID   int64 `json:"node_id"`
		Position int   `json:"position"`
	} `json:"flow_memberships"`
	Communities []struct {
		ID               int64   `json:"id"`
		Name             string  `json:"name"`
		Level            int     `json:"level"`
		ParentID         *int64  `json:"parent_id"`
		Cohesion         float64 `json:"cohesion"`
		Size             int     `json:"size"`
		DominantLanguage *string `json:"dominant_language"`
		Description      *string `json:"description"`
	} `json:"communities"`
	CommunitySummaries []struct {
		CommunityID      int64  `json:"community_id"`
		Name             string `json:"name"`
		Purpose          string `json:"purpose"`
		KeySymbols       string `json:"key_symbols"`
		Risk             string `json:"risk"`
		Size             int    `json:"size"`
		DominantLanguage string `json:"dominant_language"`
	} `json:"community_summaries"`
	FlowSnapshots []struct {
		FlowID       int64   `json:"flow_id"`
		Name         string  `json:"name"`
		EntryPoint   string  `json:"entry_point"`
		CriticalPath string  `json:"critical_path"`
		Criticality  float64 `json:"criticality"`
		NodeCount    int     `json:"node_count"`
		FileCount    int     `json:"file_count"`
	} `json:"flow_snapshots"`
	RiskIndex []struct {
		NodeID           int64   `json:"node_id"`
		QualifiedName    string  `json:"qualified_name"`
		RiskScore        float64 `json:"risk_score"`
		CallerCount      int     `json:"caller_count"`
		TestCoverage     string  `json:"test_coverage"`
		SecurityRelevant int     `json:"security_relevant"`
	} `json:"risk_index"`
	FTS []struct {
		NodeID        int64  `json:"node_id"`
		Name          string `json:"name"`
		QualifiedName string `json:"qualified_name"`
		FilePath      string `json:"file_path"`
		Signature     string `json:"signature"`
	} `json:"fts"`
}

// LoadReleaseGraph reads the release-contract graph fixture at path and
// rebases every ${REPO_ROOT} placeholder onto repoRoot.
//
// repoRoot MUST be the absolute path the caller will actually seed the store
// with (a t.TempDir(), typically). Substituting rather than hardcoding is not
// cosmetic: node identity IS the absolute path, so the expected qualified
// names, edge endpoints, community descriptions and flow_snapshots critical
// paths all move with it, and a test that hardcoded the generator's path
// would pass only on the machine that generated the fixture.
//
// The root is slash-normalized before substitution because node identity is
// slash-normalized by construction: the scanner stores
// filepath.ToSlash(filepath.Join(absRoot, rel)), so a Windows node is
// `C:/tmp/x/pkg/auth/auth.go`, never `C:\tmp\x\...`. Splicing a NATIVE root
// into the fixture would fabricate an identity the product never produces,
// and the mismatch surfaces only where an identity is re-encoded — e.g.
// flow_snapshots.critical_path, whose json.dumps escaping doubles every
// backslash the expected value carries raw.
func LoadReleaseGraph(path, repoRoot string) (ReleaseGraph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ReleaseGraph{}, fmt.Errorf("crg: read release graph %s: %w", path, err)
	}
	var raw releaseGraphJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return ReleaseGraph{}, fmt.Errorf("crg: parse release graph %s: %w", path, err)
	}
	slashRoot := filepath.ToSlash(repoRoot)
	rebase := func(s string) string { return strings.ReplaceAll(s, repoRootPlaceholder, slashRoot) }

	var g ReleaseGraph
	for _, n := range raw.Nodes {
		g.Nodes = append(g.Nodes, graphstore.GraphNode{
			ID:            n.ID,
			Kind:          n.Kind,
			Name:          rebase(n.Name),
			QualifiedName: rebase(n.QualifiedName),
			FilePath:      rebase(n.FilePath),
			LineStart:     n.LineStart,
			LineEnd:       n.LineEnd,
			Language:      derefOrEmpty(n.Language),
			ParentName:    derefOrEmpty(n.ParentName),
			Params:        derefOrEmpty(n.Params),
			ReturnType:    derefOrEmpty(n.ReturnType),
			IsTest:        n.IsTest != 0,
			Signature:     rebase(derefOrEmpty(n.Signature)),
			CommunityID:   derefOrZero(n.CommunityID),
		})
	}
	for _, e := range raw.Edges {
		g.Edges = append(g.Edges, graphstore.GraphEdge{
			ID:              e.ID,
			Kind:            e.Kind,
			SourceQualified: rebase(e.SourceQualified),
			TargetQualified: rebase(e.TargetQualified),
			FilePath:        rebase(e.FilePath),
			Line:            e.Line,
			Confidence:      e.Confidence,
			ConfidenceTier:  e.ConfidenceTier,
		})
	}
	for _, f := range raw.Flows {
		g.Flows = append(g.Flows, graphstore.FlowRow{
			ID:           f.ID,
			Name:         f.Name,
			EntryPointID: f.EntryPointID,
			Depth:        f.Depth,
			NodeCount:    f.NodeCount,
			FileCount:    f.FileCount,
			Criticality:  f.Criticality,
			PathJSON:     f.PathJSON,
		})
	}
	for _, m := range raw.FlowMemberships {
		g.FlowMemberships = append(g.FlowMemberships, graphstore.FlowMembershipRow{
			FlowID: m.FlowID, NodeID: m.NodeID, Position: m.Position,
		})
	}
	for _, c := range raw.Communities {
		g.Communities = append(g.Communities, graphstore.CommunityRow{
			ID:               c.ID,
			Name:             c.Name,
			Level:            c.Level,
			ParentID:         c.ParentID,
			Cohesion:         c.Cohesion,
			Size:             c.Size,
			DominantLanguage: derefOrEmpty(c.DominantLanguage),
			Description:      derefOrEmpty(c.Description),
		})
	}
	for _, s := range raw.CommunitySummaries {
		g.CommunitySummaries = append(g.CommunitySummaries, graphstore.CommunitySummaryRow{
			CommunityID:      s.CommunityID,
			Name:             s.Name,
			Purpose:          s.Purpose,
			KeySymbols:       rebase(s.KeySymbols),
			Risk:             s.Risk,
			Size:             s.Size,
			DominantLanguage: s.DominantLanguage,
		})
	}
	for _, s := range raw.FlowSnapshots {
		g.FlowSnapshots = append(g.FlowSnapshots, graphstore.FlowSnapshotRow{
			FlowID:       s.FlowID,
			Name:         s.Name,
			EntryPoint:   rebase(s.EntryPoint),
			CriticalPath: rebase(s.CriticalPath),
			Criticality:  s.Criticality,
			NodeCount:    s.NodeCount,
			FileCount:    s.FileCount,
		})
	}
	for _, r := range raw.RiskIndex {
		g.RiskIndex = append(g.RiskIndex, graphstore.RiskIndexRow{
			NodeID:           r.NodeID,
			QualifiedName:    rebase(r.QualifiedName),
			RiskScore:        r.RiskScore,
			CallerCount:      r.CallerCount,
			TestCoverage:     r.TestCoverage,
			SecurityRelevant: r.SecurityRelevant != 0,
		})
	}
	for _, f := range raw.FTS {
		g.FTS = append(g.FTS, ReleaseFTSRow{
			NodeID:        f.NodeID,
			Name:          rebase(f.Name),
			QualifiedName: rebase(f.QualifiedName),
			FilePath:      rebase(f.FilePath),
			Signature:     rebase(f.Signature),
		})
	}
	return g, nil
}

// NodeInfos projects the fixture's nodes onto the writer's input shape, in
// fixture ID ORDER.
//
// The order is load-bearing: AUTOINCREMENT assigns ids by insertion, so
// seeding a FRESH store in this order reproduces the fixture's node ids
// 1..N — and every derived row is keyed by them (flows.path_json,
// flow_memberships.node_id, risk_index.node_id, nodes_fts rowids). Seeding out
// of order, or into a store that already holds nodes, silently shifts every id
// and no derived row will match.
//
// QualifiedName is deliberately NOT passed through: the store computes it from
// kind/file/parent, so a round trip through this projection also verifies that
// native identity construction agrees with upstream's.
func (g ReleaseGraph) NodeInfos() []graphstore.NodeInfo {
	out := make([]graphstore.NodeInfo, 0, len(g.Nodes))
	for _, n := range sortedByNodeID(g.Nodes) {
		out = append(out, graphstore.NodeInfo{
			Kind:       n.Kind,
			Name:       n.Name,
			FilePath:   n.FilePath,
			LineStart:  n.LineStart,
			LineEnd:    n.LineEnd,
			Language:   n.Language,
			ParentName: n.ParentName,
			Params:     n.Params,
			ReturnType: n.ReturnType,
			IsTest:     n.IsTest,
		})
	}
	return out
}

// EdgeInfos projects the fixture's edges onto the writer's input shape, in
// fixture ID order for the same reason NodeInfos is.
func (g ReleaseGraph) EdgeInfos() []graphstore.EdgeInfo {
	out := make([]graphstore.EdgeInfo, 0, len(g.Edges))
	for _, e := range sortedByEdgeID(g.Edges) {
		out = append(out, graphstore.EdgeInfo{
			Kind:     e.Kind,
			Source:   e.SourceQualified,
			Target:   e.TargetQualified,
			FilePath: e.FilePath,
			Line:     e.Line,
		})
	}
	return out
}

// sortedByNodeID copies nodes into ascending id order without disturbing the
// caller's slice.
func sortedByNodeID(nodes []graphstore.GraphNode) []graphstore.GraphNode {
	out := append([]graphstore.GraphNode(nil), nodes...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// sortedByEdgeID copies edges into ascending id order without disturbing the
// caller's slice.
func sortedByEdgeID(edges []graphstore.GraphEdge) []graphstore.GraphEdge {
	out := append([]graphstore.GraphEdge(nil), edges...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// derefOrEmpty reads a nullable fixture string as the Go zero value, matching
// how the store surfaces a NULL TEXT column.
func derefOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// derefOrZero reads a nullable fixture integer as 0, matching how the store
// surfaces a NULL community_id ("unassigned").
func derefOrZero(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
