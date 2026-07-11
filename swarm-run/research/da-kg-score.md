# `da kg` (knowledge graph) & `da score` (outcome scoring) — exhaustive reference

Repo: `~/proj-docs/dot-agents` · module `github.com/AGOrcha/dot-agents` · installed binary `da` **0.4.2** · repo `VERSION` `0.4.2` (but HEAD source has drifted ahead of the shipped 0.4.2 binary — see **Divergences** below).

Scope: everything `da kg` and `da score` can do, grounded in `da <cmd> --help`, `commands/kg/**`, `commands/score.go`, `internal/graphstore/**`, `internal/scoring/**`, `internal/adapters/**`, `internal/links/**`, and live read-only runs against this repo. Cited as `path:line` / exact identifier.

---

## 0. TL;DR mental model (two graphs, two databases, three layers)

`da kg` spans **three storage layers** and **two distinct SQLite databases**:

| Layer | What | Where | Owner / built by |
|---|---|---|---|
| **Hot notes** | Markdown knowledge notes + frontmatter, `index.md`, `log.md`, `raw/inbox` | `$KG_HOME/notes/<type>/*.md` (default `~/knowledge-graph`) | `kg setup/ingest`, hand-authored |
| **Warm store** | Go SQLite graphstore: notes mirror + note→symbol links + optional CRG code mirror | `$KG_HOME/ops/graphstore.db` | `kg warm`, `kg link*` |
| **Code graph (CRG)** | Tree-sitter symbol/edge graph of the repo | `<repo>/.code-review-graph/graph.db` (188 MB here) | `kg build`/`kg update` → **external Python `code-review-graph`** |

- **`$KG_HOME`** default = `~/knowledge-graph` (`commands/kg/kg.go:46-56` `kgHome()`; override `KG_HOME`). Config at `$KG_HOME/self/config.yaml` (`kg.go:58-60`).
- **CRG graph.db** is repo-local at `<repo>/.code-review-graph/graph.db` (`internal/graphstore/crg.go:899-901` `CRGDBPath`). Confirmed live: `~/proj-docs/dot-agents/.code-review-graph/graph.db` is 188.6 MB.
- The two DBs are separate: `kg code-status` reads the CRG graph.db (14573 nodes here); `kg warm stats` reads the warm store (`~/knowledge-graph/ops/graphstore.db`, 15443 code nodes here after a `warm --include-code`). They diverge because they are synced at different times.

Global flags on every `da` command (`commands/root.go`): `-n/--dry-run`, `-f/--force`, `--json`, `-v/--verbose`, `-y/--yes`. **`--json` is honored only by some kg subcommands** — see the gotcha table in §9.

---

## 1. `da kg` command tree

Registered in `commands/kg/cmd.go:16-336` (`NewKGCmd`). Full tree (installed 0.4.2 unless flagged):

```
da kg
├── setup                 Initialize KG at $KG_HOME
├── health                KG note/queue health (JSON ✓)
├── serve                 MCP server (stdio, JSON-RPC 2.0)
├── ingest [file]         Ingest a raw source → notes   [--all --title --type --dry-run]
├── queue                 List pending inbox sources
├── query [q]             Query notes by intent          [--intent(req) --limit --scope]
├── lint                  Graph integrity/quality checks [--check]
├── maintain
│   ├── reweave           Repair broken links + add source_ref links
│   ├── mark-stale        Mark old notes stale           [--days 90]
│   └── compact           Archive superseded/archived notes
├── bridge
│   ├── query [q]         Bridge-intent query            [--intent(req)]
│   ├── health            Adapter availability (JSON ✗ — prints text)
│   └── mapping           Bridge→KG intent map (JSON ✗ — prints text)
├── sync                  git pull + lint                [--push]
├── warm                  Sync hot notes → warm SQLite   [--type --include-code]
│   └── stats             Warm layer stats (JSON ✗ — prints text)
├── link                  note→code symbol cross-refs
│   ├── add <note> <qn>   Create a link                  [--kind mentions]
│   ├── list <note>       List links for a note
│   ├── remove <link-id>  Delete a link by id
│   └── import <manifest> Bulk-apply links  ← HEAD-ONLY, not in 0.4.2 (see §8)
├── build                 Full CRG code-graph build       [--repo --skip-flows --skip-postprocess]
├── update                Incremental CRG update          [--repo --base --skip-flows --skip-postprocess]
├── code-status           CRG stats (nodes/edges/langs)   [--repo]  (JSON ✓)
├── changes               Change impact in current diff   [--repo --base --brief --require-graph] (JSON ✓)
├── impact [file...]      Blast radius                    [--repo --base --depth 2 --limit 50 --require-graph] (JSON ✓)
├── flows                 Highly-connected fns            [--repo --limit 20 --sort criticality] (JSON ✓)
├── communities           Code communities                [--repo --min-size --sort size] (JSON ✓)
├── postprocess           Rebuild flows/communities/FTS   [--repo --no-flows --no-communities --no-fts]
└── lockfile
    ├── show              Per-adapter lockfile/view state [--adapter]
    └── reconcile         Fail-closed view reconciliation
```

---

## 2. The graph data model

### 2.1 Hot-note model — `GraphNote` (`commands/kg/kg.go:107-134`)

YAML frontmatter of a knowledge page (`$KG_HOME/notes/<type>/<id>.md`):

```go
type GraphNote struct {
  SchemaVersion int
  ID            string
  Type          string   // source|entity|concept|synthesis|decision|repo|session
  Title         string
  Summary       string
  Status        string   // draft|active|stale|superseded|archived
  SourceRefs    []string
  Links         []string // note→note links (used by related_notes intent)
  CreatedAt     string
  UpdatedAt     string
  Confidence    string   // low|medium|high
  Version       int      // reserved for LWW sync
}
```
- Valid note types: `source entity concept synthesis decision repo session` (`kg.go:123-126`).
- Valid statuses: `draft active stale superseded archived` (`kg.go:128-130`).
- Subdir per type via `noteSubdir` (`kg.go:373-388`); index in `index.md`, append-only `log.md` (`kg.go:136-139`).

### 2.2 Code-graph model — nodes & edges (`internal/graphstore/store.go`)

`graphstore` is a **Go port of the Python code-review-graph storage layer, extended with KG note tables** (`store.go:1-4`).

