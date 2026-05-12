# Graph Backend Adapter Contract — Spec Amendment Proposal (v2) — ACCEPTED

**Status:** ACCEPTED 2026-05-09 — graduated to canonical sibling spec
**Canonical artifact:** [`.agents/workflow/specs/graph-backend-adapter-contract/design.md`](../workflow/specs/graph-backend-adapter-contract/design.md)
**Written:** 2026-05-09 (v1) / revised 2026-05-09 (v2 post-adversarial-review) / accepted as v3 sibling spec 2026-05-09
**Author:** drafted with agent assist
**Supersedes:** D2 lean from `2026-04-22-payout-configs-and-nonsoftware-generalization.md` handoff

---

## Acceptance note

This proposal went through three review rounds:

- **v1** — initial draft; Codex adversarial review found 6 load-bearing defects
- **v2** — defects addressed; Codex adversarial review found 6 more (3 re-surfaced at finer grain, 3 new)
- **v3** — graduated to a canonical sibling spec at `workflow/specs/graph-backend-adapter-contract/design.md` with all v2 findings closed:
  - CRG migration: tool-by-tool parity matrix in §11.1 of the new spec
  - Schema migration cutover: dependent-owned coordination via lockfile (§10.3 of new spec)
  - Ref-join OPTIONAL+WHERE lowering: normative SQL rules with conformance test (§5.4.2 of new spec)
  - Compliance env propagation: explicit `derivation: true` on edges + worked example trace (§13.2 of new spec)
  - Namespace enforcement: physical schema separation (sqlite ATTACH / postgres SCHEMA / http path) + namespace tokens via SDK (§8.1, §8.2 of new spec)
  - TTRPG `allegiance_path`: rewritten as type-correct multi-hop with allowed return surface (`.agents/sandbox/ttrpg-adapter/queries.yaml`)

Wiring edits applied to `app-type-profiles/design.md` (§2.6, §3.1, §7.3, §8.3, §8.4, §10) and `go-native-code-graph-analysis/design.md` (status header).

This proposal file is preserved as the historical record of the design journey. New work should reference the canonical sibling spec, not this proposal.

---

## Original proposal text (preserved for context)

The remaining sections of this file are the v2 proposal text as written
before acceptance. They are kept for the reviewer who wants to see the
defect → fix → graduation arc. The canonical contract is in the sibling
spec.

---

---

## 0. What changed in v2

v1 of this proposal was reviewed and found `needs-attention`. Six defects
were corrected:

1. **CRG carve-out dropped.** §11 now says all adapters — CRG included —
   write to scoped KG storage. Codex correctly observed that the v1
   exemption for CRG ("structural code graph with its own MCP server")
   broke the unified-storage claim before the first built-in backend.
   §11.10 covers the CRG migration path and flags
   `go-native-code-graph-analysis/design.md` for re-scope under the new
   contract.
2. **Full-SQL escape hatch replaced with constrained materialization
   API.** §11.6 redefined: no raw SQL anywhere; bootstrap uses the same
   DSL as queries with a `materialized_views` output target; cross-adapter
   reads must be explicitly declared via `reads_from`; namespace isolation
   is enforced at the storage layer.
3. **Ref-join semantics added to §11.3.** Typed refs (`ref<type>`)
   required; LEFT JOIN compilation; depth cap = 2; use-site analysis
   collapses duplicate ref-chains to a single JOIN; bare ref returns the
   scalar ref id without a JOIN. The TTRPG `continuity_check` query that
   v1 wrote (which Codex correctly identified as not expressible) is
   rewritten using the new semantics.
4. **Tier split unlocked + new §11.9 schema-migration design.** Adapter
   schema activation now includes a lockfile-pinned schema digest,
   compatibility check, and migration skill path. Schema-changing
   adapter releases behave like package installs, not passive config
   sync. The behavior-preservation gate (§6.2) extends to schema
   changes — corpus must include nodes from prior schema versions.
5. **v1.5 trigger replaced with a budget model.** Codex's chicken-and-egg
   critique stands: a hard two-adapter threshold biases the first adapter
   toward workarounds. v2 uses an accumulating budget across multiple
   signal sources (additional named queries, materialized views,
   adapter-spawned MCP servers, blocker-class wishlist entries). 5 points
   opens v1.5 design review; 8 points starts implementation work.
6. **`env_predicates` added to note-type schema; §8.6 rewritten.**
   Codex correctly observed that the v1 compliance example's
   `environmental_trigger` claim was not backed by predicate declarations.
   v2 adds `env_predicates` as a first-class field and rewrites the
   compliance example to distinguish environmental drivers (predicate-
   based, fires on events) from review-nudge (time-based, never tagged
   stale per scoped-KG §2.7).

The underlying direction (KG-as-substrate + pluggable adapters +
declarative DSL) is unchanged. The seams that Codex found were all
in the contract details, not the architecture.

---

## 1. Why this proposal exists (unchanged from v1)

`app-type-profiles/design.md` §2.6 currently treats `graph_backend` as a
closed enum (`crg | citation-graph | document-cross-ref | none`) bound to
skill refs. That framing has three problems exposed in design-review
conversations:

