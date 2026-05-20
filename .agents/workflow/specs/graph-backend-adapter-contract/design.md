# Graph Backend Adapter Contract — Design Spec

**Status:** draft v5 (canonical)
**Written:** 2026-05-09 (proposal v1) → 2026-05-09 v2 → 2026-05-09 v3 graduated to sibling spec → 2026-05-09 v4 → 2026-05-09 v4.1 → 2026-05-11 v5 (this revision)
**Plan:** [`.agents/workflow/plans/graph-backend-adapter-contract/graph-backend-adapter-contract.plan.md`](../../plans/graph-backend-adapter-contract/graph-backend-adapter-contract.plan.md) — implementation-detail companion (DSL grammar conformance test catalog, lockfile state-machine implementation, namespace token SDK shape, TTRPG grammar-extension decision)
**Supersedes:** `.agents/proposals/graph-backend-adapter-contract.md` (proposal accepted; this spec is the canonical artifact)

**Related:**
- [app-type-profiles](../app-type-profiles/design.md) — `graph_backend` field surface (§2.6, §3.1, §7.3, §8). This spec defines the adapter contract that field refs.
- [scoped-knowledge-graphs](../scoped-knowledge-graphs/design.md) — storage substrate, drivers, scopes, propagation. Adapters write into scoped KG storage by construction.
- [config-distribution-model](../config-distribution-model/design.md) — `sources`, `extends`, `packages`, two-pass resolution, lockfile (§7). Adapter YAML is Tier 1; bootstrap skill is Tier 2.
- [external-agent-sources](../external-agent-sources/design.md) — OCI distribution for bootstrap-skill packages.
- [go-native-code-graph-analysis](../go-native-code-graph-analysis/design.md) — superseded-in-part by §11 of this spec; CRG becomes a kg-native adapter.

---

## 0. What changed (revision history)

This spec graduated from `.agents/proposals/graph-backend-adapter-contract.md`
through four review rounds. v4 (this draft) closes the v3 review findings:

- **§5.4.2 OPTIONAL+WHERE lowering** corrected: hoist predicate to `ON`
  only when the source alias originates in `OPTIONAL MATCH`. v3
  over-corrected by treating all ref-traversals as optional, which
  caused required-MATCH ref filters to over-return (preserving rows
  with NULL refs through the LEFT JOIN). v4 keeps required-MATCH
  predicates in `WHERE` (the LEFT JOIN's NULL row is then naturally
  filtered out, matching adapter-author intent).
- **§4 ref-field schema** extended with `derivation: true` flag. Refs
  marked `derivation: true` participate in derivation propagation
  identically to edges. This makes the compliance example
  (`control.derives_from` ref → policy environmental trigger →
  control derivation-stale) actually expressible.
- **§7.3 propagation rule** updated to walk both edges and ref fields
  marked `derivation: true` uniformly.
- **§8.2 namespace tokens** redefined as a **set** of (namespace, mode)
  pairs derived from the adapter's own namespace plus its `reads_from`
  declarations. v3's single-adapter token couldn't authorize the
  cross-namespace joins that `reads_from` requires.
- **§10.3 cross-adapter cutover** corrected: the gate is mechanical,
  not procedural. The dependee blocks until the dependent ships a new
  adapter version whose DSL queries validate against the new dependee
  schema. v3's "ack" framing was wrong — there is no human
  acknowledgment surface; the lockfile state machine transitions on
  validation, not on operator action.
- **§13.2 compliance example** updated: `control.derives_from` ref now
  declares `derivation: true` per the new schema; the
  `policy_review_due_impact` query is implementable end-to-end against
  the contract.
- **§14 Q5 (TTRPG grammar)** moved to the plan document. The `*0..3`,
  reverse, and undirected pattern constructs the v3 `allegiance_path`
  query relied on are not in §5.1's documented grammar. The plan
  tracks the question of whether to extend grammar (v1.5 of the DSL)
  or rewrite the query within v1 grammar. The dogfood query is
  rewritten in v4 to fit current grammar; grammar-extension decision
  defers to implementation experience.

**v4.1 follow-on fixes** (sibling adversarial review, applied
2026-05-09):

- **§8.3 cross-adapter reads** restricted to materialized views only.
  v4 said bootstrap and named queries could also declare `reads_from`,
  but the §4 schema only had the field under `materialized_views`,
  creating a trust hole. v4.1 closes the hole by tightening the
  contract: cross-namespace reads are exclusively via materialized
  views; bootstrap/queries that need cross-adapter data read from a
  view that carries the declaration. Single enforcement point.
- **§13.4 + §13.5 built-in adapters defined.** v4 wiring referenced
  `dotagents-builtin:graph/citation@^1.0` and
  `dotagents-builtin:graph/document-cross-ref@^1.0` in app-type-profiles
  worked examples, but the contract had not defined them. v4.1 adds
  minimal canonical schemas for both, anchoring the built-in claim.
- **TTRPG queries.yaml** corrected for description overclaims (no
  `ORDER BY` in v1, no shortest-path operator) and a real bug in
  `continuity_check` (optional param used unconditionally; fixed via
  `coalesce`).

**v5 follow-on fixes** (Codex adversarial review of v4.1, applied
2026-05-11):

- **§13.4 citation adapter `impact_radius` Cartesian product fixed.**
  v4.1's `impact_radius` had `OPTIONAL MATCH (c:claim)-[:cites]->(changed)`
  and `OPTIONAL MATCH (changed)-[:supports]->(c2:claim)` in one
  rowset, producing N × M rows for sources with both citing and
  supported claims. v5 scopes `impact_radius` to the canonical `cites`
  derivation edge only; ships `claims_supported_by_source` as a
  separate named query; removes `derivation: true` from `supports`
  to prevent double-counting in propagation.
