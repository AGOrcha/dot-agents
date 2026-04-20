## 6. kg-mcp-transport-impl (2026-04-20)

**Status:** completed — both lenses accept.

**What changed (3 files in `internal/graphstore/`):**

- `crg.go`: added `Files []string` to `DetectChangesOptions`. Wired through with a comment noting CRG v1.x CLI does not accept `--files`; field is thread-safe for when upstream support lands.
- `mcp_server.go`:
  - `handleGetReviewContext` — added `bridge.Status()` pre-check returning `{"error","state","hint"}` payload on `unbuilt`/`busy_or_locked`; passes `DetectChangesOptions{Files: req.Files}` to `DetectChanges`.
  - `handleGetImpactRadius` — same `Status()` freshness guard inserted before `bridge.GetImpactRadius()`.
- `mcp_server_test.go` (new file): 4 tests using `fakeMCPBridge`: unbuilt→structured error for both handlers, busy_or_locked→structured error, ready→proceeds to DetectChanges.

**Key decisions recorded:**
- Error response is a structured JSON result payload (not a Go/RPC error) so MCP callers can introspect state.
- `get_docs_section_tool` hardcoded paths, `query_graph_tool` scope routing, MCP server transport redesign — all deferred per audit.
- Removed unused `readyOrDefault()` method added by impl worker (dead code, no callers).

**Commit:** `7fe6b152` (impl worker) + dead-code removal patch.

---

## 1. kg-freshness-audit

Recorded the audit in `docs/research/kg-freshness-audit.md`.

Key findings:

- `kg health` can report `healthy` while the code graph is still absent, so it is not a valid readiness gate for code-graph-backed planner work.
- `kg code-status` reflects repo-local CRG state, not KG home readiness; on a clean checkout it returns `0/0/0` with `Last updated: never` until a build runs.
- `kg code-status --json` still rendered prose during the audit, so the machine-readable freshness surface is incomplete.
- `kg build` failed in two operationally distinct ways during reproduction:
  - sandbox-only Python semaphore permission failure
  - real `database is locked` failure when a concurrent `kg update` held the repo graph DB open
- on a clean checkout, `kg build` succeeded, but its output counts did not match the immediately-following `kg code-status` counts, so `code-status` should be treated as the persisted source of truth until that mismatch is resolved.
- `kg update` succeeded on the clean checkout but reported `1 files updated, 0 nodes, 0 edges` on a clean tree, which is not yet trustworthy enough as a planner-facing freshness summary.