1. **It implies separate databases per backend.** A reader of §2.6
   reasonably concludes that `citation-graph` and `document-cross-ref`
   are independent storage systems alongside CRG and KG. Building three
   more graph databases throws away every commitment scoped-knowledge-
   graphs already made (scoped backends, provenance, event-driven
   staleness, write-time-only propagation, resolver purity).
2. **It closes the extension point.** A custom domain — compliance
   registers, campaign worldbuilding, lab notebooks — must either land
   in `da` core or wedge into one of the named buckets. There is no
   contract for community-published backends.
3. **It conflates schema, query, and storage.** `graph_backend` today
   bundles "what the data looks like" + "how impact radius is computed"
   + "where the data lives." Splitting these three concerns is what
   enables a pluggable adapter contract over a shared storage substrate.

This proposal:

- Revises §2.6 to make graph backends pluggable adapter refs over the
  existing KG storage substrate (no exceptions — including CRG)
- Updates §3.1 field semantics for `graph_backend` and
  `impact_radius_kind`
- Updates §7.3 orchestrator precondition wording
- Adds §11 "Graph backend adapter contract" defining the adapter schema,
  resolution path, named-query DSL with ref-joins, planner hints,
  staleness driver binding, constrained materialization, escape hatches,
  v1.5 trigger budget, distribution model, schema migration, and CRG
  migration path
- Replaces §8.3 / §8.4 graph_backend references with the adapter ref
  form, and adds §8.6 — a worked example for a compliance register
  backend that exercises all five staleness drivers (including
  predicate-based environmental triggers)
- Updates §10 open questions: closes Q7 ambiguity; adds new Q9 about
  cross-adapter view dependencies versioning

---

## 2. Section-by-section edits to `app-type-profiles/design.md`

### 2.1 §2.6 — revised text

**Before:**

> A profile names its review kind (`code-review | rubric-review | citation-review | custom`),
> its graph backend (`crg | citation-graph | document-cross-ref | none`), and its impact
> radius kind (`symbol | section | citation | custom`). Each selection binds to a skill
> reference that implements it. New review/graph kinds are added by publishing new skills,
> not by patching the pipeline.

**After:**

> A profile names three pluggable plug-points. Each is resolved through the
> standard config distribution machinery and binds to an artifact whose
> contract is defined elsewhere in this spec set:
>
> - **`review_kind`** binds to a `review_skill` ref (skill-tier artifact).
>   Built-ins: `code-review`, `rubric-review`, `citation-review`, `custom`.
> - **`graph_backend`** binds to a graph backend adapter ref (config-layer
>   artifact, contract defined in §11). Built-ins: `crg`, `none`. Custom:
>   `<source-id>:graph/<name>@<version>`.
> - **`impact_radius_kind`** is **derived** from the resolved graph backend
>   adapter rather than independently declared. The adapter's
>   `impact_radius` query and edge-type allowlist define the radius for
>   that domain.
>
> All graph backends — built-in and custom, including `crg` — write to
> scoped KG storage per the scoped-knowledge-graphs spec. They do not
> introduce new database backends, separate stores, or sidecar processes.
> Adapters differ in **schema** (note/edge types) and **query** (impact
> radius), not in storage. The CRG adapter is a kg-native adapter whose
> bootstrap skill performs Tree-sitter ingestion and writes function /
> type / call-edge notes to the shared KG storage; its impact-radius
> query is the symbol-reach traversal that callers use today.

### 2.2 §3.1 — revised field-semantics rows

**Before:**

| Field | Required | Type | Notes |
|---|---|---|---|
| `graph_backend` | yes | enum | `crg \| citation-graph \| document-cross-ref \| none` |
| `impact_radius_kind` | yes | enum | `symbol \| section \| citation \| custom` |

**After:**

| Field | Required | Type | Notes |
|---|---|---|---|
| `graph_backend` | yes | adapter-ref | refs a graph backend adapter (§11). Built-ins: `crg`, `none`. Custom: `<source-id>:graph/<name>@<version>` |
| `impact_radius_kind` | derived | enum | derived from the resolved graph backend adapter; not separately declared in profile YAML |

### 2.3 §7.3 — revised orchestrator precondition wording

**Before:**

> - The resolved profile's `graph_backend` is available in the current
>   environment (e.g., `crg` requires a CRG build present; `citation-graph`
>   requires the citation-graph MCP server)

**After:**

> - The resolved profile's `graph_backend` adapter is loaded; its declared
>   note/edge types are registered with the active KG scope chain at the
>   adapter's pinned schema version (per §11.9); its bootstrap skill (if
>   declared) has run at least once for this scope chain; its named
>   queries pass syntactic validation against the §11.3 DSL; declared
>   `reads_from` cross-adapter dependencies (per §11.6) are present at
>   compatible schema versions. Adapter resolution failures (missing
>   source, version conflict, schema validation error, unsatisfied
>   `reads_from`) surface as a single error naming the missing artifact,
>   not as opaque runtime failures during fanout.

