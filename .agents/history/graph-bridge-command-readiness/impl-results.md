## 1. inventory-current-bridge-behavior

Researched bridge gap: `kg bridge query` code-lane intents route to warm SQLite which had 0 nodes because no ETL from CRG ever ran. Health command incorrectly reported "healthy". Missing `graph-bridge.yaml` caused hard fail. All findings documented in `docs/research/graph-bridge-inventory.md`.

---

## 2. define-graph-bridge-contract

Designed full product contract at `.agents/workflow/specs/graph-bridge-contract/design.md`. Key decisions:
- Auto-scaffold graph-bridge.yaml on first use (no hard fail)
- `agentsrc.json` `kg` section for project-level KG config (graph_home, backend, bridge)
- Storage backend interface: sqlite|postgres for future modularity
- CRG→warm ETL as `kg warm --include-code`
- Sparsity score: 0=evidenced, 75=store populated/no results, 100=empty store
- Thresholds: ≤25 proceed, 26-74 proceed with note, ≥75 escalate for existing code features; new features require plan-lane notes minimum

---

## 3. implement-graph-bridge-readiness-fixes

Files changed:
- `internal/graphstore/crg.go`: `CRGDBPath()`, `ReadNodes()`, `ReadEdges()` (direct SQLite reads from CRG db)
- `internal/graphstore/sqlite.go`: `CountNodes()`, `CountKGNotes()` methods
- `internal/config/agentsrc.go`: `AgentsRCKG`, `AgentsRCKGBridge` types; `KG` field on `AgentsRC`
- `schemas/agentsrc.schema.json`: `kg` property added
- `commands/workflow/graph.go`: health struct extended with lane readiness; auto-scaffold; agentsrc kg integration; accurate degraded/partial/healthy status
- `commands/kg/query_lint_maintain.go`: `SparsityScore *int` on `GraphQueryResponse`
- `commands/kg/bridge.go`: `computeSparsityScore()` + sparse warning in `collectCodeBridgeResults()`
- `commands/kg/sync_code_warm_link.go`: `runKGWarmCodeImport()` + `--include-code` flag handling
- `commands/kg/cmd.go`: `--include-code` flag registered
- `commands/add.go`: `ensureProjectKGMCPConfigs` guard changed from `ExtraFields["kg"]` to `rc.KG != nil` (regression fix)

All tests pass: `go test ./... ✓`

---

## 4. add-graph-bridge-regression-smokes

Tests added:
- `internal/graphstore/sqlite_test.go`: `TestCountNodes_EmptyStore`, `TestCountNodes_AfterUpsert`, `TestCountKGNotes_EmptyStore`, `TestCountKGNotes_AfterUpsert`
- `internal/config/agentsrc_test.go`: `TestAgentsRCKG_Unmarshal`, `TestAgentsRCKG_NilWhenAbsent`, `TestAgentsRCKG_MarshalRoundTrip`
- `commands/kg/kg_test.go`: `TestComputeSparsityScore_EmptyStore`, `TestComputeSparsityScore_StoreHasDataNoResults`, `TestComputeSparsityScore_ResultsFound`, `TestCollectCodeBridgeResults_EmptyStore_SparsityWarning`, `TestRunKGWarm_IncludeCode_NoCRGGraceful`

Final state: all 15 packages pass `go test ./...`.
