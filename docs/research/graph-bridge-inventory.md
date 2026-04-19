# Graph Bridge Inventory Findings

## Date
2026-04-19

## Scope-lane queries (symbol_lookup, callers_of, impact_radius, tests_for, callees_of)

### Failure mode
`kg bridge query --intent symbol_lookup "runWorkflowComplete"` → "No results found."

### Root cause
`collectCodeBridgeResults()` opens warm SQLite at `~/.knowledge-graph/ops/graphstore.db`.
The `nodes` table has **0 rows**. CRG (code-review-graph Python CLI) has 34,281 nodes last
updated 2026-04-19T11:16:13, but there is no ETL pipeline importing CRG node data into the
warm SQLite. `kg warm` only syncs KG notes (markdown), not code graph nodes.

### Reproduction
```
dot-agents kg bridge query "runWorkflowComplete" --intent symbol_lookup  # no results
sqlite3 ~/.knowledge-graph/ops/graphstore.db "SELECT COUNT(*) FROM nodes;"  # 0
dot-agents kg code-status  # 34281 nodes (CRG)
```

### Affected intents
symbol_lookup, impact_radius, tests_for, callers_of, callees_of, symbol_decisions, decision_symbols

## Context-lane queries (plan_context, decision_lookup, workflow_memory, contradictions)

### workflow graph query (non-code intents)

**Failure mode:** `workflow graph query --intent plan_context "loop orchestrator"` →
"graph bridge not configured"

**Root cause:** `loadGraphBridgeConfig` looks for
`.agents/workflow/graph-bridge.yaml` in the repo root. This file does not exist. When absent,
the config returns `Enabled: false` and the command errors immediately.

**Missing degradation:** The command should either scaffold the config file or fall back to
read-only behavior rather than hard-failing with an opaque error.

### kg bridge query (context intents via LocalFileAdapter)

**Failure mode:** All KG context queries return "No results found."

**Root cause:** `~/.knowledge-graph/notes/` subdirectories (decisions, synthesis, entities, etc.)
are empty. No KG notes have been authored in this workspace.

## workflow graph health

Returns `status: healthy, note_count: 0`. Planners cannot tell from this output that
scope-lane queries will return empty results. The health check does not verify that code nodes
are present in the warm store.

## Summary table

| Query path | Intent class | Failure type | Trustworthy for planning? |
|---|---|---|---|
| workflow graph query | non-code (plan_context etc.) | missing-config hard fail | No |
| kg bridge query | code (symbol_lookup etc.) | empty results (warm store empty) | No |
| kg bridge query | context (decision_lookup etc.) | empty results (no notes) | No |
| workflow graph health | n/a | misleading "healthy" | No |

## CRG status (what works)

- 34,281 nodes, 99,893 edges, 886 files, last updated 2026-04-19T11:16:13
- `kg build`, `kg update`, `kg code-status`, `kg changes`, `kg impact` work via CRG Python CLI
- These do NOT flow through the bridge query path used by planners

## Priority fixes for planner-facing readiness

1. **Missing `.agents/workflow/graph-bridge.yaml`**: scaffold a default or degrade gracefully
2. **Code node import gap**: add ETL from CRG → warm SQLite so `kg bridge query` code intents work
3. **Health misleading**: surface warm-store node count in `workflow graph health`
4. **Empty-result disambiguation**: distinguish "query ran, no matches" vs "store is empty" in responses