### 2.4 §8.3 / §8.4 — graph_backend ref form

Replace `graph_backend: citation-graph` with
`graph_backend: dotagents-builtin:graph/citation@^1.0` in the `research`
example, and `graph_backend: document-cross-ref` with
`graph_backend: dotagents-builtin:graph/document-cross-ref@^1.0` in the
`resume-ideation` example. Built-in adapters live under the synthetic
`dotagents-builtin` source so the ref form is uniform across built-in
and custom backends.

### 2.5 §10 — open-questions delta

**Remove:** any prior open question that assumed graph_backend was a
closed enum (none currently named explicitly; the issue was implicit in
§2.6).

**Closed by §11.9 in this proposal:**

> ### Q7: Adapter schema migration across version bumps — CLOSED
>
> Closed by §11.9. Adapter schema activation requires a lockfile-pinned
> digest, a compatibility check (new schema must be a superset of old, or
> a migration skill must transform old-version notes), and the behavior-
> preservation gate (§6.2) extension to schema changes. Schema-changing
> adapter releases behave like package installs, not passive config sync.

**Add:**

> ### Q8: v1.5 trigger budget threshold tuning
>
> §11.7 sets the v1.5 trigger as a 5-point budget across multiple signal
> sources (additional named queries, materialized views, adapter-spawned
> MCP servers, wishlist entries). The 5 / 8 point thresholds are
> judgment-based defaults. The TTRPG dogfood adapter (separate project,
> see `.agents/sandbox/ttrpg-adapter/`) is the first real-world signal
> source. Re-tune after the first 3 months of dogfood data and after the
> compliance-register adapter ships.
>
> ### Q9: Cross-adapter view dependencies and version compatibility
>
> §11.6 allows adapters to declare `reads_from` cross-adapter
> dependencies for materialized views. Open: how do these dependencies
> version, and what happens when the depended-on adapter bumps a major
> version? Lean: `reads_from` declarations include a version range; when
> the depended-on adapter activates a new schema (per §11.9), all views
> citing it are flagged for re-validation; mismatches surface at adapter
> load, not at view refresh time.

---

## 3. New normative section: §11 Graph backend adapter contract

### 11.1 Purpose

A graph backend adapter is a pluggable artifact that defines:

- **(a) Schema** — what note and edge types exist in this domain;
  optional environmental predicates that bind to staleness drivers
- **(b) Impact radius** — the named query the review pipeline invokes
  when a task touches a node, returning the blast radius
- **(c) Staleness driver binding** — which of scoped-KG's drivers apply
  to this domain's nodes
- **(d) Planner hints** — algorithm choice, materialized views, index
  hints, cardinality stats
- **(e) Bootstrap skill** — a skill ref that knows how to populate the
  graph for this domain from canonical sources, using the constrained
  DSL (no raw SQL)

All adapters write to scoped KG storage (per scoped-knowledge-graphs).
They do not introduce new database backends, separate stores, or
sidecar processes. The KG infrastructure (scoped backends, provenance,
event-driven staleness, write-time-only propagation, resolver purity)
applies to all adapters by construction.

### 11.2 Adapter schema

```yaml
# graph-backend: <name>.v<version>
name: <name>
version: <semver>
description: |-
  <one-paragraph purpose>

# Schema extension — new note/edge types added to the shared KG vocabulary,
# stored in the adapter's namespaced tables (kg_<name>_notes, kg_<name>_edges).
# Validated at adapter load time; conflicts with existing types fail fast.
note_types:
  - name: <type-name>
    fields:
      - { name: <field>,
          type: <string|int|float|bool|date|enum|ref<type>>,
          required: <true|false>,
          values: [<for-enum>] }
    # Optional environmental predicates that bind to scoped-KG's
    # environmental_trigger driver. When `da kg trigger --env <kind> <args>`
    # fires, notes whose predicate no longer matches are invalidated.
    env_predicates:
      - kind: <time_after|module_version|webhook|custom>
        field: <field-name>           # for time_after: which date field
        # additional kind-specific args

edge_types:
  - { name: <edge-name>, from: <note-type>, to: <note-type>,
      cardinality: <one-to-one|one-to-many|many-to-many>,
      signed: <true|false> }

# Required: the impact-radius query the review pipeline invokes.
impact_radius:
  query: |-
    <named-query DSL — see §11.3>
  max_depth: <int>
  algorithm_hint: <bidirectional_bfs|dijkstra|astar|bfs|dfs>
  materialize:                          # optional planner hints
    - transitive_closure_of: [<edge-types>]
    - neighborhood_summary_for: [<note-types>]
  index_hints:
    - { predicate: <edge-name>, order: <SPO|SOP|PSO|POS|OSP|OPS> }
  cardinality_stats:
    auto: true                          # computed at bootstrap

# Which of scoped-KG's drivers fire on this domain's nodes.
staleness_drivers:
  - <source_mutation|derivation_mutation|explicit_revocation
     |contradiction_arrival|environmental_trigger>

# Optional: skill that populates the graph from canonical sources.
# The bootstrap skill uses the same DSL as queries — no raw SQL.
# It writes to the adapter's namespaced tables only (write isolation
# enforced at the storage layer); it can read across namespaces via
# explicitly declared materialized views (§11.6).
bootstrap_skill: <skill-ref>

# Optional: additional named queries this adapter ships beyond impact_radius.
# Surfaced via `da graph <backend> query <name> --param ...`.
queries:
  - name: <query-name>
    description: |-
      <one-line purpose>
    params:
      - { name: <param>, type: <type>, required: <bool> }
    query: |-
      <named-query DSL>
    algorithm_hint: <hint>

# Optional: materialized views — see §11.6 for the cross-adapter contract.
materialized_views:
  - name: <view-name>
    description: |-
      <purpose>
    reads_from:
      - { adapter: <adapter-name>,
          version: <semver-range>,
          note_types: [<types>],
          edge_types: [<edges>] }
    refresh_on:
      - { adapter: <adapter-name>,
          driver: <driver-name>,
          on_types: [<note-types>] }
    query: |-
      <constrained DSL — same rules as named queries>
```