**Node kinds** (`store.go:7-13`): `File`, `Class`, `Function`, `Type`, `Test`.
**Edge kinds** (`store.go:16-24`): `CALLS`, `IMPORTS_FROM`, `INHERITS`, `IMPLEMENTS`, `CONTAINS`, `TESTED_BY`, `DEPENDS_ON`.

Key structs:
- `NodeInfo` (`store.go:27-40`) — parser-output insert shape (Kind, Name, FilePath, LineStart/End, Language, ParentName, Params, ReturnType, Modifiers, IsTest, Extra).
- `EdgeInfo` (`store.go:43-50`) — Kind, Source (qualified name), Target (qualified name), FilePath, Line, Extra.
- `GraphNode` (`store.go:53-69`) — stored form + `ID int64`, `QualifiedName`, `FileHash`, `UpdatedAt float64`.
- `GraphEdge` (`store.go:72-81`) — stored edge (`SourceQualified`, `TargetQualified`).
- `GraphStats` (`store.go:84-94`) — TotalNodes/Edges, NodesByKind, EdgesByKind, Languages, FilesCount, LastUpdated, **NotesCount**, **LinksCount**.
- `ImpactResult` (`store.go:97-102`) — ChangedNodes, ImpactedNodes, ImpactedFiles, Edges.

### 2.3 Note record & symbol-link model

- `KGNote` (`store.go:104-115`) — warm-store note record: ID, Title, NoteType, Status, Summary, FilePath, Version, ArchivedAt, IndexedAt.
- **`NoteSymbolLink` (`store.go:118-124`)** — the note↔code bridge: `ID int64`, `NoteID string`, `QualifiedName string` (the code symbol), `LinkKind string` (`mentions|implements|documents|decides|references`), `CreatedAt float64`.

### 2.4 Store contract (role-segregated interfaces, `store.go:166-237`)

Interface-segregation split so callers depend on the narrowest role:
- `CodeGraphReader` (`store.go:170-181`): GetNode, GetNodesByFile, GetEdgesBySource/ByTarget/Among, GetAllFiles, `SearchNodes(query, limit)`, GetMetadata, GetStats, **`GetImpactRadius(changedFiles, maxDepth, maxNodes)`**.
- `CodeGraphWriter` (`store.go:187-194`): UpsertNode, UpsertEdge, RemoveFileData, StoreFileNodesEdges, SetMetadata, Commit.
- `KGNoteStore` (`store.go:199-204`): UpsertKGNote, GetKGNote, SearchKGNotes, ListArchivedKGNotes.
- `NoteSymbolLinkStore` (`store.go:209-214`): UpsertNoteSymbolLink, GetLinksForNote, GetLinksForSymbol, DeleteNoteSymbolLink.
- `Closer` + composed `Store` (`store.go:220-237`). Concrete backends: `*SQLiteStore`, `*PostgresStore` (compile-time asserted `store.go:239-261`).
- Provider guarantees (`store.go:135-157`): uniform hard bounds, provider-owned request timeout, single-goroutine handle, explicit cheap lifecycle.

### 2.5 Warm SQLite schema (`internal/graphstore/migrations.go:15-89`)

**Idempotent, version-less DDL** — every statement is `CREATE ... IF NOT EXISTS`, order-independent, safe to re-run; **no migration/ALTER mechanism** (`migrations.go:6-14`). **Exactly 5 tables** (no FTS/flows/communities tables in the Go warm store):

1. `nodes` — id (autoinc PK), kind, name, `qualified_name UNIQUE`, file_path, line_start/end, language, parent_name, params, return_type, modifiers, is_test, file_hash, extra (JSON default `{}`), updated_at.
2. `edges` — id, kind, source_qualified, target_qualified, file_path, line, extra, updated_at.
3. `metadata` — key PK / value (e.g. `last_warm_sync`, `last_warm_code_import`).
4. `kg_notes` — id (TEXT PK), title, note_type, status, summary, file_path, version, archived_at, indexed_at.
5. `note_symbol_links` — id, note_id, qualified_name, link_kind (default `mentions`), created_at, `UNIQUE(note_id, qualified_name, link_kind)`.

Indexes on nodes(file/kind/qualified), edges(source/target/kind/file), kg_notes(type/status/archived), note_symbol_links(note_id/qualified) (`migrations.go:74-88`).

> **Flows / communities / FTS are NOT Go warm-store tables.** They are CRG (Python) artifacts inside `.code-review-graph/graph.db`, produced by `code-review-graph postprocess` and read back via the CRG bridge. Materialized views for them are a *spec target* (adapter-contract) tracked as **lockfile state**, not implemented as Go SQL. (Scout flagged `postgres.go:47` promises tsvector/GIN FTS but `pgSchemaSQL` has none and `SearchNodes` uses ILIKE — a doc/code drift on the Postgres backend.)

### 2.6 Backends & bounds

- **SQLite** (default, `graphstore.OpenSQLite`, opened via `openKGStore`, `commands/kg/sync_code_warm_link.go:594-605`) and **Postgres** (`postgres.go`) — dual backend behind the same `Store` contract; `NewLazyStore`/`lazy.go` for lazy init; `seams.go` test seams.
- **Bounds are one chokepoint** (`internal/graphstore/bounds.go`), applied identically on native + CRG paths:
  - `hardMaxNodes=5000`, `hardMaxDepth=12`, `hardSearchLimit=2000` (hard caps).
  - `defaultMaxNodes=1000`, `defaultMaxDepth=4`, `defaultSearchLimit=100` (used when caller passes 0/negative).
  - `requestTimeout = 30s` (`bounds.go:53`) — provider-owned; wraps in-process BFS **and** the CRG Python subprocess (`exec.CommandContext`).
  - `clampBound(requested, def, hard)` (`bounds.go:60-68`): ≤0→def; >hard→hard.
- **Native impact BFS** (`internal/graphstore/impact.go`): bidirectional (fwd+rev adjacency) over **all** edge kinds, hop-by-hop to maxDepth, **hard node cap** trims to exactly maxNodes (`bfsImpacted` 52-82, `capImpactedSet` lexicographic 84-101). Seeds excluded from result. No criticality scoring on the native path (criticality is CRG-side).

### 2.7 Adapter contract & lockfile (`internal/graphstore/CONTRACT.md` §10.1)

