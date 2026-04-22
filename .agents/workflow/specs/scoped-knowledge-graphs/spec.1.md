# Scoped Knowledge Graphs — Product Contract (TTL-era snapshot, SUPERSEDED)

**Status:** SUPERSEDED — historical snapshot, do not build against this file.
**Superseded by:** `design.md` (canonical event-driven contract).
**Captured:** 2026-04-21
**Why retained:** audit trail of the TTL framing that was rejected in favor
of event-driven staleness. See `design.md` §0 for the rationale. A planner
or reviewer scanning `workflow/specs/` must not pick up this file as a
current contract — it encodes the rejected freshness-policy / TTL model
and would produce implementation work incompatible with the canonical
contract.

> **Canonical contract lives in `design.md`.** This file's body is
> preserved verbatim below as historical evidence of the pivot. Do not
> revise this file to reflect later decisions — edit `design.md` instead.

---

## 1. Problem statement

Today `AgentsRC.KG` describes a single knowledge graph: one `graph_home`, one
`backend`, one bridge. In practice a developer sits at the intersection of
several KG audiences:

- **repo** — facts only true inside this checkout (this plan's decisions, this
  repo's symbols, this project's open questions).
- **user** — facts true across everything this human works on (personal
  conventions, private notes, cross-repo memory).
- **team** — facts shared across a working group (shared decisions, on-call
  runbooks, internal APIs the team owns).
- **org** — facts true across the company (platform services, security rules,
  canonical architecture).
- **public** *(optional, read-only)* — upstream package docs, published design
  records, community-curated entities.

With a single KG we are forced to either pollute the repo graph with
cross-cutting facts or lose cross-cutting facts entirely. We also cannot answer
"where did this fact come from?" which makes trust and revocation impossible.

This spec defines the behavioral contract for a multi-scope KG: how scopes are
configured, how reads are resolved, how writes are routed, and how provenance
is preserved. It does **not** specify file paths, struct names, or migration
ordering — those belong in the plan.

---

## 2. Decisions

### 2.1 Scopes are an ordered chain, not a flat set

A project configures an **ordered list** of scopes. Precedence runs
most-local → most-global: `repo → user → team → org → public`. The order is
explicit in config, not hardcoded — a repo can omit `team` or insert a custom
scope.

**Why ordered, not flat:** every query ultimately has to produce a single
answer per node id. Ordering makes "repo overrides org" a config fact, not a
tie-breaker buried in query code.

**Rejected alternative — single merged store with tags:** tried conceptually;
writing to org from a repo checkout is a permission problem, and tags don't
give us per-scope backends (sqlite local vs postgres shared).

### 2.2 Each scope is independently backed

A scope declares its own `backend` (`sqlite`, `postgres`, `http`) and its own
connection target. `repo` and `user` default to sqlite on local disk. `team`
and `org` default to postgres behind a shared URL. `public` is `http` and
always read-only.

**Why:** the storage question is per-scope, not global. A team graph hosted
in postgres must be able to coexist with a repo sqlite file on the same
machine.

### 2.3 Reads fan out; writes target exactly one scope

**Read semantics.** A query walks the scope chain and returns results
annotated with the scope they came from. By default, when the same node id
appears in multiple scopes, the most-local scope wins (precedence). Consumers
can opt into union semantics with an explicit flag; the default is precedence
because most consumers want a single answer.

**Write semantics.** Every write names a target scope. The default target is
`repo`. Writing to `team`/`org` requires the scope to be declared writable in
config and the backend to accept the caller's credentials. `public` is never
writable from this tool.

**Why split:** merging writes is the hard problem. By forcing writes to pick
one home we avoid it entirely and push conflict detection to read time, where
we can surface it rather than silently reconcile it.

### 2.4 Every note and node carries its origin scope

Results returned from any query include the scope that produced them. Notes
stored in any scope carry a `scope` field in their frontmatter/record. This
is non-negotiable: without provenance we cannot answer "who said this" or
"revoke everything team X contributed."

### 2.5 Staleness is a per-scope property, not a global TTL

See §4. Each scope declares its own freshness policy because the decay rate
of a repo fact (minutes — tied to HEAD) is nothing like the decay rate of an
org fact (weeks — tied to platform releases).