### 11.3 Named-query DSL (v1)

The `impact_radius` query, additional named queries, and materialized-
view definitions all use a constrained pattern-match DSL that translates
to parameterized SQL against the active KG scope chain. The DSL is
intentionally narrow.

#### 11.3.1 Allowed clauses

- `MATCH (alias:<note-type>)-[:<edge-type>|<edge-type>...]->(alias:<note-type>)`
- `OPTIONAL MATCH` for left-join semantics
- `WHERE alias.<field> <op> $<param>` — substitution-only, no concatenation
- `WHERE alias.id IN $<param>` — list parameters bound to query-time arrays
- Variable-length edge patterns `[:<edge>*1..<max_depth>]`, bounded by
  the declared `max_depth`
- `RETURN <alias>.<field>, hop_count, ...`

#### 11.3.2 Forbidden

- String concatenation in any clause
- Subqueries
- Recursion outside variable-length patterns
- DDL of any kind (no `CREATE`, `MERGE`, `SET`, `DELETE`)
- Functions other than `hop_count`, `count`, `min`, `max`, `coalesce`

#### 11.3.3 Substitution model

- `$<param_name>` placeholders bind to caller-supplied values at execution
  time. Types are checked against the `params` declaration at adapter load.
- The query body is parsed once at adapter load; the AST is reused for
  every invocation. This is what enables planner-hint precomputation in
  §11.4.

#### 11.3.4 Ref-join semantics

A field declared with type `ref<type>` is a single-valued reference to a
node of the named type. Refs are cheaper than edges (no edge-table
lookup) and are appropriate for inherent single-valued relationships
(a character has one stated location, an evidence has one collecting
owner). Many-to-many or metadata-bearing relationships should use edge
types instead.

**Untyped `ref` fields are forbidden** once ref-joins land. Schema
validator rejects `{ type: ref }` and requires `{ type: ref<location> }`.

**Compilation rules:**

1. **Bare ref usage** — `RETURN c.stated_location` returns the scalar
   ref id from the source row. **No JOIN.**
2. **Field traversal through ref** — `c.stated_location.region` resolves
   the ref to a node and reads the named field. **One LEFT JOIN.**
3. **Use-site analysis** — when the same ref-chain prefix appears
   multiple times in a single query (across MATCH / WHERE / RETURN),
   generate **one JOIN** with a stable alias and reuse it. Example:
   `WHERE c.stated_location.region = 'EU' RETURN c.stated_location.kind`
   produces one JOIN on `c.stated_location → location`, aliased
   `c__stated_location`, used in both the WHERE and RETURN.
4. **Bare-ref + field-traversal mix** — if a query uses both
   `c.stated_location` (bare) and `c.stated_location.region` (traversal),
   the JOIN is generated once for the traversal; the bare reference
   reads the scalar column directly. No duplication.
5. **LEFT JOIN, not INNER** — null refs short-circuit to null on the
   joined fields rather than dropping the row. Adapter authors who want
   inner-join semantics use `WHERE c.stated_location IS NOT NULL`.
6. **Depth cap = 2.** Ref-chains deeper than two hops
   (`c.stated_location.ruler.faction`) are rejected at adapter load.
   Adapter authors needing deeper traversal write explicit MATCH
   clauses, where the planner can apply algorithm hints.

**SQL translation (illustrative):**

```
DSL:     MATCH (c:character)
         WHERE c.stated_location.region = $region
         RETURN c.id, c.stated_location.kind

SQL:     SELECT c.id, c__stated_location.kind
         FROM kg_ttrpg_notes c
         LEFT JOIN kg_ttrpg_notes c__stated_location
           ON c__stated_location.id = c.stated_location_ref
           AND c__stated_location.note_type = 'location'
         WHERE c.note_type = 'character'
           AND c__stated_location.region = $1
```

**Modeling guidance:** when both a ref and an edge would work, prefer:
- **Ref** for: single-valued, no metadata, inherent property of source
  (character → stated_location, evidence → owner, control → policy)