- The graph-backend-adapter-contract §10.1 governs **lockfile / materialized-view reconciliation**: per-adapter `view_status` (a 4-value enum), atomic write, and a **fail-closed `Reconcile`** (nil/absent presence ⇒ treated as absent). State machine lives in `internal/kg/lockfile/lockfile.go`; bridge-consumer drift detection in `internal/graphstore/parity_drift.go`. Surfaced by `da kg lockfile show|reconcile`.
- **Live state now: empty.** `da kg lockfile show --json` → `No adapters activated.` The lockfile/native-adapter framework is forward-looking; in 0.4.2 the CRG path is still the legacy Python bridge, so no adapters are activated.

---

## 3. `da kg build` / `update` / `postprocess` — the CRG code-graph pipeline

All three shell out to the **external Python `code-review-graph` CLI** via `graphstore.CRGBridge`.

### 3.1 CRGBridge (`internal/graphstore/crg.go`)

> "CRGBridge delegates code-graph build, update, and query operations to the Python code-review-graph CLI... it shells out to the CRG executable and marshals its output back to Go" (`crg.go:1-8`).

- Binary name: `code-review-graph` (`crg.go:31` `crgBinName`).
- **Binary discovery `DiscoverCRGBin(repoRoot)` (`crg.go:65-82`)**, in order:
  1. `<repoRoot>/.venv/{bin,Scripts}/code-review-graph[.exe]`
  2. `<parent-of-repoRoot>/.venv/{bin,Scripts}/code-review-graph[.exe]`
  3. `code-review-graph` on `PATH` (`exec.LookPath`, PATHEXT on Windows).
  - Not found → error: `code-review-graph not found in .venv or PATH; install with: uv pip install code-review-graph`.
  - venv subdir order `bin` then `Scripts` (`crg_venv_unix.go:11-13`); python names `python3` then `python`, fallback bare `python3` (`crg_venv_unix.go:28-37`).
- Two invocation styles (**this split is the source of the ENOENT gotcha in §10**):
  - **`run`/`runStreamed`/`runCaptured`** exec the `code-review-graph` binary directly (`crg.go:95-109`, 165-173, 424-443) — used by build/update/postprocess/detect-changes/status.
  - **`runPyQuery(pyExpr)`** (`crg.go:130-163`) invokes the venv **python interpreter directly** (`pythonBin()` `crg.go:113-125`), runs a `-c` script that imports `code_review_graph.tools.*`, prints one JSON doc. Used by impact/flows/communities. Applies `requestContext` (30s) via `exec.CommandContext`.

### 3.2 `kg build` (`sync_code_warm_link.go:131-183`)

- Repo root: `--repo` or `crgRepoRoot()` (nearest `.git` ancestor, `sync_code_warm_link.go:111-129`).
- `bridge.BuildReport(BuildOptions{SkipFlows, SkipPostprocess})` → `code-review-graph build --repo <root> [--skip-flows] [--skip-postprocess]` (`crg.go:196-207`).
- After build, reads `Status()` and classifies `Outcome` into `ready|unbuilt|busy_or_locked|error` (`crg.go:213-238`).
- Journals a KG decision event (`journal.CmdKGBuild`) recording **counts only, never node/edge bodies (D4)** (`sync_code_warm_link.go:139-196`).
- `--skip-flows` = code signatures only, skip flow/community detection; `--skip-postprocess` = raw parse only.

### 3.3 `kg update` (`sync_code_warm_link.go:198-261`)

- **Graceful no-op if CRG absent**: `DiscoverCRGBin` failure → prints `code-review-graph not installed; skipping code graph update` and **exits 0** (`sync_code_warm_link.go:203-212`) — because the graph-update post-tool hook runs on every edit and must not fail sessions.
- `bridge.UpdateReport(UpdateOptions{Base, SkipFlows, SkipPostprocess})` → `code-review-graph update` on the diff since `--base` (default `HEAD~1`). Outcomes: `no_diff|no_mutation|updated` (`crg.go:258-318`).

### 3.4 `kg postprocess` (`sync_code_warm_link.go:494-531`)

- `bridge.Postprocess(PostprocessOptions{NoFlows, NoCommunities, NoFTS})` → `code-review-graph postprocess --repo <root> [--no-flows] [--no-communities] [--no-fts]` (`crg.go:832-844`, `runStreamed`).
- **Rebuilds derived data: execution flows, code communities, FTS index** — the three things missing from the Go warm schema. Runs automatically inside build/update; run manually to repair stale derived data.

### 3.5 `kg code-status` (`sync_code_warm_link.go:263-287`)

- `(&CRGBridge{RepoRoot}).Status()` (`crg.go:344-400`) reads `.code-review-graph/graph.db` **directly via Go sqlite** (read-only pragma `?_pragma=query_only(true)`, `crg.go:30,355`) — works even when the CRG binary/venv is broken. `CRGStatus` (`crg.go:330-339`): Nodes, Edges, Files, Languages, LastUpdated, State, Ready, Message. Readiness: nodes>0 ∧ files>0 ∧ last_updated≠never ⇒ `ready`+`Ready=true`.

Live ground truth (`da kg code-status --json`):
```json
{ "nodes":14573, "edges":178196, "files":885,
  "languages":"go, javascript, python, ruby, typescript",
  "last_updated":"2026-07-09T22:58:22Z", "state":"ready", "ready":true }
```

Readiness enum (`crg.go:417-422`): `CRGReadinessUnbuilt="unbuilt"`, `Ready="ready"`, `BusyOrLocked="busy_or_locked"`, `Error="error"`. `checkCRGReadiness(root, requireGraph)` (`sync_code_warm_link.go:308-334`) is the shared gate used by `impact`/`changes`; with `--require-graph` it returns non-zero on unbuilt/busy/unknown.

---

## 4. `da kg impact` / `changes` / `flows` / `communities` — code-graph queries

### 4.1 `kg impact [file...]` — blast radius (`sync_code_warm_link.go:348-390`)

- Flags: `--repo`, `--base HEAD~1`, `--depth 2`, `--limit 50`, `--require-graph`. Files = positional args; empty ⇒ current git diff vs `--base`.
- `checkCRGReadiness` gate → `bridge.GetImpactRadius(ImpactOptions{ChangedFiles, MaxDepth, MaxResults, Base})` (`crg.go:687-734`).
- Path: `runPyQuery` calling `code_review_graph.tools.query.get_impact_radius(changed_files, max_depth, max_results, repo_root, base)`. Bounds normalized through the SAME `normalizeTraversalBounds` caps as native (`crg.go:694`).
- `CRGImpactResult` (`crg.go:663-672`): status, summary, changed_files, `changed_nodes[]`, `impacted_nodes[]`, impacted_files[], **truncated**, total_impacted. `ImpactNode` (`crg.go:675-685`): id, kind, name, qualified_name, file_path, line_start/end, language, is_test.
- JSON wrapper adds `graph_state` (`kgImpactJSONOutput`, `sync_code_warm_link.go:337-340`).

