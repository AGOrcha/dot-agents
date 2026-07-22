# Go Native Code Graph Analysis

**Status:** superseded-in-part (2026-05-09)
**Superseded by:** [graph-backend-adapter-contract §11 (CRG migration path)](../graph-backend-adapter-contract/design.md)
**Remaining scope:** (a) the Tree-sitter binding choice for the CRG adapter's bootstrap skill; (b) the bootstrap-skill design itself. The broader Python-vs-Go architectural question is now answered by the adapter contract: CRG becomes a kg-native adapter writing to scoped KG storage; the subprocess bridge is decommissioned once the §11.1 tool-by-tool parity matrix passes. Sections of this document that argue for or against the subprocess bridge as an architecture choice are obsolete.

**Original status (preserved for context):** design artifact for the long-range replacement of the Python `code-review-graph` bridge. This is not the near-term readiness plan; short-term operational fixes belong in [Graph Bridge Command Readiness](../../plans/graph-bridge-command-readiness/PLAN.yaml).

## Goal

- analyze and break down the work required to remove reliance on the Python `code-review-graph` bridge for code-graph features
- link that work back to the exact historical decision that deferred to the Python path
- prepare the shape of a proper future plan without treating it as an immediate blocker for higher-priority bridge and readiness work

## Historical Decision Being Reopened

The deferred choice lives in `.agents/history/crg-kg-integration/crg-kg-integration.plan.md`.

Phase B explicitly chose the Python `code-review-graph` CLI via subprocess bridge instead of a full Go tree-sitter port because the subprocess bridge delivered functionality faster.

That decision propagated into:

- `internal/graphstore/crg.go`
- `commands/kg/*` code-graph commands
- `internal/graphstore/mcp_server.go`
- skills and workflow surfaces that assume those commands are the authoritative graph backend

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

1. Are the current commands operationally trustworthy enough to use right now?
2. Should the long-term implementation keep relying on Python CRG?

Question 1 is higher priority and belongs to current readiness work.

Question 2 is architectural and product work. It matters, but it should not be mixed into the immediate readiness fix unless the readiness audit proves the Python path is untenable in the short term.

## Target End State

The repo’s own specs still imply a stronger end state than the current implementation:

- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` Phase CRG-B expects parser port in Go
- Phase CRG-C expects change detection and flow/community work in Go
- acceptance language still points toward Go-native parity rather than permanent subprocess dependence

The current bridge should therefore be treated as an implementation shortcut, not the final architecture.

## Required Analysis Areas

### 1. Parser and ingest layer

- supported languages and their priority
- tree-sitter library choice and maintenance burden
- how to port node and edge extraction from Python CRG
- how to preserve repo-relative paths and symbol identity stability

### 2. Storage and schema ownership

- which parts of the current SQLite schema are already native and reusable
- which fields still mirror Python assumptions
- whether compatibility with Python-produced graphs matters during migration

### 3. Build and update pipeline

- full build path
- incremental update path
- file hashing and invalidation
- branch switch and stale-graph behavior

### 4. Query parity

- `code-status`
- `changes`
- `impact`
- `bridge query` code intents
- `flows`
- `communities`
- `postprocess`

### 5. MCP and skill parity

- `kg serve` tool behavior without the Python bridge
- skill migration assumptions
- how CLI and MCP should share one implementation

### 6. Rollout and cutover

- dual-run or shadow-run period
- fixture-driven equivalence tests against Python-backed outputs
- migration path for existing `.code-review-graph/graph.db`

## Suggested Planning Breakdown

### Phase A: parity audit and fixture strategy

- define representative repos and fixtures
- record current Python-backed outputs as comparison artifacts
- choose the first supported language set

### Phase B: Go-native parser MVP

- implement graph build for the first target language set
- produce nodes and edges in the native store
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
- remove skill and docs language that treats Python CRG as the normative backend

## Priority Statement

Priority order should remain:

1. current bridge and query command readiness
2. broader KG command-surface readiness
3. Go-native code-graph replacement planning
4. Go-native implementation waves

## Code Structure Graph Reference Model

This section records the code-structure-graph model the Go-native replacement must reproduce, plus the original CRG phase labels for directional context. It is descriptive, not a competing plan: storage-backend design (the `GraphStore` interface, SQLite/Postgres selection, hot/cold layering) is owned by [graph-backend-adapter-contract](../graph-backend-adapter-contract/design.md) and is not restated here.

### What It Indexes

The graph is AST-level, derived from the `code-review-graph` project, and exists to replace grep-based exploration with exact symbol traces:

- **Nodes**: File, Class, Function, Type, Test — carrying qualified names, line ranges, signatures, and language
- **Edges**: CALLS, IMPORTS_FROM, INHERITS, IMPLEMENTS, CONTAINS, TESTED_BY, DEPENDS_ON
- **Flows**: traced execution paths from entry points through the call graph
- **Communities**: auto-detected code clusters via Louvain-style community detection

### Key Capabilities

These define the semantics the query-parity commands above (`impact`, `changes`, `flows`, `communities`, search) must preserve:

- **Impact radius**: given a symbol, find everything affected by a change via BFS over call edges
- **Change detection**: git diff intersected with the graph, producing risk scores, test-gap analysis, and blast radius
- **FTS search**: full-text search across symbol names and qualified paths
- **Community analysis**: which modules are tightly coupled and where the boundaries lie
- **Flow tracing**: entry-point-to-leaf execution paths with criticality scoring

### Decision-To-Code Traceability

Knowledge notes cross-reference code symbols so decisions and the code that realizes them stay linked:

- a decision note links to the functions that implement it
- a concept note links to all symbols related to that concept
- when a symbol changes, linked notes surface as potentially stale
- when reviewing code, linked decisions supply the rationale for why the code exists

This is stored as `note_symbol_links` with relation types `implements`, `documents`, `decides`, and `references`. The Go-native replacement owns the `note_symbol_links` CRUD path and the `kg link` command.

### Original CRG Phase Labels

The retired spec sequenced the port as CRG-A through CRG-F. They are preserved here for cross-referencing older plans and history; the storage phases defer to the adapter contract:

- **CRG-A** — GraphStore interface and SQLite backend (storage-backend scope; see graph-backend-adapter-contract)
- **CRG-B** — parser port: tree-sitter Go bindings, node/edge extraction from `parser.py`, incremental update via file hashing, `kg build`/`kg update`
- **CRG-C** — change detection and flows: git-diff intersection, flow detection, Louvain community detection, `kg changes`/`kg impact`
- **CRG-D** — hot/cold note lifecycle: filesystem↔database note sync, active→stale→archived archive lifecycle, `note_symbol_links` CRUD, `kg link`
- **CRG-E** — Postgres backend (storage-backend scope; see graph-backend-adapter-contract)
- **CRG-F** — skill integration: `build-graph` on `da kg build`; `review-delta`/`review-pr` on `da kg changes` and bridge queries; graph awareness in `self-review`/`agent-start`; canonical graph hooks

Consolidated from the retired docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md.