- **Edge** for: many-to-many, metadata-bearing, bidirectional, signed
  (character ↔ event via present_at, control ↔ regulation via
  satisfies, faction ↔ faction via allied_with)

### 11.4 Planner hints (unchanged from v1)

Adapter authors declare intent; `da` chooses execution strategy.

| Hint | What it does |
|---|---|
| `algorithm_hint: bidirectional_bfs` | Use bidirectional BFS for shortest-path queries (collapses k^d to 2·k^(d/2)) |
| `algorithm_hint: dijkstra` | Use Dijkstra for weighted edges (adapter must declare edge weights) |
| `algorithm_hint: astar` | Dijkstra + heuristic (adapter must provide heuristic function via skill ref) |
| `materialize: transitive_closure_of: [<edges>]` | Precompute (ancestor, descendant) pairs at bootstrap; refresh on staleness driver fire |
| `materialize: neighborhood_summary_for: [<types>]` | Precompute per-node edge-type counts and adjacent-type histograms |
| `index_hints: [{predicate, order}]` | Build the named permutation index (one of SPO/SOP/PSO/POS/OSP/OPS) for this predicate |
| `cardinality_stats: { auto: true }` | Compute per-(predicate, object) cardinality at bootstrap and refresh on driver fire; planner uses for join ordering |

`da` is permitted to override or supplement hints based on observed query
performance (e.g., add an index it estimates as load-bearing). Adapter
hints are never load-bearing on correctness — only performance.

### 11.5 Staleness driver binding

The adapter's `staleness_drivers` list names which scoped-KG drivers (per
scoped-KG §2.5) fire on this domain's nodes. Drivers not listed do not
fire — a `compliance-register` adapter that omits `environmental_trigger`
will not invalidate evidence nodes when their `expires_at` passes, even
if the predicate is declared on the node.

