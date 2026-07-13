# TTRPG dogfood — author-UX findings

This is the real payload of the t3 dogfood (graph-backend-adapter-contract
§13.3): what it actually felt like to author a graph-backend adapter against
the **currently shipped** contract + SDK, where the friction is, and which
signals feed the §12 budget. Everything below is observed from authoring this
adapter (schema, 8 named queries, bootstrap skill, hard test) — not from the
spec's prose.

## What worked well

- **Schema authoring is genuinely just YAML.** `schema.yaml` validated against
  the *same* `registry.LoadSchema` the built-in `none` adapter uses, on the
  first structurally-correct draft. The §4 schema shape (note_types /
  edge_types / impact_radius / staleness_drivers) maps cleanly to a campaign
  domain. 7 note types + 13 edge types fit with room to spare.
- **Ref vs edge guidance (§5.6) is the right call and easy to apply.**
  "single-valued inherent property → ref; many-to-many/signed/metadata → edge"
  decided every modeling question without ambiguity (e.g. `held_by` is a ref,
  `present_at` is an edge).
- **The SDK surface (§8.4.1) is small enough to hold in your head.**
  `WriteNotes` / `WriteEdges` / `Query` / `MaterializeView` /
  `DeclarePredicateFired` was the entire vocabulary the bootstrap needed. The
  token-scoped boundary is invisible until you try to cross it — which is the
  point.
- **The namespace token contract is self-enforcing.** I never had to think
  about authorization in the bootstrap: `For("ttrpg", store)` is the only
  handle, and it cannot reach another namespace. The one place it mattered
  (cross-namespace read) is gated behind `MaterializeView`'s `reads_from`, so
  the trust boundary is exactly where §8.3 says it is.

## Top 3 friction findings

### 1. The DSL has no execution engine yet, so the adapter can't *prove itself*

The largest friction: `internal/kg/dsl/` does not exist yet (it's a sibling
task — the t2 STATE NOTE in TASKS.yaml says so explicitly). That means
`queries.yaml` is, today, **prose that nothing compiles or runs**. To make the
hard test real I had to hand-write Go runners (`runners_test.go`) that
re-implement v1 §5 semantics (ref-traversal-as-LEFT-JOIN, required-vs-optional
predicate placement, variable-length BFS, `coalesce` param-normalization). An
adapter author in the wild cannot do this — they have a `queries.yaml` and no
way to execute it until the DSL ships. **This is a sequencing gap, not a
contract gap**, but it is the single biggest thing standing between "I wrote an
adapter" and "my adapter works." Recommendation: the DSL compiler + a
`da graph <adapter> query <name>` CLI is the load-bearing missing piece for
adapter-author UX; everything else in the contract is authorable today.

### 2. Bootstrap output counts are an opaque target without a counting tool

The §13.3 hard test demands "expected note/edge counts," but the contract gives
the author no help computing them. With idempotent-upsert-by-id + synthesized
`session`/`documents` provenance, the distinct-id count is *not* the sum of the
per-file note arrays (40 distinct notes from 51 raw note entries; 92 distinct
edges from raw edges + 10 synthesized `documents`). I had to write a throwaway
script to derive the oracle, then encode it. An adapter author shipping a real
bootstrap needs a `da graph <adapter> bootstrap --dry-run --stats` that reports
the counts the SDK *would* write, so the "expected counts" oracle is a tool
output, not a hand-computation. Without it, the hard-test bar is real but the
path to clearing it is undocumented.

### 3. v1 grammar gaps force query rewrites that read as workarounds

Three of the eight named queries wanted a construct v1 doesn't have, and the
rewrite-to-fit-v1 is visibly less natural than the intent:

- **Reverse/undirected reads** ("who knows X?", "factions allied with Y") are
  the campaign's most common shape, but v1 is directed-positive-hop only. The
  workaround — emit symmetric edges at bootstrap or query from the far endpoint
  — works but doubles edge volume or inverts the author's mental model.
- **`ORDER BY` / top-N** ("3 most recent events") has no v1 expression; the
  caller sorts client-side. Confirmed by the spec itself (§11.1 flows row
  closes the named-query path here).
- **Shortest-path with the `travel_days` weight** is unreachable: v1 returns
  end-node + `hop_count`, never the path (§5.2 paths-as-objects forbidden), so
  `connects_to.travel_days` can be *stored* but a weighted route can't be
  *returned*.

None of these is a blocker (every one has a v1 workaround), so per §12.4 they
stay budget signals, not fast-path amendments. They are logged in `WISHLIST.md`
at the §12.1 weights (total 4 points, under the 5-point review threshold). The
honest read: **v1's grammar is sufficient for a campaign graph, but the three
gaps above are the ones a real DM will hit first.** I deliberately did NOT
extend the grammar (Anti-scope) — the dogfood's job is to *generate the signal*,
not pre-empt the v1.5 decision.

## Smaller observations

- `weight_field: travel_days` on `connects_to` is declarable but inert in v1
  (no `dijkstra` execution without the DSL engine + path return). It's correct
  to declare it now so the schema is forward-compatible, but an author might
  reasonably expect it to *do* something and be surprised it doesn't yet.
- `derivation: true` on `allegiance` (ref) and `documents` (edge) is declarable
  and matches §7.3, but with no driver runtime wired in this sandbox its effect
  is unobservable here — it's a forward-declaration. Fine, but the author gets
  no feedback that they declared it correctly.
- The bootstrap's idempotent-upsert-by-id semantics are an *author* decision,
  not a contract one — the SDK `WriteNotes` is append-style; dedup/upsert is on
  the skill. That's the right layering, but it means every adapter author
  re-implements upsert. A shared `sdk.UpsertNotes` helper keyed by id would
  remove boilerplate every bootstrap will otherwise duplicate.

## Deferred (human-in-the-loop, out of agent scope)

The task's "DM friend onboarded for live dogfood / DM-validated results /
wishlist signal from a real DM" is a human step and is **explicitly deferred**.
It is substituted here by:

- a **synthetic 10-session corpus** (`corpus/`) with a coherent campaign arc, and
- a **machine oracle** (`oracle.yaml`) encoding the exact expected note/edge
  counts and named-query results, asserted by `internal/adapters/sdk/dogfood`.

This makes the hard test runnable with zero humans in the loop while leaving the
live-DM validation as a clearly-scoped follow-up. The contract-driven wishlist
signals above are real now; the domain-driven ones a live DM would add come
later.
