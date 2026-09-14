package crgrelease

import "fmt"

// Backend names the engine that answers a tool call.
type Backend string

const (
	// BackendNative is the in-process kg-native code-graph engine.
	BackendNative Backend = "native"
	// BackendBridge is the retained Python `code-review-graph` subprocess.
	// Phase A keeps it as the rollback path AND as the only implementation of
	// the capabilities the native engine cannot reproduce exactly.
	BackendBridge Backend = "bridge"
)

// ToolCapability records where one published tool can be served from, and why.
//
// The `why` is the load-bearing part. "Route through the bridge" is only an
// honest answer if the reason is a named upstream capability the native
// engine genuinely does not have; otherwise it is a silent regression.
type ToolCapability struct {
	Tool string
	// NativeBackend is true when a native handler reproduces this tool's
	// v2.3.8 response exactly for a repository the native scanner fully
	// covers. It is NOT a claim about repositories it does not cover — that
	// is decided per repository by the source-capability diagnostic.
	NativeBackend bool
	// BridgeOnlyReason names the upstream capability that keeps the tool on
	// the bridge. Empty exactly when NativeBackend is true.
	BridgeOnlyReason string
}

// Backend returns where the tool is served from for a repository whose
// sources the native scanner fully covers.
func (c ToolCapability) Backend() Backend {
	if c.NativeBackend {
		return BackendNative
	}
	return BackendBridge
}

// Bridge-only reasons. Each names a concrete upstream dependency, so a future
// phase can retire one by implementing that dependency rather than by
// relabelling the tool.
const (
	reasonEmbeddings = "upstream computes vector embeddings through sentence-transformers " +
		"or a hosted embedding provider; the native backend has no embedding pipeline"
	reasonBetweenness = "upstream ranks by betweenness centrality, which networkx " +
		"estimates from a random sample above 5000 nodes with no seed, so the ranking " +
		"is not reproducible even by the release against itself"
	reasonWiki = "upstream generates and reads a markdown wiki under " +
		".code-review-graph/wiki/, which the native backend does not materialise"
	reasonRefactor = "upstream's rename preview/apply owns a server-side, ten-minute " +
		"refactor-preview store and rewrites source files; the native backend has no " +
		"equivalent mutation surface"
	reasonRegistry = "upstream reads the cross-repository registry at " +
		"~/.code-review-graph/registry.json, which is CRG-owned state the native " +
		"backend must not write to or interpret"
	reasonDiffRanges = "upstream attributes changes to functions from git's own " +
		"--unified=0 hunk boundaries, which come from xdiff's Myers pass plus " +
		"xdl_change_compact and the default indent heuristic; go-git's differ " +
		"produces different boundaries, and a boundary that slides across a " +
		"function changes the changed-function set, its risk scores and its totals"
	reasonPackagedDocs = "upstream answers from docs/LLM-OPTIMIZED-REFERENCE.md shipped " +
		"inside the code-review-graph wheel; that reference prose is not part of this " +
		"repository, so a native handler would report not_found for sections the " +
		"release resolves"
)