This is the same opt-in model as scoped-KG §3.1 ("a scope with no
drivers declared produces no staleness signals"), with one addition:
when an adapter declares a driver, the scope chain it writes to must
have that driver enabled. Mismatches fail loud at adapter load time,
not silently at write time.

**Environmental driver vs review-nudge — distinction:**

- **Environmental driver** is predicate-based. The adapter declares
  `env_predicates` on a note type; an external trigger
  (`da kg trigger --env <kind> <args>`) fires the driver against
  matching predicates; matching notes are tagged
  `stale: { reason: "environmental", because: [<trigger>] }`.
- **Review-nudge** is time-based (per scoped-KG §2.7). It is a
  **separate dimension** from staleness — review-nudged notes are still
  fresh, just tagged `review_due: true`. Adapters that want time-based
  re-confirmation declare `review_nudge` on the scope, not in the
  adapter contract.

Adapters that want evidence to expire (compliance) use environmental
drivers with a `time_after` predicate fired by a recurring `da kg
trigger --env time_after <field>` cron. Adapters that want reviewers
to periodically re-confirm a fact use review-nudge.

### 11.6 Constrained materialization API and cross-adapter reads

**No raw SQL anywhere.** Bootstrap skills, materialized views, and
named queries all use the §11.3 DSL.

#### 11.6.1 Namespace isolation

Each adapter gets table-prefixed storage:
- `kg_<adapter>_notes` — note rows
- `kg_<adapter>_edges` — edge rows
- `kg_<adapter>_views_<view-name>` — materialized view rows

Writes outside the adapter's prefix fail at the storage layer, not at
the DSL layer. This is defense in depth: even if an adapter's DSL
somehow generated a write outside its namespace (it can't, but if a
bug allowed it), the storage layer would reject it.

#### 11.6.2 Cross-adapter reads

An adapter's bootstrap skill, queries, and views can read across
namespaces — but only when the cross-namespace dependency is explicitly
declared via `reads_from` in the materialized view definition (per
§11.2 schema). Cross-namespace reads outside a `reads_from` declaration
are rejected at adapter load.

This makes cross-adapter coupling visible:

```yaml
# In compliance-register adapter
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

When CRG bumps to a major version that changes the `function` schema,
this view's `reads_from` flags it for re-validation per §11.9.

#### 11.6.3 Three escape hatches before v1.5

When a user wants a query the adapter's named templates don't cover,
three options exist before promoting the DSL:

1. **Adapter ships more named queries.** Adapter author publishes a new
   minor version with the requested query as an additional template.
2. **Adapter ships materialized views via the §11.6 API.** Same DSL,
   `materialized_views` output target, explicit `reads_from`
   declarations. Named queries can read from the view.
3. **Adapter exposes its own MCP server.** The adapter is the authority
   on its domain. If users want ad-hoc patterns, the adapter ships an
   MCP tool that handles them over the same KG storage. `da` core does
   not need to be in the loop. The CRG adapter is expected to follow
   this pattern for the rich code-graph queries it ships today.

### 11.7 v1.5 trigger budget

Promote the named-query DSL to a richer pattern-match DSL only when an
accumulating budget threshold is reached.

**Signal sources and weights:**

| Signal | Weight |
|---|---|
| Adapter ships an additional named query that's clearly a workaround for a missing primitive | 1 |
| Adapter ships a materialized view to express what the DSL can't | 1 |
| Adapter spawns its own MCP server primarily for query expressiveness (not domain-specific tooling) | 2 |
| Adapter author logs a wishlist entry tagged `needs-author-unanticipatable-composition` | 2 |
| Same conceptual pattern appears as a workaround in 3+ places across all adapters | 3 |
| Single adapter reports a blocker-class request (user cannot proceed without it) | 3 |

**Thresholds:**

- **5 points** → opens v1.5 design review
- **8 points** → starts v1.5 implementation work

**Dispute mechanism:** an adapter author can dispute a signal by writing
a justification (e.g., "this is a legitimately domain-specific pattern,
not a missing core primitive"). Successful disputes remove the points
but log the dispute itself as evidence — repeated dispute patterns
become signal in their own right.

This avoids the v1 chicken-and-egg failure mode (Codex finding) where a
hard two-adapter threshold biases the first adapter toward workarounds
before evidence for a missing primitive can accumulate.

### 11.8 Distribution and tiers

Per config-distribution-model:

| Artifact | Tier | Distribution | Activation |
|---|---|---|---|
| Adapter YAML (schema + queries + hints + driver list + view definitions) | 1 (config layer) | git/http/local source via `sources` + `extends` | **Pinned in lockfile per §11.9; activation requires schema-compatibility check; not passive config sync when schema changes** |
| Bootstrap skill | 2 (executable package) | OCI artifact via `packages` | Code-executable; high blast radius; needs digest pinning |

Built-in adapters (`crg`, `none`) ship inside `da` and require no
external resolution; their refs use the synthetic `dotagents-builtin`
source.

A team layer that declares
`graph_backend: team-config:graph/compliance-register@^1.0` propagates
to every repo inheriting that layer via `extends`. An org layer can
compose:
`graph_backend: org-config:graph/compliance-register-base + acme-overrides@^1.0`
for org-specific control libraries. Sandbox/CI inherit transparently.
Public-source adapters distribute the same way as public-source skills
(per external-agent-sources).

### 11.9 Adapter schema migration

Adapter YAML defines persisted note/edge vocabulary. A normal config
refresh that changes the schema would change how existing KG state is
interpreted across every inheriting repo. v1's tier classification
treated this as a Tier 1 concern (passive config sync); v2 corrects
that — schema-changing adapter releases are activated like package
installs, not like ordinary config sync.

#### 11.9.1 Lockfile schema pinning

The lockfile (per config-distribution-model §7) gains a per-adapter
schema digest:

```
adapters:
  acme-config:graph/compliance-register@1.2.3:
    source_digest: sha256:abc...      # adapter YAML content hash
    schema_digest: sha256:def...      # canonical hash over note_types,
                                      #   edge_types, env_predicates
    activated_at: 2026-05-09T12:00:00Z
```

Activation requires the schema digest in the lockfile to match the
declared schema digest in the adapter YAML.

#### 11.9.2 Compatibility check on adapter version bump

When an adapter bumps to a new version, the new schema digest is
compared to the lockfile's pinned digest:

- **If identical** — no schema change; activation proceeds.
- **If new schema is a superset of old** (added optional fields, added
  note/edge types, added env_predicates) — backward-compatible;
  activation proceeds; lockfile updates.
- **If new schema is not a superset** (removed fields, changed types,
  renamed enums) — activation blocks until either:
  - The behavior-preservation gate (§6.2 extended) passes against a
    corpus of existing notes, or
  - The adapter author ships a migration skill (§11.9.3) that
    transforms old-version notes to new-schema shape

#### 11.9.3 Migration skills

A migration skill is a Tier 2 OCI package that:

1. Reads notes from the adapter's namespaced tables at the old schema
   version
2. Transforms them per the migration logic (rename fields, infer values
   for new required fields, drop removed fields with optional archival)
3. Writes them back with the new `adapter_schema_version` tag
4. Reports per-note migration status (succeeded, failed, requires-
   manual-review)

Migration skills are invoked via `da kg migrate --adapter <name>
--from <old-version> --to <new-version>`. Activation of the new
adapter schema requires the migration to complete with zero
`requires-manual-review` notes (or explicit operator override).

#### 11.9.4 Behavior-preservation gate extension

The §6.2 gate extends to schema changes. The corpus must include notes
from prior schema versions and the new adapter must produce identical
results on them when queried via the impact_radius query and any
shipped named queries. Pass→fail outcome regressions block the version
bump.

### 11.10 CRG migration path

CRG today is implemented as a separate Python subprocess with its own
SQLite store, accessed through the bridge defined in
`internal/graphstore/crg.go` and the MCP server at
`internal/graphstore/mcp_server.go`. Under this proposal CRG becomes a
kg-native adapter:

1. **CRG adapter YAML** declares the existing schema (note types
   `function`, `type`, `package`, `interface`; edge types `calls`,
   `implements`, `imports`, `defines`) as `dotagents-builtin:graph/crg@1.0`.
2. **Bootstrap skill** (Tier 2 OCI package) performs Tree-sitter
   ingestion and writes to `kg_crg_notes` / `kg_crg_edges` in the
   shared KG storage.
3. **MCP server** for ad-hoc rich queries is retained as the §11.6
   escape-hatch pattern — same code, just reading from the shared
   storage instead of a separate SQLite store.
4. **Bridge subprocess is decommissioned** once the bootstrap skill
   reaches parity with the existing `kg build` / `kg update` flow.

**Implication for `go-native-code-graph-analysis/design.md`:** that
spec's scope was framed against the assumption CRG remains a separate
process with its own SQLite store. Under v2 of this proposal, the
"Python subprocess vs Go-native" question collapses to "which Tree-
sitter binding does the CRG adapter's bootstrap skill use?" That spec
should be re-scoped to retain only the binding choice and bootstrap-
skill design, with the broader Python-vs-Go architecture question
superseded by §11.10. Recommend updating the spec status to
`superseded-in-part` and adding a header pointing to this proposal.

---

## 4. New worked example: §8.6 Compliance register backend (revised)

**Domain:** governance / risk / compliance — controls, risks, findings,
evidence, regulations. Stresses every staleness driver including
predicate-based environmental triggers.

```yaml
# graph-backend: compliance-register.v1
name: compliance-register
version: 1.0.0
description: |-
  GRC graph for controls, risks, findings, evidence, regulations.
  Loads canonical SOC2/NIST/HIPAA frameworks at bootstrap; tracks
  evidence expiry and policy review windows via environmental drivers
  (predicate-based, not time-based).

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
      - { name: derives_from,      type: ref<policy> }

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
    # Predicate fires when `da kg trigger --env time_after evidence.expires_at`
    # is invoked (typically by a daily cron). Notes whose expires_at <= now
    # are tagged stale: { reason: "environmental", because: [<trigger-id>] }.
    env_predicates:
      - kind: time_after
        field: expires_at

  - name: policy
    fields:
      - { name: version,          type: string }
      - { name: last_reviewed_at, type: date }
      - { name: review_window_days, type: int }
    # Predicate fires when an external policy-management webhook posts
    # to `da kg trigger --env webhook policy.review_due <policy-id>`.
    # Distinct from review-nudge (scoped-KG §2.7) which is purely time-
    # based and never tags notes stale.
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
  - { name: cited_by,   from: evidence,   to: control,    cardinality: many-to-many }
  - { name: affects,    from: finding,    to: control,    cardinality: many-to-many }
  - { name: supersedes, from: regulation, to: regulation, cardinality: many-to-one  }
  - { name: contradicts, from: finding,   to: control,    cardinality: many-to-many }

# The load-bearing query: review pipeline invokes this when a control's
# status changes during an audit pass. Demonstrates ref-join (control's
# owner is a ref, surfaced in the result so the auditor knows who to ping).
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
  - source_mutation         # control status changes
  - derivation_mutation     # regulation supersession cascades
  - environmental_trigger   # evidence.expires_at predicate, policy.review_due webhook
  - explicit_revocation     # finding closed / false_positive
  - contradiction_arrival   # pentest finding contradicts control "effective" claim

bootstrap_skill: compliance-register-bootstrap@^1.0

queries:
  - name: regulations_satisfied_by
    description: What regulations does control C satisfy?
    params: [{ name: control_id, type: string, required: true }]
    query: |-
      MATCH (c:control)-[:satisfies]->(reg:regulation)
      WHERE c.control_id = $control_id
      RETURN reg.id, reg.authority, reg.version

  - name: superseded_controls
    description: Controls citing a regulation version that has been superseded.
    params: [{ name: authority, type: string, required: true }]
    query: |-
      MATCH (c:control)-[:satisfies]->(old:regulation)-[:supersedes]->(new:regulation)
      WHERE old.authority = $authority
      RETURN c.control_id, old.version, new.version, c.owner.name
    algorithm_hint: bfs

  - name: stale_evidence_unsupported_controls
    description: |-
      Controls left unsupported because their evidence was invalidated by
      the environmental_trigger driver (evidence.expires_at predicate fired).
      The query reads the stale tag rather than the date directly — the
      driver is what marks evidence stale, the query consumes the result.
    params: []
    query: |-
      MATCH (e:evidence)-[:cited_by]->(c:control)
      WHERE e.stale.reason = 'environmental'
      RETURN c.control_id, c.owner.name, e.id, e.stale.fired_at

  - name: coverage_gaps
    description: Risks with no mitigating control (set difference).
    params: []
    query: |-
      MATCH (r:risk)
      OPTIONAL MATCH (c:control)-[:mitigates]->(r)
      WHERE c.id IS NULL
      RETURN r.id, r.severity, r.likelihood, r.owner.name
```

### What this example demonstrates (revised)

| Aspect | Demonstration |
|---|---|
| **Adapter is just YAML** | Whole backend definition above is one config-layer artifact (Tier 1). No new DB, no new MCP server required. |
| **Impact radius is the load-bearing query** | When the auditor flips a control to `failed`, the review pipeline gets back regulations newly uncovered, risks newly unmitigated, findings needing re-evaluation, evidence orphaned, and the control's owner — in one query. |
| **All five staleness drivers exercised, including env predicates** | Source mutation (control status), derivation (regulation supersession), environmental (evidence `time_after expires_at` predicate + policy review webhook), revocation (finding closure), contradiction (pentest vs. control claim). The `env_predicates` field is the load-bearing addition v1 was missing. |
| **Ref-joins simplify queries** | `c.owner.name` returns the owner name without an explicit `MATCH (c)-[:owned_by]->(o:owner)` — the ref field carries enough information. The auditor result includes who-to-ping without query bloat. |
| **Stale tag is consumed, not recomputed** | The `stale_evidence_unsupported_controls` query reads `e.stale.reason = 'environmental'` rather than checking expires_at directly. The environmental driver is what tags notes stale; queries consume the result. This is the correct staleness contract per scoped-KG §3.5. |
| **Planner hints are intent-shaped** | Adapter author knows `satisfies` is queried "what satisfies regulation R?" 90% of the time → `POS` index. Adapter author knows the `(satisfies, supersedes)` closure is queried frequently and changes rarely → materialize it. |
| **Distribution path is concrete** | Tier 1 git source ships `compliance-register-base@^1.0`. Insurance customer's org layer extends with `acme-overrides@^1.0` adding bespoke controls. Bootstrap skill is OCI-distributed (Tier 2). |
| **Schema migration is in scope** | If the adapter bumps from 1.0 → 2.0 and changes the `evidence` schema (e.g., adds `signed_by` as required), §11.9 requires a migration skill that backfills `signed_by` for existing evidence notes before activation. |
| **v1 ceiling visible** | First five queries are cleanly named-query-shaped. The "findings opened Q3 2026 affecting controls owned by Team X with evidence collected before incident I" type query is where v1.5 might trigger — but escape hatches §11.6 cover it first; budget signals (§11.7) accumulate from any unanticipated requests. |

---

## 5. Decision summary (v2)

| Decision | Lock |
|---|---|
| Graph backends are adapters over scoped KG, no exceptions (CRG included) | ✅ Lock |
| Adapter YAML is Tier 1, **but schema activation is not passive sync** — pinned, validated, migrated per §11.9 | ✅ Lock |
| Bootstrap skill is Tier 2; no raw SQL anywhere; uses constrained DSL + materialized_views API | ✅ Lock |
| Cross-adapter reads via explicit `reads_from` declarations; namespace isolation enforced at storage layer | ✅ Lock |
| v1 = named queries + ref-joins (depth ≤ 2, use-site analysis) + planner hints + escape hatches | ✅ Lock |
| v1.5 trigger = 5-point budget across multiple signal sources; dispute mechanism preserves signal integrity | ✅ Lock |
| `impact_radius_kind` becomes derived (not separately declared) | ✅ Lock |
| Environmental driver is predicate-based via `env_predicates`; review-nudge stays time-based and is a separate dimension | ✅ Lock |
| First worked example: compliance register (revised with env_predicates and ref-joins) | ✅ Lock |
| First dogfood adapter: TTRPG (separate sandbox project, queries.yaml updated for ref-join syntax) | ✅ Lock |
| `go-native-code-graph-analysis` re-scope flagged in §11.10 | ✅ Lock |

---

## 6. Application steps

When the user accepts this proposal:

1. Edit `.agents/workflow/specs/app-type-profiles/design.md`:
   - Replace §2.6 text per §2.1 above
   - Replace §3.1 field rows per §2.2
   - Replace §7.3 precondition wording per §2.3
   - Update §8.3 / §8.4 graph_backend refs per §2.4
   - Add §8.6 worked example per §4 above
   - Add new §11 (with §§11.1–11.10) per §3 above
   - Close Q7 + add Q8 + Q9 to §10 per §2.5
2. Update `.agents/sandbox/ttrpg-adapter/`:
   - `schema.yaml` — change `ref` field declarations to typed `ref<type>`
   - `queries.yaml` — rewrite `continuity_check` using ref-join syntax
3. Update `.agents/workflow/specs/go-native-code-graph-analysis/design.md`:
   - Add `**Status:** superseded-in-part` header
   - Point to §11.10 of `app-type-profiles/design.md`
   - Re-scope to retain only the Tree-sitter binding choice and the
     bootstrap-skill design
4. Bump `app-type-profiles` `Status:` from `draft` to
   `draft-2026-05-09-v2` (or whatever the project convention is for
   revision tagging)
5. Run `/codex:adversarial-review` on the amendment before commit
6. Commit the amendment as a single logical commit:
   `feat(spec): add graph backend adapter contract v2 with compliance worked example`
7. Initialize the TTRPG sandbox dogfood per
   `.agents/sandbox/ttrpg-adapter/README.md`

This proposal is **not** auto-applied. It is a design artifact awaiting
user confirmation per `proposal-routing.md` (project-local proposal,
markdown format, no `da review` automation today).
