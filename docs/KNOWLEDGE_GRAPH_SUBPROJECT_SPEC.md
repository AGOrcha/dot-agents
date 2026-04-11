# Knowledge Graph Subproject Spec

Status: Active
Last updated: 2026-04-10
Related:
- `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md`

This document defines the separate knowledge-graph subproject that sits alongside `dot-agents` workflow automation.

`dot-agents` remains the workflow and configuration layer.

This subproject is the long-lived knowledge layer that agents query, maintain, and evolve over time.

The knowledge graph has two complementary subsystems:

1. **Knowledge notes** — human-curated decisions, concepts, entities, and synthesis maintained as markdown files with YAML frontmatter.
2. **Code structure graph** — auto-parsed AST-level code structure (functions, classes, call edges, flows, communities) derived from the `code-review-graph` system and ported into the Go codebase.

These subsystems share a unified storage backend and query surface, enabling decision-to-code traceability.

## Purpose

Agents lose too much time rediscovering context that should already exist:

- prior decisions and their rationale
- recurring patterns across repos
- research synthesis and contradictions
- meeting outcomes and tacit knowledge
- useful implementation notes and handoffs from past work

The knowledge-graph subproject exists to turn that scattered context into a local-first, agent-maintained, deterministic knowledge system that compounds instead of resetting.

The core product idea is simple:

- raw sources remain available
- the graph stores curated, structured knowledge derived from those sources
- agents query the graph through stable operations instead of reconstructing context from scratch

## Problem Statement

Traditional RAG and ad hoc note folders have three recurring failures:

- repeated rediscovery
  - agents keep rereading raw material because no curated layer exists
- maintenance collapse
  - human-maintained wikis decay because cross-linking, indexing, and updates are tedious
- unstable query behavior
  - agents rely on prompt conventions or document spelunking instead of deterministic query contracts

The research in this repo converges on a better pattern:

- plain files, not opaque cloud storage
- structured and interlinked notes, not giant memory dumps
- ingestion and maintenance performed by agents
- deterministic query and health operations

## Product Boundary

This subproject owns:

- the canonical graph layout
- graph-native schemas for notes, indexes, logs, and operations
- ingestion of raw sources into curated graph notes
- code-structure parsing, indexing, and incremental updates
- deterministic query contracts over graph content (notes and code symbols)
- linting, contradiction detection, and graph maintenance
- the unified storage backend (SQLite and Postgres)
- optional adapters to search/index/query backends

This subproject does not own:

- `dot-agents` workflow state, plans, or proposal review
- platform-specific hook rendering
- repo config distribution
- generic session memory such as `CLAUDE.md` or `AGENTS.md`
- cryptographic shared-memory or DKG protocols in the first implementation waves

## Relationship To dot-agents

The intended stack is:

1. Session and workflow layer
   - `dot-agents`
   - orient, persist, plan/task state, proposals, workflow health
2. Knowledge layer
   - this subproject
   - curated knowledge graph, ingestion, deterministic graph queries
3. External search and retrieval adapters
   - structured and semantic query backends
4. Optional multi-agent shared-memory layer
   - future work such as DKG-style verification and cross-machine shared memory

Key rule:

`dot-agents` should integrate with the graph through bridge contracts and query intents, but it should not absorb ownership of graph ingestion or graph storage.

## Product Goals

- Give agents a persistent, queryable knowledge system they can maintain themselves.
- Keep the source of truth local, human-browsable, and portable.
- Separate raw sources from curated knowledge.
- Make query behavior deterministic enough that agents do not need repo-specific prompt conventions.
- Support contradiction detection, staleness tracking, and graph health checks.
- Make the graph usable as a bridgeable context source for `dot-agents` and other agent systems.

## Non-Goals

- No cloud-only storage requirement.
- No database-only source of truth in the first implementation waves.
- No assumption that Obsidian is required; markdown files are the canonical layer.
- No requirement that all knowledge be globally shared across machines or users.
- No cryptographic verification or DKG dependency in the initial product.
- No attempt to replace repo-local docs, specs, or workflow artifacts.

## Design Principles

- Local-first: the graph belongs to the user or team, not a hosted provider.
- File-native: markdown plus frontmatter and predictable indexes are canonical.
- Curated over raw retrieval: the graph stores synthesized knowledge, not only source pointers.
- Agent-maintained: agents do the bookkeeping, cross-linking, and update work.
- Deterministic query surface: graph access should expose stable intents and response shapes.
- Provenance first: every meaningful claim should trace back to one or more sources.
- Human-browsable: a person should be able to inspect the graph without special tooling.
- Adapter-friendly: search, semantic retrieval, and MCP access can vary without changing canonical storage.

