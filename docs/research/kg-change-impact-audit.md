# KG Changes and Impact Audit

**Audited:** 2026-04-20  
**Plan:** kg-command-surface-readiness  
**Task:** kg-change-impact-audit  
**Graph state at audit time:** READY (34635 nodes, 102244 edges, last updated 2026-04-20T00:44:49Z)

---

## Commands Audited

- `kg changes [--base <sha>] [--brief] [--repo <path>]`
- `kg impact [file...] [--base <sha>] [--depth N] [--limit N] [--repo <path>]`

---

## What Works

### `kg changes`

Delegates to `CRGBridge.DetectChanges()` which shells out to `code-review-graph detect-changes`.

**Observed on fresh graph with real code diff (HEAD~1):**
- Human output shows: Changed symbols (58), Test gaps (46), Review priorities (10), Overall risk score 0.50
- `--json` emits valid `CRGChangeReport` struct with `summary`, `risk_score`, `changed_functions[]`, `affected_flows[]`, `test_gaps[]`, `review_priorities[]`
- No prose before JSON — clean machine-readable output
- `--base <sha>` is threaded through to CRG correctly

**Observed with docs-only diff (--base 7c733c8 which adds only YAML/plan files):**
- Reports 47 changed files, but still shows 58 changed functions — because `--base 7c733c8` compares that SHA all the way to current HEAD, not just that commit in isolation. This is correct behavior, not a bug.

### `kg impact`

Delegates to `CRGBridge.GetImpactRadius()`.

**Observed on fresh graph with real code diff:**
- Human output: 328 nodes directly changed, 50 nodes impacted (within 2 hops), 6 additional files affected, 378 total impacted
- `--json` emits valid `CRGImpactResult` with `status`, `summary`, `changed_files[]`, `changed_nodes[]`, `impacted_nodes[]`, `impacted_files[]`, `truncated`, `total_impacted`
- `--depth` and `--limit` flags work

---

## Empty-Result Disambiguation: The Core Finding

**Neither `kg changes` nor `kg impact` have any graph-freshness pre-check.**

`runKGChanges` and `runKGImpact` (`commands/kg/sync_code_warm_link.go`) call `bridge.DetectChanges()` / `bridge.GetImpactRadius()` directly without calling `Status()` or checking `code-status` first.

The `Status()` / code-status check exists only in `runKGCodeStatus`.

### Scenario 1: empty-because-no-impact

When `kg impact` is run on a file not in the code graph (e.g., a YAML plan file):

```
$ kg impact .agents/workflow/plans/.../PLAN.yaml
Impact Radius
  Blast radius for 1 changed file(s):
  - 0 nodes directly changed
  - 0 nodes impacted (within 2 hops)
  - 0 additional files affected
```

JSON:
```json
{
  "status": "ok",
  "summary": "Blast radius for 1 changed file(s):\n  - 0 nodes directly changed\n  ...",
  "changed_nodes": [],
  "impacted_nodes": [],
  ...
  "total_impacted": 0
}
```

**Problem:** `"status": "ok"` is returned even though the file has zero graph presence. A consumer cannot distinguish "this file genuinely has no dependents" from "this file was never indexed into the code graph." Both cases produce identical output.

### Scenario 2: empty-because-graph-stale

Not directly reproducible with a READY graph (by design — audit policy was clean graph). However, the mechanism is fully clear from code inspection:

- `runKGChanges` / `runKGImpact` have no pre-flight `Status()` call
- If `code-status` reports `unbuilt` (no CRG DB), CRG's `detect-changes` and `impact` commands would either error or return empty results depending on CRG behavior
- The Go CLI layer adds no warning like "graph not built — results may be incomplete"
- From `kg-freshness-audit.md`: a fresh checkout shows `kg code-status` reporting `0/0/0, Last updated: never` — in that state `kg changes` would report 0 changed functions with no staleness signal

### Comparison

| Condition | `kg changes` output | `kg impact` output | Caller can distinguish? |
|-----------|--------------------|--------------------|------------------------|
| Real code diff, fresh graph | Changed symbols listed | Changed/impacted nodes listed | N/A |
| Docs-only diff, fresh graph | `"summary": "0 changed function(s)"` | `0 nodes` | No — same as stale |
| Any diff, graph unbuilt | `"summary": "0 changed function(s)"` or CRG error | `0 nodes` or error | No |
| Any diff, graph busy/locked | CRG returns error (classifyCRGRunError wraps it) | Same | Yes, because error is surfaced |