### 2.6 Staleness propagates along derivation edges

An entry can go stale two ways. **Source staleness** — the thing this entry
describes has moved (covered by §2.5 / §4.1). **Derivation staleness** —
the entry's own source is unchanged, but something it was built from or
cites has changed, so the claim may no longer hold.

The KG already has the substrate for this: `NoteSymbolLink` connects notes
to code symbols, and the code graph has edges between symbols. Every stored
note/decision also implicitly cites the node ids it was derived from — the
plan must make those cites **explicit and stored**, not reconstructed.

When any node mutates, staleness propagates outward along derivation edges
up to a bounded depth. Reachable entries are marked **derivation-stale**,
distinct from source-stale. Consumers get both signals separately so they
can decide whether to trust, tag, or refresh.

**Why mandatory, not optional:** the whole value of a KG over flat notes is
that ideas are linked. If a rename of one function silently leaves fifty
decisions citing its old name as "fresh," the graph is actively misleading.
Propagation is what makes linkage worth maintaining.

**Why bounded:** unbounded propagation makes every trivial edit taint half
the graph. The plan must pick a depth limit and an edge-type allowlist so
propagation tracks load-bearing derivations, not incidental co-occurrence.

### 2.6 Scope resolution is a pure function of config + query

Given `(agentsrc.kg.scopes, query)` the resolver must produce the same result
regardless of caller (CLI, MCP tool, hook). No implicit environment lookups,
no per-command defaults. Callers that want a subset pass
`--scopes=repo,user`; the resolver does not invent scopes.

**Why:** today's single-KG path has hidden fallbacks (`KG_HOME` env,
`~/.knowledge-graph` default) that make "what graph did this query hit"
unanswerable. Scoped KGs would multiply that problem. Purity of the resolver
is the fix.

---

## 3. Requirements

### 3.1 Configuration

- `agentsrc.kg.scopes` is an ordered list. Each entry declares: name,
  backend, connection target, writable flag, freshness policy.
- Legacy `agentsrc.kg.graph_home` / `agentsrc.kg.backend` continue to work
  and are interpreted as a single-scope chain named `repo` (or `user`,
  depending on path). No existing `.agentsrc.json` breaks.
- Per-user scopes resolve from `~/.agents/` regardless of repo. Per-repo
  scopes resolve from `.agents/`. Team/org scopes resolve from a DSN.
- A scope may be declared `read_only: true`. `public` must be read-only.

### 3.2 Query behavior

- All KG read tools (`query_graph`, `semantic_search_nodes`,
  `get_review_context`, `get_impact_radius`, bridge intents) accept an
  optional `--scopes=<list>` selector. Default is "all configured scopes."
- Every result row is annotated with its origin scope.
- When the same node id appears in multiple scopes, the most-local wins
  unless `--merge=union` is passed. Precedence collisions are counted and
  surfaced in a `[scope-overrides: N]` structured warning so planners can
  tell when their repo fact is masking an org fact.
- When no configured scope returns results, the response includes a
  `[scopes-empty: repo,user,team]` warning listing which scopes were
  consulted. This replaces "empty results" ambiguity.

### 3.3 Write behavior

- Every write names its target scope. Commands that write default to `repo`;
  CLI and MCP tools expose `--scope=<name>` to override.
- Writes to a non-writable scope fail with a structured error naming the
  scope and its config entry.
- Writes to `team`/`org` that fail auth fail loud — they do **not** silently
  fall back to a local scope.

### 3.4 Provenance

- Every returned node/note carries `scope: <name>`.
- Every stored note carries `scope: <name>` in its persisted form.
- A `kg scopes status` command reports, per scope: backend, connection
  target, writable, note count, last sync, freshness policy, health.

### 3.5 Health and failure modes

- If a scope backend is unreachable, reads continue against the remaining
  scopes and the response carries a `[scope-degraded: team]` warning naming
  the failed scope.
- Health checks are per-scope. `workflow graph health` reports each scope
  independently; a single broken team scope does not mark the whole graph
  unhealthy.

---

## 4. Open questions

These must be resolved by the plan, not left open at implementation time.

### 4.1 Staleness policy — the shape, not the values

Each scope declares a **freshness policy**. The plan must choose the
policy's shape. Candidates:

- **TTL-based.** A scope entry expires N seconds after `IndexedAt`. Reads
  filter expired entries or tag them `[stale]`. Simple; wrong for
  repo-scope facts that should expire on HEAD change, not on a clock.
- **Source-hash-based.** Each entry records a hash of its source
  (git SHA for repo, schema version for org). Entries whose source hash
  no longer matches current are stale. Correct but requires every scope
  to define what "source hash" means.
- **Hybrid** *(recommended starting point)*. Repo scope uses source-hash
  (HEAD SHA or file mtime). User scope uses TTL. Team/org scopes declare
  their own policy in config. Public scope uses an `ETag`/`Last-Modified`
  from the HTTP source.

The plan must pick one and specify how each scope's policy is declared
in config.

### 4.2 What does "stale" mean to a reader?

Three options — pick one:

- **Hide.** Stale entries are filtered out of results. Safe but hides
  potentially-still-correct facts.
- **Tag.** Stale entries are returned with a `stale: true` flag; the
  reader (human or agent) decides. Preferred — matches the existing
  `[bridge-sparse]` warning pattern.
- **Refresh on read.** Stale reads trigger a re-index. Expensive;
  reserved for an opt-in flag, not the default.

### 4.3 Revocation and contradiction

When a user scope says "X" and an org scope says "not X," what happens?
Precedence gives one answer. But the planner probably wants to see both
and be told they disagree. Define a `contradictions` field in the
structured response that lists node ids where scopes disagreed, even
when precedence already picked a winner.

### 4.4 Public scope — deferred

Public scope is in the model from day one (so provenance and resolver
semantics cover it) but is explicitly deferred from the first
implementation plan. No plan task should build a public-scope backend
until a second spec covers discovery, trust, and caching.

### 4.5 Derivation propagation — shape and bounds

§2.6 commits to propagating staleness along derivation edges. The plan must
pin down:

- **What counts as a derivation edge.** Options, cumulative:
  - `NoteSymbolLink` rows with `LinkKind` in a load-bearing set
    (`documents`, `implements`, `decided_on`) — propagate when the linked
    symbol changes. Exclude weak kinds like `mentions`.
  - **Explicit `derived_from` cites** stored in a note's persisted form,
    listing the node ids / note ids the claim rests on. This is new; the
    plan must define the field and how writers populate it (manual at
    write time vs. captured by the tool that produced the note).
  - **Code-graph edges between cited symbols.** If a note cites `FuncA`
    and `FuncA` calls `FuncB`, does a change to `FuncB` taint the note?
    Probably yes at depth 1, probably no at depth 3 — see depth limit.

- **Depth limit.** Propagation walks N hops through derivation edges.
  Recommended starting value: **1** for code-graph edges (direct callers/
  callees of a cited symbol), **unlimited within a single note→note
  derivation chain** (if A cites B cites C and C changes, A is tainted).
  The plan must commit to a value.

- **Edge-type allowlist.** Which `NoteSymbolLink.LinkKind` values
  propagate and which don't. `mentions` almost certainly should not;
  `implements` and `documents` almost certainly should.