- **§11.2 / §11.3 dual-read parity surface added.** v4.1's §8.3
  tightening forbid bootstrap and named queries from reading
  cross-adapter, which left CRG migration without a live parity
  surface (offline corpus alone can't catch repo-local drift). v5
  models the legacy bridge as a temporary read-only adapter
  `dotagents-builtin:graph/crg-bridge@^0.x` with `migration_only:
  true`, allowing parity views via the same `reads_from` machinery
  any other cross-adapter view uses. The bridge adapter retires with
  the bridge itself per the §11.4 decommissioning gate.
- **§5.1.1 allowed functions in WHERE.** v4.1's TTRPG
  `continuity_check` fix used `coalesce($since_session, 0)` in
  WHERE, but the original §5.1 grammar only allowed `coalesce` in
  RETURN. v5 extends §5.1 to permit allowed-function calls on
  params and literals in WHERE (no field-level computation —
  param-normalization only). The `continuity_check` fix is now
  contract-conformant.
- **§13.5 `section.in_document` no longer marked derivation.** v4.1
  marked this containment ref as derivational, which caused
  whole-document stale storms (every document revision tainted
  every section in the document). v5 keeps `derivation: true` only
  on `concept.defined_in` (genuine content dependency) and on
  `references` edges (cross-section content dependency); pure
  containment refs stay non-derivational.
- **Plan/spec split: normative content moved back into spec.** v4
  introduced the plan document but Codex correctly observed that
  lockfile enums, conformance test catalogs, command surface, SDK
  contract, and CRG parity tests are contract-normative behavior,
  not work breakdown. v5 ports those sections back into the spec
  (§5.5 conformance test catalog, §8.2.1 token tests, §8.4
  bootstrap-skill SDK, §10.1.1–§10.1.4 lockfile state machine and
  command surface, §11.6 CRG parity criteria). The plan retains
  only ordering rationale, decision deferrals, and implementation
  open questions. Canonical `PLAN.yaml` + `TASKS.yaml` are created
  via `da workflow plan create graph-backend-adapter-contract`.

**v6 follow-on fixes** (gcc + pgGraph reconciliation, 2026-05-19):

- **§2.7 executor-tier separation added** (new section). v1 makes the
  executor an explicit architectural tier separate from the adapter
  contract. The contract guarantees `(schema, DSL queries, adapter
  semantics)` — it does NOT pin a single execution engine. v1 ships
  with a B-tree / recursive-traversal executor over the gcc-shipped
  scoped-KG primitives (SQLite Path A; Postgres adapters extending
  the same role-segregated `Store` / `CodeGraphReader` /
  `CodeGraphWriter` / `Closer` / `Handle` shape). A future in-memory
  CSR executor (the pgGraph approach independently validated this
  direction) is a **v2 swap of the executor tier, not a contract
  change** — adapter authors do not re-author against a new contract.
- **§2.1 substrate language reconciled with gcc.** v5 said "shared KG
  storage substrate" abstractly; gcc1 (#30 keystone) shipped the
  concrete role-segregated `Store` interface family in
  `internal/graphstore/store.go`, and gcc2 (#34) shipped Path A
  (bounded ephemeral SQLite with reaper-aware Close). v6 names those
  primitives as the substrate. Postgres adapters MUST follow the same
  role-segregation pattern (separate `CodeGraphReader` / `Writer` /
  `Closer` / `Handle` shape; do not stuff everything in one mega
  interface) — both SQLite and Postgres back-ends present the same
  shape to adapter code.
- **§1 non-goal added.** "v1 targets shallow traversal" stated
  explicitly. pgGraph's empirical claim is that recursive-CTE /
  B-tree pointer-chasing degrades at 10+ hops on 30M+ edges; v1's
  executor commits to ≤5-hop workloads. Deep traversal (>5 hops on
  >100K edges) is a v2 executor-tier concern, not a contract gap.
- **§8.2 resolve-at-boundary framing added (non-normative supplement).**
  pgGraph's pattern — "SQL boundary resolves coordinates, labels,
  filters, tenant scopes BEFORE entering the traversal loop; the loop
  sees pre-resolved tenant bitmaps" — is a cleaner operational model
  for the existing namespace token. v6 documents this as the
  preferred lowering pattern: tokens are resolved once at the DSL
  boundary; the executor's inner loop sees a fixed, pre-resolved
  capability set, not a per-statement rewrite token.
- **§12 v1 blocker fast-path added.** v5's evidence-budget table only
  promotes to v1.5 design review at 5 points; a single blocker-class
  request scores 3, so a real v1 blocker waited for cumulative
  signals. v6 adds a v1-side fast-path: a verified blocker-class
  request (user/adapter cannot proceed without it) is immediately
  scoped into v1, not deferred to v1.5. The accumulating budget
  remains the mechanism for non-blocking richness requests.
- **§4 `max_depth` advisory.** Adapter-declared `max_depth` may exceed
  2 in v1 (the §5.4.1 ref-chain cap of 2 does NOT carry over to
  variable-length edge patterns). Executor-aware advisory: depths
  above 5 on the v1 executor incur the asymptotics pgGraph
  benchmarks call out; adapter authors targeting that regime should
  flag it as a §12 fast-path candidate so v2 executor planning can
  bind to real workloads.

Per the **plan/spec split** introduced in v4: implementation-detail
content (full DSL conformance test catalog, lockfile state-machine
serialization shape, namespace token SDK enforcement hooks, namespace
token query-rewrite mechanics) lives in the
[plan document](../../plans/graph-backend-adapter-contract/graph-backend-adapter-contract.plan.md).
The spec retains the contract; the plan retains the build details.

### Prior revision summary

v1 → v2 → v3 closed nine load-bearing defects across two adversarial
review rounds:

- **CRG carve-out** dropped (v1 finding); CRG MCP tools enumerated with
  per-tool kg-native replacements; bridge decommissioning gated on
  parity tests (v2 finding) — see §11
- **Full-SQL escape hatch** replaced with constrained materialization
  API (v1 finding); namespace enforcement now physical (sqlite ATTACH /
  postgres SCHEMA / http path), not naming-convention (v2 finding) — see §8
- **Ref-join semantics** added with typed refs, depth cap, use-site
  analysis (v1 finding); normative SQL lowering rules for OPTIONAL +
  WHERE predicates that lift into ON clause (v2 finding) — see §5.4
- **Tier split** unlocked; schema migration via lockfile-pinned digest
  (v1 finding); explicit cutover protocol with read-freeze, view
  versioning, and dependent-owned cross-adapter coordination (v2
  finding) — see §10
- **v1.5 trigger** is a 5-point budget across multiple signal sources
  (v1 finding) — see §12
- **`env_predicates`** added to schema; environmental-driver propagation
  rule made explicit; compliance worked example traces policy → control
  through derives_from (v1 + v2 findings) — see §7 and §13.2
- **TTRPG `allegiance_path` query** rewritten as type-correct multi-hop
  with allowed return surface (v2 finding) — see TTRPG dogfood adapter
  at `.agents/sandbox/ttrpg-adapter/queries.yaml`

The underlying direction (KG-as-substrate + pluggable adapters +
declarative DSL) is unchanged across all three drafts. v3 corrects the
contract details, not the architecture.

---

## 1. Problem statement

`app-type-profiles/design.md` §2.6 currently treats `graph_backend` as a
closed enum (`crg | citation-graph | document-cross-ref | none`) bound to
skill refs. That framing has three problems:

1. **It implies separate databases per backend.** Building three more
   graph databases throws away every commitment scoped-knowledge-graphs
   already made (scoped backends, provenance, event-driven staleness,
   write-time-only propagation, resolver purity).
2. **It closes the extension point.** Custom domains — compliance
   registers, campaign worldbuilding, lab notebooks — must either land
   in `da` core or wedge into a named bucket.
3. **It conflates schema, query, and storage.** Splitting these three
   concerns is what enables a pluggable adapter contract over a shared
   storage substrate.

This spec defines the **graph backend adapter contract**: a pluggable
artifact that declares schema, queries, planner hints, staleness driver
binding, materialized views, and a bootstrap skill — all writing into
scoped KG storage with no separate databases or sidecar processes.

### 1.1 Non-goals (v1)

- **Deep-traversal performance is out of scope for v1.** v1's executor
  is B-tree / recursive-traversal over the gcc-shipped scoped-KG
  primitives (§2.7). pgGraph's published asymptotics establish that
  recursive-CTE / B-tree pointer-chasing degrades around 10+ hops on
  >30M edges; v1 commits to ≤5-hop workloads on graphs measured in
  thousands-to-low-millions of edges. Adapter authors targeting deeper
  regimes flag them as §12 fast-path candidates so a v2 executor swap
  can bind to real workloads — the adapter contract itself does not
  change.
- **No adapter-private storage.** §2.1 already forbids separate
  per-adapter databases. Restated as a non-goal so it stays visible.
- **No raw SQL escape hatches.** §2.2 + §5.2 already forbid. Restated.
- **No silent executor swap.** Switching v1's executor for a future v2
  (e.g. in-memory CSR) is a permitted contract behavior (§2.7), but it
  is not silent: it surfaces under a release with measured perf and a
  documented compatibility statement. Authors can rely on v1's
  observable semantics through the transition window.

---

## 2. Decisions

### 2.1 All adapters write to scoped KG storage; no exceptions

CRG included. The CRG adapter's bootstrap skill performs Tree-sitter
ingestion and writes function/type/call-edge notes to shared KG storage
(per §11). Adapters differ in **schema** and **query**, never in
**storage**.

**Why:** the v1 carve-out for CRG ("structural code graph with its own
MCP server") broke the unified-storage claim before the first built-in
backend. Re-litigating storage per adapter throws away the scoped-KG
commitments.

**Rejected alternative — separate adapter databases:** explored in the
proposal-v1 phase. Rejected because it required reimplementing
provenance, scope, staleness, and propagation per backend.

### 2.2 The DSL is declarative and the only query interface

No raw SQL anywhere — bootstrap skills, materialized views, and named
queries all use the §5 DSL.

**Why:** raw SQL in the bootstrap skill (proposed in v1) defeated the
named-query DSL's safety boundary by laundering arbitrary writes through
the bootstrap path.

### 2.3 Cross-adapter reads are explicit and locally-coordinated

An adapter can read across namespaces only when the dependency is
declared via `reads_from` in a materialized view. Cutover coordination
between adapters happens in the consumer's lockfile, not by the
dependee adapter knowing its dependents.

**Why:** the dependee can't know its dependents in cross-app /
public-private scenarios. Coordination must live where both adapters
are co-installed (the consumer's `da` instance), not at publication
time.

### 2.4 Schema changes are package installs, not config sync

Adapter YAML is Tier 1, but schema activation pins a digest in the
lockfile and requires compatibility checking, view rebuild, and
optional migration skills before the new schema goes live.

**Why:** v1 marked the tier split as locked, claiming Tier 1 was "low
urgency" because it's pure data. But adapter YAML defines persisted
note/edge vocabulary — passive sync of a schema change reinterprets
existing KG state silently.

### 2.5 v1.5 of the DSL is opened by accumulating evidence, not threshold counting

A 5-point budget across multiple signal sources (additional named
queries that work around missing primitives, materialized views, MCP
servers spawned for query expressiveness, blocker-class wishlist
entries) opens v1.5 design review.

**Why:** v1's two-distinct-adapter threshold created a chicken-and-egg
where the first adapter's pain was redirected into local workarounds
before evidence for a missing primitive could accumulate.

### 2.6 Environmental driver and review-nudge are kept distinct

Environmental drivers (predicate-based, fire on declared events) are a
staleness driver — matching notes get tagged `stale: { reason:
"environmental" }`. Review-nudge (time-based, per scoped-KG §2.7) is a
separate dimension — never tags notes stale.

**Why:** v1 conflated the two in the compliance example by framing
`evidence.expires_at` as "environmental driver" while not declaring
predicates. v2 added `env_predicates` as a first-class field; v3 adds
the propagation rule for environmental events into derivation chains.

### 2.7 The executor is an architectural tier separate from the contract

The adapter contract guarantees `(schema, DSL queries, propagation
semantics, namespace enforcement)`. It does **not** pin a single
execution engine. v1 ships with a B-tree / recursive-traversal
executor over the gcc-shipped scoped-KG primitives. A future executor
swap is permitted by the contract; adapter authors are unaffected.

**v1 substrate (normative).** The executor reads and writes through
the role-segregated `Store` family already shipped by gcc1:

- `CodeGraphReader` — node/edge/metadata reads, impact-radius, etc.
- `CodeGraphWriter` — upserts + `StoreFileNodesEdges` (transactional bulk)
- `KGNoteStore`, `NoteSymbolLinkStore` — adapter note + ref machinery
- `Closer` — reaper-aware Close (gcc2 contract; no goroutine/conn
  outlives the store)
- `Handle` — the constructor-injected client view

For the SQLite backend, Path A (gcc2) is the v1 implementation:
bounded ephemeral pool, abandon-and-fail timeouts, modernc-safe.

**For Postgres-backed adapters (normative).** The same role
segregation MUST hold. Postgres adapters present a `Store` with the
same `CodeGraphReader` / `CodeGraphWriter` / `Closer` / `Handle`
shape backed by `pgxpool`. Do not collapse the roles back into a
single mega interface; do not introduce store types that bypass the
shape. pgxpool's `Close()` already provides the graceful-shutdown
guarantee that `SQLiteStore.Close()` had to hand-build, so the
Postgres `Closer` is a thin pass-through — but it MUST be there so
adapter code is portable across backends. The current open
`requestContext(nil)` / per-call ctx items (gcc3 / gcc4) tighten this
boundary; their resolution is the same contract everywhere.

**Permitted v2 swap.** An in-memory CSR executor (the pgGraph
approach independently validated this direction) is a v2 swap of the
executor tier — the adapter contract is unchanged. Whether to
actually build a Go-native CSR executor is a separate, measured
decision: pgGraph's wins materialize at deep traversals (>5 hops on
>100K edges) under heavy multi-backend Postgres load, which dot-agents
is not in today. The right gate is "instrument the v1 executor;
trigger a CSR build only when a real workload exceeds its envelope" —
not "port pgGraph pre-emptively." See the project-local note
`executor-csr-research-2026-05` for the deferred plan.

**Why this tier separation matters.** Without it, a future executor
change would read as a contract break and adapter authors would
panic. With it, executor experiments (different storage layouts,
in-memory caches, materialization tiers) stay invisible to adapter
code. The DSL §5 is the front-end contract; gcc's `Store` family is
the back-end contract; the executor is everything between them and is
free to evolve.

---

## 3. Adapter overview

A graph backend adapter declares:

- **(a) Schema** — note types, edge types, optional environmental
  predicates that bind to staleness drivers
- **(b) Impact radius** — the named query the review pipeline invokes
  when a task touches a node, returning the blast radius
- **(c) Staleness driver binding** — which of scoped-KG's drivers fire
  on this domain's nodes
- **(d) Planner hints** — algorithm choice, materialized views, index
  hints, cardinality stats
- **(e) Materialized views** — declarative view definitions including
  cross-adapter `reads_from` declarations
- **(f) Additional named queries** — surfaced via `da graph <backend>
  query <name>` for adapter UX beyond the impact radius
- **(g) Bootstrap skill** — Tier 2 OCI package that populates the
  graph using the DSL (no raw SQL)

---

## 4. Adapter schema

```yaml
# graph-backend: <name>.v<version>
name: <name>
version: <semver>
description: |-
  <one-paragraph purpose>

note_types:
  - name: <type-name>
    fields:
      - { name: <field>,
          type: <string|int|float|bool|date|enum|ref<type>>,
          required: <true|false>,
          values: [<for-enum>],
          derivation: <true|false> }      # for ref<...> fields only:
                                          # marks the ref as a derivation
                                          # path for stale propagation (§7.3)
    env_predicates:                       # optional
      - kind: <time_after|module_version|webhook|custom>
        field: <field-name>               # for time_after: which date field
        # additional kind-specific args

edge_types:
  - { name: <edge-name>, from: <note-type>, to: <note-type>,
      cardinality: <one-to-one|one-to-many|many-to-many>,
      signed: <true|false>,
      weight_field: <field-on-edge>        # optional, for dijkstra
      derivation: <true|false> }           # marks edge as a derivation
                                           # path for stale propagation (§7.3)

impact_radius:                            # required
  query: |-
    <DSL — see §5>
  max_depth: <int>
  algorithm_hint: <bidirectional_bfs|dijkstra|astar|bfs|dfs>
  materialize:
    - transitive_closure_of: [<edge-types>]
    - neighborhood_summary_for: [<note-types>]
  index_hints:
    - { predicate: <edge-name>, order: <SPO|SOP|PSO|POS|OSP|OPS> }
  cardinality_stats:
    auto: true

staleness_drivers:
  - <source_mutation|derivation_mutation|explicit_revocation
     |contradiction_arrival|environmental_trigger>

bootstrap_skill: <skill-ref>              # optional Tier 2 OCI ref

queries:                                  # optional, beyond impact_radius
  - name: <query-name>
    description: |- <purpose>
    params:
      - { name: <param>, type: <type>, required: <bool> }
    query: |- <DSL>
    algorithm_hint: <hint>

materialized_views:                       # optional, see §8
  - name: <view-name>
    description: |- <purpose>
    reads_from:
      - { adapter: <adapter-name>,
          version: <semver-range>,
          note_types: [<types>],
          edge_types: [<edges>] }
    refresh_on:
      - { adapter: <adapter-name>,
          driver: <driver-name>,
          on_types: [<note-types>] }
    query: |- <DSL>
```

---

## 5. Named-query DSL (v1)

The DSL is intentionally narrow. It is the only interface for queries,
materialized views, and bootstrap operations. All inputs are
parameterized; no raw SQL surface exists.

### 5.1 Allowed clauses

- `MATCH (alias:<note-type>)-[:<edge-type>|<edge-type>...]->(alias:<note-type>)`
- `OPTIONAL MATCH` for left-join semantics
- `WHERE alias.<field> <op> <param-expr>` — where `<param-expr>` is
  one of: `$<param>`, a literal, or an allowed-function call applied
  to params and literals only (see §5.1.1)
- `WHERE alias.id IN $<param>` — list parameters bound to query-time arrays
- Variable-length edge patterns `[:<edge>*1..<max_depth>]`, bounded by
  the declared `max_depth`
- `RETURN <alias>.<field>, hop_count, count(*), min(...), max(...), coalesce(...)`

#### 5.1.1 Allowed-function set and where they apply

The allowed function set is `{ coalesce, count, min, max, hop_count }`.
Function application is constrained by clause:

| Function | RETURN | WHERE |
|---|---|---|
| `coalesce(<param-or-literal>, <literal>, ...)` | yes | yes — for param normalization only |
| `count(*)` | yes | no |
| `min(<alias.field>)`, `max(<alias.field>)` | yes | no |
| `hop_count` | yes | no |

**WHERE-side constraint:** functions in WHERE can only operate on
params and literals, **never on note fields**. This rules out
`WHERE coalesce(alias.field, ...)` (field-level computation) but
allows `WHERE alias.field >= coalesce($param, 0)` (param
normalization for genuinely optional filters). The rationale: WHERE-
side field computation is an attack surface (predicate timing,
side-channel leaks via expression evaluation) and a planner
foot-gun (function application defeats predicate-pushdown into the
index); WHERE-side param normalization is neither — the function
folds to a constant before predicate evaluation begins.

### 5.2 Forbidden

- String concatenation in any clause
- Subqueries
- Recursion outside variable-length patterns
- DDL of any kind (no `CREATE`, `MERGE`, `SET`, `DELETE`)
- Functions outside the §5.1.1 allowed set
- Allowed functions applied to note fields in WHERE (only params and
  literals — see §5.1.1)

### 5.3 Substitution model

- `$<param_name>` placeholders bind to caller-supplied values at
  execution time
- Types are checked against the `params` declaration at adapter load
- The query body is parsed once at adapter load; the AST is reused
  for every invocation (this enables planner-hint precomputation)

### 5.4 Ref-join semantics

A field declared with type `ref<type>` is a single-valued reference to
a node of the named type. Refs are cheaper than edges (no edge-table
lookup) and are appropriate for inherent single-valued relationships
(a character has one stated location, a control has one owner).

**Untyped `ref` fields are forbidden.** Schema validator rejects
`{ type: ref }` and requires `{ type: ref<location> }`.

#### 5.4.1 Compilation rules

1. **Bare ref usage** — `RETURN c.stated_location` returns the scalar
   ref id from the source row. **No JOIN.**
2. **Field traversal through ref** — `c.stated_location.region` resolves
   the ref to a node and reads the named field. **One LEFT JOIN.**
3. **Use-site analysis** — when the same ref-chain prefix appears
   multiple times in a single query (across MATCH / WHERE / RETURN),
   generate **one JOIN** with a stable alias and reuse it.
4. **Bare-ref + field-traversal mix** — if a query uses both
   `c.stated_location` (bare) and `c.stated_location.region` (traversal),
   the JOIN is generated once for the traversal; the bare reference
   reads the scalar column directly.
5. **Depth cap = 2.** Ref-chains deeper than two hops
   (`c.stated_location.ruler.faction`) are rejected at adapter load.
6. **LEFT JOIN, not INNER.** Null refs short-circuit to null on the
   joined fields.

#### 5.4.2 OPTIONAL + WHERE lowering (normative)

The lowering rule is **source-context-aware**. Whether a predicate on
a ref-traversal field hoists to the JOIN's `ON` clause or stays in
`WHERE` depends on the match context of the **alias from which the
ref was traversed**, not on the fact that refs are nullable.

**Lowering algorithm:**

1. Walk the AST tagging each alias with its match context (`required`
   from `MATCH`, `optional` from `OPTIONAL MATCH`)
2. For each `WHERE` predicate `<alias>.<refField>.<field> <op> <value>`:
   - Identify the source alias (the alias from which the ref was
     traversed)
   - If the source alias's match context is **`optional`**: lift the
     predicate into the LEFT JOIN's `ON` clause (combine with existing
     JOIN condition via `AND`). This preserves the OPTIONAL semantics
     of the source — rows where the source matched but the joined ref
     is NULL or fails the predicate are kept with NULL on the joined
     fields.
   - If the source alias's match context is **`required`**: keep the
     predicate in `WHERE`. The LEFT JOIN's NULL row for missing or
     non-matching refs is naturally filtered out by the WHERE
     predicate, which is the intended behavior — the adapter author
     wrote the filter to mean "include only rows where the ref
     resolves and the predicate holds."
3. Predicates on bare ref usage (`alias.refField` with no `.field`
   selector) read the scalar ref id from the source row directly; no
   JOIN is generated, no hoisting question arises.

**Why this matches adapter-author intent.** A required-MATCH source
alias represents "this entity must exist in the result." An adapter
author writing `WHERE c.stated_location.region = $region` against
`MATCH (c:character)` is asking for "characters whose stated_location
is non-null and has region = $region." The required-MATCH context is
the signal that null refs should not pass.

An adapter author who explicitly wants null-preserving semantics
(e.g., "all characters, plus their EU-region location if they have
one") uses `OPTIONAL MATCH` on the source alias or, in v1.5+, an
explicit `OPTIONAL WHERE` construct (deferred per §14 Q4).

**Conformance test 1 — required source, predicate stays in WHERE
(must pass):**

```
DSL:
  MATCH (c:character)
  WHERE c.stated_location.region = $region
  RETURN c.id, c.stated_location.kind

Required SQL shape:
  SELECT c.id, c__stated_location.kind
  FROM kg_ttrpg_notes c
  LEFT JOIN kg_ttrpg_notes c__stated_location
    ON c__stated_location.id = c.stated_location_ref
       AND c__stated_location.note_type = 'location'
  WHERE c.note_type = 'character'
    AND c__stated_location.region = $1   -- predicate stays in WHERE

Required behavior:
  - Characters with NULL stated_location are NOT in the result
    (LEFT JOIN's NULL row fails the WHERE predicate)
  - Characters with stated_location.region != $region are NOT
    in the result (joined row fails the WHERE predicate)
  - Only characters whose stated_location is non-null and has
    matching region appear
```

**Conformance test 2 — optional source, predicate hoists to ON (must
pass):**

```
DSL:
  MATCH (c:character)
  OPTIONAL MATCH (c)-[:home]->(loc:location)
  WHERE loc.region = $region
  RETURN c.id, loc.id

Required SQL shape:
  SELECT c.id, loc.id
  FROM kg_ttrpg_notes c
  LEFT JOIN kg_ttrpg_edges e_home
    ON e_home.from_id = c.id AND e_home.edge_type = 'home'
  LEFT JOIN kg_ttrpg_notes loc
    ON loc.id = e_home.to_id
       AND loc.note_type = 'location'
       AND loc.region = $1                 -- predicate lifted into ON
  WHERE c.note_type = 'character'

Required behavior:
  - All characters appear in the result (LEFT JOIN preserves source
    rows even when joined fields fail the predicate)
  - Characters with no home, or home in wrong region, have loc.id
    = NULL in the result row
  - Characters with home in matching region have loc.id populated
```

The full conformance test catalog (10+ cases covering all DSL
constructs) lives in the
[plan document §3](../../plans/graph-backend-adapter-contract/graph-backend-adapter-contract.plan.md#3-dsl-conformance-test-catalog).
Implementation must produce equivalent semantics for all listed
cases; literal SQL form may differ.

### 5.5 Conformance test catalog (normative)

Implementations MUST pass every test in this catalog. Tests are
expressed as `(DSL input, required SQL shape, required behavior)`
triples. The literal SQL form may differ; the behavior must be
identical.

#### 5.5.1 Basic query forms (T1–T5)

- T1: Single-type MATCH with WHERE on scalar field
- T2: MATCH with single edge traversal
- T3: MATCH with variable-length edge traversal `*1..N`
- T4: OPTIONAL MATCH with no WHERE
- T5: RETURN of bare ref id (no JOIN)

#### 5.5.2 Ref-join forms (T6–T12)

- T6: Field traversal through ref, single use
- T7: Field traversal through ref, multiple uses (use-site dedup)
- T8: Bare ref + field traversal mix (one JOIN, both uses served)
- T9: Depth-2 ref chain (`a.refX.refY.field` — at the cap)
- T10: Depth-3 ref chain (must reject at adapter load)
- T11: Untyped ref field (must reject at adapter load)
- T12: NULL ref short-circuits to NULL on joined fields

#### 5.5.3 OPTIONAL+WHERE lowering (T13–T18)

- T13: **required-MATCH source, ref-traversal in WHERE** — predicate
  stays in WHERE; rows with NULL ref are excluded (canonical case
  from §5.4.2 Conformance test 1)
- T14: **OPTIONAL MATCH source, ref-traversal in WHERE** — predicate
  hoists to ON; rows with NULL ref preserved with NULL on joined
  fields (Conformance test 2)
- T15: required-MATCH source, ref in RETURN only (no WHERE filter)
  — JOIN happens, NULL refs preserved (no filter to reject them)
- T16: OPTIONAL MATCH source, no WHERE — JOIN happens, all source
  rows preserved
- T17: required-MATCH source with multiple WHERE predicates on the
  same ref — all stay in WHERE; row excluded if any fail
- T18: Mixed — required-MATCH source A, OPTIONAL MATCH source B,
  WHERE predicates on both — A's stay in WHERE, B's hoist to ON

#### 5.5.4 Stale-tag traversal (T19–T22)

- T19: `WHERE n.stale.reason = 'environmental'` — reads stale tag
  from primary alias
- T20: `WHERE n.refField.stale.reason = 'environmental'` — ref-
  traversal then stale tag (depth-1 ref + stale field selector,
  within depth cap)
- T21: Fresh notes return with `stale = NULL`
- T22: Stale notes return with structured `stale: { reason, because, fired_at }`

#### 5.5.5 Allowed functions in WHERE (T23–T25)

- T23: `WHERE alias.field >= coalesce($param, <literal>)` — accepted;
  null param folds to literal default before predicate evaluation
- T24: `WHERE coalesce(alias.field, 'fallback') = $param` — **must
  reject** at adapter load (function applied to a note field, not
  a param)
- T25: `WHERE alias.field = upper($param)` — **must reject**
  (function outside allowed set)

#### 5.5.6 Forbidden constructs (T26–T31) — must reject

- T26: String concatenation in WHERE — reject at adapter load
- T27: Subquery — reject
- T28: DDL (`CREATE`, `MERGE`, `SET`, `DELETE`) — reject
- T29: Function outside allowed set — reject
- T30: Variable-length pattern with no upper bound — reject
- T31: Variable-length pattern exceeding declared `max_depth` — reject

### 5.6 Modeling guidance

When both a ref and an edge would work:

- **Ref** for: single-valued, no metadata, inherent property of source
  (character → stated_location, evidence → owner, control → policy)
- **Edge** for: many-to-many, metadata-bearing, bidirectional, signed
  (character ↔ event via present_at, control ↔ regulation via
  satisfies, faction ↔ faction via allied_with)

---

## 6. Planner hints

Adapter authors declare intent; `da` chooses execution strategy.

| Hint | What it does |
|---|---|
| `algorithm_hint: bidirectional_bfs` | Bidirectional BFS for shortest-path queries (collapses k^d to 2·k^(d/2)) |
| `algorithm_hint: dijkstra` | Dijkstra for weighted edges (adapter must declare `weight_field` on the edge) |
| `algorithm_hint: astar` | Dijkstra + heuristic (adapter provides heuristic via skill ref) |
| `materialize: transitive_closure_of: [<edges>]` | Precompute (ancestor, descendant) pairs at bootstrap; refresh on staleness driver fire |
| `materialize: neighborhood_summary_for: [<types>]` | Precompute per-node edge-type counts and adjacent-type histograms |
| `index_hints: [{predicate, order}]` | Build the named permutation index (one of SPO/SOP/PSO/POS/OSP/OPS) |
| `cardinality_stats: { auto: true }` | Compute per-(predicate, object) cardinality at bootstrap; planner uses for join ordering |

`da` is permitted to override or supplement hints based on observed
query performance. Adapter hints are never load-bearing on
correctness — only performance.

---

## 7. Staleness driver binding

The adapter's `staleness_drivers` list names which scoped-KG drivers
(per scoped-KG §2.5) fire on this domain's nodes. Drivers not listed
do not fire. When an adapter declares a driver, the scope chain it
writes to must have that driver enabled — mismatches fail loud at
adapter load time.

### 7.1 Environmental driver vs review-nudge

- **Environmental driver** is predicate-based. The adapter declares
  `env_predicates` on a note type; an external trigger
  (`da kg trigger --env <kind> <args>`) fires the driver against
  matching predicates; matching notes are tagged
  `stale: { reason: "environmental", because: [<trigger-id>] }`.
- **Review-nudge** is time-based (per scoped-KG §2.7). It is a
  **separate dimension** from staleness — review-nudged notes are
  still fresh, just tagged `review_due: true`.

Adapters that want evidence to expire (compliance) use environmental
drivers with a `time_after` predicate fired by a recurring `da kg
trigger --env time_after <field>` cron. Adapters that want reviewers
to periodically re-confirm a fact use review-nudge.

### 7.2 Predicate kinds

| Kind | Fires when | Adapter declares |
|---|---|---|
| `time_after` | A clock observation crosses a date field | `field: <date-field-name>` |
| `module_version` | An external version trigger fires | `module: <name>, range_field: <field>` |
| `webhook` | An HTTP POST to `da kg trigger --env webhook <endpoint>` | `endpoint: <name>` |
| `custom` | An adapter-defined trigger | `kind: custom, handler_skill: <skill-ref>` |

### 7.3 Environmental → derivation propagation

When an environmental driver fires on node X, the propagation to
downstream notes follows the rule from scoped-KG §2.6:

1. X is tagged `stale: { reason: "environmental" }`
2. `derivation_mutation` driver fires on every note reachable from X
   via a derivation path. Derivation paths are:
   - A declared `derivation_from` cite on the downstream note
   - A `NoteSymbolLink` edge with a load-bearing `LinkKind`
   - An adapter-declared **edge type** marked `derivation: true`
   - An adapter-declared **ref field** marked `derivation: true` (new
     in v4; refs and edges propagate uniformly)
3. Reachable notes are tagged `derivation-stale` per the bounded
   propagation rules in scoped-KG §2.6

**Stale tag schema on returned notes.** Every note returned by a query
carries a `stale` field whose shape is the standard scoped-KG §3.5
payload:

```
stale: {
  reason: "source" | "derivation" | "revocation" | "contradiction" | "environmental",
  because: [<node_id or trigger_id>, ...],
  fired_at: <timestamp>
}
```

When a note is fresh, the `stale` field is omitted (or `null` per
backend convention). The DSL exposes `stale` as a structured field
readable via `<alias>.stale.<subfield>` (e.g., `n.stale.reason`,
`n.stale.fired_at`). Ref-traversal applies — `c.derives_from.stale.reason`
is a depth-2 ref-traversal that resolves the `derives_from` ref and
reads `stale.reason` on the resolved node. (Subject to the depth cap
in §5.4.1; one ref hop + one structured field selector counts as
depth-1 traversal for the purposes of the cap.)

The adapter's `impact_radius` query, when invoked, surfaces both
source-stale and derivation-stale notes via this `stale` field.
Queries that explicitly want derivation-stale notes filter on
`WHERE n.stale.reason = 'derivation'`.

**Propagation requires `derivation: true` to be declared on the relevant
edges or ref fields.** A ref-traversal does not propagate derivation
by default — adapters opt in per relationship. This avoids the
unbounded-taint failure mode scoped-KG §2.6 warned about. Refs without
`derivation: true` remain pure structural pointers used for query
ergonomics only.

---

## 8. Constrained materialization API and namespace enforcement

### 8.1 Physical namespace separation

Each adapter gets physically separate storage in the underlying scope
backend:

| Backend | Mechanism |
|---|---|
| sqlite | Each adapter is a separate database file; attached via `ATTACH DATABASE 'kg_<adapter>.db' AS <adapter>`; queries reference `<adapter>.notes` |
| postgres | Each adapter is a Postgres `SCHEMA`; queries reference `<adapter>.notes` |
| http | Each adapter has a distinct path prefix `/v1/adapters/<adapter>/notes`; the backend rejects requests outside the granted adapter scope |

Naming conventions are not the enforcement mechanism. The storage
backend itself rejects cross-namespace operations that the granted
capability does not authorize.

### 8.2 Namespace tokens

`da` core grants each adapter operation a **namespace token** that
encodes a **set** of authorized (namespace, mode) pairs. The token is
not a single-adapter capability; it is a multi-namespace capability
derived from the executing adapter's own namespace plus any cross-
namespace dependencies declared in the operation being authorized.

**Token shape:**

```
token: {
  primary_adapter: <name>,           # the adapter executing the operation
  authorized: [
    { namespace: <name>, mode: "read" | "write" },
    ...
  ],
  issued_for: <operation-name>,      # query name, view name, or "bootstrap"
  expires_at: <timestamp>            # short-lived; one operation
}
```

**Token derivation rules:**

- For an adapter's own queries against its own namespace: token grants
  `{ adapter, "read" }` (and `"write"` if the operation is a write).
- For an adapter's bootstrap skill: token grants `{ adapter, "write" }`
  for the adapter's own namespace.
- For a materialized view that declares `reads_from`: token grants
  `{ adapter, "write" }` for the adapter's own namespace plus
  `{ <dep-adapter>, "read" }` for each adapter named in `reads_from`.
- For impact-radius queries invoked by `da` core: token is derived
  from the adapter's `impact_radius` declaration; if the impact-
  radius query reads only from the adapter's own namespace (the
  common case), token grants only `{ adapter, "read" }`.

**Storage-layer enforcement:** the storage backend validates **every
namespace** referenced in the compiled query plan against the
token's `authorized` set. References to namespaces outside the
authorized set fail before any rows are read. Token validation is
mandatory, not advisory — storage backends that cannot enforce
multi-namespace tokens are not contract-compliant.

**Compiler obligation:** the DSL compiler emits the full set of
namespaces a compiled query touches as part of the query metadata;
the storage layer cross-checks against the token before executing.
A query that references namespaces beyond what `reads_from` declares
fails at compile time, not at runtime.

Adapter-owned MCP servers (per §8.5) **must** use the
`da-adapter-sdk` library to issue queries — the SDK obtains and
attaches tokens scoped to the operation. Direct DB connections by
adapters are forbidden by contract.

#### 8.2.1 Token conformance tests (normative)

Implementations MUST pass these tests:

**Token derivation:**

- N1: Adapter A's own query against A's namespace — token grants
  `{ A, read }` only
- N2: Adapter A's bootstrap — token grants `{ A, write }` only
- N3: Materialized view with `reads_from: [B]` — token grants
  `{ A, write }` + `{ B, read }`
- N4: Materialized view with `reads_from: [B, C]` — token grants
  `{ A, write }` + `{ B, read }` + `{ C, read }`
- N5: Impact-radius query reading only own namespace — token grants
  `{ A, read }` only

**Token enforcement:**

- N6: Adapter A query referencing B's namespace without `reads_from`
  — compile-time rejection
- N7: Adapter A query referencing B's namespace with `reads_from: [B]`
  — compiles; SQL plan validates token; executes
- N8: Adapter A query attempting write to B's namespace — rejected at
  storage layer regardless of token
- N9: Adapter A query with token granting `{ B, read }`, query plan
  somehow references C — storage layer rejects (defense in depth)
- N10: Token expiry — operations after `expires_at` rejected; new
  token must be issued

**SDK enforcement:**

- N11: Adapter MCP server bypassing `da-adapter-sdk` and connecting
  directly to DB — rejected by storage layer (no token attached)
- N12: Adapter MCP server using SDK with token scoped to operation —
  succeeds
- N13: Adapter MCP server attempting to forge a token (e.g., crafting
  a token granting wider access than its operation requires) —
  rejected at storage layer (token signature validation)

### 8.3 Cross-adapter reads via `reads_from` (materialized views only)

**Cross-namespace reads are permitted only through materialized views.**
Bootstrap skills and named queries operate only within their adapter's
own namespace. When they need cross-adapter data, they read from a
materialized view that carries the explicit `reads_from` declaration.

This single-declaration-site rule keeps the trust boundary auditable:
every cross-adapter dependency is visible in the adapter YAML's
`materialized_views[*].reads_from` field, and the namespace token
derivation (§8.2) keys off the same field. There is no other surface
where a cross-namespace dependency can be introduced.

**Why not allow cross-adapter reads from bootstrap or named queries
directly?** Two reasons:

1. **Single enforcement point.** Token derivation, lockfile cutover
   tracking (§10), and dependency version pinning all key off
   `materialized_views[*].reads_from`. Adding `bootstrap_reads_from`
   and `queries[].reads_from` would multiply the declaration sites
   without adding capability — anything those constructs would
   express can be expressed by writing a materialized view that
   serves the same data.
2. **Cutover coordination needs a stable read surface.** Per §10.3,
   when a dependee adapter bumps its schema, dependent views are
   the unit that gets validated and rebuilt. Bootstrap operations
   are point-in-time; named queries are stateless. Materialized
   views are the durable cross-adapter surface that the cutover
   protocol can manage.

**Pattern:** when a bootstrap skill or named query needs cross-adapter
data, declare a materialized view that reads it, then read from the
view in the bootstrap or query. Example: a bootstrap that wants to
prioritize indexing based on CRG signal materializes a view declaring
`reads_from: [crg]`, then reads from the view to drive its work.

```yaml
materialized_views:
  - name: controls_with_changed_function_evidence
    description: |-
      Controls whose cited evidence references a function that has
      changed in CRG since the evidence was collected.
    reads_from:
      - { adapter: crg, version: ^1.0,
          note_types: [function],
          edge_types: [defines] }
      - { adapter: compliance-register, version: ^1.0,
          note_types: [evidence, control],
          edge_types: [cited_by, references] }
    refresh_on:
      - { adapter: crg, driver: source_mutation, on_types: [function] }
      - { adapter: compliance-register, driver: source_mutation, on_types: [evidence] }
    query: |-
      MATCH (e:evidence)-[:references]->(f:function)
      MATCH (e)-[:cited_by]->(c:control)
      WHERE f.last_changed > e.collected_at
      RETURN c.id, e.id, f.id
```

```yaml
materialized_views:
  - name: controls_with_changed_function_evidence
    description: |-
      Controls whose cited evidence references a function that has
      changed in CRG since the evidence was collected.
    reads_from:
      - { adapter: crg, version: ^1.0,
          note_types: [function],
          edge_types: [defines] }
      - { adapter: compliance-register, version: ^1.0,
          note_types: [evidence, control],
          edge_types: [cited_by, references] }
    refresh_on:
      - { adapter: crg, driver: source_mutation, on_types: [function] }
      - { adapter: compliance-register, driver: source_mutation, on_types: [evidence] }
    query: |-
      MATCH (e:evidence)-[:references]->(f:function)
      MATCH (e)-[:cited_by]->(c:control)
      WHERE f.last_changed > e.collected_at
      RETURN c.id, e.id, f.id
```

Cross-namespace reads outside a `reads_from` declaration are rejected
at adapter load. The version range pins compatibility — see §10 for
how cutover handles version drift.

### 8.4 Bootstrap-skill SDK contract (normative)

The bootstrap skill is an OCI-distributed Tier 2 package. It runs in
`da`'s skill-execution sandbox with restricted I/O. Bootstrap skills
MUST use the `da-adapter-sdk` library; direct DB connections are
forbidden (per §8.2).

#### 8.4.1 SDK surface

| Operation | Purpose |
|---|---|
| `sdk.write_notes(notes)` | Bulk-write notes to the adapter's own namespace |
| `sdk.write_edges(edges)` | Bulk-write edges |
| `sdk.query(dsl, params)` | Execute a DSL query (read-only) within the adapter's own namespace; returns rows |
| `sdk.materialize_view(view_name, dsl, params)` | Compute and persist a view; SDK derives the appropriate multi-namespace token from the view's `reads_from` declaration |
| `sdk.declare_predicate_fired(predicate, args)` | Fire an env predicate (for adapters that integrate with their own webhook receivers) |

All SDK operations attach the appropriate namespace token derived
from the adapter's declared schema, the `reads_from` declarations of
the operation's target view (if any), and the operation kind.

#### 8.4.2 What the SDK does NOT expose

- Direct DB connection — forbidden by §8.2
- Raw SQL — forbidden by §2.2
- Cross-namespace writes — namespace tokens prevent
- Adapter-config mutation — bootstrap reads schema declarations, does
  not modify them
- Cross-namespace reads from named queries or bootstrap operations
  themselves — only materialized views can declare `reads_from`
  (per §8.3)

#### 8.4.3 Failure modes

- Bootstrap skill exits non-zero — adapter activation fails; lockfile
  reverts to previous state
- Bootstrap skill writes notes failing schema validation — write is
  rejected; bootstrap is responsible for handling
- Bootstrap skill exceeds time budget (default 30 min, configurable)
  — skill is killed; adapter activation fails

### 8.5 Three escape hatches before v1.5

When a user wants a query the adapter's named templates don't cover,
three options exist before the DSL itself is extended:

1. **Adapter ships more named queries.** Author publishes a new minor
   version with the requested query as an additional template.
2. **Adapter ships materialized views via the §8 API.** Same DSL,
   `materialized_views` output target, explicit `reads_from`
   declarations.
3. **Adapter exposes its own MCP server.** The adapter is the
   authority on its domain. MCP server must use `da-adapter-sdk` for
   storage access (per §8.2). The CRG adapter is expected to follow
   this pattern for the rich code-graph queries it ships today.

---

## 9. Distribution and tiers

| Artifact | Tier | Distribution | Activation |
|---|---|---|---|
| Adapter YAML (schema + queries + hints + driver list + view definitions) | 1 (config layer) | git/http/local source via `sources` + `extends` | **Pinned in lockfile per §10; activation requires schema-compatibility check; not passive config sync when schema changes** |
| Bootstrap skill | 2 (executable package) | OCI artifact via `packages` | Code-executable; high blast radius; needs digest pinning per external-agent-sources |

Built-in adapters (`crg`, `none`) ship inside `da` and require no
external resolution; their refs use the synthetic `dotagents-builtin`
source.

A team layer that declares
`graph_backend: team-config:graph/compliance-register@^1.0` propagates
to every repo inheriting via `extends`. An org layer can compose
overlays. Sandbox/CI inherit transparently.

---

## 10. Adapter schema migration and cutover

Schema-changing adapter releases are activated like package installs,
not passive config sync. The cutover protocol distinguishes
**within-adapter** state (notes, own views) from **cross-adapter**
state (dependent views).

### 10.1 Lockfile schema pinning

The lockfile (per config-distribution-model §7) gains per-adapter
state including a per-view state machine for cross-adapter cutover:

```
adapters:
  acme-config:graph/compliance-register@1.2.3:
    source_digest: sha256:abc...        # adapter YAML content hash
    schema_digest: sha256:def...        # canonical hash over note_types,
                                        #   edge_types, env_predicates,
                                        #   ref-field derivation flags
    activated_at: 2026-05-09T12:00:00Z
    materialized_views:
      controls_with_changed_function_evidence:
        view_digest: sha256:ghi...
        view_status: ready              # see §10.3 state machine
        depends_on:
          - { adapter: crg, schema_digest: sha256:jkl..., version: 1.0.5 }
          - { adapter: compliance-register, schema_digest: sha256:def..., version: 1.2.3 }
        last_rebuilt_at: 2026-05-09T12:01:00Z
        last_validation_at: 2026-05-09T12:01:00Z
```

The `depends_on` block records the dependee's schema digest **at the
time the view was last rebuilt and validated**. This is the local
ledger for cross-adapter coordination.

#### 10.1.1 `view_status` enumeration (normative)

The `view_status` field is a string enum with exactly four values:

```
view_status: ready | pending-recompat-check | pending-rebuild | dsl-update-required
```

Transitions follow the state machine in §10.3.1; no other transitions
are valid. Implementations MUST record state transitions in a
per-view audit log:

```
materialized_views:
  controls_with_changed_function_evidence:
    view_status: ready
    state_history:
      - { at: <ts>, from: null,                     to: ready,                  trigger: initial-bootstrap }
      - { at: <ts>, from: ready,                    to: pending-recompat-check, trigger: dependee-bump:crg@1.1.0 }
      - { at: <ts>, from: pending-recompat-check,   to: pending-rebuild,        trigger: dsl-validation-passed }
      - { at: <ts>, from: pending-rebuild,          to: ready,                  trigger: bootstrap-rebuild-complete }
```

State history is bounded — implementations keep the last 20 entries
per view, then truncate oldest.

#### 10.1.2 Atomic-update contract (normative)

Lockfile updates use the standard config-distribution-model atomic
write protocol (§7 of that spec): write to a temp file alongside the
target, fsync, rename. State machine transitions either succeed
(lockfile updated) or fail (lockfile unchanged) — never half-applied.

#### 10.1.3 Fail-closed reconciliation (normative)

On every `da` startup and every `da config sync`, `da` cross-checks
the lockfile against on-disk view-table state. The required reactions:

| Lockfile state | View tables present | Required action |
|---|---|---|
| `ready` | yes, schema digest matches `view_digest` | OK; no action |
| `ready` | no, OR digest mismatch | Inconsistency. Log warning. Force `view_status: pending-rebuild`. |
| `pending-rebuild` | (any) | OK; no action; bootstrap will rebuild |
| `pending-recompat-check` | (any) | Re-run validation immediately; transition per §10.3 |
| `dsl-update-required` | (any) | OK; gate active; wait for dependent update |

Reconciliation is read-only with respect to view rows — it never
deletes view tables, only flips lockfile state.

#### 10.1.4 Command surface (normative)

The following commands are part of the contract:

| Command | What it does |
|---|---|
| `da kg lockfile show --adapter <name>` | Print per-adapter lockfile state including all views |
| `da kg lockfile reconcile` | Force a reconciliation pass (normally automatic on startup/sync) |
| `da kg view rebuild --view <name>` | Force-rebuild a specific view; transitions `pending-rebuild → ready` |
| `da kg view validate --view <name>` | Force DSL validation against current dependee schemas; transitions `pending-recompat-check → pending-rebuild` or `→ dsl-update-required` |

There is intentionally **no** `da kg view ack-breaking-change` or
similar — the cutover gate is mechanical per §10.3, not procedural.

### 10.2 Within-adapter cutover (own schema, own notes, own views)

When an adapter's own schema changes:

1. **Read-freeze on the adapter's namespace.** Block writes during
   migration; reads can either block or route to the old schema
   version (reader's choice via opt-in flag; default is route-to-old)
2. **Compatibility check.** If the new schema is a superset of the
   old (added optional fields, added types, added env_predicates), no
   migration skill is needed; proceed to step 5.
3. **Run migration skill** (if declared). Skill reads notes at old
   version, transforms, writes back with new `adapter_schema_version`
   tag. Skill reports per-note status.
4. **Detect own materialized views referencing migrated note types.**
   Mark each as `view_status: stale-needs-rebuild`.
5. **Rebuild own materialized views** in dependency order (views that
   read from other views are rebuilt last).
6. **Behavior-preservation gate** (§6.2 of app-type-profiles, extended
   to schema changes). Run impact_radius and named queries against the
   pre-migration corpus; pass→fail outcome regressions block
   activation.
7. **Flip schema_digest in lockfile.** The new schema is now active.
8. **Unfreeze writes.**

### 10.3 Cross-adapter cutover (dependent owns its consistency)

When a dependee adapter (e.g., CRG) bumps a version:

**Key principle:** the dependee never needs to know about its dependents.
Coordination lives in the consumer's local lockfile, not at publication
time. This handles the public-dependee / private-dependent case where
the dependee publisher has no visibility into who depends on it.

**The gate is mechanical, not procedural.** There is no human "ack"
step. The dependee activation gate releases when the dependent
adapter ships a new version whose DSL queries validate against the
new dependee schema — that's the only state transition that opens
the gate. Operators publish dependent updates; the validation runs
automatically; the gate releases when validation passes.

#### 10.3.1 Per-view state machine

```
              dependee bump pulled
ready ────────────────────────────────────────► pending-recompat-check
                                                       │
                              da validates dependent's │ DSL queries
                              against new dependee schema
                                                       │
                          ┌────────────────────────────┴───────────────────────────┐
                          │                                                        │
                  validation passes                                       validation fails
                  (queries compile, types check)                          (DSL references fields/types
                          │                                                that no longer exist)
                          │                                                        │
                          ▼                                                        ▼
                  pending-rebuild                                          dsl-update-required
                  (view tables stale; rebuild scheduled)                   (BLOCKS dependee activation)
                          │                                                        │
              bootstrap rebuilds view rows                          dependent author publishes
                          │                                         new adapter version with
                          ▼                                         DSL queries fixed for new
                       ready                                        dependee schema
                          ▲                                                        │
                          └────────────────────────────────────────────────────────┘
                                  consumer pulls dependent update;
                                  validation re-runs; passes; back to pending-rebuild
```

#### 10.3.2 Activation flow

1. **Consumer pulls the new dependee version** via normal config sync.
2. **Lockfile validation runs before dependee activation.** `da` walks
   every `materialized_views[].depends_on` entry in the lockfile. For
   each dependent view that names the upgrading dependee:
   - If the new dependee `schema_digest` matches the recorded one, no
     action required.
   - Otherwise, transition the view to `pending-recompat-check` and
     run DSL validation: compile the view's query against the new
     dependee schema, check that all referenced note types, fields,
     edges, and ref types still exist with compatible signatures.
   - **If validation passes:** transition to `pending-rebuild` (the
     view tables are now stale relative to the new schema; bootstrap
     refresh re-builds them). Dependee activation proceeds.
   - **If validation fails:** transition to `dsl-update-required`
     and **block dependee activation**. The block remains until the
     dependent adapter ships a new version whose DSL queries pass
     validation. There is no operator override — fixing the broken
     queries is the unblocking action.
3. **Dependent fix path.** The dependent adapter author publishes a
   new version with DSL queries updated for the new dependee schema.
   When the consumer pulls that update, validation re-runs
   automatically. If passing, the view transitions to
   `pending-rebuild`; bootstrap rebuilds it; transitions to `ready`;
   dependee activation proceeds.

**Why mechanical:** acks introduce two failure modes the mechanical
gate avoids: (a) deadlock if the dependent is abandoned (no one
acks); (b) silent bypass if operators learn to ack reflexively. The
mechanical gate releases on observable state — DSL validation
passing — so it is reproducible and auditable.

#### 10.3.3 Stale-view query semantics

While a view is in `pending-recompat-check`, `pending-rebuild`, or
`dsl-update-required`, queries against it fail loud:

```
[view-unavailable: controls_with_changed_function_evidence]
  state:        dsl-update-required
  depended_on:  crg@^1.0 (recorded schema sha256:jkl...)
  current:      crg@1.1.0 (schema sha256:mno...)
  reason:       view query references field `function.last_changed`
                which was renamed to `function.modified_at` in crg@1.1.0
  unblock:      publish a new compliance-register adapter version
                with the view query updated for crg@1.1.0
```

The dependent author can opt-in to fall-through-to-source-of-truth:
queries against a stale view fall through to the underlying join
(slower but always correct), while the view rebuilds in the
background. Default is fail-loud. Fall-through is only available
when DSL validation passes (i.e., view is in `pending-rebuild`,
not `dsl-update-required`) — a query whose compiled form references
non-existent fields cannot fall through.

### 10.4 Public-dependee / private-dependent

In the cross-app case where the dependee is published publicly and the
dependent is private to an org, the protocol works without any
coordination at publication time:

1. Dependee publisher releases `crg@1.1.0` to the public source. No
   awareness of downstream consumers required.
2. Private consumer's `da config sync` pulls `crg@1.1.0`. The
   consumer's local lockfile has dependent views (e.g., a private
   compliance adapter view that reads from CRG).
3. Consumer's local pre-activation check (§10.3 step 2) detects the
   dependee schema change and either auto-marks dependent views as
   pending-recompat or blocks activation if the change breaks the
   `reads_from` version range.
4. Consumer's private adapter handles its own rebuild via its own
   bootstrap skill.

The dependee publisher never knew or needed to know. **All three
states** — notes, views, cross-adapter deps — **are kept consistent by
the consumer's local lockfile and the dependent adapter's
self-managed rebuild path.**

### 10.5 Behavior-preservation gate extension

The app-type-profiles §6.2 gate extends to schema changes. The corpus
must include notes from prior schema versions. The new adapter must
produce identical query results on them via the impact_radius query
and any shipped named queries. Pass→fail outcome regressions block
the version bump unless explicitly justified per §6.2's allowed
reasons.

---

## 11. CRG migration path

CRG today is implemented as a separate Python subprocess with its own
SQLite store, accessed through the bridge in `internal/graphstore/crg.go`
and the MCP server at `internal/graphstore/mcp_server.go`. Under this
spec, CRG becomes a kg-native adapter. Bridge decommissioning is gated
on tool-by-tool parity, not just storage unification.

### 11.1 Tool-by-tool parity matrix

The CRG MCP server today exposes operations that go beyond simple
note/edge reads. Each requires an explicit kg-native replacement
before the bridge can be decommissioned.

| Current MCP tool / bridge operation | Replacement under this contract | Parity requirement |
|---|---|---|
| `build` | CRG adapter bootstrap skill performs initial Tree-sitter ingestion; writes to `kg_crg.*` namespace | Bootstrap on a fresh repo produces note/edge counts within ±1% of current `kg build` output |
| `update` | CRG adapter incremental reload; bootstrap skill called with `--mode=incremental --since=<commit>` | Same set of nodes inserted/updated/deleted as current `kg update` |
| `status` | `da graph crg status` — namespace existence, node/edge counts, last-bootstrap-at, schema_digest from lockfile | Identical fields surfaced (with one-time rename of any bridge-specific fields) |
| `impact-radius` | Adapter's `impact_radius` named query | Same result set on a corpus of 100 changed-symbol queries |
| `flows` | Either an additional named query (if expressible in DSL) **or** materialized view + adapter MCP server (if too rich for v1 DSL) | Symbol-flow output equivalent; budget signal logged if implemented as MCP server (per §12) |
| `communities` | Materialized view computed at bootstrap; adapter `materialized_views[community_clusters]` with `refresh_on` driver | Community membership stable across runs at same input |
| `postprocess` | Materialized view(s) computed by bootstrap; one view per derived data shape | Output bytes-equivalent for each derived computation |
| `detect-changes` | Built into `source_mutation` driver; CRG adapter declares `staleness_drivers: [source_mutation]` and the driver fires on note hash change | `kg changes` output equivalent for a corpus of branch-switch test cases |

### 11.6 Parity test corpus and criteria (normative)

For each row in §11.1, the parity test runs both implementations
(current bridge + new kg-native adapter) against a corpus:

- 100 commits from `master` (pinned commits, deterministic)
- For each commit, run the current bridge tool and the kg-native
  replacement
- Compare outputs (allowing minor field renames documented in the
  parity row)

Per-row parity criteria:

| Row | Pass criterion |
|---|---|
| build | Bootstrap on commit C produces note/edge counts within ±1% of `kg build` on commit C |
| update | Bootstrap incremental from C1 → C2 produces same set of upserts/deletes as `kg update` |
| status | Output fields equivalent (one-time rename of bridge-specific fields documented) |
| impact-radius | Result set identical for 100 changed-symbol queries (same node set, may differ in order) |
| flows | Symbol-flow output equivalent; if implemented as MCP server, log §12 budget signal |
| communities | Community membership stable across runs at same input commit; cluster IDs may differ but partitions equivalent |
| postprocess | Output bytes-equivalent for each derived computation |
| detect-changes | `kg changes` output equivalent for 50 branch-switch test cases |

The bridge subprocess and `crg-bridge` adapter are decommissioned
together when (per §11.4): all eight rows pass in CI for 3 consecutive
weeks, the behavior-preservation gate (§6.2 of app-type-profiles)
passes on a corpus of 100 recent code-review tasks, a migration plan
exists for any out-of-tree consumer of the bridge commands (`da
workflow drift` flags consumers that have not migrated), and zero
materialized views in any consumer's lockfile declare `reads_from:
[crg-bridge]`.

### 11.2 Migration-only `crg-bridge` adapter (dual-read parity surface)

During dual-read mode (defined in §11.3), the legacy Python CRG
subprocess + SQLite store remain accessible. Offline parity corpus
testing (§11.6) catches systematic divergence but cannot detect
repo-local drift after retries, partial incremental updates, or
failed rebuilds. Operators and migration tooling need a way to
compare bridge state against new kg-native state for the **current
repo, right now**.

§8.3 (cross-adapter reads only via materialized views) is otherwise
the right model for adapter-to-adapter reads, but it forecloses the
live-comparison path the migration needs. To resolve this without
relaxing §8.3, `da` ships a temporary read-only adapter modeling the
bridge:

```yaml
# dotagents-builtin:graph/crg-bridge@^0.x — MIGRATION ONLY
name: crg-bridge
version: 0.1.0
migration_only: true                    # consumers must not depend on this long-term
description: |-
  Read-only adapter exposing the legacy Python CRG bridge state under
  a stable namespace, so migration tooling can compare bridge output
  to the new kg-native CRG adapter via materialized views. Retires
  when the §11.4 decommissioning gate passes.

# Schema mirrors the kg-native CRG adapter's note/edge types one-to-one
# so view queries can MATCH the same shapes against both adapters.
note_types:
  - name: function
    fields: [ ... mirror crg adapter ... ]
  - name: type
    fields: [ ... mirror crg adapter ... ]
  # etc.

edge_types:
  # mirror crg adapter

# No impact_radius — this adapter is not for review-pipeline use.
# It exists solely as a read target for parity views.
impact_radius:
  query: |-
    RETURN $changed_ids AS id
  max_depth: 0

# No drivers — bridge state mutation is observed externally, not
# tracked through KG driver events.
staleness_drivers: []

# Bootstrap reads from the bridge's SQLite store at the bridge's
# documented path; writes are forbidden by the adapter contract.
bootstrap_skill: crg-bridge-mirror@^0.1
```

**Contract properties:**
- `migration_only: true` is a first-class adapter field (added in
  v4.2). Adapter authors writing new long-term adapters must not
  declare `reads_from` against any `migration_only` adapter — the
  loader rejects this at adapter load
- The bridge adapter is read-only at the storage layer: namespace
  tokens granted for `crg-bridge` are always `mode: read`, never
  `mode: write`
- Compliance, research, or other adapters that need to compare
  bridge state to new kg-native state during migration write
  materialized views declaring `reads_from: [crg, crg-bridge]` —
  exactly the same machinery as any other cross-adapter view
- When the §11.4 decommissioning gate passes, the bridge adapter is
  removed from `da` core. Any view still declaring `reads_from:
  [crg-bridge]` fails the §10.3 dependee-bump validation and is
  flagged via the standard cutover protocol — adapter authors update
  their views to drop the bridge dependency

### 11.3 Dual-read mode

Until the §11.4 decommissioning gate passes, the CRG adapter ships in
**dual-read mode**:

- The CRG adapter's bootstrap skill writes to `kg_crg.*` namespace
  (the new kg-native path)
- The `crg-bridge` adapter (§11.2) exposes the legacy bridge state
  under `kg_crg-bridge.*` namespace, read-only
- `da` core prefers the new `kg_crg` namespace when both are
  available; migration tooling reads from both via parity views
- Both surfaces remain accessible to consumers throughout the
  migration window

### 11.4 Decommissioning gate

The bridge (`internal/graphstore/crg.go` subprocess machinery) and
the `crg-bridge` adapter are decommissioned together, only when:

1. All eight rows in the §11.1 parity matrix have a passing
   conformance test in CI
2. The behavior-preservation gate (§6.2 of app-type-profiles) passes
   on a corpus of recent code-review tasks that consumed CRG output
3. A migration plan exists for any consumer of `kg build|update|status|
   code-status|changes|impact|flows|communities|postprocess` that does
   not route through the documented tool surface
4. Zero active materialized views in any consumer's lockfile declare
   `reads_from: [crg-bridge]` — verified by sweeping lockfiles across
   the dot-agents-managed repo set (per `workflow drift`)

Until all three are satisfied, the CRG adapter ships in **dual-read
mode**: bootstrap writes to the new kg-native namespace, the bridge
remains active for tools without parity, and `da` core prefers the
new namespace when both are available.

### 11.5 `go-native-code-graph-analysis` re-scope

That spec was framed against the assumption CRG remains a separate
process with its own SQLite store. Under this contract:

- The "Python subprocess vs Go-native" architectural question
  collapses to "which Tree-sitter binding does the CRG adapter's
  bootstrap skill use?"
- The "storage and schema ownership" analysis (§2 of that spec)
  collapses to the schema this adapter declares
- The "build and update pipeline" question becomes the bootstrap
  skill's lifecycle, identical to any other adapter
- The "MCP and skill parity" question is exactly §11.1

That spec is marked `superseded-in-part`: scope reduced to (a) the
Tree-sitter binding choice and (b) the bootstrap-skill design. The
broader Python-vs-Go architecture question is superseded by §11 of
this spec.

---

## 12. v1.5 trigger budget

Promote the named-query DSL to a richer pattern-match DSL only when an
accumulating budget threshold is reached.

### 12.1 Signal sources and weights

| Signal | Weight |
|---|---|
| Adapter ships an additional named query that's clearly a workaround for a missing primitive | 1 |
| Adapter ships a materialized view to express what the DSL can't | 1 |
| Adapter spawns its own MCP server primarily for query expressiveness (not domain-specific tooling) | 2 |
| Adapter author logs a wishlist entry tagged `needs-author-unanticipatable-composition` | 2 |
| Same conceptual pattern appears as a workaround in 3+ places across all adapters | 3 |
| Single adapter reports a blocker-class request (user cannot proceed without it) | 3 |

### 12.2 Thresholds

- **5 points** → opens v1.5 design review
- **8 points** → starts v1.5 implementation work

### 12.3 Dispute mechanism

An adapter author can dispute a signal by writing a justification
("this is a legitimately domain-specific pattern, not a missing core
primitive"). Successful disputes remove the points but log the dispute
itself as evidence — repeated dispute patterns become signal in their
own right.

### 12.4 v1 blocker fast-path

The accumulating-budget machinery in §12.1–§12.3 is for non-blocking
*richness* requests — points add up, design review opens at 5,
implementation at 8. A verified **blocker** — a user or adapter
provably cannot proceed without the missing primitive, and no v1
workaround exists — bypasses the budget entirely and is scoped into
the **v1 schedule**, not deferred to v1.5.

A blocker-class request goes through this fast-path when:

1. The adapter author or a `da` core maintainer files a blocker
   declaration (one sentence + link to the failing workflow / failed
   query / failing test).
2. A `da` core maintainer either confirms or rebuts the blocker on
   the linked artifact. Confirmation triggers an explicit v1 schedule
   addition; rebuttal routes the request back to the §12.1 budget at
   the appropriate weight.
3. If confirmed: the v1 grammar / contract / SDK change required to
   unblock is treated as a v1 amendment (§0 revision-history entry),
   not as a v1.5 promotion.

**Why this exists.** v5's table assigned a single blocker 3 points,
under a 5-point threshold, so a real v1 blocker would wait for an
unrelated second signal to even open *v1.5* review — which is the
wrong tier (it's a v1 blocker). The accumulating budget remains the
right mechanism for *additive* richness; blockers are a different
class and route directly to v1.

---

## 13. Worked examples

### 13.1 Built-in `none` adapter

```yaml
name: none
version: 1.0.0
description: |-
  Null adapter for profiles that do not consume a graph backend.
note_types: []
edge_types: []
impact_radius:
  query: |-
    RETURN $changed_ids AS id
  max_depth: 0
staleness_drivers: []
```

Profiles that select `graph_backend: dotagents-builtin:graph/none@^1.0`
get a no-op impact radius (the changed nodes themselves, no
expansion). Useful for review pipelines where the review skill is
self-sufficient.

### 13.2 Compliance register backend

GRC graph for controls, risks, findings, evidence, regulations.
Stresses every staleness driver including predicate-based
environmental triggers and policy → control derivation propagation.

```yaml
name: compliance-register
version: 1.0.0
description: |-
  GRC graph for controls, risks, findings, evidence, regulations.
  Loads canonical SOC2/NIST/HIPAA frameworks at bootstrap; tracks
  evidence expiry and policy review windows via environmental drivers.

note_types:
  - name: regulation
    fields:
      - { name: authority,      type: string }
      - { name: version,        type: string }
      - { name: jurisdiction,   type: enum, values: [us, eu, uk, global] }
      - { name: effective_date, type: date }

  - name: control
    fields:
      - { name: control_id,        type: string }
      - { name: framework,         type: string }
      - { name: status,            type: enum, values: [effective, degraded, failed, untested] }
      - { name: owner,             type: ref<owner> }
      - { name: derives_from,      type: ref<policy>, derivation: true }   # ref-field derivation per §7.3

  - name: risk
    fields:
      - { name: severity,    type: enum, values: [low, medium, high, critical] }
      - { name: likelihood,  type: enum, values: [rare, possible, likely, certain] }
      - { name: status,      type: enum, values: [open, accepted, mitigated, transferred] }
      - { name: owner,       type: ref<owner> }

  - name: finding
    fields:
      - { name: source,    type: enum, values: [audit, pentest, incident, self_assessment] }
      - { name: severity,  type: enum, values: [low, medium, high, critical] }
      - { name: status,    type: enum, values: [open, remediated, accepted, false_positive] }
      - { name: opened_at, type: date }

  - name: evidence
    fields:
      - { name: artifact_url, type: string }
      - { name: collected_at, type: date }
      - { name: expires_at,   type: date }
      - { name: type,         type: enum, values: [screenshot, log, policy_doc, test_result, attestation] }
    env_predicates:
      - kind: time_after
        field: expires_at

  - name: policy
    fields:
      - { name: version,            type: string }
      - { name: last_reviewed_at,   type: date }
      - { name: review_window_days, type: int }
    env_predicates:
      - kind: webhook
        endpoint: policy.review_due

  - name: owner
    fields:
      - { name: name,  type: string }
      - { name: team,  type: string }
      - { name: role,  type: string }

edge_types:
  - { name: satisfies,  from: control,    to: regulation, cardinality: many-to-many }
  - { name: mitigates,  from: control,    to: risk,       cardinality: many-to-many }
  - { name: cited_by,   from: evidence,   to: control,    cardinality: many-to-many,
      derivation: true }                     # evidence staleness propagates to controls
  - { name: affects,    from: finding,    to: control,    cardinality: many-to-many }
  - { name: supersedes, from: regulation, to: regulation, cardinality: many-to-one,
      derivation: true }                     # regulation supersession propagates to satisfies-edge controls
  - { name: contradicts, from: finding,   to: control,    cardinality: many-to-many }
```

The `derives_from` ref on `control` is declared `derivation: true`
(per the §4 ref-field schema and §7.3 propagation rule). When a
policy is environmentally invalidated (webhook fires), `control`
notes whose `derives_from` ref points at that policy are tagged
`derivation-stale`. The propagation walks ref fields and edges
uniformly, so this declaration is enough to make the policy → control
trace work end-to-end without an explicit `derives_from` edge type.

```yaml
impact_radius:
  query: |-
    MATCH (changed:control)
    OPTIONAL MATCH (changed)-[:satisfies]->(reg:regulation)
    OPTIONAL MATCH (changed)-[:mitigates]->(r:risk)
    OPTIONAL MATCH (f:finding)-[:affects]->(changed)
    OPTIONAL MATCH (e:evidence)-[:cited_by]->(changed)
    WHERE changed.id IN $changed_ids
    RETURN reg.id, r.id, f.id, e.id, changed.owner.name, hop_count
  max_depth: 2
  algorithm_hint: bidirectional_bfs
  materialize:
    - transitive_closure_of: [satisfies, supersedes]
    - neighborhood_summary_for: [control]
  index_hints:
    - { predicate: satisfies, order: POS }
    - { predicate: cited_by,  order: POS }
    - { predicate: affects,   order: POS }
  cardinality_stats:
    auto: true

staleness_drivers:
  - source_mutation
  - derivation_mutation
  - environmental_trigger
  - explicit_revocation
  - contradiction_arrival

bootstrap_skill: compliance-register-bootstrap@^1.0

queries:
  - name: regulations_satisfied_by
    description: What regulations does control C satisfy?
    params: [{ name: control_id, type: string, required: true }]
    query: |-
      MATCH (c:control)-[:satisfies]->(reg:regulation)
      WHERE c.control_id = $control_id
      RETURN reg.id, reg.authority, reg.version

  - name: stale_evidence_unsupported_controls
    description: |-
      Controls left unsupported because their evidence was invalidated
      by the environmental_trigger driver. Reads stale tags rather than
      computing date filtering at query time.
    params: []
    query: |-
      MATCH (e:evidence)-[:cited_by]->(c:control)
      WHERE e.stale.reason = 'environmental'
      RETURN c.control_id, c.owner.name, e.id, e.stale.fired_at

  - name: policy_review_due_impact
    description: |-
      Controls that derive from a policy whose review-due webhook has
      fired. Demonstrates the env → derivation propagation rule (§7.3):
      policy environmentally invalidated → controls derived_from policy
      tagged derivation-stale → query surfaces them.
    params: []
    query: |-
      MATCH (c:control)
      WHERE c.stale.reason = 'derivation'
        AND c.derives_from.stale.reason = 'environmental'
      RETURN c.control_id, c.owner.name, c.derives_from.id, c.derives_from.version
```

Demonstrates the v3 contract end-to-end:

| Aspect | Demonstration |
|---|---|
| Adapter is just YAML | One config-layer artifact (Tier 1) |
| Impact radius is the load-bearing query | Auditor flips control to `failed` → review pipeline gets uncovered regulations, unmitigated risks, affected findings, orphaned evidence, owner-to-ping |
| All five staleness drivers exercised | source_mutation (status), derivation (regulation supersession + policy → control), environmental (evidence time_after + policy webhook), revocation (finding closure), contradiction (pentest vs. control) |
| Env propagation rule visible | `derivation: true` declared on `cited_by`, `supersedes`, and the `derives_from` ref-edge; `policy_review_due_impact` query traces the path explicitly |
| Ref-joins simplify queries | `c.owner.name`, `c.derives_from.id` — ref-traversal vs explicit edge MATCH |
| Stale tags consumed, not recomputed | Queries read `e.stale.reason = 'environmental'` rather than checking expires_at; the driver does the work |
| Planner hints are intent-shaped | POS index for `satisfies`; transitive closure for `(satisfies, supersedes)` |
| Distribution path concrete | Tier 1 git-sourced base; org-layer overlay; bootstrap as Tier 2 OCI |
| Schema migration in scope | If 1.0 → 2.0 adds required `signed_by` field, §10.2 migration skill backfills before activation |

### 13.3 First dogfood adapter: TTRPG campaign

See `.agents/sandbox/ttrpg-adapter/` for the working adapter:

- `schema.yaml` — 7 note types, 13 edge types, typed refs throughout
- `queries.yaml` — named queries demonstrating v1 boundary cases
- `bootstrap-skill/SKILL.md` — stub for session-log parsing
- `WISHLIST.md` — DM feedback log feeding §12 budget signals

The TTRPG dogfood is the first real-world stress test of the contract
against an open-ended domain (DMs invent new relationships every
session). Compliance has stable, well-documented frameworks; TTRPG
does not. Both together cover the spectrum.

### 13.4 Built-in `citation` adapter

Minimal canonical schema for the citation graph backend. Used by the
`research` profile in app-type-profiles §8.3 and any custom profile
that selects `graph_backend: dotagents-builtin:graph/citation@^1.0`.

```yaml
name: citation
version: 1.0.0
description: |-
  Citation graph for research, knowledge-building, and source-grounded
  writing tasks. Tracks sources, claims, and the cite/support/contradict
  relationships among them.

note_types:
  - name: source
    fields:
      - { name: url,               type: string,                                  required: true }
      - { name: title,             type: string,                                  required: true }
      - { name: author,            type: string,                                  required: false }
      - { name: published_at,      type: date,                                    required: false }
      - { name: accessed_at,       type: date,                                    required: false }
      - { name: reliability_tier,  type: enum, values: [primary, secondary, tertiary], required: false }

  - name: claim
    fields:
      - { name: statement,   type: string, required: true }
      - { name: confidence,  type: float,  required: false }
      - { name: scope,       type: enum, values: [local, working, broad], required: false }

edge_types:
  # `cites` is the canonical derivation edge: a claim citing a source
  # derives its authority from that source. Source mutation propagates
  # to citing claims via this single edge.
  - { name: cites,       from: claim,  to: source, cardinality: many-to-many, derivation: true }
  # `supports` is query-only (no derivation flag) to avoid double-
  # counting in impact_radius — a single source mutation would
  # otherwise tag the same downstream claim twice if it both cites
  # and is supported by the source. Query callers who want the
  # supports relation use the `claims_supported_by_source` named
  # query below.
  - { name: supports,    from: source, to: claim,  cardinality: many-to-many }
  - { name: contradicts, from: claim,  to: claim,  cardinality: many-to-many }

# When a source's content changes (e.g., URL fetched and the document
# was updated), all claims that cite it need re-evaluation. Scoped to
# `cites` only to avoid the Cartesian product `N citers × M supported`
# that combining both relations in one rowset would produce.
impact_radius:
  query: |-
    MATCH (changed:source)
    OPTIONAL MATCH (c:claim)-[:cites]->(changed)
    WHERE changed.id IN $changed_ids
    RETURN c.id, hop_count
  max_depth: 2
  algorithm_hint: bfs
  index_hints:
    - { predicate: cites,    order: POS }
    - { predicate: supports, order: POS }

staleness_drivers:
  - source_mutation         # url-fetched content hash changes
  - derivation_mutation     # cited source mutating propagates to citing claims
  - explicit_revocation     # author retracts a claim
  - contradiction_arrival   # new claim contradicts an existing one

# Bootstrap is optional for citation: research workflows typically
# populate the graph as articles are processed, not from a canonical
# source library.
bootstrap_skill: null

queries:
  - name: claims_citing_source
    description: All claims that cite a given source.
    params: [{ name: source_id, type: string, required: true }]
    query: |-
      MATCH (c:claim)-[:cites]->(s:source)
      WHERE s.id = $source_id
      RETURN c.id, c.statement, c.confidence

  - name: claims_supported_by_source
    description: |-
      Claims that a given source supports. Not derivational (per the
      schema rationale above); callers needing combined "all dependent
      claims" can union with `claims_citing_source` client-side.
    params: [{ name: source_id, type: string, required: true }]
    query: |-
      MATCH (s:source)-[:supports]->(c:claim)
      WHERE s.id = $source_id
      RETURN c.id, c.statement, c.confidence

  - name: contradicting_claims
    description: Claims that contradict a given claim.
    params: [{ name: claim_id, type: string, required: true }]
    query: |-
      MATCH (a:claim)-[:contradicts]->(b:claim)
      WHERE a.id = $claim_id
      RETURN b.id, b.statement, b.confidence
```

Distribution: ships inside `da` under the synthetic `dotagents-builtin`
source. Adapter authors who want a richer citation graph (e.g., adding
journal/venue, doi, author-affiliation modeling) publish their own
adapter via a custom source.

### 13.5 Built-in `document-cross-ref` adapter

Minimal canonical schema for the document cross-reference graph backend.
Used by the `resume-ideation` profile in app-type-profiles §8.4 and any
custom profile that selects
`graph_backend: dotagents-builtin:graph/document-cross-ref@^1.0`.

```yaml
name: document-cross-ref
version: 1.0.0
description: |-
  Document cross-reference graph for structured-artifact workflows
  (resume drafts, design docs, RFCs). Tracks documents, sections, and
  the concepts those sections define or reference.

note_types:
  - name: document
    fields:
      - { name: path,        type: string, required: true }
      - { name: doc_kind,    type: enum, values: [resume, rfc, design_doc, runbook, other], required: false }
      - { name: revised_at,  type: date,   required: false }

  - name: section
    fields:
      - { name: section_path,  type: string, required: true }   # e.g. "H2:Methodology"
      # `in_document` is containment, not derivation. Document-level
      # source_mutation does not cascade through this ref; section-level
      # mutation is tracked on the section's own content hash. Marking
      # this ref derivational would taint every section on any document
      # revision and defeat section-granular review.
      - { name: in_document,   type: ref<document>, required: true }

  - name: concept
    fields:
      - { name: name,         type: string, required: true }
      # `defined_in` IS derivational: a concept's meaning is anchored
      # to its defining section. Section mutation propagates to the
      # concept.
      - { name: defined_in,   type: ref<section>, required: false, derivation: true }

edge_types:
  - { name: references,  from: section, to: section,  cardinality: many-to-many, derivation: true }
  - { name: appears_in,  from: concept, to: document, cardinality: many-to-many }

# When a section changes, what other sections reference it (and
# therefore need re-review for drift)?
impact_radius:
  query: |-
    MATCH (changed:section)
    OPTIONAL MATCH (other:section)-[:references]->(changed)
    OPTIONAL MATCH (concept:concept)
    WHERE changed.id IN $changed_ids AND concept.defined_in.id = changed.id
    RETURN other.id, concept.id, hop_count
  max_depth: 2
  algorithm_hint: bfs
  index_hints:
    - { predicate: references, order: POS }

staleness_drivers:
  - source_mutation
  - derivation_mutation
  - explicit_revocation

bootstrap_skill: null

queries:
  - name: sections_referencing
    description: All sections that reference a given section.
    params: [{ name: section_id, type: string, required: true }]
    query: |-
      MATCH (other:section)-[:references]->(target:section)
      WHERE target.id = $section_id
      RETURN other.id, other.section_path, other.in_document.path

  - name: concepts_in_document
    description: All concepts that appear in a given document.
    params: [{ name: document_id, type: string, required: true }]
    query: |-
      MATCH (c:concept)-[:appears_in]->(d:document)
      WHERE d.id = $document_id
      RETURN c.id, c.name, c.defined_in.section_path
```

Distribution: ships inside `da` under the synthetic `dotagents-builtin`
source.

---

## 14. Open questions

### Q1: v1.5 trigger budget threshold tuning

§12.2 sets thresholds at 5 / 8 points. These are judgment-based
defaults. Re-tune after the first 3 months of TTRPG dogfood data and
after the compliance-register adapter ships.

### Q2: Cross-adapter view dependency version compatibility

§10.3's pre-activation check uses the dependent's `reads_from` version
range to gate dependee upgrades. Open: should dependents be allowed
to declare `accepts_breaking_changes: true` to opt out of the gate
(letting the dependee upgrade and accepting view-stale until manual
rebuild)? Trade-off: faster dependee upgrades vs. potential silent
breakage. Lean: yes, opt-in only, requires acknowledgement in lockfile.

### Q3: Query result caching across schema migration boundaries

The v2 review noted that named-query result caches could become
incoherent during migration. §10.2 step 4 addresses materialized
views; ad-hoc query-result caches (if `da` adds them) would need the
same versioning. Out of scope until query-result caching is on the
roadmap.

### Q4: Explicit `OPTIONAL WHERE` for null-preserving filters on required-MATCH refs

§5.4.2's lowering rule is source-context-aware: required-MATCH
refs keep predicates in `WHERE` (null-rejecting); OPTIONAL MATCH
refs hoist to `ON` (null-preserving). What about the rare case
where an adapter author wants null-preserving filtering on a
required-MATCH ref (e.g., "all characters, with EU-region location
when present, but I want this filter as a soft hint not a hard
filter")? Lean: defer to v1.5+ with an explicit `OPTIONAL WHERE`
construct, so the source-context rule stays unambiguous in v1.

### Q5: Adapter versioning of the DSL itself

When v1.5 ships, adapters written against v1 should continue to work
unchanged. Open: declare DSL version in the adapter YAML
(`dsl_version: 1`)? Compatibility matrix between DSL versions and
adapter declarations? Lean: yes, adapter declares minimum DSL version;
`da` version determines max supported DSL; mismatches fail loud.

### Q6: Adapter MCP server discovery

§8.5 allows adapter-owned MCP servers. Open: how does `da` discover
them? Static declaration in the adapter YAML
(`mcp_server: <skill-ref>`)? Auto-spawn on adapter load? Manual
configuration? Lean: declare the MCP-server skill ref in adapter YAML;
`da` exposes a flag to enable/disable per scope.

### Q7: TTRPG `allegiance_path` grammar gap (deferred to plan)

The v3 dogfood query used DSL constructs (`*0..3` zero-hop, reverse
`<-[:edge]-`, undirected `-[:edge]-`) that §5.1's documented grammar
does not include. v4 rewrites the query to fit current grammar (split
into "same faction" and "multi-hop" cases). The question of whether
to extend §5.1 grammar with these constructs (a v1.5 work item) or
keep adapter authors within v1's directed-positive-hop grammar is
tracked in
[plan §5](../../plans/graph-backend-adapter-contract/graph-backend-adapter-contract.plan.md#5-dsl-grammar-extensions-decision).
Decision deferred until 3 months of adapter dogfood data accumulates
and the §12 budget signals indicate which direction is load-bearing.

---

## 15. Completeness note

This spec graduated from `.agents/proposals/graph-backend-adapter-contract.md`
through three review rounds. The spec is the canonical artifact; the
proposal file is preserved as a historical record of the design
journey.

The spec is **not** auto-applied to `app-type-profiles/design.md`. The
edits in §3.1 of the proposal (revised §2.6, §3.1, §7.3, §8.3/§8.4,
§10 of app-type-profiles) need to be applied as a separate commit.
This spec is the contract reference; the app-type-profiles edits are
the wiring.