**Live ground truth** (`da kg impact commands/kg/cmd.go --json`, verified working here): returns `graph_state:"ready"`, 2 changed nodes (File + `NewKGCmd`), 50 impacted (truncated from 52 at depth 2), qualified names like `.../commands/kg/cmd.go::NewKGCmd`. **This is the canonical swarm blast-radius readback.**

### 4.2 `kg changes` — change-impact report (`sync_code_warm_link.go:533-589`)

- Flags: `--repo`, `--base HEAD~1`, `--brief`, `--require-graph`. `bridge.DetectChanges(DetectChangesOptions{Base, Brief})` → `code-review-graph detect-changes --repo <root> [--base] [--brief]` (`crg.go:851-875`). `--brief` ⇒ CRG emits plain text (only `Summary` populated).
- `CRGChangeReport` (`crg.go:599-635`): summary, risk_score, `changed_functions[]` (CRGChangedNode: name, qualified_name, file_path, risk_score, callers), affected_flows[], **test_gaps[]** (changed symbol lacking tests), **review_priorities[]** (qualified_name, reason, risk_score).
- ⚠ `DetectChangesOptions.Files` is **reserved / non-functional** — the CRG v1.x `detect-changes` has no `--files` arg (`crg.go:641-647`).

### 4.3 `kg flows` (`sync_code_warm_link.go:428-461`)

- `bridge.ListFlows(limit, sortBy)` → `code_review_graph.tools.flows_tools.list_flows(repo_root, sort_by, limit)` via `runPyQuery` (`crg.go:756-778`). Defaults limit 20, sort `criticality` (also `size`).
- `FlowInfo` (`crg.go:746-753`): id, name, entry_point, step_count, criticality, kind.
- **Documented caveat (`cmd.go:281-286`)**: flow *step chains and entry points are not populated* by the engine — results are highly-connected functions sorted by criticality, **not** full execution paths. Use `kg impact` for blast radius.

### 4.4 `kg communities` (`sync_code_warm_link.go:463-492`)

- `bridge.ListCommunities(minSize, sortBy)` → `code_review_graph.tools.community_tools.list_communities_func(repo_root, sort_by, min_size)` (`crg.go:801-820`). Defaults sort `size` (also `cohesion`); `--min-size`.
- `CommunityInfo` (`crg.go:790-798`): id, name, size, cohesion, dominant_language, description, members.
- **Caveat (`cmd.go:300-304`)**: `members` is always empty; dependency dirs (node_modules) dominate size sort.

---

## 5. Note queries — `da kg query` (intents) & `da kg bridge`

### 5.1 `kg query --intent <I> [q]` (`commands/kg/query_lint_maintain.go`)

Reads the **hot filesystem note layer** (provider `local-index`). Valid intents (`query_lint_maintain.go:63-73`): `source_lookup entity_context concept_context decision_lookup repo_context synthesis_lookup related_notes contradictions graph_health`.

Dispatch (`executeQuery` `query_lint_maintain.go:247-308`):
- The six `*_lookup`/`*_context` intents map 1:1 to a note **type** (`intentToNoteType` `query_lint_maintain.go:266-273`: source_lookup→source, entity_context→entity, concept_context→concept, decision_lookup→decision, repo_context→repo, synthesis_lookup→synthesis) → `searchNotes`.
- `related_notes` → `searchByLinks(q=noteID)` walks the note's `Links[]` frontmatter (`query_lint_maintain.go:204-242`).
- `contradictions` → `findContradictions`. `graph_health` → reads `graph-health.json`.
- Flags: `--intent` (required), `--limit 10`, `--scope`. Requires `kg setup` first (`runKGQuery` `query_lint_maintain.go:342-390`).

### 5.2 `kg bridge` — code + note fan-out (`commands/kg/bridge.go`)

`bridge query --intent <I> [q]` fans one bridge intent to KG/warm/CRG backends. **14 bridge intents** (`defaultBridgeMappings` `bridge.go:26-43`):

| Bridge intent | Routes to | Backend |
|---|---|---|
| `plan_context` | decision_lookup + synthesis_lookup | note (local-index) |
| `decision_lookup` | decision_lookup | note |
| `entity_context` | entity_context | note |
| `workflow_memory` | related_notes + source_lookup | note |
| `contradictions` | contradictions | note |
| `symbol_lookup` | warm graphstore SearchNodes | warm-graphstore |
| `impact_radius` | warm graphstore | warm-graphstore |
| `change_analysis` | CRG `detect-changes` | CRG bridge |
| `tests_for` | warm graphstore (TESTED_BY) | warm-graphstore |
| `callers_of` | warm graphstore (CALLS, reverse) | warm-graphstore |
| `callees_of` | warm graphstore (CALLS, forward) | warm-graphstore |
| `community_context` | CRG `list_communities` | CRG bridge |
| `symbol_decisions` | warm graphstore | warm-graphstore |
| `decision_symbols` | warm graphstore | warm-graphstore |

- **Code bridge intents** (`codeBridgeIntents` `bridge.go:55-65`): symbol_lookup, impact_radius, change_analysis, tests_for, callers_of, callees_of, community_context, symbol_decisions, decision_symbols. Note intents use `local-index`; code intents use `warm-graphstore` (`$KG_HOME/ops/graphstore.db`) — **except** `change_analysis` (→ CRG `DetectChanges`, `bridge.go:590-591`) and `community_context` (→ CRG `ListCommunities`, `bridge.go:593-594`).
- Warm-store dispatch (`dispatchWarmStoreBridgeIntent` `bridge.go:621-645`): callers_of = `runNeighbors(...EdgeKindCalls, reverse=true)`, callees_of = forward; tests_for uses TESTED_BY; symbol_lookup = `findCodeNodes` (`bridge.go:648-655`).
- ⚠ warm-store code intents require `kg warm --include-code` to have mirrored the CRG graph into `$KG_HOME/ops/graphstore.db`; if not run, results are empty. `annotateBridgeSparsity` warns.
- `bridge health` (`bridge.go:...`) — live: `Adapter: local-file [available]  Notes: 1707`. `bridge mapping` prints the table above. Contract written to `$KG_HOME/self/schema/bridge-contract.yaml` (`writeBridgeContract` `bridge.go:899-928`).

