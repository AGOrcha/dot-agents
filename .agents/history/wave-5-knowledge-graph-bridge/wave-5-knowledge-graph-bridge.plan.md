# Wave 5: Knowledge-Graph Bridge And Integration Readiness

Spec: `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — Wave 5
Status: Completed (2026-04-10)
Depends on: Wave 4 (shared preferences), KG Phase 3+ (deterministic query surface)

## Goal

Make `dot-agents` a stable bridge to external knowledge systems. Normalize query intents and response shapes so agents can query graph-backed context through one deterministic contract without repo-specific prompt conventions.

## Artifacts Introduced

| Path | Purpose |
|------|---------|
| `.agents/workflow/graph-bridge.yaml` | Repo-local bridge policy, allowed query intents, context-mapping rules |
| `~/.agents/context/<project>/graph-bridge-health.json` | Local adapter availability and last-query health |

## Bridge Query Intents

The initial bridge supports these deterministic query intents:

- `plan_context` — supporting decisions, specs, and lessons for a plan or task
- `decision_lookup` — prior decisions and rationale by topic or identifier
- `entity_context` — graph context for a subsystem, service, or project area
- `workflow_memory` — prior handoffs, learnings, or linked artifacts relevant to current work
- `contradictions` — conflicting or stale knowledge relevant to active workflow state

## Implementation Steps

### Step 1: Bridge configuration types

- [ ] `GraphBridgeConfig` struct:
  - schema_version, enabled (bool)
  - graph_home (string — path to KG_HOME, default `~/knowledge-graph/`)
  - allowed_intents ([]string — subset of supported intents enabled for this repo)
  - context_mappings ([]ContextMapping — maps repo concepts to graph query scopes)
- [ ] `ContextMapping` struct: repo_scope (string), graph_scope (string), intent (string)
- [ ] `loadGraphBridgeConfig(projectPath string) (*GraphBridgeConfig, error)` — read `.agents/workflow/graph-bridge.yaml`, graceful if absent (bridge disabled)
- [ ] `isValidBridgeIntent(intent string) bool`
- [ ] Tests: load config, validate intents, absent config means disabled

### Step 2: Normalized query contract

- [ ] `GraphBridgeQuery` struct: intent, project, scope, query (string)
- [ ] `GraphBridgeResponse` struct:
  - schema_version, intent, query (string)
  - results []GraphBridgeResult — each with id, type, title, summary, path, source_refs
  - warnings []string
  - provider (string)
  - timestamp (string)
- [ ] `GraphBridgeResult` struct: id, type, title, summary, path, source_refs[]
- [ ] These match the normalized query contract from the KG spec

### Step 3: Local graph adapter

- [ ] `GraphBridgeAdapter` interface:
  - `Query(query GraphBridgeQuery) (GraphBridgeResponse, error)`
  - `Health() (GraphBridgeHealth, error)`
- [ ] `LocalGraphAdapter` — reads graph notes from KG_HOME filesystem:
  1. Resolve `graph_home` from bridge config
  2. For each intent, scan relevant `notes/` subdirectories (decisions/, entities/, concepts/, etc.)
  3. Match query against note frontmatter (title, summary, type) using simple string matching
  4. Return normalized results
- [ ] This is intentionally simple — semantic/vector search comes from KG adapters, not the bridge
- [ ] Tests: adapter returns results from fixture graph, handles missing graph_home

### Step 4: Bridge health tracking

- [ ] `GraphBridgeHealth` struct:
  - schema_version, timestamp
  - adapter_available (bool)
  - graph_home_exists (bool)
  - note_count, last_query_time, last_query_status
  - status: "healthy"/"warn"/"error"
  - warnings[]
- [ ] `writeGraphBridgeHealth(project string, health GraphBridgeHealth) error`
- [ ] `readGraphBridgeHealth(project string) (*GraphBridgeHealth, error)`
- [ ] Health is updated on each query
- [ ] Tests: health write/read, derive status from conditions

### Step 5: `workflow graph query` subcommand

- [ ] `graphCmd` (Use: "graph") parent command
- [ ] `graphQueryCmd` (Use: "query") with `runWorkflowGraphQuery()`:
  - Required flag: `--intent` (from supported set)
  - Required arg or flag: query string
  - Optional flag: `--scope`
  1. Load bridge config; error if bridge not configured
  2. Validate intent is allowed
  3. Execute query via adapter
  4. Update bridge health
  5. Display results (table or JSON via `Flags.JSON`)
- [ ] Tests: query with valid intent, disabled bridge, invalid intent

### Step 6: `workflow graph health` subcommand

- [ ] `graphHealthCmd` (Use: "health") with `runWorkflowGraphHealth()`:
  1. Load bridge config
  2. Check adapter availability (graph_home exists, notes dir exists)
  3. Read last health snapshot
  4. Display: adapter status, graph stats, last query info, warnings
  5. `--json` flag for machine output
- [ ] Tests: health with available/unavailable graph

### Step 7: Integration with orient

- [ ] Add bridge health summary to `workflowOrientState` (adapter_available, note_count)
- [ ] Update `renderWorkflowOrientMarkdown()` — add "# Knowledge Graph" section if bridge is configured
- [ ] Update health snapshot to include graph bridge status
- [ ] Tests: orient output reflects bridge state

## Files Modified

- `commands/workflow.go`
- `commands/workflow_test.go`

## Acceptance Criteria

An agent can obtain graph-backed workflow context through one deterministic query contract (`workflow graph query --intent ...`) without needing repo-specific prompt conventions.

## Verification

```bash
go test ./commands -run 'GraphBridge|BridgeHealth|BridgeQuery'
go test ./commands
go test ./...
```
