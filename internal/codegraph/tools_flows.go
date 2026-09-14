package codegraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/crgrelease"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// Execution-flow tools: list_flows, get_flow and get_affected_flows.
//
// Ported from code_review_graph/tools/flows_tools.py and the
// get_affected_flows half of tools/review.py, together with the three query
// helpers they use from flows.py (get_flows, get_flow_by_id,
// get_affected_flows).
//
// This file PROJECTS flows; it never derives them. The rows come from the
// `flows` and `flow_memberships` tables through graphstore.CodeGraphDerived,
// which the postprocess lifecycle owns. Re-tracing a flow here to answer a
// query would give two independent definitions of what a flow is, and the
// first divergence between them would be invisible.

func init() {
	RegisterTool("list_flows_tool", listFlowsTool)
	RegisterTool("get_flow_tool", getFlowTool)
	RegisterTool("get_affected_flows_tool", getAffectedFlowsTool)
}

// Hard ceilings from flows_tools.py and review.py. They are per-tool rather
// than shared because they bound different things: a flow COUNT, a step count
// within one flow, a source-line budget, and a step budget shared across
// flows.
const (
	// maxListedFlows is flows_tools._MAX_FLOWS.
	maxListedFlows = 200
	// maxFlowSteps is flows_tools._MAX_FLOW_STEPS.
	maxFlowSteps = 200
	// maxFlowSourceLines is flows_tools._MAX_FLOW_SOURCE_LINES.
	maxFlowSourceLines = 2000
	// maxAffectedFlowsStandard / maxAffectedFlowsMinimal are review.py's
	// per-detail-level ceilings: a standard flow carries a full step list at
	// roughly 980 tokens against 18 for a minimal one.
	maxAffectedFlowsStandard = 25
	maxAffectedFlowsMinimal  = 500
	// maxAffectedFlowSteps is review.py's step budget shared across the
	// returned flows, so a codebase with very deep call chains cannot blow
	// the response budget with a legal flow count.
	maxAffectedFlowSteps = 400
	// flowNameSearchLimit is the number of flows get_flow scans when
	// selecting by name.
	flowNameSearchLimit = 500
	// fetchAllFlows is flows_tools._FETCH_ALL: get_flows slices in SQL, so
	// asking for "all" is how list_flows counts the untruncated total before
	// keeping a bounded prefix.
	fetchAllFlows = 1_000_000_000
)

// flowDetailMinimal is the projection both list_flows and get_affected_flows
// switch on.
const flowDetailMinimal = "minimal"

// affectedFlowMinimalFields is review.py's _DETECT_FLOW_FIELDS: the per-flow
// metadata minimal mode keeps, dropping the step list that made this tool
// return 247k tokens before it was bounded.
var affectedFlowMinimalFields = []string{
	"id", "name", "criticality", "depth", "node_count", "file_count",
}

// listFlowsTool answers list_flows_tool.
func listFlowsTool(e *Engine, args crgrelease.Args) (any, error) {
	limit := args.Int("limit")
	if err := ValidatePositiveInt(limit, "limit"); err != nil {
		// Upstream validates BEFORE opening the store, so the failure is a
		// raised exception and the MCP result is an error, not a payload.
		return nil, err
	}

	store, err := e.readStore()
	if err != nil {
		return flowToolError(err), nil
	}
	rows, err := readFlowRows(store)
	if err != nil {
		return flowToolError(err), nil
	}
	flows, err := releaseFlows(rows, args.String("sort_by"), fetchAllFlows)
	if err != nil {
		return flowToolError(err), nil
	}

	// `if kind:` — an empty string is falsy upstream, so it filters nothing.
	if kind := args.String("kind"); kind != "" {
		kinds, err := entryPointKinds(store)
		if err != nil {
			return flowToolError(err), nil
		}
		filtered := make([]map[string]any, 0, len(flows))
		for _, flow := range flows {
			if kinds[flow["entry_point_id"].(int64)] == kind {
				filtered = append(filtered, flow)
			}
		}
		flows = filtered
	}

	flows, total, truncated := Bounded(flows, limit, maxListedFlows)
	if args.String("detail_level") == flowDetailMinimal {
		minimal := make([]map[string]any, 0, len(flows))
		for _, flow := range flows {
			minimal = append(minimal, map[string]any{
				"name":        flow["name"],
				"criticality": flow["criticality"],
				"node_count":  flow["node_count"],
			})
		}
		flows = minimal
	}

	result := map[string]any{
		"status": "ok",
		"summary": fmt.Sprintf("Found %d execution flow(s)", total) +
			ShownOf(len(flows), total),
		"flows":     flows,
		"total":     total,
		"truncated": truncated,
	}
	return e.AttachHints("list_flows", result), nil
}