---

## 6. `da kg link` — note→code symbol cross-references

The **note↔symbol bridge** in the warm store (`note_symbol_links` table). Link kinds (`sync_code_warm_link.go:851-857`): `mentions, implements, documents, decides, references` (default `mentions`).

- **`kg link add <note-id> <qualified-name> [--kind]`** (`runKGLinkAdd` `sync_code_warm_link.go:862-911`): validates kind, `--dry-run` previews, else `store.UpsertNoteSymbolLink(NoteSymbolLink{NoteID, QualifiedName, LinkKind})` (idempotent via UNIQUE constraint). Journals a content-delta event with counts + ids (D4). Prints `Link created (id=N): <note> -[kind]-> <qn>`.
- **`kg link list <note-id>`** (`runKGLinkList` `sync_code_warm_link.go:1021+`): `GetLinksForNote`.
- **`kg link remove <link-id>`** (`runKGLinkRemove` `sync_code_warm_link.go:1047+`): `DeleteNoteSymbolLink`.
- **`kg link import <manifest>`** ← **HEAD-ONLY, NOT in 0.4.2** (`runKGLinkImport` `sync_code_warm_link.go:957-990`, registered `cmd.go:214-221`). Manifest = one `<note-id> <qualified-name> [kind]` per line; `#` comments and blanks skipped; 2–3 fields; invalid rows collected & reported while valid rows still applied (idempotent); non-zero exit if any row failed; `--dry-run` validates+previews. `parseLinkManifest` `sync_code_warm_link.go:924-948`; `applyLinkRows` `sync_code_warm_link.go:1002-1019`. **The agent authoring the manifest makes the documents/implements/references judgement; the command is mechanical batch execution.**

Live: `kg warm stats` shows `Symbol links: 0` — no links created in this repo yet.

---

## 7. Lifecycle: setup / ingest / warm / lint / maintain / sync / lockfile / serve

- **`kg setup`** (`runKGSetup` `kg.go:515-625`): scaffolds `$KG_HOME` (`self/config.yaml`, `notes/<type>/`, `raw/inbox`, `ops/`, bridge contract).
- **`kg health`** (JSON ✓): counts notes/sources/orphans/broken-links/stale/contradictions/queue → status `healthy|warn`. Live: `{note_count:1707, source_count:245, orphan_count:0, ..., status:"healthy"}`.
- **`kg ingest [file]`** (`runKGIngest` `kg.go:1223-1267`): raw source → source/entity/decision draft notes. Flags `--all` (drain inbox), `--title`, `--type` (markdown|text|pdf|url|transcript|meeting_notes|repo_doc), `--dry-run`. **`kg queue`** lists `raw/inbox/`.
- **`kg warm [--type] [--include-code]`** (`runKGWarm` `sync_code_warm_link.go:685-745`): upserts hot notes → warm SQLite (`warmActiveNotes`+`warmArchivedNotes`), sets `last_warm_sync`. `--type` filters (`source|entity|concept|synthesis|decision|repo|session`). **`--include-code`** additionally mirrors the CRG graph into the warm store via `runKGWarmCodeImport` (`sync_code_warm_link.go:632-677`): `bridge.ReadNodes(0)`/`ReadEdges(0)` (bulk export, **no limit clamp** — `crg.go:906-917`) → `store.UpsertNode/UpsertEdge`. **`kg warm stats`** (JSON ✗) — live: notes 1707, symbol links 0, code nodes 15443, code edges 130558, DB `~/knowledge-graph/ops/graphstore.db`.
- **`kg lint [--check]`** (`runKGLint`): checks `broken_links|orphan_pages|missing_source_refs|stale_pages|index_drift|oversize_pages|contradictions`.
- **`kg maintain`**: `reweave` (repair broken links, add source_ref links), `mark-stale [--days 90]`, `compact` (archive superseded/archived).
- **`kg sync [--push]`** (`runKGSync` `sync_code_warm_link.go:38-107`): thin wrapper = `git pull` (or `--push`) then `kg lint`. No custom protocol — git is the transport.
- **`kg lockfile show [--adapter] | reconcile`** (`commands/kg/lockfile.go`): per-adapter view state + fail-closed reconciliation (§2.7). Live: `No adapters activated.`
- **`kg serve`** (`runKGServe` `kg.go:709-716`): MCP server, stdio JSON-RPC 2.0 (impl `internal/graphstore/mcp_server.go`).

---

## 8. Adapter framework (native CRG migration in progress)

`internal/adapters/` holds a **newer, Go-native adapter model** that is migrating off the Python subprocess bridge (spec §11). It is **not the active path in 0.4.2** but is where the code is heading — relevant for a durable swarm design.

- **`internal/adapters/builtin/crg/schema.yaml`** (kg-native CRG adapter, v1.0.0): "Bootstrap performs Tree-sitter ingestion of a repo's symbols into the **`kg_crg.*`** namespace... Replaces the legacy Python subprocess bridge (§11). Dual-read mode (§11.3)." note_type `symbol` (qualified_name, kind ∈ {Function,Type}, language, file_path, line_start, content_hash); edge_types `CALLS/TESTED_BY/IMPORTS`; `staleness_drivers: [source_mutation]`; `impact_radius` Cypher `MATCH (s:symbol)-[:CALLS|TESTED_BY|IMPORTS*1..max_depth]->(n:symbol) ... max_depth:3`.
- **`crg-bridge/schema.yaml`** (`migration_only: true`, v0.1.0): read-only mirror of legacy bridge state under `kg_crg-bridge.*` for **parity oracles** comparing bridge vs kg-native; `max_depth:0`. Long-term adapters may not declare `reads_from` against it (loader rejects).
- **`internal/adapters/sdk/sdk.go`** — the `da-adapter-sdk` (contract §8.4): bootstrap skills must **not open direct DB connections** (§8.2); every op carries a namespace token validated at the storage layer. Surface: `Note`/`Edge`, `Token`/`Grant`/`Mode`, `OwnReadToken`/`BootstrapToken`/`ViewToken`, `WriteNotes`/`WriteEdges`/`Query`/`MaterializeView`/`DeclarePredicateFired`. `MaterializeView` enforces the §11.2 `migration_only` rule via a `ReadsFromValidator` before running.
- `internal/graphstore/parity.go`/`parity_drift.go` = parity comparison between the two CRG paths.

