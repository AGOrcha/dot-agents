# Go Native Code Graph Analysis

Status: active

## Goal

- Analyze and break down the work required to remove reliance on the Python `code-review-graph` bridge for code-graph features.
- Link that work back to the exact historical decision that deferred to the Python path.
- Prepare the shape of a proper future plan without treating it as an immediate blocker for higher-priority bridge/readiness work.

## Historical Decision Being Reopened

The specific deferred choice lives in:

- `.agents/history/crg-kg-integration/crg-kg-integration.plan.md`

Phase B explicitly says:

- the AST parser work was delegated to the Python `code-review-graph` CLI via subprocess bridge
- a full Go tree-sitter port was deferred because the subprocess bridge delivered functionality immediately

That decision also propagated into:

- `internal/graphstore/crg.go`
- `commands/kg/*` code-graph commands
- `internal/graphstore/mcp_server.go`
- skill and workflow surfaces that assume those commands are the authoritative graph backend

## Current Dependency Map

### Direct Python dependency

- `internal/graphstore.CRGBridge`
- `kg build`
- `kg update`
- `kg code-status`
- `kg changes`
- `kg impact`
- `kg flows`
- `kg communities`
- `kg postprocess`

### Indirect dependency

- `kg bridge query` for code intents
- `workflow graph query` for code intents
- `kg serve` MCP tools that use the bridge
- graph-aware skills and hooks

## Why This Is Separate From Command Readiness

There are two different questions:

1. **Are the current commands operationally trustworthy enough to use right now?**
2. **Should the long-term implementation keep relying on Python CRG?**

Question 1 is higher priority and belongs to current command-readiness work.

Question 2 is architectural/product work. It matters, but it should not be mixed into the immediate readiness fix unless the readiness audit proves the Python path is untenable in the short term.

## Target End State

The repo’s own specs still imply a stronger end state than the current implementation:

- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` Phase CRG-B expects parser port in Go
- Phase CRG-C expects change detection and flow/community work in Go
- acceptance language still points toward Go-native parity rather than permanent subprocess dependence

So the current bridge should be treated as an implementation shortcut, not the final architecture.

## Required Analysis Areas

### 1. Parser and ingest layer

- supported languages and their priority
- tree-sitter library choice and maintenance burden
- how to port node/edge extraction from Python CRG
- how to preserve repo-relative paths and symbol identity stability

### 2. Storage and schema ownership

- which parts of the current SQLite schema are already native and reusable
- which fields still mirror Python assumptions
- whether schema compatibility with old Python-produced graphs matters during migration

### 3. Build/update pipeline

- full build path
- incremental update path
- file hashing and invalidation
- branch switch / stale graph behavior

### 4. Query parity

- `code-status`
- `changes`
- `impact`
- `bridge query` code intents
- `flows`
- `communities`
- `postprocess`

### 5. MCP and skill parity

- `kg serve` tool behavior without Python bridge
- skill migration assumptions
- how CLI and MCP should share one implementation

### 6. Rollout and cutover

- dual-run or shadow-run period
- fixture-driven equivalence tests against the current Python-backed outputs
- migration path for existing `.code-review-graph/graph.db`

## Suggested Planning Breakdown

### Phase A: parity audit and fixture strategy

- define representative repos and fixtures
- record current Python-backed outputs as comparison artifacts
- choose the first supported language set

### Phase B: Go-native parser MVP

- implement graph build for the first target language set
- produce nodes/edges in the native store
- verify basic search and status

### Phase C: incremental update and change detection

- add update pipeline
- add `changes` and `impact`
- prove practical parity on fixture repos

### Phase D: advanced graph analysis

- add flows
- add communities
- add postprocess rebuild logic

### Phase E: bridge, MCP, and command cutover

- move `kg bridge query` code intents to the Go-native graph
- move `kg serve` tool handlers to the native implementation
- remove or sharply reduce `CRGBridge` responsibility

### Phase F: deprecation and cleanup

- document Python bridge retirement
- preserve fallback only if truly needed
- remove skill/docs language that treats Python CRG as the normative backend

## Priority Statement

This is important, but it is **not** the immediate highest-priority work.

Priority order should be:

1. current bridge/query command readiness
2. broader KG command-surface readiness
3. Go-native code-graph replacement planning
4. Go-native implementation waves

## Dependency Relationship

- This analysis is linked to the historical defer-in-favor-of-Python decision in `.agents/history/crg-kg-integration/crg-kg-integration.plan.md`.
- It is also related to [kg-command-surface-readiness-analysis.plan.md](/Users/nikashp/Documents/dot-agents/.agents/active/kg-command-surface-readiness-analysis.plan.md), but should stay separate so short-term operational fixes do not get swallowed by a long-range rewrite.