// getFlowTool answers get_flow_tool.
func getFlowTool(e *Engine, args crgrelease.Args) (any, error) {
	maxSteps := args.Int("max_steps")
	if err := ValidatePositiveInt(maxSteps, "max_steps"); err != nil {
		return nil, err
	}
	maxSourceLines := args.Int("max_source_lines")
	if err := ValidatePositiveInt(maxSourceLines, "max_source_lines"); err != nil {
		return nil, err
	}

	store, err := e.readStore()
	if err != nil {
		return flowToolError(err), nil
	}
	rows, err := readFlowRows(store)
	if err != nil {
		return flowToolError(err), nil
	}

	selected, err := selectFlowRow(rows, args)
	if err != nil {
		return flowToolError(err), nil
	}

	if selected == nil {
		// Neither selector given, or no match. Upstream returns before
		// generating hints, so this payload deliberately carries no `_hints`.
		return map[string]any{
			"status":  "not_found",
			"summary": "No flow found matching the given criteria.",
		}, nil
	}

	nodes, err := nodesByID(store)
	if err != nil {
		return flowToolError(err), nil
	}
	flow, path, err := flowRowToMap(*selected)
	if err != nil {
		return flowToolError(err), nil
	}
	allSteps := flowSteps(path, nodes)

	steps, totalSteps, truncated := Bounded(allSteps, maxSteps, maxFlowSteps)
	flow["steps"] = steps
	flow["total_steps"] = totalSteps
	flow["truncated"] = truncated

	if args.Bool("include_source") {
		attachFlowSource(e.root, flow, steps, min(maxSourceLines, maxFlowSourceLines))
	}

	// The summary reads the SANITIZED name out of the payload, because that
	// is the name a client sees; the three numbers come from the row so the
	// format verbs are statically typed.
	name, _ := flow["name"].(string)
	result := map[string]any{
		"status": "ok",
		"summary": fmt.Sprintf("Flow '%s': %d nodes, depth %d, criticality %.4f",
			name, selected.NodeCount, selected.Depth, selected.Criticality) +
			ShownOf(len(steps), totalSteps),
		"flow": flow,
	}
	return e.AttachHints("get_flow", result), nil
}

// selectFlowRow resolves get_flow's two mutually exclusive selectors, in
// upstream's precedence: `flow_id` first, then `flow_name`. Neither selector
// set reads as "no flow", which the caller reports as not_found.
func selectFlowRow(rows []graphstore.FlowRow, args crgrelease.Args) (*graphstore.FlowRow, error) {
	if flowID, byID := args.OptInt("flow_id"); byID {
		return flowRowByID(rows, int64(flowID)), nil
	}
	if flowName, byName := args.OptString("flow_name"); byName {
		return flowRowByName(rows, flowName)
	}
	return nil, nil
}