## Code Structure Graph

The code structure graph is derived from the `code-review-graph` project and ported into Go as a subsystem of the knowledge graph. It provides AST-level code navigation that replaces grep-based exploration with exact symbol traces.

### What It Indexes

- **Nodes**: File, Class, Function, Type, Test — with qualified names, line ranges, signatures, language
- **Edges**: CALLS, IMPORTS_FROM, INHERITS, IMPLEMENTS, CONTAINS, TESTED_BY, DEPENDS_ON
- **Flows**: Traced execution paths from entry points through the call graph
- **Communities**: Auto-detected code clusters via Louvain-style community detection

### Key Capabilities

- **Impact radius**: Given a symbol, find everything affected by a change (BFS over call edges)
- **Change detection**: Git diff intersected with the graph — risk scores, test gaps, blast radius
- **FTS search**: Full-text search across symbol names and qualified paths
- **Community analysis**: Which modules are tightly coupled, what are the boundaries
- **Flow tracing**: Entry-point-to-leaf execution paths with criticality scoring

### Decision-To-Code Traceability

Knowledge notes can reference code symbols through cross-reference links:

- A decision note ("use connection pooling for DB access") links to the functions that implement it
- A concept note ("retry policy") links to all retry-related symbols
- When a symbol changes, linked notes surface as potentially stale
- When reviewing code, linked decisions provide rationale for why code exists

This is stored as `note_symbol_links` in the database with relation types: `implements`, `documents`, `decides`, `references`.

## Storage Architecture

### Hot/Cold Model

The knowledge graph uses a three-layer storage model:

```
HOT (filesystem, git-tracked)
├── Active knowledge notes (markdown + YAML frontmatter)
├── Current session context, plans, handoffs
├── Working memory for the current agent session
└── Authoritative for human-editable content

WARM (database: SQLite or Postgres)
├── Code structure: nodes, edges, flows, communities
├── Archived knowledge notes (notes table)
├── Cross-references: note→symbol links
├── FTS index, query engine
└── Authoritative for structural queries

COLD (pre-computed, token-efficient)
├── Community summaries
├── Flow snapshots
├── Risk index
├── Note digests for archived content
└── Rebuilt from warm layer on demand
```

### Note Lifecycle

Notes start in the hot layer as markdown files under `KG_HOME/notes/`. As notes age or are superseded:

1. **Active**: hot layer only (filesystem). Agents read directly.
2. **Stale**: hot layer, flagged for review. Lint surfaces these.
3. **Archived/Superseded**: warm layer (database). Metadata and body stored in `kg_notes` table. Filesystem copy is optional.

Code structure data is warm-layer only — it never exists as individual files. It is built by parsing source code and stored directly in the database.

### Multi-Backend Storage

The graph supports pluggable storage backends through a `GraphStore` interface.

#### SQLite (default, solo developer)

- Zero configuration — database file at `.code-review-graph/graph.db` in the repo root
- WAL mode for concurrent reads (agent session + MCP server)
- FTS5 for full-text search with porter stemming
- Pure Go driver (`modernc.org/sqlite`), no CGO required
- Suitable for repositories up to ~100K symbols

#### Postgres (teams, CI, cloud agents)

- Shared graph across a team — multiple developers and CI agents write to the same graph
- Full MVCC concurrency — parallel CI jobs can update different file scopes
- `LISTEN/NOTIFY` for real-time graph update notifications
- `tsvector` + `pg_trgm` replaces FTS5
- Connection pooling for cloud agent fleets
- Suitable for large monorepos and multi-repo team setups

#### Backend Selection

```yaml
# KG_HOME/self/config.yaml or .agentsrc.json
kg:
  backend: sqlite
  sqlite:
    path: .code-review-graph/graph.db
  postgres:
    url: postgres://team@db.internal:5432/code_graph
```

#### Migration Between Backends

`dot-agents kg migrate --to postgres --url <connection_string>` exports the SQLite graph to Postgres. This is a one-time operation for teams scaling up.

## Core Model

The subproject has four logical layers:

1. Raw sources
2. Curated graph
3. Operations and maintenance
4. Query and adapter surface

### 1. Raw Sources

Raw sources are immutable or append-only inputs:

- articles
- docs
- PDFs
- transcripts
- meeting notes
- links
- codebase summaries
- workflow artifacts imported as references