---

## 9. `da score` — outcome scoring

Scores every **iteration** of an agent run against the versioned outcome-scoring rubric (`docs/OUTCOME_SCORING_RUBRIC.md`). CLI in `commands/score.go`; engine in `internal/scoring/**`.

### 9.1 What "R1 outcome scoring" is

R1 = the requirement from the proposal `agent-run-scoring-observability-platform.md`: an **explainable** quality score per agent-run iteration/session, computed **from already-captured telemetry** (`OUTCOME_SCORING_RUBRIC.md:9-26`). Later requirements bolted on signals: **R1.5** added `hook_outcomes` (rubric 2.1.0); **R5** added `human_label` (rubric 3.0.0).

### 9.2 The rubric (`internal/scoring/rubric.go`)

- `RubricVersion` const (`rubric.go:23`). **Repo HEAD = `3.0.0`; shipped 0.4.2 binary = `2.1.0`** (see Divergences §11).
- Combination = **`weighted_mean_renormalized`** (`rubric.go:77-84`): `score = Σ(wᵢ·subᵢ)/Σ(wᵢ)` over **present** signals; absent signals drop from both sums (they neither inflate nor deflate); all-absent ⇒ **unscored** (`Scored=false`, Value 0, Band `unscored`).
- Bands (`rubric.go:197-202`): excellent ≥0.85, good ≥0.70, fair ≥0.50, poor ≥0.0.
- Signals (`rubric.go` SignalID `32-56`, `DefaultRubric` `135-204`). **Two weight sets by version:**

| Signal | Two-way? | HEAD 3.0.0 weight | Shipped 0.4.2 (2.1.0) weight |
|---|---|---|---|
| `landed` (survived to master) | ✓ | 0.17 | **0.20** |
| `verifier` (gates passed) | ✓ | 0.15 | **0.18** |
| `tests` (tests passed) | ✓ | 0.14 | **0.17** |
| `human_label` (reviewer judgement) | ✗ | 0.15 | **absent** |
| `correction_pressure` (retries/corrections/errors) | ✗ | 0.11 | **0.13** |
| `scope` (write-scope adherence) | ✓ | 0.11 | **0.13** |
| `hook_outcomes` (hook-gate results) | ✗ | 0.09 | **0.10** |
| `token_efficiency` (model/cache) | ✗ | 0.08 | **0.09** |

(HEAD weights verified `rubric.go:140-195`; 2.1.0 weights verified live from `da score run --no-write --json` NominalWeight on this repo.) `Validate()` enforces weights sum to 1.0 (`rubric.go:244-277`). `TwoWaySignals()` (`rubric.go:217-227`) = the integrity-track signals.

### 9.3 SignalSet & extractors (`internal/scoring/signals.go`)

`SignalSet` (`signals.go:12-35`) = the 8 typed inputs + `Integrity[]` (claimed-vs-observed, **never** affects the number) + `Objective` (process-discipline facts). `AssembleSignalSet` (`signals.go:170-184`) wires:
- `Landed` ← `GitSignals.LandedObserved` (git topology, `signal_git.go`).
- `Verifier` ← `IterlogSignals.Verifier` (iter-log, `signal_iterlog.go`).
- `Tests` ← `IterlogSignals.TestsClaimed` — ⚠ **self-reported**, not the objective artifact (scout flag, `signals.go:175`).
- `HumanLabel` ← `iter-N.labels.yaml` sidecar (`signal_human_label.go`).
- `CorrectionPressure` ← `1/(1+retries+userCorrections+2·errorRate)` (`signals.go:90-104`); always present (clean run = 1.0).
- `Scope` ← `coalesce(git ScopeObserved, iter-log ScopeClaimed)` (objective first).
- `HookOutcomes` ← `iter-N.hook-outcomes.yaml` (§9.5).
- `TokenEfficiency` ← `BackfillSignals` (token/cache backfill from transcripts, `signal_backfill.go`).
- `BuildSignalSets(iterLogDir, repoDir, transcriptDirs...)` (`signals.go:193-226`) loads the iter-log, runs every extractor per iteration in order.

### 9.4 Iteration-log record & origin (`internal/scoring/iterlog.go`)

- `IterationRecord` (`iterlog.go:71-90`): SchemaVersion, Iteration, Date, Wave, TaskID, Commit, FilesChanged, Lines+/-, FirstCommit, CheckpointAt, Agent (SessionID/Harness/Model), SessionTokens, Impl block, Verifiers[], Review block.
- **v1 (flat) vs v2 (role-owned impl/verifiers/review blocks)** normalize into one shape (`iterlog.go:63-90`); `OptionalBool` tri-state for test flags (`iterlog.go:22-61`).
- Records come from **`.agents/active/iteration-log/iter-N.yaml`**, written by `workflow checkpoint --log-to-iter N` (the canonical per-iteration record). Loader `LoadIterationLog` matches strictly `^iter-\d+\.yaml$` so score sidecars are not re-parsed (`iterlog.go:15-20`).

### 9.5 Hook-outcome scoring (`internal/scoring/signal_hook_outcomes.go`)

Reads `.agents/active/iteration-log/iter-N.hook-outcomes.yaml` (written by **`da workflow hook-outcome write`**, `commands/workflow/hook_outcome.go`). Full record `HookOutcomeRecord` (`hook_outcome.go:127-139`): schema_version, **sentinel_id**, skill, **lifecycle_point**, **intervention_class**, **result**, **rule_id**, platform, ts, archived_sentinel_path, **correlation_id**.

Scoring rules (`signal_hook_outcomes.go`):
- **Only `intervention_class ∈ {prevent_before_action, remediate_at_stop}` vote** (`filterScoredHookOutcomes` 138-147); `continuity_advice`/`observe_tool_result` are audit-only (deferred to R1.5.1). Any unknown class dropped.
- **D4 dedup**: a `prevent_before_action` + `remediate_at_stop` sharing `(correlation_id, rule_id)` collapse to the more severe (`dedupHookOutcomesByCorrelation` 149-178); empty correlation_id never collapses.
- **Three-band sub-score** (`foldHookOutcomeSubScore` 201-228): any `remediate` ⇒ **0.0**; else any `advise` ⇒ **0.6**; else all `allow` ⇒ **1.0**; none in-scope / missing / malformed ⇒ **absent** (does not vote). Severity order `remediate>advise>allow` (`hookResultSeverity`).