// flowRowByID finds a stored flow by primary key, reporting nil for an id the
// graph does not have.
func flowRowByID(rows []graphstore.FlowRow, id int64) *graphstore.FlowRow {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

// flowRowByName searches the criticality-ordered list and takes the first
// case-insensitive substring match. An explicitly supplied empty name
// therefore matches the most critical flow rather than nothing, because
// `"" in name` is true — which is why the selector has to be read as "was it
// SET", not "is it non-empty".
//
// The search stops at the first matching name even if that candidate's row has
// since gone, which is upstream's unconditional `break`.
func flowRowByName(rows []graphstore.FlowRow, flowName string) (*graphstore.FlowRow, error) {
	candidates, err := releaseFlows(rows, "criticality", flowNameSearchLimit)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(flowName)
	var selected *graphstore.FlowRow
	for _, candidate := range candidates {
		if strings.Contains(strings.ToLower(candidate["name"].(string)), needle) {
			selected = flowRowByID(rows, candidate["id"].(int64))
			break
		}
	}
	return selected, nil
}

// getAffectedFlowsTool answers get_affected_flows_tool.
func getAffectedFlowsTool(e *Engine, args crgrelease.Args) (any, error) {
	changedFiles, explicit := args.StringSlice("changed_files")
	if !explicit {
		// An UNSET list means "auto-detect"; an empty list means "nothing
		// changed" and must not trigger detection. Upstream distinguishes
		// them with `changed_files is None`.
		// e.changedFiles is the shared ReleaseChangedFiles port; going
		// through the seam is what lets a test drive a diff without
		// building a git history.
		detected, err := e.changedFiles(e.root, args.String("base"))
		if err != nil {
			return flowToolError(err), nil
		}
		if len(detected) == 0 {
			detected, err = ReleaseWorkingTreeFiles(e.root)
			if err != nil {
				return flowToolError(err), nil
			}
		}
		changedFiles = detected
	}

	if len(changedFiles) == 0 {
		// Upstream returns here, before the store is queried and before
		// hints are generated, so this payload carries neither `truncated`
		// nor `_hints` nor `changed_files`.
		return map[string]any{
			"status":         "ok",
			"summary":        "No changed files detected.",
			"affected_flows": []map[string]any{},
			"total":          0,
		}, nil
	}

	store, err := e.readStore()
	if err != nil {
		return flowToolError(err), nil
	}
	flows, err := flowsAffectedByFiles(store, e.root, changedFiles)
	if err != nil {
		return flowToolError(err), nil
	}
	total := len(flows)

	ceiling := maxAffectedFlowsStandard
	minimal := args.String("detail_level") == flowDetailMinimal
	if minimal {
		ceiling = maxAffectedFlowsMinimal
	}
	// `max_flows=0` keeps its documented "no caller limit" meaning, but the
	// per-detail-level ceiling still applies: an escape hatch that can return
	// 250k tokens is the bug this bound was added for, not a feature.
	limit := ceiling
	if maxFlows := args.Int("max_flows"); maxFlows > 0 {
		limit = min(maxFlows, ceiling)
	}
	truncated := total > limit
	if len(flows) > limit {
		flows = flows[:limit]
	}
	if minimal {
		flows = projectFlows(flows, affectedFlowMinimalFields)
	} else {
		var stepsCut bool
		flows, stepsCut = boundFlowSteps(flows)
		truncated = truncated || stepsCut
	}

	result := map[string]any{
		"status": "ok",
		"summary": fmt.Sprintf("%d flow(s) affected by changes in %d file(s)",
			total, len(changedFiles)) + ShownOf(len(flows), total),
		"changed_files":  changedFiles,
		"affected_flows": flows,
		"total":          total,
		"truncated":      truncated,
	}
	return e.AttachHints("get_affected_flows", result), nil
}

// ── flow projection ──────────────────────────────────────────────────────────

// releaseFlows is flows.get_flows: the stored flows ordered by sortBy and cut
// to limit.
//
// The sort is STABLE over primary-key order, which is what SQLite's sorter
// produces for the `ORDER BY <column> LIMIT ?` upstream runs: ties keep their
// row order, so `sort_by=node_count` on equal counts stays id-ordered.
func releaseFlows(rows []graphstore.FlowRow, sortBy string, limit int) ([]map[string]any, error) {
	ordered := make([]graphstore.FlowRow, len(rows))
	copy(ordered, rows)
	sortFlowRows(ordered, sortBy)
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	flows := make([]map[string]any, 0, len(ordered))
	for _, row := range ordered {
		flow, _, err := flowRowToMap(row)
		if err != nil {
			return nil, err
		}
		flows = append(flows, flow)
	}
	return flows, nil
}

// sortFlowRows applies upstream's ORDER BY. An unrecognised column falls back
// to criticality, and only `name` sorts ascending.
func sortFlowRows(rows []graphstore.FlowRow, sortBy string) {
	switch sortBy {
	case "depth":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Depth > rows[j].Depth })
	case "node_count":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].NodeCount > rows[j].NodeCount })
	case "file_count":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].FileCount > rows[j].FileCount })
	case "name":
		// SQLite's default BINARY collation compares bytes, so "ValidateToken"
		// sorts before "main". Go's string comparison is byte-wise too.
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	default:
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i].Criticality > rows[j].Criticality
		})
	}
}

