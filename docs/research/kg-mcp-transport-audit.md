# KG MCP Transport Audit

**Audited:** 2026-04-20  
**Plan:** kg-command-surface-readiness  
**Task:** kg-mcp-transport-audit

---

## Background

`kg serve` (`commands/kg/kg.go:runKGServe`) starts a Go-native MCP server (`internal/graphstore/mcp_server.go`) that exposes tools over stdio JSON-RPC 2.0. The `code-review-graph` library also provides an MCP server (configured globally via dot-agents, registered as `mcp__code-review-graph__*` in the session). Both expose the same tool names.

This audit compares the two server implementations per-tool and produces keep/implement/defer decisions for `kg-mcp-transport-impl`.

---

## Tool inventory: `kg serve` (Go-native server)

| Tool | Handler | Backend |
|------|---------|---------|
| `build_or_update_graph_tool` | `handleBuildOrUpdateGraph` | `CRGBridge.Build()` / `CRGBridge.Update()` |
| `embed_graph_tool` | `handleEmbedGraph` | `CRGBridge.Postprocess()` |
| `list_graph_stats_tool` | `handleListGraphStats` | `SQLiteStore.GetStats()` + `CRGBridge.ListCommunities()` |
| `get_impact_radius_tool` | `handleGetImpactRadius` | `SQLiteStore.SearchNodes()` + `CRGBridge.GetImpactRadius()` |
| `semantic_search_nodes_tool` | `handleSemanticSearchNodes` | `SQLiteStore.SearchNodes()` (warm FTS) |
| `query_graph_tool` | `handleQueryGraph` | intent dispatch → delegates to other handlers |
| `get_review_context_tool` | `handleGetReviewContext` | `CRGBridge.DetectChanges()` + `SQLiteStore.GetImpactRadius()` |
| `get_docs_section_tool` | `handleGetDocsSection` | hardcoded doc file list + markdown section extraction |

Total: 8 tools. Exact name match with the `code-review-graph` MCP server.

---

## Per-tool parity analysis

### 1. `build_or_update_graph_tool`

**`kg serve`:** Checks `Status()` first. If `Nodes == 0` → `Build()`, otherwise `Update()`. Returns nodes/edges/files/duration.

**Bridge server:** Also build-or-update, CRG-native.

**Gap:** None material. `kg serve` version has a status pre-check for the build/update decision which is good.

**Decision: KEEP — no changes needed.**

---

### 2. `embed_graph_tool`

**`kg serve`:** Calls `bridge.Postprocess()`. Returns `{"status": "ok"}` or error.

**Bridge server:** Same CRG postprocess.

**Gap:** None.

**Decision: KEEP — no changes needed.**

---

### 3. `list_graph_stats_tool`

**`kg serve`:** Uses `SQLiteStore.GetStats()` for node/edge counts plus a `ListCommunities()` call for community count, then `Status()` for languages if the store doesn't have them.

**Bridge server:** Returns stats directly from CRG.

**Gap:** None material. `kg serve` uses the warm store for primary stats which is faster.

**Decision: KEEP — no changes needed.**

---

### 4. `get_impact_radius_tool`

**Input:** `{"symbol": string, "depth": int}`

**`kg serve`:** Takes a symbol name string, resolves it to file paths via `store.SearchNodes(symbol, 20)`, then calls `bridge.GetImpactRadius(ImpactOptions{ChangedFiles: files, MaxDepth: depth})`.

**Bridge server:** Likely takes files or symbols directly in a similar way.