- **What "change" means for a cited symbol.** Options:
  - Any mutation of the symbol's node row.
  - Only signature/structure changes (body edits don't count).
  - Hash-based — source hash of the symbol changed.

- **Taint decay.** Once marked derivation-stale, does the tag persist
  until explicit refresh, or does it fade (e.g., after human
  acknowledgement, after a subsequent note-level re-verification)?

### 4.6 Semantic (non-structural) propagation — deferred

Structural propagation (§2.6, §4.5) only catches changes connected by
stored edges. Two notes can be *semantically* about the same idea without
a stored edge between them — e.g., a decision about "retry policy" and a
new entity about "backoff strategy" may need to co-invalidate even though
neither cites the other.

Embedding-similarity-based propagation ("when node X changes, also tag
nodes with cosine > 0.9 to X as review-worthy") is out of scope for v1.
The plan should note this as a future lane and ensure the data model
doesn't preclude it — specifically, that the `stale` field is structured
enough to carry a reason (`source`, `derivation`, future: `semantic`).

### 4.7 Conflict between two writable scopes at write time

A user types "remember X" with no `--scope` flag. Default is `repo`. If
the fact is obviously user-global (e.g., "I prefer tabs"), we want it in
`user`, not `repo`. Out of scope for v1 — v1 always uses the configured
default. A later spec may introduce write-routing heuristics.

---

## 5. Staleness management — design commitments

Regardless of which option §4.1 picks, these commitments hold:

- **Staleness is visible, not silent.** Stale reads are tagged in output,
  never quietly dropped.
- **Staleness is per-scope.** No global TTL setting; each scope's config
  declares its own policy.
- **Freshness evidence is stored, not computed at read time.** Each entry
  records whatever the policy needs (source hash, expires-at, etag,
  derivation cites) at write time. Read time does a cheap comparison, not
  a re-derivation.
- **Re-indexing is an explicit command.** `kg refresh --scope=<name>`
  reruns the scope's index pipeline. Staleness tags are hints that a
  refresh is needed, not triggers that run it automatically.
- **Existing `IndexedAt` is the v0 signal.** We already store
  `IndexedAt` on every note. The first implementation can ship with
  TTL-based staleness for all scopes using that field, and tighten per
  scope later.
- **Staleness carries a reason, not just a bit.** The `stale` field on
  returned entries is structured: `{ stale: true, reason: "source" |
  "derivation", because: [<node_id or note_id>, ...] }`. A consumer must
  be able to tell *why* something is stale and *what* tainted it.
- **Propagation is write-time, not read-time.** When a node is updated,
  the write pipeline walks derivation edges and marks reachable entries
  derivation-stale. Reads then filter on a stored flag. Walking the graph
  at every read would be prohibitively expensive and would mean stale
  status depends on read latency budget, not on truth.
- **Derivation cites are first-class, not reconstructed.** Every written
  note/decision records the node ids and note ids its claim rests on.
  Without stored cites, propagation has nothing to walk. Writers that
  cannot produce cites (e.g., free-form human notes) are allowed but
  are tagged `derivation: untracked` so consumers know propagation
  won't protect them.
- **Propagation is bounded.** A configured depth limit and edge-type
  allowlist prevent one symbol rename from tainting half the graph.
  Bounds live in scope config, not hardcoded.

---

## 6. Done criteria

- `.agentsrc.json` with an ordered `kg.scopes` list validates and round-trips.
- A legacy `.agentsrc.json` with only `kg.graph_home` still works without edits.
- A KG query against a repo with `repo + user + org` scopes returns results
  annotated with origin scope.
- A `team` scope with a bad DSN returns a `[scope-degraded]` warning and
  does not fail the whole query.
- A write with `--scope=team` against a non-writable scope fails loud with
  the scope name in the error.
- `kg scopes status` reports per-scope health and freshness.
- Every stored note carries `scope` provenance.
- An entry older than its scope's freshness policy is returned with
  `stale: true, reason: "source"` (not hidden) under default settings.
- When a cited node is mutated, every entry reachable within the
  configured derivation bound is returned on the next read with
  `stale: true, reason: "derivation", because: [<mutated-id>]`.
- Every written note carries structured `derived_from` cites or the
  explicit `derivation: untracked` marker. No note silently lacks
  provenance.

---

## 7. Deferred

- Public-scope backend implementation (discovery, trust, caching).
- Write-routing heuristics ("this fact smells like a user fact, store in
  user scope").
- Cross-scope merge UIs for the contradiction case beyond the
  `contradictions` field.
- Per-scope access control beyond the `read_only` flag.
- Scope inheritance / composition (team inherits from org).
- Semantic-similarity-based propagation (§4.6). v1 handles structural
  propagation only.
- Automatic re-verification of derivation-stale entries. v1 tags them and
  leaves resolution to an explicit `kg refresh` or human review.

---

## 8. Relationship to other specs

- **graph-bridge-contract** — defines the single-KG bridge surface this
  spec generalizes. The bridge's `[bridge-sparse]` warning is the model
  for this spec's `[scope-degraded]` / `[scopes-empty]` /
  `[scope-overrides]` warnings.
- **kg-command-surface-readiness** — defines the `kg` command surface
  that must grow a `--scope` selector and a `kg scopes status` subcommand.
- **org-config-resolution** — defines how org-level config reaches a repo;
  the org scope's DSN likely resolves through the same mechanism.