### 9.6 Persisted sidecars (`internal/scoring/persist.go`)

- **`iter-N.score.yaml`** — `PersistedScore` (`persist.go:18-30`): iteration, rubric_version, scored, value, band, `breakdown[]` (`PersistedContribution` 33-42: signal, label, present, sub_score, detail, nominal_weight, effective_weight, contribution), `linked_traces_to_outcomes`. Written atomically (temp+rename) via `WriteIterationScore(WithRecord)`.
- **`session-<id>.score.yaml`** — `SessionScore` (`persist.go:51-59`): session_id, rubric_version, iterations[], scored, value (mean of scored iters), band, per_iteration[] (`SessionIterRef` 63-68). `AggregateSessions` groups by iter `session_id`; empty session_id skipped.
- **`iter-N.hook-outcomes.yaml`** — `HookOutcomeSidecar` (`hook_outcome.go:141-145`): schema_version + records[] (the HookOutcomeRecord above).
- Path helpers: `IterationScorePath`/`SessionScorePath` (`persist.go:72-84`).

### 9.7 `da score` subcommands (`commands/score.go`)

- **`score run`** (`runScoreRun` `score.go:171-218`): `LoadIterationLog` → `BuildSignalSets` → `rubric.ScoreAll` → `AggregateSessions` → **writes** all `iter-N.score.yaml` + `session-*.score.yaml` sidecars (unless `--no-write`). Flags: `--iter-log-dir` (default `.agents/active/iteration-log`, `score.go:55`), `--repo-dir` (default cwd), `--transcript-dir` (repeatable; `~/.claude/projects`, `~/.codex/sessions`), `--no-write`.
- **`score iteration <N>`** (`score.go:303-321`): renders the **persisted** `iter-N.score.yaml` (fast, no git/transcript scan). `--recompute` (`score.go:277-298`) scores fresh from canonical inputs + git topology + transcripts and rewrites the sidecar. Older sidecars stay valid until a RubricVersion bump.
- **`score session <id>`** (`score.go:472-495`): renders persisted `session-<id>.score.yaml`.

**Feed from the workflow loop:** `da workflow close-task` (`commands/workflow/close_task.go`) = `checkpoint --log-to-iter N → score iteration N → advance → focus → commit`. It calls `scoring.ScoreIteration(iterDir, repoDir, N)` and `WriteIterationScoreWithRecord` (`close_task.go:137-141`), emitting `score_value`/`score_band` (`close_task.go:43-44,164-165`). Default `--score-recompute=current` (only mode implemented; `recent-N`/`all` error, `close_task.go:184-185`). This is the automatic per-iteration scoring hook.

### 9.8 Score JSON casing quirk (gotcha)

`PersistedScore`/`SessionScore` carry **only `yaml:` tags, no `json:` tags** (`persist.go:18-68`). So `--json` output has **lowercase top-level** keys but **PascalCase nested** keys. Verified live from 0.4.2:
- `score run --json`: top-level `{rubric_version, iterations, sessions}`; nested iteration `{Iteration, RubricVersion, Scored, Value, Band, Breakdown, LinkedTracesToOutcomes}`; breakdown rows `{Signal, Label, Present, SubScore, Detail, NominalWeight, EffectiveWeight, Contribution}`; session `{SessionID, RubricVersion, Iterations, Scored, Value, Band, PerIteration}`.
- `score iteration N --json`: PascalCase at the **top** level too (the `PersistedScore` is inlined) plus optional `hook_outcome_sources[]`.
- **Do not assume snake_case in score JSON.** Parse the PascalCase nested keys.

---

## 10. CRG-coupling gotchas (critical for a swarm)

1. **`kg build`/`update`/`postprocess`/`changes`/`code-status` require the Python `code-review-graph` toolchain; `impact`/`flows`/`communities` require a *working* venv python.** Different failure surfaces per invocation style (§3.1).
2. **Stale-venv-shebang ENOENT (observed live in this repo).** `.venv/bin/code-review-graph` is a Python console-script whose shebang is `#!/Users/nikashp/Documents/dot-agents/.venv/bin/python3` — an absolute path to a **missing** interpreter (venv was created in `~/Documents/dot-agents`, repo is `~/proj-docs/dot-agents`). Result:
   - `da kg changes` → **fails** `fork/exec .../.venv/bin/code-review-graph: no such file or directory` (kernel reports the *script* path when the shebang interpreter is missing). Same for `build`/`update`/`postprocess` (they exec the binary directly via `run`/`runStreamed`).
   - `da kg impact`/`flows`/`communities` → **work**, because `runPyQuery` invokes `.venv/bin/python3` (a local symlink → `python`) directly and bypasses the console-script shebang.
   - `da kg code-status` → **works** (reads `graph.db` via Go sqlite, no python at all).
   - **Swarm lesson:** never assume "CRG present" is binary. Probe with `da kg code-status --json` (state must be `ready`) AND, if you need build/postprocess, verify the venv shebang interpreter exists. Prefer `impact`/`code-status` for read paths.
3. **`kg update` degrades to exit 0 when CRG is absent** (`sync_code_warm_link.go:203-212`) — a "success" that did nothing. Check the output message / re-run `code-status` to confirm freshness.
4. **`graph.db` is huge** (188 MB here) and repo-local; the CRG build re-parses all files (slow). Warm-store `--include-code` bulk-imports it with **no row cap** (`crg.go:906-917`) — can be large.
5. **Warm code intents need `kg warm --include-code` first.** `bridge query --intent callers_of/callees_of/tests_for/symbol_lookup` read `$KG_HOME/ops/graphstore.db`; without a code import they return empty (sparsity warning), while `change_analysis`/`community_context` go straight to CRG.
6. **Two clocks / two DBs drift.** `code-status` (CRG graph.db, 14573 nodes) vs `warm stats` (warm store, 15443 nodes) reflect different sync times. Treat `code-status.last_updated` as the code-graph freshness signal; `warm stats last_warm_sync` as the note/mirror freshness.
7. **`--json` is not universal.** `bridge health`, `bridge mapping`, `warm stats`, `lockfile show` print **text** even with `--json`. JSON-clean: `kg health`, `code-status`, `impact`, `changes`, `flows`, `communities`, `query`, `bridge query`, `score run/iteration/session`.
8. **`kg link import` is HEAD-only** — absent from the 0.4.2 binary and the checked-in `bin/da` dev build; committed `2026-07-11 10:38` (`7aeef499`). A swarm on 0.4.2 must loop `kg link add` instead.