**Advantage:** Symbol→file resolution via warm FTS is user-friendly (callers don't need to know file paths).

**Gap:** No freshness guard. If the graph is unbuilt, `requireBridge()` will error or `GetImpactRadius` will return empty results with no warning.

**Decision: KEEP schema; minor — add freshness guard consistent with CLI commands.**

---

### 5. `semantic_search_nodes_tool`

**Input:** `{"query": string, "limit": int}`

**`kg serve`:** Uses `s.store.SearchNodes()` (SQLite FTS on warm layer). Returns `[{name, type, file, summary}]`.

**Bridge server:** Likely uses CRG's own FTS index.

**Gap:** None material. Warm store FTS is fast and doesn't require CRG binary.

**Decision: KEEP — no changes needed.**

---

### 6. `query_graph_tool`

**Input:** `{"intent": string, "query": string, "scope": string}`

**`kg serve`:** Dispatches by intent:
- `symbol_lookup`, `semantic_search`, `search` → `handleSemanticSearchNodes`
- `impact_radius` → `handleGetImpactRadius`
- `review_context` → `handleGetReviewContext`
- `docs_section` → `handleGetDocsSection`
- default → warm store note search + `warnings: ["unsupported query intent"]`

**Bridge server:** Has richer CRG-native intent routing.

**Gap:** The `scope` field is accepted but never used in any intent path. This is dead input.

**Decision: KEEP — intent coverage is adequate for the current contract. `scope` field is documented as unused until a future task.**

---

### 7. `get_review_context_tool` — ★ PRIMARY GAP

**Input:** `{"files": [string]}`

**`kg serve` behavior:**
```go
report, err := bridge.DetectChanges(DetectChangesOptions{})  // NO base, NO files — uses HEAD~1 diff
// ...
impact, err := s.store.GetImpactRadius(req.Files, 2, 50)  // files ARE used here
```

**Problem 1: `files` parameter is ignored for change detection.**  
`bridge.DetectChanges()` is called with empty options — it always uses the HEAD~1 diff, regardless of what files are passed. The `files` input only affects the warm-store impact radius call. A caller passing `files: ["commands/kg/sync_code_warm_link.go"]` expecting to see changes to that file will get the HEAD~1 diff result instead.

**Problem 2: No freshness guard.**  
`handleGetReviewContext` calls `bridge.DetectChanges()` without any `Status()` pre-check. Post `kg-freshness-impl` and `kg-change-impact-impl`, the CLI commands have `checkCRGReadiness()` guards. The MCP handler does not.

**Bridge server:** The `code-review-graph` MCP server's `get_review_context_tool` is the canonical reference — it likely accepts files and computes review context for those specific files.

**Decision: IMPLEMENT — two fixes required:**
1. Add `checkCRGReadiness()` call (or equivalent inline guard) before `DetectChanges`; return structured error if unbuilt.
2. Pass `req.Files` as a base to `DetectChanges` when provided, so the changed-symbols section reflects the actual files passed rather than the default HEAD~1 diff.

---

### 8. `get_docs_section_tool`

**Input:** `{"section": string}`

**`kg serve`:** Searches a hardcoded list of doc files for a markdown heading match.

**Bridge server:** Similar capability.

**Gap:** Hardcoded doc paths may become stale. Not a blocking concern for this task.

**Decision: KEEP — no changes needed for this task.**

---

## Parity matrix summary

| Tool | Status | Decision |
|------|--------|----------|
| `build_or_update_graph_tool` | ✅ at parity | KEEP |
| `embed_graph_tool` | ✅ at parity | KEEP |
| `list_graph_stats_tool` | ✅ at parity | KEEP |
| `get_impact_radius_tool` | ✅ at parity (minor: no freshness guard) | KEEP — minor guard add |
| `semantic_search_nodes_tool` | ✅ at parity | KEEP |
| `query_graph_tool` | ✅ functional (scope field unused) | KEEP |
| `get_review_context_tool` | ❌ files ignored in DetectChanges; no freshness guard | **IMPLEMENT** |
| `get_docs_section_tool` | ✅ at parity | KEEP |

---

## Implementation scope for `kg-mcp-transport-impl`

Based on this audit, the implementation task should address exactly:

### Required (blocking)

**Fix `handleGetReviewContext`:**

1. **Files → DetectChanges**: When `req.Files` is non-empty, pass them as the target for change detection instead of the default HEAD~1 diff. The `DetectChangesOptions` struct has a `Base` string field — we can pass the first file from `req.Files` as a CRG `--files` argument, or expose a new `Files []string` field in `DetectChangesOptions`.

   The simplest safe fix: add `Files []string` to `DetectChangesOptions` in `internal/graphstore/crg.go`, pass them as `--diff-filter` or equivalent CRG CLI argument. If CRG doesn't support a `--files` flag for `detect-changes`, fall back to using `req.Files` for impact only (current behavior) and add a comment explaining the limitation.

2. **Freshness guard**: Add a `Status()` pre-check before `bridge.DetectChanges()`. If state is `unbuilt` or `busy_or_locked`, return an MCP error response (JSON-RPC error or structured `{"error": "graph not ready", "state": "unbuilt"}`) rather than silently returning empty changed symbols.

### Optional (minor improvement)

**`get_impact_radius_tool`** — add same freshness guard as fix 2 above, applied before `bridge.GetImpactRadius()`.

### Out of scope

- Do not redesign the MCP server transport or JSON-RPC framing
- Do not change external tool name surface (tool names must stay identical)
- Do not implement `scope` routing in `query_graph_tool` (defer)
- Do not fix hardcoded doc paths in `get_docs_section_tool` (defer)
- Do not rewrite the Go server to delegate entirely to CRG's MCP server (not this task)