// flowRowToMap is the flow dict flows.get_flows and flows.get_flow_by_id
// share, minus the steps get_flow_by_id adds.
//
// The decoded node-id path is returned alongside it so a caller that also
// needs the steps decodes the stored blob once rather than twice.
func flowRowToMap(row graphstore.FlowRow) (map[string]any, []int64, error) {
	path, err := decodeFlowPath(row.PathJSON)
	if err != nil {
		return nil, nil, err
	}
	return map[string]any{
		"id":             row.ID,
		"name":           SanitizeName(row.Name),
		"entry_point_id": row.EntryPointID,
		"depth":          row.Depth,
		"node_count":     row.NodeCount,
		"file_count":     row.FileCount,
		"criticality":    row.Criticality,
		"path":           path,
		"created_at":     row.CreatedAt,
		"updated_at":     row.UpdatedAt,
	}, path, nil
}

// decodeFlowPath reads the stored node-id path. A malformed path is an error
// rather than an empty list: the path IS the flow, and reporting a flow with
// no steps would misrepresent it as trivial.
func decodeFlowPath(pathJSON string) ([]int64, error) {
	if pathJSON == "" {
		return []int64{}, nil
	}
	var path []int64
	if err := json.Unmarshal([]byte(pathJSON), &path); err != nil {
		return nil, fmt.Errorf("decode flow path: %w", err)
	}
	if path == nil {
		path = []int64{}
	}
	return path, nil
}

// flowSteps expands a flow's node-id path into the release's step records.
// A path entry with no surviving node is skipped, exactly as upstream's
// `if node:` does — a flow can outlive a node the last incremental update
// removed.
func flowSteps(
	path []int64, nodes map[int64]graphstore.GraphNode,
) []map[string]any {
	steps := make([]map[string]any, 0, len(path))
	for _, id := range path {
		node, ok := nodes[id]
		if !ok {
			continue
		}
		steps = append(steps, map[string]any{
			"node_id":        node.ID,
			"name":           SanitizeName(node.Name),
			"kind":           node.Kind,
			"file":           node.FilePath,
			"line_start":     node.LineStart,
			"line_end":       node.LineEnd,
			"qualified_name": SanitizeName(node.QualifiedName),
		})
	}
	return steps
}

// projectFlows keeps only the named fields on each flow, dropping keys a flow
// does not have — review.py's _project.
func projectFlows(flows []map[string]any, fields []string) []map[string]any {
	projected := make([]map[string]any, 0, len(flows))
	for _, flow := range flows {
		kept := make(map[string]any, len(fields))
		for _, field := range fields {
			if value, ok := flow[field]; ok {
				kept[field] = value
			}
		}
		projected = append(projected, kept)
	}
	return projected
}

// boundFlowSteps spends review.py's shared step budget across flows, most
// critical first — the flows a reviewer cares about keep their full step
// list, the tail keeps its metadata and is marked `steps_omitted`. Every flow
// reports `total_steps`, so the untruncated depth is never lost.
func boundFlowSteps(flows []map[string]any) ([]map[string]any, bool) {
	budget := maxAffectedFlowSteps
	truncated := false
	bounded := make([]map[string]any, 0, len(flows))
	for _, flow := range flows {
		out := make(map[string]any, len(flow)+2)
		for key, value := range flow {
			out[key] = value
		}
		steps, _ := out["steps"].([]map[string]any)
		out["total_steps"] = len(steps)
		if len(steps) > budget {
			out["steps"] = steps[:budget]
			out["steps_omitted"] = true
			truncated = true
		}
		budget -= min(len(steps), budget)
		bounded = append(bounded, out)
	}
	return bounded, truncated
}

// ── affected flows ───────────────────────────────────────────────────────────

