# Graph Bridge Command Readiness — Product Contract

**Status:** active  
**Written:** 2026-04-19  
**Plan:** graph-bridge-command-readiness / define-graph-bridge-contract

---

## 1. Problem statement

The current `workflow graph query` and `kg bridge query` surfaces return empty results or hard
errors in all practical repo states:

- Non-code intents (`plan_context`, `decision_lookup`, etc.) error with "graph bridge not
  configured" when `.agents/workflow/graph-bridge.yaml` is absent.
- Code intents (`symbol_lookup`, `callers_of`, etc.) return empty results because the CRG code
  graph (34 K nodes) is never imported into the warm SQLite layer that bridge queries use.
- `workflow graph health` reports "healthy" with `note_count: 0`, giving planners no signal
  that the bridge is non-functional.

This contract defines the behavior fixes, configuration model, and planner-facing readiness
criteria that gate the implementation and smoke coverage tasks.

---

## 2. Auto-scaffold decision

### Decision: auto-scaffold graph-bridge.yaml on first use

When `workflow graph query` is called and `.agents/workflow/graph-bridge.yaml` is absent, the
command must:

1. Create a minimal `.agents/workflow/graph-bridge.yaml` with all non-code intents enabled and
   `graph_home` resolved from the agentsrc `kg.graph_home` field, falling back to
   `~/.knowledge-graph`.
2. Print a one-line notice: `graph-bridge.yaml created with defaults — results may be sparse
   until the KG is populated`.
3. Continue with the query (do not error out).

For code intents, no config file is required — `workflow graph query --intent symbol_lookup`
routes directly to `kg bridge query` regardless of `graph-bridge.yaml`.

### Degradation wording when results are empty

When any bridge query returns 0 results and the warm store or note directory is empty, the
command must append a structured warning to the response:

```
[bridge-sparse] warm store has N nodes / M notes — results may be incomplete.
Run 'dot-agents kg build' to index code nodes or 'dot-agents kg warm' to sync notes.
```

This replaces the current silent empty-result output.

---

## 3. agentsrc `kg` section

Add a top-level `kg` key to `agentsrc.json` for project-level KG configuration.

```json
{
  "kg": {
    "graph_home": "~/.knowledge-graph",
    "backend": "sqlite",
    "bridge": {
      "enabled": true,
      "allowed_intents": []
    }
  }
}
```

### Fields

| Field | Type | Default | Description |
|---|---|---|---|
| `kg.graph_home` | string | `~/.knowledge-graph` | Path to KG_HOME. Overrides `KG_HOME` env var when set in agentsrc. |
| `kg.backend` | `"sqlite"` \| `"postgres"` | `"sqlite"` | Storage backend. `postgres` requires `KG_POSTGRES_URL`. |
| `kg.bridge.enabled` | bool | `true` | Whether bridge queries are active for this project. |
| `kg.bridge.allowed_intents` | string[] | `[]` (all) | If non-empty, restrict bridge to this intent set. |

**Precedence:** `KG_HOME` env var > `agentsrc kg.graph_home` > `~/.knowledge-graph`.

**Access control / KG segregation:** deferred. Personal vs. project/team/company KG separation,
per-intent access controls, and multi-tenant KG routing are not in scope for this stabilization
cycle. The `kg.graph_home` field is the initial hook point for per-project isolation via
separate KG_HOME paths.

### agentsrc schema update

Add `kg` as an optional property in `agentsrc.schema.json` with the fields above. The schema
must not require `kg` — repos without it get all defaults.

---

## 4. Storage backend modularity

The `internal/graphstore` package already exposes a `Store` interface implemented by
`SQLiteStore` and `PostgresStore`. The ETL pipeline (section 5) must write through the `Store`
interface, not directly to SQLite, so Postgres is a drop-in replacement.

Backend selection order:
1. `agentsrc kg.backend` (explicit)
2. `KG_POSTGRES_URL` env var present → postgres
3. Fallback → sqlite at `kg.graph_home/ops/graphstore.db`

No other backend changes are required in this plan.

---

## 5. Code-lane readiness (CRG → warm store ETL)

### The gap

Code bridge intents query the warm SQLite `nodes` table which has 0 rows. CRG (the
code-review-graph Python CLI) maintains a separate store with 34 K+ nodes. There is no ETL
between them.

### Required ETL: `kg warm --include-code`

Extend `kg warm` (or add `kg warm code`) to import CRG node data into the warm store's `nodes`
table using the existing `Store.UpsertNode` / `Store.UpsertEdge` methods:

1. Call `CRGBridge.Status()` to confirm CRG is available.
2. Pull nodes and edges from CRG via a new `CRGBridge.ExportNodes(limit int)` method (shells
   out to the CRG Python CLI with JSON output).
3. Upsert into the warm store via `UpsertNode` / `UpsertEdge`.
4. Report: `N nodes, M edges imported`.

**Code-lane intents are considered operationally trustworthy when:**
- CRG is available (`kg code-status` reports > 0 nodes)
- The warm store `nodes` table has > 0 rows (ETL has run at least once)
- `kg warm --include-code` has been run since the last `kg build`/`kg update`

**No KG notes are required** for code-lane intents to be live. Code-lane and context-lane
readiness are independent.

### Incremental sync