The busy/locked case is already handled by `classifyCRGRunError` (from kg-freshness-impl). The `unbuilt` and `no-impact` cases remain indistinguishable.

---

## JSON Output Contracts (as-built)

### `kg changes --json` → `CRGChangeReport`

```json
{
  "summary": "Analyzed N changed file(s): ...",
  "risk_score": 0.50,
  "changed_functions": [{"name": "", "qualified_name": "", "file_path": "", "risk_score": 0.3, "callers": 0}],
  "affected_flows": [],
  "test_gaps": [{"qualified_name": "", "file_path": ""}],
  "review_priorities": [{"qualified_name": "", "reason": "", "risk_score": 0.5}]
}
```

**No graph-state field.** A consumer cannot determine if the empty `changed_functions` list means no impact or a stale/absent graph.

### `kg impact --json` → `CRGImpactResult`

```json
{
  "status": "ok",
  "summary": "Blast radius for N changed file(s): ...",
  "changed_files": [],
  "changed_nodes": [],
  "impacted_nodes": [],
  "impacted_files": [],
  "truncated": false,
  "total_impacted": 0
}
```

`status` field is always `"ok"` when CRG runs successfully. It does not reflect code graph readiness.

---

## Human-Readable Output Gaps

`runKGChanges` (`sync_code_warm_link.go:369`): When `ChangedFunctions` is empty, only `ui.Header("Change Impact")` and `ui.Info(report.Summary)` print — no hint that graph staleness may explain the empty result.

`runKGImpact` (`sync_code_warm_link.go:218`): When `ChangedNodes` and `ImpactedNodes` are both empty, only the header and summary print. No advisory message.

---

## Decisions for `kg-change-impact-impl`

Based on this audit, the implementation task (`kg-change-impact-impl`) should address:

### 1. Pre-flight graph readiness check (required)

Both `runKGChanges` and `runKGImpact` must call `Status()` before delegating to CRG:

- If `state == unbuilt`: emit a `ui.WarnBox("Code graph not built", "Run 'kg build' first — results will be empty or incomplete.")` and return a non-zero exit
- If `state == busy_or_locked`: emit a `ui.WarnBox("Code graph is busy or locked", ...)` (already handled in build/update; add the same for changes/impact)
- If `state == ready`: proceed normally
- If `state == error`: emit warning but still delegate (CRG may provide partial results)

### 2. `--require-graph` flag (noted in TASKS.yaml)

Add `--require-graph` bool flag to `kg changes` and `kg impact`:
- When set: if `state != ready`, return exit code 1 with an actionable error message
- When not set (default): warn but still run (backwards-compatible)

### 3. JSON output: add `graph_state` field

Augment the JSON output with a `graph_state` field at the CLI wrapper level (not in `CRGChangeReport` / `CRGImpactResult` which are CRG-owned schemas):

```json
{
  "graph_state": "ready",
  "summary": "...",
  "changed_functions": [...]
}
```

This lets callers (planner, orchestrator) distinguish empty-because-no-impact from empty-because-graph-stale without running a separate `kg code-status` first.

### 4. Human output: advisory for empty results

When `ChangedFunctions` / `ChangedNodes` are both empty, append:
```
Note: run 'kg code-status' to verify the code graph is current.
```

---

## Non-Issues / Out of Scope

- `kg changes --brief` mode returns plain text; this is intentional for human consumption and not part of the machine-readable contract
- Test-gap reporting is CRG-native behavior; no change needed to the Go wrapper
- The `--base` flag is already correctly threaded through to CRG
- MCP parity for `changes` / `impact` is a separate `kg-mcp-transport-audit` task; do not fold here

---

## Summary

| Item | Status |
|------|--------|
| `kg changes` basic behavior on fresh graph | Works correctly |
| `kg impact` basic behavior on fresh graph | Works correctly |
| `--json` output for both commands | Works, no prose prefix |
| Empty-result disambiguation: no-impact vs stale | **Missing — core gap** |
| Graph freshness pre-check in changes/impact | **Missing** |
| `--require-graph` flag | **Not implemented** |
| `"graph_state"` in JSON output | **Not present** |
| Human advisory when results are empty | **Missing** |