// flowsAffectedByFiles is flows.get_affected_flows: the flows whose membership
// includes a node from one of the changed files, most critical first.
func flowsAffectedByFiles(
	store graphstore.Store, root string, changedFiles []string,
) ([]map[string]any, error) {
	if store == nil {
		return []map[string]any{}, nil
	}
	wanted := changedFilePathSet(root, changedFiles)

	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	changedNodes := nodeIDsInFiles(nodes, wanted)
	if len(changedNodes) == 0 {
		return []map[string]any{}, nil
	}

	memberships, err := store.ReadFlowMemberships()
	if err != nil {
		return nil, err
	}
	flowIDs := flowIDsTouchingNodes(memberships, changedNodes)
	if len(flowIDs) == 0 {
		return []map[string]any{}, nil
	}

	rows, err := readFlowRows(store)
	if err != nil {
		return nil, err
	}
	affected, err := affectedFlowMaps(rows, flowIDs, graphNodesByID(nodes))
	if err != nil {
		return nil, err
	}
	sort.SliceStable(affected, func(i, j int) bool {
		return affected[i]["criticality"].(float64) > affected[j]["criticality"].(float64)
	})
	return affected, nil
}

// changedFilePathSet is the set of graph file paths a diff touches.
//
// Graph identity is the absolute, POSIX-separated path, so a relative diff
// path has to be joined to the root before it can match a node. An
// already-absolute path passes through unchanged, the way pathlib's `/`
// operator does.
func changedFilePathSet(root string, changedFiles []string) map[string]bool {
	wanted := make(map[string]bool, len(changedFiles))
	for _, file := range changedFiles {
		absolute := file
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(root, absolute)
		}
		wanted[NormalizeFilePath(absolute)] = true
	}
	return wanted
}

// nodeIDsInFiles selects the nodes that live in one of the wanted files.
func nodeIDsInFiles(nodes []graphstore.GraphNode, wanted map[string]bool) map[int64]bool {
	changed := map[int64]bool{}
	for _, node := range nodes {
		if wanted[NormalizeFilePath(node.FilePath)] {
			changed[node.ID] = true
		}
	}
	return changed
}

// flowIDsTouchingNodes lists the distinct flows whose membership includes one
// of the changed nodes.
//
// Memberships arrive in (flow_id, node_id) order — the table's primary key,
// and the scan order of upstream's `SELECT DISTINCT flow_id ... WHERE node_id
// IN (...)`. Preserving it keeps the criticality sort's tie-breaking
// identical.
func flowIDsTouchingNodes(memberships []graphstore.FlowMembershipRow, changedNodes map[int64]bool) []int64 {
	var flowIDs []int64
	seen := map[int64]bool{}
	for _, membership := range memberships {
		if !changedNodes[membership.NodeID] || seen[membership.FlowID] {
			continue
		}
		seen[membership.FlowID] = true
		flowIDs = append(flowIDs, membership.FlowID)
	}
	return flowIDs
}

// affectedFlowMaps projects the named flows, steps included. A flow id with no
// surviving row is skipped: a membership can outlive the flow it named.
func affectedFlowMaps(
	rows []graphstore.FlowRow, flowIDs []int64, nodes map[int64]graphstore.GraphNode,
) ([]map[string]any, error) {
	byID := flowRowsByID(rows)
	affected := make([]map[string]any, 0, len(flowIDs))
	for _, id := range flowIDs {
		row, ok := byID[id]
		if !ok {
			continue
		}
		flow, path, err := flowRowToMap(row)
		if err != nil {
			return nil, err
		}
		flow["steps"] = flowSteps(path, nodes)
		affected = append(affected, flow)
	}
	return affected, nil
}

