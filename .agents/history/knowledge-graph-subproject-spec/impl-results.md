# Implementation Results — Knowledge Graph Subproject Spec

---

## 1. KG Spec Authoring

Date: 2026-04-10

Added `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — standalone spec for the knowledge-graph layer. Defines product boundary, local-first canonical layout, graph page/index/log/health/query contracts, core operations (ingest, query, lint, maintain), and phased roadmap from file-native core to bridge readiness and shared-memory research.

---

## 2. KG Phase 1 (Graph Core) + KG Phase 2 (Basic Ingest)

Date: 2026-04-10

### Phase 1 — Graph Core
- Types: `KGConfig`, `GraphNote`, `IndexEntry`, `GraphHealth`
- Path helpers, config CRUD, note parse/render (YAML frontmatter + markdown body)
- Index/log, health compute/write/read
- Commands: `kg setup`, `kg health`

### Phase 2 — Basic Ingest
- `RawSource` + source queue/import helpers
- Extraction heuristics: claims, entities, decisions
- Note creation/update pipeline with cross-linking and health update
- Commands: `kg ingest [file] [--all] [--dry-run]`, `kg queue`

**Bug fixed:** `TestUpdateIndex_AddAndReplace` — changed assertion from `strings.Count(content, "dec-001")` to `strings.Count(content, "- [dec-001]")` to avoid double-counting ID in link anchor vs file path.

---

## 3. KG Phase 3 — Deterministic Query Surface

Date: 2026-04-10

### Types
- `GraphQuery`, `GraphQueryResult`, `GraphQueryResponse` — normalized query contract

### Search engine
- `scoreMatch()` — 5-tier relevance: exact title > prefix > substring > summary > body
- `searchNotes()`, `searchByLinks()`

### Intent dispatch
- `executeQuery()` — 9 intents; logs every query to `notes/log.md`
- `executeBatchQuery()`

**Command:** `kg query [query] --intent <intent> [--limit N] [--scope s]`

---

## 4. KG Phase 4 — Lint & Maintenance

Date: 2026-04-10

### Lint engine
- `buildLinkGraph()` — adjacency + metadata maps
- 7 lint checks: broken_links (error), orphan_pages (warn), missing_source_refs (info), stale_pages (warn), index_drift (warn), oversize_pages (info), contradictions (warn)
- `LintReport` + `runGraphLint` — writes `ops/lint/lint-report.json`, updates health

### Commands
- `kg lint [--check <name>] [--json]` — exits 1 on errors
- `kg maintain reweave / mark-stale / compact`

**Bug fixed:** `TestExecuteQuery_Contradictions_Stub` updated to `_NoConflict` after live detection replaced the stub.

---

## 5. KG Phase 5 (Bridge Readiness) + Wave 5 (Workflow Graph Bridge)

Date: 2026-04-10

### KG Phase 5 (`commands/kg.go`)
- `BridgeIntentMapping` + `defaultBridgeMappings()` — 5 bridge→KG intent fanouts
- `KGAdapter` interface, `LocalFileAdapter`, `collectAdapterHealth`
- `executeBridgeQuery()` → resolve → execute → merge → update adapter health
- `writeBridgeContract()` called from `kg setup` → `self/schema/bridge-contract.yaml`
- Commands: `kg bridge query / health / mapping`

### Wave 5 (`commands/workflow.go`)
- Bridge config (`GraphBridgeConfig`), normalized query/response types
- `GraphBridgeAdapter` interface + `GraphBridgeHealth`
- `LocalGraphAdapter` — independent filesystem scanner (no kg.go import), supports 5 intents
- Commands: `workflow graph query / health`