// toolCapabilities is the per-tool routing decision for the pinned release.
//
// Every published tool appears exactly once — `Capabilities` verifies that
// against the embedded surface, so adding a tool to the release without
// deciding where it is served is a hard failure rather than a default.
var toolCapabilities = map[string]ToolCapability{
	"build_or_update_graph_tool":      {NativeBackend: true},
	"run_postprocess_tool":            {NativeBackend: true},
	"get_minimal_context_tool":        {BridgeOnlyReason: reasonDiffRanges},
	"get_impact_radius_tool":          {NativeBackend: true},
	"query_graph_tool":                {NativeBackend: true},
	"get_review_context_tool":         {NativeBackend: true},
	"semantic_search_nodes_tool":      {NativeBackend: true},
	"list_graph_stats_tool":           {NativeBackend: true},
	"get_docs_section_tool":           {BridgeOnlyReason: reasonPackagedDocs},
	"find_large_functions_tool":       {NativeBackend: true},
	"list_flows_tool":                 {NativeBackend: true},
	"get_flow_tool":                   {NativeBackend: true},
	"get_affected_flows_tool":         {NativeBackend: true},
	"list_communities_tool":           {NativeBackend: true},
	"get_community_tool":              {NativeBackend: true},
	"get_architecture_overview_tool":  {NativeBackend: true},
	"detect_changes_tool":             {BridgeOnlyReason: reasonDiffRanges},
	"get_hub_nodes_tool":              {NativeBackend: true},
	"get_knowledge_gaps_tool":         {NativeBackend: true},
	"get_surprising_connections_tool": {NativeBackend: true},
	"get_suggested_questions_tool":    {BridgeOnlyReason: reasonBetweenness},
	"traverse_graph_tool":             {NativeBackend: true},

	"embed_graph_tool":       {BridgeOnlyReason: reasonEmbeddings},
	"get_bridge_nodes_tool":  {BridgeOnlyReason: reasonBetweenness},
	"generate_wiki_tool":     {BridgeOnlyReason: reasonWiki},
	"get_wiki_page_tool":     {BridgeOnlyReason: reasonWiki},
	"refactor_tool":          {BridgeOnlyReason: reasonRefactor},
	"apply_refactor_tool":    {BridgeOnlyReason: reasonRefactor},
	"list_repos_tool":        {BridgeOnlyReason: reasonRegistry},
	"cross_repo_search_tool": {BridgeOnlyReason: reasonRegistry},
}

// Capability returns where the named tool is served from.
func Capability(tool string) (ToolCapability, bool) {
	capability, ok := toolCapabilities[tool]
	if !ok {
		return ToolCapability{}, false
	}
	capability.Tool = tool
	return capability, true
}

// Capabilities returns the routing decision for every published tool, in the
// release's published tool order, and fails if the two ever disagree.
func Capabilities() ([]ToolCapability, error) {
	tools := Surface()
	out := make([]ToolCapability, 0, len(tools))
	for _, tool := range tools {
		capability, ok := Capability(tool.Name)
		if !ok {
			return nil, fmt.Errorf(
				"crgrelease: tool %q has no routing decision; every published tool must "+
					"be assigned a backend", tool.Name)
		}
		if capability.NativeBackend == (capability.BridgeOnlyReason != "") {
			return nil, fmt.Errorf(
				"crgrelease: tool %q must be either native or bridge-only with a reason",
				tool.Name)
		}
		out = append(out, capability)
	}
	if len(toolCapabilities) != len(tools) {
		return nil, fmt.Errorf(
			"crgrelease: %d routing decisions for %d published tools",
			len(toolCapabilities), len(tools))
	}
	return out, nil
}

// BridgeUnavailableMessage is what a tools/call answers with when the call
// must reach the bridge and the bridge is not installed.
//
// It is deliberately an explicit failure naming the missing capability rather
// than an empty successful result: an empty result would be indistinguishable
// from "the graph really contains nothing", which is the silent regression the
// Phase-A cutover must not introduce.
func BridgeUnavailableMessage(tool, reason, detail string) string {
	message := fmt.Sprintf(
		"%s is not available from the kg-native code-graph backend: %s. "+
			"It is served by the retained code-review-graph %s bridge, which is not "+
			"available here", tool, reason, Version)
	if detail != "" {
		message += ": " + detail
	}
	return message + ". Install code-review-graph==" + Version +
		" (or select the crg-bridge backend) to use this tool."
}

// SourcesUnsupportedReason is the routing reason used when a tool IS
// implemented natively but the repository contains sources the native scanner
// does not extract. Serving it natively would answer from a graph that is
// missing those files, which is a wrong answer rather than a partial one.
func SourcesUnsupportedReason(languages []string) string {
	return fmt.Sprintf(
		"the repository contains sources the kg-native scanner does not extract (%v), "+
			"so a native answer would be computed from an incomplete graph", languages)
}