// flowRowsByID indexes stored flows by primary key.
func flowRowsByID(rows []graphstore.FlowRow) map[int64]graphstore.FlowRow {
	byID := make(map[int64]graphstore.FlowRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	return byID
}

// ── source snippets ──────────────────────────────────────────────────────────

// attachFlowSource inlines each visible step's source, spending a shared line
// budget so a deep flow cannot inline its whole call chain.
//
// Running out of budget marks the flow truncated AND sets `source_truncated`,
// which is how a caller tells "the step list was cut" from "the steps are all
// here but some carry no source".
func attachFlowSource(root string, flow map[string]any, steps []map[string]any, budget int) {
	for _, step := range steps {
		if budget <= 0 {
			flow["truncated"] = true
			flow["source_truncated"] = true
			return
		}
		source, spent, inlinable := flowStepSource(root, step, budget)
		if !inlinable {
			continue
		}
		step["source"] = source
		budget -= spent
	}
}

// flowStepSource renders one step's numbered source slice within the remaining
// shared line budget, reporting the lines that slice spends.
//
// The last result is false when the step carries nothing inlinable at all — no
// file, or a file that is not a readable regular file — in which case the step
// record is left exactly as it was, spending no budget.
func flowStepSource(root string, step map[string]any, budget int) (source string, spent int, inlinable bool) {
	file, _ := step["file"].(string)
	if file == "" {
		return "", 0, false
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(root, file)
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, false
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "(could not read file)", 0, true
	}
	// splitSourceLines is the package-shared port of Python's
	// read_text(errors="replace").splitlines(), owned by tools_context.go:
	// both this tool and get_review_context number source lines, and two
	// splitters would mean two numberings for the same file.
	lines := splitSourceLines(string(data))
	start, end := flowStepLineRange(step, len(lines), budget)
	return renderFlowSourceLines(lines, start, end), max(0, end-start), true
}

// flowStepLineRange turns a step's stored 1-based span into a 0-based
// half-open range over a file of lineCount lines, clipped to the budget.
//
// A missing or non-positive start reads as the first line and a missing end as
// the end of the file, which is what upstream's `or` defaults do.
func flowStepLineRange(step map[string]any, lineCount, budget int) (start, end int) {
	start = flowLineNumber(step["line_start"])
	if start < 1 {
		start = 1
	}
	start--
	end = flowLineNumber(step["line_end"])
	if end < 1 {
		end = lineCount
	}
	return start, min(lineCount, end, start+budget)
}

// renderFlowSourceLines joins lines[start:end) one per line, each prefixed
// with its 1-based number. An empty range renders as the empty string.
func renderFlowSourceLines(lines []string, start, end int) string {
	var rendered strings.Builder
	for i := start; i < end; i++ {
		if i > start {
			rendered.WriteByte('\n')
		}
		fmt.Fprintf(&rendered, "%d: %s", i+1, lines[i])
	}
	return rendered.String()
}

// ── store access ─────────────────────────────────────────────────────────────

// readFlowRows reads the stored flows. A graph that was never built reads as
// no flows rather than as an error: upstream's store constructor creates an
// empty database on demand, so `list_flows` on an unbuilt repository reports
// zero flows instead of failing.
func readFlowRows(store graphstore.Store) ([]graphstore.FlowRow, error) {
	if store == nil {
		return nil, nil
	}
	return store.ReadFlows()
}

// nodesByID indexes every node by primary key, which is how a flow's stored
// id path is resolved to steps.
func nodesByID(store graphstore.Store) (map[int64]graphstore.GraphNode, error) {
	if store == nil {
		return map[int64]graphstore.GraphNode{}, nil
	}
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	return graphNodesByID(nodes), nil
}

// graphNodesByID indexes nodes by primary key.
func graphNodesByID(nodes []graphstore.GraphNode) map[int64]graphstore.GraphNode {
	index := make(map[int64]graphstore.GraphNode, len(nodes))
	for _, node := range nodes {
		index[node.ID] = node
	}
	return index
}

// entryPointKinds maps node id to kind for list_flows' `kind` filter, which
// selects flows by the kind of the node they start from.
func entryPointKinds(store graphstore.Store) (map[int64]string, error) {
	if store == nil {
		return map[int64]string{}, nil
	}
	nodes, err := store.ReadAllNodes()
	if err != nil {
		return nil, err
	}
	kinds := make(map[int64]string, len(nodes))
	for _, node := range nodes {
		kinds[node.ID] = node.Kind
	}
	return kinds, nil
}

// flowToolError is the in-band failure these three tools return.
//
// It is deliberately NOT ErrorResponse: flows_tools.py and review.py wrap
// their bodies in `except Exception as exc: return {"status": "error",
// "error": str(exc)}`, with no `summary` key, unlike the tools that call
// `_error_response`. Adding a summary here would invent a field the release
// does not send.
func flowToolError(err error) map[string]any {
	return map[string]any{"status": "error", "error": err.Error()}
}

// flowLineNumber reads an integer out of a step record, whose values are stored as
// `any` because they are handed straight to the JSON encoder.
func flowLineNumber(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	}
	return 0
}
