# CRG+KG Integration: Code-Review-Graph Into Knowledge Graph Subsystem

Spec references:
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md`
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` (Wave 5)
- `.agents/active/kg-phase-5-bridge-readiness.plan.md`

Status: **Complete** (2026-04-12). Phases A–G done in canonical TASKS: skills live under `src/share/templates/standard/skills/global/`; global hook bundles ship as `internal/scaffold/hooks/global/{graph-update,graph-orient,graph-precommit}/` (copied to `~/.agents/hooks/global/` on init when missing). Pre-commit behavior uses Claude `PreToolUse`/`Bash` + script (Claude Code has no `PreCommit` event).
Created: 2026-04-10

## Problem

Two graph systems exist in parallel:

1. **Knowledge Graph (KG)** — file-based markdown notes with YAML frontmatter under `KG_HOME`. Human-curated decisions, concepts, entities, synthesis. No database, no query engine beyond index scanning. Implemented as `dot-agents kg` subcommands in Go.

2. **code-review-graph (CRG)** — SQLite-backed AST-parsed code structure graph. Functions, classes, call edges, data flows, communities, FTS5 search, risk scoring. Python tool with MCP server. Installed separately via `uvx`.

These serve complementary purposes but are disconnected:
- CRG knows code structure but not project decisions or context.
- KG knows decisions and context but cannot navigate code.
- Skills like `review-delta` and `review-pr` call CRG MCP tools but have no bridge to KG.
- No shared storage, no shared query surface, no decision-to-code traceability.

## Vision

Port CRG into the dot-agents KG subsystem as the **code-structure layer**, sitting alongside the existing **knowledge-note layer**. A unified graph store with pluggable backends (SQLite for solo, Postgres for teams) serves both layers. Hot/cold architecture keeps the filesystem authoritative for active notes while the database handles structured queries, archived notes, and code symbols.

## Architecture

### Three-Layer Storage Model

```
HOT (filesystem, git-tracked)
├── Active knowledge notes (markdown + YAML frontmatter)
├── Current session context, plans, handoffs
├── Working memory for the current agent session
└── Authoritative for human-editable content

WARM (database: SQLite or Postgres)
├── Code structure: nodes, edges, flows, communities
├── Archived/cold knowledge notes (notes table)
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

### Hot ↔ Warm Lifecycle

- Notes start HOT: created as markdown files under `KG_HOME/notes/`
- Notes go WARM when: status changes to `archived` or `superseded`, or agent marks them cold
- Warm notes: metadata + body stored in `notes` table, filesystem copy optional
- Code structure is WARM-only: never exists as individual files, always in DB
- Cold layer: pre-computed summaries rebuilt by `kg postprocess` or equivalent

### Multi-Backend Storage

```yaml
# .agentsrc.json or KG_HOME/self/config.yaml
kg:
  backend: sqlite                          # default
  sqlite:
    path: .code-review-graph/graph.db      # per-repo
  postgres:
    url: postgres://team@db:5432/kg        # team/cloud