Raw sources are preserved so synthesis can be audited or regenerated later.

### 2. Curated Graph

The curated graph is the agent-maintained knowledge layer:

- source summaries
- entities
- concepts
- synthesis notes
- decision records
- repo or subsystem context pages

This is the layer agents query most often.

### 3. Operations And Maintenance

The graph must support ongoing maintenance:

- ingestion queues
- operation logs
- contradiction checks
- staleness checks
- cross-link repair
- index rebuilds
- schema upgrades

### 4. Query And Adapter Surface

The graph must expose deterministic operations independent of any one retrieval backend:

- structured query
- semantic search
- health and lint results
- export or bridge responses for `dot-agents`

## Canonical Storage Layout

The graph root is configurable. In this spec it is referenced as `KG_HOME`.

Recommended default:

- `~/knowledge-graph/`

Canonical layout:

```text
KG_HOME/
├── self/
│   ├── schema/
│   ├── prompts/
│   ├── policies/
│   └── config.yaml
├── raw/
│   ├── inbox/
│   ├── imported/
│   └── assets/
├── notes/
│   ├── sources/
│   ├── entities/
│   ├── concepts/
│   ├── synthesis/
│   ├── decisions/
│   ├── repos/
│   ├── index.md
│   └── log.md
└── ops/
    ├── queue/
    ├── sessions/
    ├── lint/
    ├── adapters/
    └── health/
```

### Layout Semantics

- `self/`
  - system identity, schema, prompts, and operating policies
- `raw/`
  - source material before or alongside curation
- `notes/`
  - the curated knowledge graph itself
- `ops/`
  - queues, logs, runs, maintenance output, and adapter state

This combines the stable `self/notes/ops` pattern from the research with an explicit raw-source layer.

## Canonical Graph Artifacts

Required artifacts:

| Path | Purpose |
|------|---------|
| `self/config.yaml` | graph root configuration and enabled adapters |
| `notes/index.md` | content-oriented catalog of graph pages |
| `notes/log.md` | append-only operation log |
| `ops/health/graph-health.json` | current graph health snapshot |

Graph pages live under `notes/` and are canonical markdown files with frontmatter.

## Graph Page Schema

Every canonical graph page should include frontmatter with at least:

```yaml
schema_version: 1
id: "concept-memory-graphs-beat-giant-files"
type: "concept"
title: "Memory graphs beat giant memory files"
summary: "Linked, curated notes outperform monolithic memory files for agent retrieval."
status: "active"
source_refs:
  - "source-claude-obsidian-memory-stack"
links:
  - "concept-hybrid-retrieval"
  - "decision-agent-maintained-memory"
created_at: "2026-04-10T12:00:00Z"
updated_at: "2026-04-10T12:00:00Z"
confidence: "medium"
```

### Field Rules

- `schema_version` is required and starts at `1`
- `id` is required and stable
- `type` is required and must be one of:
  - `source`
  - `entity`
  - `concept`
  - `synthesis`
  - `decision`
  - `repo`
  - `session`
- `title` is required
- `summary` is required
- `status` is required and must be one of:
  - `draft`
  - `active`
  - `stale`
  - `superseded`
  - `archived`
- `source_refs` is required and may be empty only for raw source summaries or temporary drafts
- `links` is required and may be empty
- `created_at` and `updated_at` are required UTC RFC3339 timestamps
- `confidence` is required and must be one of:
  - `low`
  - `medium`
  - `high`

## Index And Log Contracts

### `notes/index.md`

`index.md` is the first page agents should read for graph-native browsing.

It should:

- organize notes by category
- give each page a one-line summary
- include lightweight metadata when useful
- remain compact enough for fast inspection

### `notes/log.md`

`log.md` is append-only and records operations such as:

- ingest
- query exports
- lint runs
- refactors
- schema upgrades

Entries should use a stable parseable heading style:

```md
## [2026-04-10] ingest | Claude + Obsidian Memory Stack
```

## Operations

The subproject must support four core operations:

1. Ingest
2. Query
3. Lint
4. Maintain

### Ingest

Ingest turns raw material into curated graph knowledge.

Minimum ingest pipeline:

1. record raw input under `raw/`
2. extract structured claims, entities, and decisions
3. create or update graph pages under `notes/`
4. update `index.md`
5. append to `log.md`
6. emit any unresolved contradictions or review warnings

### Query

Query retrieves graph-backed context through deterministic intents, not prompt folklore.

Required initial query intents:

- `source_lookup`
- `entity_context`
- `concept_context`
- `decision_lookup`
- `repo_context`
- `synthesis_lookup`
- `related_notes`
- `contradictions`
- `graph_health`

### Lint

Lint checks graph integrity and knowledge quality.

Required initial lint checks:

- broken links
- orphan pages
- missing source references
- stale pages
- contradictory active claims
- index drift
- oversize or unsafely broad pages

### Maintain

Maintenance is graph-native housekeeping:

- reweave cross-links
- merge duplicate notes
- split overly broad notes
- mark stale or superseded content
- upgrade schemas
- compact or archive noisy operational artifacts

## Deterministic Query Contract

Every query must resolve to a normalized response shape:

```json
{
  "schema_version": 1,
  "intent": "decision_lookup",
  "query": "What did we decide about workflow graph storage?",
  "results": [
    {
      "id": "decision-local-first-graph",
      "type": "decision",
      "title": "Keep the graph local-first and markdown-native",
      "summary": "The graph should use files as canonical storage and adapters for retrieval.",
      "path": "notes/decisions/local-first-graph.md",
      "source_refs": ["source-karpathy-llm-wiki", "source-ars-contexta"]
    }
  ],
  "warnings": [],
  "provider": "local-index",
  "timestamp": "2026-04-10T12:30:00Z"
}
```

### Query Rules

- `intent` is required and must be from the supported query set
- `results` is required and may be empty
- `warnings` is required and may be empty
- `provider` is required
- `timestamp` is required

This contract exists so other systems, including `dot-agents`, can query the graph deterministically.

## Ingestion Source Types

The first implementation waves should support these source classes:

- markdown files
- local documents such as PDF or text
- URLs saved as captured markdown or metadata-backed source entries
- transcripts
- meeting notes
- repo docs and design docs
- workflow artifacts imported as references, not as the graph’s source of truth

## Adapters And Retrieval Backends

The canonical graph is file-based, but adapters may provide richer retrieval.

Supported adapter categories:

- structured metadata query
- semantic search
- hybrid BM25/vector search
- MCP query bridge

Adapter state must never replace canonical note files as the source of truth.

## Health Model

The graph should expose a compact health snapshot at:

- `ops/health/graph-health.json`

Minimum fields:

- `schema_version`
- `timestamp`
- `note_count`
- `source_count`
- `orphan_count`
- `broken_link_count`
- `stale_count`
- `contradiction_count`
- `queue_depth`
- `status`
  - `healthy`, `warn`, or `error`
- `warnings`

## Provenance And Verification

The first implementation waves use provenance, not cryptographic proof.

Required first-wave guarantees:

- graph pages cite raw or summarized sources
- decisions can be traced to supporting notes
- contradictions are surfaced explicitly
- maintenance actions are logged

Later shared-memory or DKG-style verification may strengthen this, but it is not required for the core product.

## Relationship To Workflow Automation

This subproject should integrate with `dot-agents` through a bridge contract, not through implicit prompt conventions.

Expected integration points:

- `dot-agents` asks deterministic graph query intents
- graph results can support workflow orient, plan/task context, contradiction checks, and repo memory
- workflow artifacts may be imported into the graph as sources or references
- graph health may be surfaced back into workflow health summaries

The graph should remain usable without `dot-agents`, and `dot-agents` should remain usable without the graph.

## Initial Product Surface

The CLI surface covers both knowledge notes and code structure:

### Knowledge note commands

- `kg setup` — initialize graph home and schema
- `kg ingest` — ingest raw sources into curated notes
- `kg query` — query by deterministic intent
- `kg lint` — check graph integrity
- `kg health` — inspect combined graph health

### Code structure commands

- `kg build` — full graph parse of source code (replaces `code-review-graph build`)
- `kg update` — incremental update of changed files (replaces `code-review-graph update`)
- `kg changes` — detect change impact with risk scores (replaces `code-review-graph detect-changes`)
- `kg impact <symbol>` — impact radius for a symbol
- `kg search <query>` — FTS across notes and code symbols
- `kg status` — combined note + code graph statistics

### Cross-reference commands

- `kg link <note-id> <symbol>` — link a knowledge note to a code symbol
- `kg bridge query --intent ...` — unified query across both subsystems
- `kg bridge health` — bridge adapter availability and health

### Server commands

- `kg serve` — start MCP server (stdio transport) for AI coding platforms

## MVP Scope

The MVP for this subproject is:

- local-first file-native graph layout
- canonical note schema
- index and log contracts
- basic ingest from markdown and local-source inputs
- deterministic structured query contract
- basic lint and health outputs

## MVP Addendum: Code Structure Graph

The code structure MVP adds to the knowledge note MVP:

- Go `GraphStore` interface with SQLite backend
- AST parsing via tree-sitter for Go, Python, TypeScript, JavaScript, Ruby, Rust, Java
- Incremental update via file hashing
- Change detection with risk scoring and test gap analysis
- FTS5 search across code symbols
- `kg_notes` table for warm-layer note storage
- `note_symbol_links` table for decision-to-code traceability
- `kg build`, `kg update`, `kg changes`, `kg search` commands

## Post-MVP Scope

After the MVP is stable, likely next additions are:

- richer ingestion for media and transcripts
- semantic and hybrid retrieval adapters
- automated contradiction clustering
- repo-aware graph views
- `dot-agents` bridge adapters
- Postgres backend for teams and cloud agents
- multi-agent shared-memory experiments
- Go-native MCP server (`kg serve`)
- pgvector embeddings for semantic code search

## Explicitly Deferred Work

- cloud-first storage
- mandatory hosted indexing
- DKG-style verification and publishing
- cryptographic truth oracles
- fully automatic graph mutation without logs or review signals
- transport-specific coordination protocols embedded into graph storage

## Phase Mapping

### Phase 1: Graph Core

Must deliver:

- canonical layout under `KG_HOME`
- note schema
- `index.md`
- `log.md`
- graph health snapshot

### Phase 2: Basic Ingest

Must deliver:

- raw-source recording
- source-to-note ingest flow
- note creation and update rules
- index and log updates

### Phase 3: Deterministic Query Surface

Must deliver:

- query intents
- normalized query response contract
- basic structured retrieval over the canonical graph

### Phase 4: Lint And Maintenance

Must deliver:

- contradiction checks
- stale note checks
- orphan and broken-link checks
- maintenance operation logging

### Phase 5: Bridge Readiness

Must deliver:

- stable bridgeable query surface for `dot-agents`
- adapter health reporting
- documented mapping from graph intents to workflow integration intents

### Phase 6: Shared-Memory Research

Must deliver only after earlier phases are stable:

- evaluation of stronger verification models
- evaluation of DKG-like shared-memory approaches
- explicit boundary between graph truth, workflow truth, and coordination truth

### Phase CRG-A: GraphStore Interface And SQLite Backend

Must deliver:

- `GraphStore` interface in `internal/graphstore/`
- SQLite implementation with schema and migrations ported from code-review-graph
- `kg_notes` and `note_symbol_links` tables
- Node/edge CRUD, impact radius queries, FTS search
- Tests against SQLite

### Phase CRG-B: Parser Port

Must deliver:

- Tree-sitter Go bindings for supported languages
- AST node and edge extraction ported from code-review-graph `parser.py`
- Incremental update via file hashing
- `kg build` and `kg update` commands

### Phase CRG-C: Change Detection And Flows

Must deliver:

- Git diff intersected with graph for change impact analysis
- Flow detection from entry points
- Community detection via Louvain clustering
- `kg changes` and `kg impact` commands

### Phase CRG-D: Hot/Cold Note Lifecycle

Must deliver:

- Hot filesystem ↔ warm database note sync
- Archive lifecycle (active → stale → archived)
- `note_symbol_links` CRUD
- `kg link` command

### Phase CRG-E: Postgres Backend

Must deliver:

- `GraphStore` Postgres implementation using `pgx`
- Dialect-specific migrations
- Connection pooling
- `kg migrate --to postgres` command
- Config-driven backend selection

### Phase CRG-F: Skill Integration

Must deliver:

- `build-graph` skill updated to use `dot-agents kg build`
- `review-delta` and `review-pr` skills wired to `dot-agents kg changes` and bridge queries
- Graph awareness added to `self-review` and `agent-start` skills
- Canonical graph hooks in `~/.agents/hooks/global/`

## Acceptance Standard

The subproject is on the right track only when:

- agents can answer recurring context questions from the graph instead of rereading raw sources
- the graph stays inspectable as plain files
- knowledge quality improves through maintenance instead of degrading over time
- `dot-agents` can consume graph context through deterministic contracts instead of ad hoc prompting
- future shared-memory work can layer on top without replacing the local-first graph core
- agents navigate code through exact symbol traces instead of grep-and-hope
- knowledge notes can trace to the code symbols they describe
- teams can scale from local SQLite to shared Postgres without changing query contracts