---

## 11. Divergences: shipped 0.4.2 vs repo HEAD source

- **Rubric version.** `internal/scoring/rubric.go:23` HEAD = `3.0.0` (8 signals incl. `human_label` 0.15). Installed `da 0.4.2` scores under **`2.1.0`** (7 signals, no human_label; weights landed 0.20/verifier 0.18/tests 0.17/correction 0.13/scope 0.13/hook 0.10/token 0.09) — verified live. `docs/OUTCOME_SCORING_RUBRIC.md` HEAD = 3.0.0.
- **`kg link import`.** Source-registered (`cmd.go:214-221`) and committed to HEAD; **not** exposed by 0.4.2 nor the checked-in `bin/da` (`da version dev`).
- **Native adapter framework** (`internal/adapters/**`, `kg_crg.*` namespace, lockfile §10.1) exists in source but is dormant in 0.4.2 (lockfile shows `No adapters activated`; CRG path is still the legacy Python bridge).
- **Postgres FTS drift** (scout): `postgres.go:47` comments promise tsvector/GIN FTS but `pgSchemaSQL` has none; `SearchNodes` uses ILIKE.

---

## 12. Swarm-relevant hooks

How a swarm agent (DAG of subagents over shared files) would use `da kg` / `da score` **non-interactively**. All commands accept global `--json`, `-y/--yes`, `-n/--dry-run` (honor the §10.7 JSON-support caveat).

### 12.1 KG readback BEFORE editing (impact / blast-radius)

1. **Freshness probe (fast, no python):**
   `da kg code-status --json` → require `.state == "ready"`. If `unbuilt`/`busy_or_locked`, block or refresh.
2. **Refresh the code graph for the working tree (needs CRG toolchain):**
   `da kg update --base <merge-base> --json` (graceful no-op if CRG absent — re-check `code-status`). Full rebuild: `da kg build --json` (slow; `--skip-flows` for speed).
3. **Blast radius for the files the agent will touch:**
   `da kg impact <file...> --depth 2 --limit 50 --json --require-graph` → parse `changed_nodes[]` (qualified names `<abs-path>::Symbol`), `impacted_nodes[]`, `impacted_files[]`, `truncated`/`total_impacted`. `--require-graph` makes it fail-closed (non-zero) if the graph is not ready — use it so a swagent never edits blind on a stale graph.
   - Use `impact`/`flows`/`communities` (via `runPyQuery`) as the resilient read path; they survive the stale-shebang failure that breaks `changes`/`build`.
4. **Change-risk / test gaps (needs healthy CRG binary):**
   `da kg changes --base <base> --json` → `changed_functions[].risk_score`, `test_gaps[]`, `review_priorities[]`. Falls back on stale-venv; guard with `code-status` first.
5. **Neighbor / decision context (warm store; run `kg warm --include-code` once per graph refresh first):**
   `da kg bridge query --intent callers_of|callees_of|tests_for|symbol_lookup "<qualified-name>" --json`; note-side: `da kg bridge query --intent decision_lookup|plan_context "<topic>" --json`, or `da kg query --intent decision_lookup|repo_context "<q>" --json`.
   - Gotcha: `bridge health`/`mapping` ignore `--json`; scrape text or avoid.
6. **Record edit↔symbol provenance (so the next readback is richer):**
   0.4.2: `da kg link add <note-id> <qualified-name> --kind documents|implements|decides -y`.
   HEAD: `da kg link import <manifest> -y` (`#`-commented file, one `note-id qn [kind]` per line; `--dry-run` to validate).

### 12.2 Record & score OUTCOMES after work

1. **Per-iteration record** is `.agents/active/iteration-log/iter-N.yaml`, produced by `da workflow checkpoint --log-to-iter N` (the swarm's per-node iteration record). Hook-gate outcomes: `da workflow hook-outcome write ...` → `iter-N.hook-outcomes.yaml`. Reviewer judgement (HEAD/3.0.0): `iter-N.labels.yaml`.
2. **Score the just-closed iteration** (the automatic path): `da workflow close-task` runs `checkpoint → score iteration N → advance → commit` and emits `score_value`/`score_band`. Manual equivalent: `da score iteration N --recompute --json`.
3. **Score the whole run / write all sidecars:** `da score run --json` (writes `iter-N.score.yaml` + `session-*.score.yaml`); `--no-write` for a read-only preview; `--transcript-dir ~/.claude/projects --transcript-dir ~/.codex/sessions` for token backfill.
4. **Read back a score:** `da score iteration N --json` (fast, persisted sidecar) or `da score session <id> --json`. **Parse PascalCase nested keys** (`Value`, `Band`, `Breakdown[].Signal/SubScore/Contribution`); top-level `score run --json` is snake_case (`rubric_version/iterations/sessions`) but nested is PascalCase (§9.8).
5. **Interpret the number:** `Scored=false`/band `unscored` ⇒ no signals present, not a zero-quality run. Bands: ≥0.85 excellent, ≥0.70 good, ≥0.50 fair, else poor. `hook_outcomes` sub-score 0.0 means a gate had to **remediate** — a hard signal a swarm node went off-scope. `landed`/`scope`/`verifier`/`tests` are the correctness/discipline core.

### 12.3 Non-interactive invariants a swarm must honor

- Pin to the running binary's `RubricVersion` (`da score run --json` → `rubric_version`) — **0.4.2 = 2.1.0, no `human_label`**; don't assume 3.0.0 weights.
- Every score is stamped with the rubric version it was computed under; a rubric bump does not silently invalidate old sidecars (`score iteration N` reads the immutable sidecar; use `--recompute` to refresh under the new rubric).
- `da kg` mutations (`build`/`update`/`postprocess`/`warm`/`link`) journal counts-only KG decision/content-delta events (D4) — never node/edge bodies; safe to run in a shared repo. `kg sync --push` and `kg build` are the only heavy/side-effecting ones; gate them behind a single coordinator node.
- `$KG_HOME` (`~/knowledge-graph`) is a **shared global** across a repo's swarm; the CRG `graph.db` is **repo-local**. Concurrent `kg build`/`warm` against the same store are serialized by the provider (single-writer SQLite/WAL) but a swarm should still funnel graph writes through one node to avoid `busy_or_locked`.