```

| Concern | SQLite | Postgres |
|---------|--------|----------|
| Setup | Zero config, file per repo | Connection string |
| Concurrency | Single writer (WAL) | Full MVCC |
| FTS | FTS5 (porter stemming) | tsvector + pg_trgm |
| Scale | Fine to ~100K nodes | Millions |
| Shared access | Single machine | Team-wide, CI, cloud agents |
| Graph traversal | Recursive CTE or in-memory | Recursive CTE, pg_graphql |

### GraphStore Interface (Go)

```go
type GraphStore interface {
    // Code structure (ported from CRG)
    UpsertNode(node NodeInfo, fileHash string) (int64, error)
    UpsertEdge(edge EdgeInfo) (int64, error)
    DeleteFileNodes(filePath string) error
    GetNode(qualifiedName string) (*GraphNode, error)
    SearchNodes(query string, limit int) ([]*GraphNode, error)
    ImpactRadius(qualifiedName string, depth int) ([]*GraphNode, error)
    DetectChanges(base string) (*ChangeReport, error)

    // Knowledge notes (new)
    UpsertNote(note GraphNote, body string) error
    GetNote(id string) (*GraphNote, string, error)
    SearchNotes(query string, limit int) ([]*GraphNote, error)
    ArchiveNote(id string) error

    // Cross-references (new: decision→code traceability)
    LinkNoteToSymbol(noteID string, qualifiedName string, relation string) error
    GetSymbolNotes(qualifiedName string) ([]*GraphNote, error)
    GetNoteSymbols(noteID string) ([]*GraphNode, error)

    // Flows and communities (ported from CRG)
    ListFlows(limit int) ([]*Flow, error)
    GetFlow(id int64) (*Flow, error)
    ListCommunities() ([]*Community, error)
    GetCommunity(id int64) (*Community, error)

    // Health and metadata
    Stats() (*GraphStats, error)
    RunMigrations() error
    Close() error
}
```

### Schema (extends CRG schema with notes + cross-refs)

New tables beyond existing CRG schema:

```sql
-- Knowledge notes in the database (warm/cold layer)
CREATE TABLE IF NOT EXISTS kg_notes (
    id TEXT PRIMARY KEY,           -- matches GraphNote.ID
    type TEXT NOT NULL,            -- source|entity|concept|synthesis|decision|repo|session
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    status TEXT NOT NULL,          -- draft|active|stale|superseded|archived
    confidence TEXT DEFAULT '',
    source_refs TEXT DEFAULT '[]', -- JSON array
    links TEXT DEFAULT '[]',       -- JSON array
    body TEXT DEFAULT '',          -- markdown body
    file_path TEXT DEFAULT '',     -- path to hot-layer file if still on disk
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Cross-references between notes and code symbols
CREATE TABLE IF NOT EXISTS note_symbol_links (
    note_id TEXT NOT NULL,
    qualified_name TEXT NOT NULL,
    relation TEXT NOT NULL,         -- implements|documents|decides|references
    created_at TEXT NOT NULL,
    PRIMARY KEY (note_id, qualified_name)
);

CREATE INDEX IF NOT EXISTS idx_kg_notes_type ON kg_notes(type);
CREATE INDEX IF NOT EXISTS idx_kg_notes_status ON kg_notes(status);
CREATE INDEX IF NOT EXISTS idx_note_symbol_note ON note_symbol_links(note_id);
CREATE INDEX IF NOT EXISTS idx_note_symbol_symbol ON note_symbol_links(qualified_name);
```

## Port Strategy: Python → Go

### What to port

| CRG Python module | Go equivalent | Notes |
|-------------------|---------------|-------|
| `graph.py` (GraphStore) | `internal/graphstore/store.go` | Core interface + SQLite impl |
| `parser.py` | `internal/graphstore/parser.go` | Use tree-sitter Go bindings |
| `migrations.py` | `internal/graphstore/migrations.go` | Same migration framework |
| `flows.py` | `internal/graphstore/flows.go` | Flow detection |
| `communities.py` | `internal/graphstore/communities.go` | Louvain clustering |
| `changes.py` | `internal/graphstore/changes.go` | Change impact detection |
| `search.py` | `internal/graphstore/search.go` | FTS queries |
| `incremental.py` | `internal/graphstore/incremental.go` | File-hash based incremental |
| `skills.py` | Remove — dot-agents owns skill generation | Skills become native |
| `cli.py` | Remove — becomes `dot-agents kg` subcommands | CLI absorbed |
| `main.py` | Remove | Entry point absorbed |

### What NOT to port (stays as-is or drops)

- MCP server: keep as Python `code-review-graph serve` initially, replace with Go MCP server later
- Visualization: `visualization.py` can stay as optional Python tool
- Wiki generation: `wiki.py` can stay as optional Python tool

### New `dot-agents kg` subcommands

```
dot-agents kg setup          # existing — also initializes DB
dot-agents kg health         # existing — adds code graph stats
dot-agents kg build          # NEW: full graph parse (replaces `code-review-graph build`)
dot-agents kg update         # NEW: incremental update (replaces `code-review-graph update`)
dot-agents kg status         # NEW: combined note + code graph stats
dot-agents kg impact <sym>   # NEW: impact radius query
dot-agents kg search <query> # NEW: FTS across notes + symbols
dot-agents kg changes        # NEW: detect-changes (replaces `code-review-graph detect-changes`)
dot-agents kg link           # NEW: link note to symbol
dot-agents kg bridge query   # existing plan — now queries both layers
dot-agents kg bridge health  # existing plan — reports both layers
```

## Skill ↔ Command Integration

### Skills that consume graph (via MCP or direct CLI)

| Skill | Current CRG MCP calls | Integrated equivalent |
|-------|----------------------|----------------------|
| `build-graph` | `list_graph_stats_tool`, `build_or_update_graph_tool` | `dot-agents kg build` / `dot-agents kg status` |
| `review-delta` | `build_or_update_graph_tool`, `get_review_context_tool`, `get_impact_radius_tool`, `query_graph_tool` | `dot-agents kg update` + `dot-agents kg changes` + bridge queries |
| `review-pr` | Same as review-delta + `semantic_search_nodes_tool` | Same + `dot-agents kg search` |
| `agent-start` | Prefers graph over manual scans | `dot-agents workflow orient` (already includes graph health via bridge) |
| `self-review` | None currently | Should call `dot-agents kg changes --brief` for impact awareness |
| `split-reviewable-commits` | None currently | Should call `dot-agents kg` communities to suggest semantic commit boundaries |
| `gh-fix-ci` | None currently | Could call `dot-agents kg changes --base <failing-sha>` to scope investigation |

### Commands that inject/reference skills

| Command | Skills it should reference/inject |
|---------|----------------------------------|
| `dot-agents init` | Generates skill files from templates; should include graph-aware skills |
| `dot-agents add` | Registers MCP server config; should register `kg serve` MCP |
| `dot-agents refresh` | Updates skill content from source; should update graph skill templates |
| `dot-agents review approve` | Could trigger `self-review` skill as pre-check |
| `dot-agents workflow orient` | Invokes `agent-start` context; should include graph status |

### Commands that use graph data internally

| Command | Graph usage |
|---------|-------------|
| `dot-agents review approve` | Call `kg changes` to validate proposal impact before applying |
| `dot-agents workflow orient` | Query bridge for `plan_context`, `decision_lookup` from KG |
| `dot-agents workflow checkpoint` | Record which symbols were modified in checkpoint metadata |
| `dot-agents explain` | Could use graph to answer structural questions about the project |
| `dot-agents doctor` | Check graph health alongside config health |

## Hooks Integration

### Current CRG hooks (Python, in skills.py)

```json
{
  "PostToolUse": [{"matcher": "Edit|Write|Bash", "command": "code-review-graph update --skip-flows"}],
  "SessionStart": [{"command": "code-review-graph status"}],
  "PreCommit": [{"command": "code-review-graph detect-changes --brief"}]
}
```

### Target: canonical hooks managed by dot-agents

These become entries in `~/.agents/hooks/global/`:

```yaml
# ~/.agents/hooks/global/graph-update/HOOK.yaml
name: graph-update
event: post_tool_use
matcher: "Edit|Write|Bash"
command: "dot-agents kg update --skip-flows"
timeout: 5000

# ~/.agents/hooks/global/graph-orient/HOOK.yaml
name: graph-orient
event: session_start
command: "dot-agents kg status"
timeout: 3000

# ~/.agents/hooks/global/graph-precommit/HOOK.yaml
name: graph-precommit
event: pre_commit
command: "dot-agents kg changes --brief"
timeout: 10000
```

These integrate with the existing `session-orient` hook which already calls `dot-agents workflow orient`.

## Measurement and Observability

### How to measure effectiveness

1. **Graph adoption rate**: Count MCP tool calls vs grep/glob fallbacks per session (via session-capture hook)
2. **Context hit rate**: Did `kg search` or `kg bridge query` return useful results? Track query→result→usage
3. **Test gap reduction**: `risk_index.test_coverage` changes after review-pr/review-delta sessions
4. **Risk score trends**: `risk_index.risk_score` over time per community
5. **Decision traceability**: Percentage of active decisions with `note_symbol_links` to code
6. **Staleness**: How many notes are `stale` vs `active` over time

### Health dashboard (future)

`dot-agents kg health` should show:
- Code graph: X nodes, Y edges, Z files, N languages
- Knowledge notes: X hot, Y warm, Z archived
- Cross-refs: X note→symbol links
- Risk: top 5 highest-risk uncovered symbols
- Staleness: X notes need review

## Implementation Phases

### Phase A: GraphStore interface + SQLite backend (Go port core)

Port the core storage and query engine from Python to Go.

1. Define `GraphStore` interface in `internal/graphstore/`
2. Implement SQLite backend using `modernc.org/sqlite` (pure Go)
3. Port schema + migrations from CRG's `migrations.py`
4. Port `upsert_node`, `upsert_edge`, `delete_file_nodes`, `get_node`, `search`
5. Port `impact_radius` (BFS/recursive CTE)
6. Add `kg_notes` and `note_symbol_links` tables
7. Tests against SQLite

### Phase B: Parser port (CRG subprocess bridge) ✓ COMPLETE

Delegated AST parsing to the Python code-review-graph CLI via subprocess bridge.
Decision: full Go tree-sitter port is ~3000 lines of Python; subprocess bridge delivers equivalent
functionality immediately since `.venv` is already set up with the Python CRG installed.

1. ✓ `internal/graphstore/crg.go` — CRGBridge type, DiscoverCRGBin(), Build(), Update(), Status(), DetectChanges()
2. ✓ `internal/graphstore/crg_test.go` — unit tests for status parsing and bin discovery
3. ✓ `dot-agents kg build` — full graph build (wraps `code-review-graph build`)
4. ✓ `dot-agents kg update` — incremental update (wraps `code-review-graph update`)
5. ✓ `dot-agents kg code-status` — graph stats (nodes, edges, languages)
6. ✓ `dot-agents kg changes [--brief]` — change impact (wraps `code-review-graph detect-changes`)

### Phase C: Change detection + flows ✓ COMPLETE

Implemented via CRG Python tool bridge (same pattern as Phase B).

1. ✓ `CRGBridge.GetImpactRadius()` — blast-radius for given files or current diff
2. ✓ `CRGBridge.ListFlows()` — execution flow listing  
3. ✓ `CRGBridge.ListCommunities()` — code community listing
4. ✓ `CRGBridge.Postprocess()` — flows/communities/FTS rebuild
5. ✓ `dot-agents kg impact [file...]` — blast-radius query with --depth/--limit
6. ✓ `dot-agents kg flows` — execution flows with --sort/--limit
7. ✓ `dot-agents kg communities` — code communities with --min-size/--sort
8. ✓ `dot-agents kg postprocess` — rebuild flows/communities/FTS

### Phase D: Hot/cold note lifecycle

Bridge filesystem KG notes with database storage.

1. Note sync: hot filesystem ↔ warm database
2. Archive lifecycle: active → stale → archived (moves to DB)
3. `note_symbol_links` CRUD
4. `dot-agents kg link` command

### Phase E: Postgres backend

Add Postgres as alternative backend.

1. Implement `GraphStore` interface for Postgres using `pgx`
2. Dialect-specific migrations (SERIAL, tsvector, etc.)
3. Connection pooling
4. `dot-agents kg migrate --to postgres` for SQLite→PG migration
5. Config-driven backend selection

### Phase F: MCP server in Go

Replace the Python `code-review-graph serve` MCP server with a Go-native stdio MCP server embedded in the `dot-agents` binary. Clients (Claude Code, Cursor, etc.) configure it as `dot-agents kg serve` instead of the Python `uvx` invocation.

#### Deployment model

Embedded in `dot-agents` binary as `dot-agents kg serve`. NOT a separate binary. Stdio transport only (JSON-RPC 2.0 over stdin/stdout). HTTP/SSE transport is out of scope for this phase.

#### Tool catalog

The Go MCP server exposes exactly these eight tools (drop-in replacement for existing Python server):

| Tool name | Input schema | Output schema |
|-----------|-------------|---------------|
| `build_or_update_graph_tool` | `{}` | `{nodes: int, edges: int, files: int, duration_ms: int}` |
| `embed_graph_tool` | `{}` | `{status: "ok"\|"error", message: string}` |
| `list_graph_stats_tool` | `{}` | `{nodes: int, edges: int, languages: {[lang]: int}, communities: int}` |
| `get_impact_radius_tool` | `{symbol: string, depth?: int}` (depth default 2) | `{nodes: [{name: string, type: string, file: string, risk_score?: float64}]}` |
| `semantic_search_nodes_tool` | `{query: string, limit?: int}` (limit default 20) | `{results: [{name: string, type: string, file: string, summary?: string}]}` |
| `query_graph_tool` | `{intent: string, query: string, scope?: string}` | `{results: [{type: string, title: string, summary: string}], warnings?: [string]}` |
| `get_review_context_tool` | `{files: [string]}` | `{changed_symbols: [...], impact_radius: [...], risk_summary: string}` |
| `get_docs_section_tool` | `{section: string}` | `{content: string, source: string}` |

All input types are JSON objects. All output types are JSON objects. Tools that require the graph to be built first return `{error: "graph not built — run build_or_update_graph_tool first"}` as the result (not as a JSON-RPC error).

#### New file: `internal/graphstore/mcp_server.go`

```go
type MCPServer struct {
    bridge  *CRGBridge   // Phase B bridge — delegates heavy lifting to Python CRG binary
    workDir string
}

func NewMCPServer(workDir string) *MCPServer

// Serve reads JSON-RPC 2.0 requests from r and writes responses to w until r returns EOF.
func (s *MCPServer) Serve(r io.Reader, w io.Writer) error

// dispatch routes a single JSON-RPC call to the appropriate handler.
func (s *MCPServer) dispatch(method string, id json.RawMessage, params json.RawMessage) (json.RawMessage, error)

// One handler per tool:
func (s *MCPServer) handleBuildOrUpdateGraph(params json.RawMessage) (json.RawMessage, error)
func (s *MCPServer) handleEmbedGraph(params json.RawMessage) (json.RawMessage, error)
func (s *MCPServer) handleListGraphStats(params json.RawMessage) (json.RawMessage, error)
func (s *MCPServer) handleGetImpactRadius(params json.RawMessage) (json.RawMessage, error)
func (s *MCPServer) handleSemanticSearchNodes(params json.RawMessage) (json.RawMessage, error)
func (s *MCPServer) handleQueryGraph(params json.RawMessage) (json.RawMessage, error)
func (s *MCPServer) handleGetReviewContext(params json.RawMessage) (json.RawMessage, error)
func (s *MCPServer) handleGetDocsSection(params json.RawMessage) (json.RawMessage, error)
```

The MCP protocol method for tool-call is `tools/call`. The `method` in the JSON-RPC request is `tools/call`; the tool name is inside the `params` object as `params.name`. The `dispatch` function routes by `params.name` after confirming `method == "tools/call"`. Also handle `tools/list` to return the tool catalog (for MCP client capability negotiation).

#### New subcommand in `commands/kg.go`

Add `kgServeCmd` alongside existing kg subcommands:
```go
kgServeCmd := &cobra.Command{
    Use:   "serve",
    Short: "Start the MCP server (stdio transport, JSON-RPC 2.0)",
    RunE:  runKGServe,
}
```

`runKGServe` implementation:
```go
func runKGServe(_ *cobra.Command, _ []string) error {
    workDir, err := os.Getwd()
    if err != nil {
        return err
    }
    srv := graphstore.NewMCPServer(workDir)
    return srv.Serve(os.Stdin, os.Stdout)
}
```

#### `dot-agents add` changes (auto-registration)

In `commands/add.go`, when registering an MCP server for a platform that supports it (Claude Code, Cursor, Copilot), check if `dot-agents kg serve` is available (i.e., `os.Executable()` succeeds). If so, include a `dot-agents-kg` MCP entry alongside any existing entries:

```json
{
  "mcpServers": {
    "dot-agents-kg": {
      "command": "<path-to-dot-agents>",
      "args": ["kg", "serve"],
      "type": "stdio"
    }
  }
}
```

Use `os.Executable()` to resolve the exact binary path. Do not hardcode `"dot-agents"`.

#### `dot-agents init` changes

In the generated platform config templates for Claude Code (`.claude/settings*.json`) and Cursor (`.cursor/mcp.json`), add the `dot-agents-kg` MCP server entry using the same pattern as `dot-agents add` above. Only add it when `agentsrc.json` has a `kg` block (indicating the user has configured the KG subsystem).

#### Tests in `commands/kg_test.go`

- `TestKGServeToolsList`: write a JSON-RPC `tools/list` request to a pipe; assert the response includes all eight tool names.
- `TestKGServeBuildOrUpdateGraph`: write a `tools/call` request with `name: "build_or_update_graph_tool"`; assert the response is a valid JSON object with `nodes`, `edges`, `files`, `duration_ms` fields (mock the CRGBridge to return a canned result).
- `TestKGServeGetImpactRadius`: write a `tools/call` request with `name: "get_impact_radius_tool"` and `arguments: {symbol: "main.run", depth: 1}`; assert the response has a `nodes` array.
- `TestKGServeUnknownTool`: write `tools/call` with an unknown `name`; assert the JSON-RPC response has an `error` field with code -32601 (method not found).

#### Open questions resolved for this phase

- **Separate binary vs embedded**: embedded in `dot-agents` as `dot-agents kg serve`. No separate binary.
- **HTTP/SSE transport**: out of scope; stdio only.
- **Multi-repo with Postgres**: out of scope for Phase F; the server uses the current working directory's graph DB.
- **Tree-sitter binding**: unchanged — Phase B subprocess bridge handles all parsing; Phase F MCP server delegates to the same CRGBridge.

### Phase G: Skill integration

Wire skills to use native graph commands.

1. Update `build-graph` skill to use `dot-agents kg build`
2. Update `review-delta` to use `dot-agents kg changes` + bridge queries
3. Update `review-pr` similarly
4. Add graph awareness to `self-review`, `agent-start`
5. Add canonical graph hooks to `~/.agents/hooks/global/`
6. Update `session-orient` hook to include graph status

## Dependencies

- Phase A: standalone, can start immediately
- Phase B: depends on Phase A (needs GraphStore)
- Phase C: depends on Phase B (needs parsed graph)
- Phase D: depends on Phase A (needs notes table)
- Phase E: depends on Phase A (same interface, new impl)
- Phase F: depends on Phase C (needs full query surface)
- Phase G: depends on Phase C + D (needs changes + notes)

Parallelizable: A+D can start together. B+E can proceed in parallel once A lands.

## Files Created/Modified

New:
- `internal/graphstore/store.go` — interface + types
- `internal/graphstore/sqlite.go` — SQLite implementation
- `internal/graphstore/postgres.go` — Postgres implementation (Phase E)
- `internal/graphstore/migrations.go` — schema migrations
- `internal/graphstore/parser.go` — tree-sitter AST parsing (Phase B)
- `internal/graphstore/changes.go` — change detection (Phase C)
- `internal/graphstore/flows.go` — flow detection (Phase C)
- `internal/graphstore/communities.go` — community detection (Phase C)

Modified:
- `commands/kg.go` — new subcommands (build, update, changes, impact, search, link, serve)
- `commands/kg_test.go` — tests for all new commands
- `cmd/dot-agents/main.go` — if new top-level commands needed
- `docs/KNOWLEDGE_GRAPH_SUBPROJECT_SPEC.md` — CRG integration, multi-backend, hot/cold
- `docs/WORKFLOW_AUTOMATION_FOLLOW_ON_SPEC.md` — Wave 5 expansion for code-structure queries
- `go.mod` / `go.sum` — new dependencies (sqlite driver, tree-sitter)

## Acceptance Criteria

1. `dot-agents kg build` parses a Go/Python/TS repo into SQLite with the same fidelity as Python CRG
2. `dot-agents kg changes` produces equivalent output to `code-review-graph detect-changes`
3. `dot-agents kg search` returns symbols and notes from a single query
4. Knowledge notes can reference code symbols via `note_symbol_links`
5. `dot-agents kg health` reports unified health across both layers
6. Skills (`review-delta`, `review-pr`, `build-graph`) work with native Go graph instead of Python CRG
7. Postgres backend passes the same test suite as SQLite
8. Hot/cold lifecycle: notes transition from filesystem to DB correctly

## Open Questions

- Should the Go MCP server be a separate binary or embedded in `dot-agents`?
- Tree-sitter Go bindings: use `smacker/go-tree-sitter` or `tree-sitter/go-tree-sitter`?
- Should `dot-agents kg serve` support HTTP/SSE transport for remote agents?
- How should multi-repo graph registration work with Postgres? (single DB, schema-per-repo, or DB-per-repo?)