After `kg update` (incremental CRG build), `kg warm --include-code` should only upsert nodes
from changed files (honor `fileHash` change detection in `UpsertNode`). A full re-import is
only needed after `kg build` or when `--force` is passed.

---

## 6. Context-lane readiness

Context-lane intents (`plan_context`, `decision_lookup`, `entity_context`, `workflow_memory`,
`contradictions`) are trustworthy when:
- KG notes exist in `kg.graph_home/notes/` for the relevant note types
- `kg warm` has been run to sync them into the warm `kg_notes` table

Until notes exist, context-lane queries return empty results with the sparse warning from
section 2. This is acceptable for the current stabilization — context-lane readiness depends
on the future ingestion pipeline.

### Future ingestion pipeline (deferred, not in this plan)

Planned but not in scope for this contract:

- **Auto-ingestion daemon**: watches `.agents/workflow/plans/`, `.agents/workflow/specs/`,
  `docs/`, and configured doc services (Confluence) for new or updated content. Queues raw
  sources for processing.
- **Curation layer**: raw ingestion → normalization processing → agent-curated notes →
  human-reviewed first-class notes.
- **Layer model**: cold (raw), warm (agent-curated), hot (human-approved). Notes surface
  differently to planners based on layer.
- **Ranking system**: background process manages note scoring across layers.
- **Minimum for new feature work**: plan artifacts (PLAN.yaml, TASKS.yaml) and spec documents
  must be indexable as context-lane sources before code exists. The ingestion daemon should
  treat these as first-class inputs even when note_count is otherwise 0.

---

## 7. write_scope evidence contract

### Principle

Bridge query results are *evidence* for write scope, not authoritative declarations. When
evidence is sparse, the planner must note the sparsity and either widen the search or flag
the gap.

### Sparsity score

Every bridge result set carries a `sparsity_score` (0–100):

```
sparsity_score = 100 × (missing_evidence_lanes / total_expected_lanes)
```

Where `evidence_lanes` for a given task are:
- code-lane: `symbol_lookup` + `callers_of` + `impact_radius` results
- context-lane: `plan_context` + `decision_lookup` results
- plan-lane: plan/tasks/spec artifacts (always available if the plan exists)

**Example:** code-lane returns 3 hits, context-lane 0 hits, plan-lane present → 1 missing of 3
lanes → `sparsity_score ≈ 33`.

### Thresholds

| Mode | Condition | Threshold | Action |
|---|---|---|---|
| Existing feature | Code files exist for the target area | sparsity_score ≤ 25 | proceed |
| Existing feature | Code files exist | sparsity_score 26–74 | proceed with `[sparse-evidence]` note on write_scope |
| Existing feature | Code files exist | sparsity_score ≥ 75 | escalate: request code-lane rebuild before delegation |
| New feature | No code files exist yet | code-lane expected to be 0 | treat as sparsity_score 0 if plan-lane present |
| New feature | No code files, no plan artifacts | n/a | escalate: minimum plan/tasks/spec required |

### Fallback search order for sparse results

When code-lane returns < 3 results for a `symbol_lookup`:

1. Try `callers_of` and `callees_of` for related symbols.
2. Try `impact_radius` on the candidate files named in plan write_scope hints.
3. Try `decision_lookup` for related architectural notes.
4. Report `sparsity_score` in the result set.

### New feature special case

For tasks where no code yet exists (new feature work), skip code-lane queries. The planner
must confirm plan-lane evidence (PLAN.yaml + TASKS.yaml + spec if present) before authoring a
write_scope. The sparsity score for a new-feature task with plan-lane present is 0.

---

## 8. `workflow graph health` fix

The health command must report warm-store readiness, not just adapter availability:

```json
{
  "adapter_available": true,
  "graph_home_exists": true,
  "warm_store_node_count": 34281,
  "warm_store_note_count": 0,
  "code_lane_ready": true,
  "context_lane_ready": false,
  "status": "partial",
  "note": "code-lane ready; context-lane needs KG notes (run 'kg warm' after authoring notes)"
}
```

`status` is `"healthy"` only when both lanes are ready. `"partial"` when one lane is ready.
`"degraded"` when neither lane has data.

---

## 9. Done criteria for this plan

The implementation and smoke tasks (tasks 3 and 4) are done when:

| Criterion | Verifiable by |
|---|---|
| `workflow graph query --intent plan_context "..."` does not error on a repo without `graph-bridge.yaml`; auto-scaffolds and returns sparse-warning | smoke test |
| `workflow graph query --intent symbol_lookup "runWorkflowComplete"` returns ≥ 1 result after `kg warm --include-code` | smoke test |
| `kg bridge query --intent callers_of "runWorkflowComplete"` returns ≥ 1 result after ETL | smoke test |
| `workflow graph health` reports `warm_store_node_count > 0` and correct `code_lane_ready` | smoke test |
| `agentsrc.schema.json` accepts `kg` section with `graph_home`, `backend`, `bridge` | schema lint |
| Sparsity score present in JSON output of `kg bridge query` | schema / output check |
| `go test ./...` passes | CI |

---

## 10. Deferred items (not in this plan)

- Access control and KG segregation (personal / project / team / company KG separation)
- Auto-ingestion daemon for plans, specs, docs, Confluence
- Curation layer (raw → agent curated → human approved)
- Cold/warm/hot layer model and note ranking system
- Postgres production deployment and migration tooling
