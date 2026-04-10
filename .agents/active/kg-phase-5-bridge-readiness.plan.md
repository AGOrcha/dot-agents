# KG Phase 5: Bridge Readiness

Spec: `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — Phase 5
Status: Fifth phase
Depends on: KG Phase 4 (lint/maintenance), Wave 5 (dot-agents graph bridge)

## Goal

Make the knowledge graph's query surface stable and bridgeable for `dot-agents` and other agent systems. Document the mapping from graph query intents to workflow integration intents. Provide adapter health reporting.

## Relationship to Wave 5

Wave 5 (dot-agents side) defines the bridge consumer — what `dot-agents` expects.
This phase (KG side) ensures the graph can serve those expectations.

The mapping:

| Wave 5 Bridge Intent | KG Query Intent |
|----------------------|-----------------|
| `plan_context` | `decision_lookup` + `synthesis_lookup` scoped to plan topic |
| `decision_lookup` | `decision_lookup` |
| `entity_context` | `entity_context` |
| `workflow_memory` | `related_notes` + `source_lookup` scoped to workflow artifacts |
| `contradictions` | `contradictions` |

## Implementation Steps

### Step 1: Bridge query mapping layer

- [ ] `BridgeIntentMapping` struct: bridge_intent (string), kg_intents ([]string), scope_transform (function or config)
- [ ] `defaultBridgeMappings() []BridgeIntentMapping` — the mapping table above
- [ ] `resolveBridgeQuery(bridgeIntent string, query string) ([]GraphQuery, error)`:
  1. Look up mapping for the bridge intent
  2. Fan out to 1+ KG queries with appropriate scoping
  3. Return list of KG queries to execute
- [ ] `mergeBridgeResults(responses []GraphQueryResponse, bridgeIntent string) GraphQueryResponse`:
  1. Merge results from multiple KG queries
  2. Deduplicate by note ID
  3. Return single normalized response with bridge intent
- [ ] Tests: mapping resolution, result merging, deduplication

### Step 2: Adapter interface formalization

- [ ] `KGAdapter` interface:
  ```go
  type KGAdapter interface {
      Name() string
      Query(query GraphQuery) (GraphQueryResponse, error)
      Health() (KGAdapterHealth, error)
      Available() bool
  }
  ```
- [ ] `KGAdapterHealth` struct: adapter_name, available (bool), last_query_time, last_query_status, note_count, warnings
- [ ] `LocalFileAdapter` — the existing index-based search (from Phase 3) wrapped in the adapter interface
- [ ] Tests: adapter interface compliance for LocalFileAdapter

### Step 3: Adapter health reporting

- [ ] `collectAdapterHealth(kgHome string, adapters []KGAdapter) []KGAdapterHealth`
- [ ] Write adapter health to `ops/adapters/adapter-health.json`
- [ ] Include adapter health in `graph-health.json` (add `adapters` field)
- [ ] Tests: health reporting with available/unavailable adapters

### Step 4: Bridge endpoint

- [ ] `executeBridgeQuery(kgHome string, bridgeIntent string, query string) (GraphQueryResponse, error)`:
  1. Resolve bridge query to KG queries
  2. Execute each KG query via adapter
  3. Merge results
  4. Set provider to adapter name
  5. Return normalized response
- [ ] Tests: end-to-end bridge query

### Step 5: `kg bridge` subcommands

- [ ] `kgBridgeCmd` (Use: "bridge") parent command
- [ ] `kgBridgeQueryCmd` (Use: "query") with `runKGBridgeQuery()`:
  - Required flag: `--intent` (bridge intent, not KG intent)
  - Arg: query string
  1. Execute bridge query
  2. Display results
  3. `--json` flag
- [ ] `kgBridgeHealthCmd` (Use: "health") with `runKGBridgeHealth()`:
  1. Collect adapter health
  2. Show: adapters available, note counts, last query status
  3. `--json` flag
- [ ] `kgBridgeMappingCmd` (Use: "mapping") with `runKGBridgeMapping()`:
  - Display the intent mapping table (bridge intent -> KG intents)
  - Useful for debugging and documentation
- [ ] Tests: bridge commands with fixture data

### Step 6: Stability contract documentation

- [ ] Write `KG_HOME/self/schema/bridge-contract.yaml`:
  - Supported bridge intents
  - Response schema version
  - Adapter requirements
  - This is a machine-readable contract that `dot-agents` can validate against
- [ ] Generate contract on `kg setup` and update on `kg bridge health`
- [ ] Tests: contract file is valid YAML with expected fields

### Step 7: Integration test with dot-agents bridge

- [ ] Create integration test that:
  1. Sets up KG_HOME with fixture notes
  2. Configures `.agents/workflow/graph-bridge.yaml` pointing to KG_HOME
  3. Runs `dot-agents workflow graph query --intent plan_context`
  4. Verifies results come from KG through bridge
- [ ] This validates the full stack: dot-agents -> bridge config -> KG adapter -> KG query -> results

## Files Modified

- `commands/kg.go`
- `commands/kg_test.go`

## Acceptance Criteria

- Graph exposes a stable bridgeable query surface
- Adapter health is reported and available
- Bridge intent mapping is documented and machine-readable
- `dot-agents` can query graph context through the bridge without knowing KG internals
- Future adapters (semantic search, vector DB) can plug in without changing the bridge contract

## Verification

```bash
go test ./commands -run 'KGBridge|BridgeQuery|BridgeHealth|Adapter'
go test ./commands
go test ./...
```
