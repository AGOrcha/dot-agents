# CRG dual-read parity surface (project-local analysis, 2026-05-19)

Source: subagent analysis of `internal/graphstore/crg.go` + `mcp_server.go`
+ `internal/graphstore/store.go` + the §11 parity matrix in
`.agents/workflow/specs/graph-backend-adapter-contract/design.md`,
cross-referenced against live CRG SQLite at
`/Users/nikashp/Documents/dot-agents/.code-review-graph/graph.db` and
`/Users/nikashp/Documents/payout/client-se/.code-review-graph/graph.db`,
and three real review-time query intents from the payout codebase.

This note feeds the **t4-crg-dual-read** task in plan
`graph-backend-adapter-contract`. The spec's §11 parity matrix is
**not testable as written**; this proposal lists the concrete edits
needed before t4 starts.

---

## Headline findings

1. **3 parity-matrix rows are not testable today.** `update` /
   `postprocess` / `detect-changes` rely on free-text stdout
   (`parseCRGMutationSummary` regex) or LLM-derived output and have
   no structured upsert/diff surface to compare against.
2. **2 rows are under-specified.** `build` ("±1% of `kg build`
   output") and `impact-radius` ("same result set on 100-query
   corpus") will produce false passes — totals can stay within 1%
   while per-kind distributions diverge; "result set" doesn't say
   whether order, edges, or just node-ids count.
3. **`flows` row's trichotomy IS the defect.** §11.1 offers "named
   query OR materialized view OR adapter MCP server" — that
   ambiguity will yield divergent implementations. The DSL §5.1 has
   no `ORDER BY` in `RETURN`, so reconstructing
   `flow_memberships(flow_id, node_id, position)` in path order is
   **not v1-DSL-expressible** as a named query. **Materialized view
   is the only path** that does not invoke the §12 budget; spec
   should commit to it.
4. **Two DSL gaps surfaced by real payout query intents.** Reviewers
   asking "which tests cover this function" (intent A) need
   `RETURN e.id` for an aliased edge (today §5.1 only lists
   alias.field / hop_count / aggregates). Cross-app monorepo queries
   (intent B) need `WHERE alias.field LIKE '%client-ui%'` or
   `STARTS_WITH` — §5.1 doesn't enumerate WHERE operators.
5. **The migration-only `crg-bridge` adapter has no consumers
   today.** §11.2 / §11.3 model it as a read-only mirror but nothing
   actually reads from it. **5 SQL-callable views** (modeled on
   pgGraph's `graph.build()` / `graph.sync()` / `graph.vacuum()`
   pattern) turn dual-read into "verifiable from a SQL query" rather
   than "verifiable from a Go test fixture."

---

## 7 concrete spec additions (maintainer to triage)

### A. §11.1 / §11.6 — define per-kind tolerance (spec text)

Replace *"±1% of current `kg build` output"* with:
- `nodes`: ±1% per `(kind)` AND per `(language)`; total file count
  exact.
- `edges`: ±1% per `(kind)`.

Anchor columns: `nodes.kind`, `nodes.language`, `edges.kind` — all
already in the CRG schema. A bootstrap that drops every `Type` row
but doubles `Function` rows stays within 1% on the total. Per-kind
required.

### B. §11.1 `flows` row — commit to materialized view (spec text)

Drop the trichotomy. Reason: `flow_memberships(flow_id, node_id,
position)` is a stored shape the DSL §5.1 RETURN cannot reconstruct
(no `ORDER BY`). Materialized view precomputed at bootstrap is the
only path that doesn't invoke §12 v1.5 promotion.

### C. §11.1 `postprocess` row — weaken "bytes-equivalent" (spec text)

Replace with "structurally equivalent" with one criterion per
derived table:
- `flow_memberships`: set equality
- `communities`: partition equivalence (pin: pair-agreement on a
  seeded RNG sample — simplest, defensible, computable via a
  `community_pairs()` view, see §3 below)
- `risk_index`: Spearman rank correlation > τ (pin τ — likely 0.85)

"Bytes-equivalent" will never pass because `community_summaries`
holds LLM-derived `purpose` / `key_symbols` / `risk` fields.

### D. §11.1 `update` row — require a structured upsert surface (spec text)

The bridge's `parseCRGMutationSummary` regex (`crg.go:259+`) over
free-text stdout is not a usable oracle. Spec should normatively
state: each adapter MUST expose the set of `(qualified_name, kind,
file_path, line_start, op)` tuples produced by an `update` call.
Today the kg-native side has this (it's writing notes); the bridge
side needs an SQL-callable diff like `nodes WHERE updated_at >
$prev_run_at` to make the comparison.

### E. §5.1 RETURN — allow returning edge identity (spec text)

Today §5.1 RETURN lists `alias.field` / `hop_count` /
aggregates. Reviewers want the chain (intent A above) — `RETURN
e.id, e.kind` for an aliased `[e:CALLS]` pattern. Add edge-identity
returns to the grammar; do NOT add edge metadata or paths-as-objects
(those are explicit non-goals in v1).

### F. §5.1 WHERE — enumerate allowed operators (spec text)

Today §5.1 says "WHERE alias.field <op> <expr>" with `<op>` not
enumerated. Add normative list: `=, !=, <, <=, >, >=, IN`. Decide
explicitly on `LIKE` / `STARTS_WITH`. Monorepo cross-app review
queries (intent B above) need one — if rejected, document the
workaround (per-app scope filter at bootstrap, or an explicit
`app_root` / `project` field on nodes).

### G. §11.2 — add the SQL parity-view surface (spec text + 5 views)

The migration-only `crg-bridge` adapter is currently dead weight
(nothing reads from it). Make it load-bearing by specifying the
following SQL-callable views as the canonical consumers. This turns
dual-read into "verifiable from one SQL query" rather than "run the
parity Go test in CI."

| View | Signature | What it unlocks |
|---|---|---|
| `crg_adapter.parity_snapshot()` | `(adapter, schema_digest, nodes_total, nodes_by_kind JSON, edges_by_kind JSON, files, languages JSON, last_bootstrap_at, last_source_commit)` per adapter | Operator query "are the two namespaces in sync as of this commit"; replaces under-specified `build` row test |
| `crg_adapter.compare(a, b)` | per-`(qualified_name)` `(in_a, in_b, kind_a, kind_b, file_a, file_b, line_start_a, line_start_b)` | The set-of-upserts oracle the `update` row needs |
| `crg_adapter.impact_radius(adapter, files_json, max_depth, max_results)` | `(node_id, kind, qualified_name, file_path, hop, source)` same shape from both adapters | Fixed-bound oracle for `impact-radius` row — single view diffed for parity |
| `crg_adapter.community_pairs(adapter, sample_size)` | `(node_a, node_b, same_community)` over seeded-RNG sample | Partition-equivalence oracle for `communities` (criterion C above) |
| `crg_adapter.divergence_summary()` | scalar `(parity_score, build_count_drift, impact_set_drift, community_drift, last_compared_at)` over a stored corpus | Replaces §11.4's "3 consecutive weeks of CI green" boolean with a continuous numeric gate the operator can read |

Views 1 + 2 + 3 are net-required for §11.1 rows to be testable at
all. Views 4 + 5 are higher-investment — defensible only if the
§11.4 decommissioning gate keeps demanding continuous evidence. If
the maintainer cuts views 4 and 5, §11.4 must be re-worded to "no CI
failures for N weeks" rather than "below divergence threshold."

---

## 6 open questions (maintainer decisions)

1. **`crg_adapter.parity_snapshot` shape across both adapters.** Should
   both `crg` (kg-native) AND `crg-bridge` expose the view under the
   same shape so `SELECT * FROM crg_adapter.parity_snapshot WHERE
   adapter = $name` works against either? Cleaner than per-adapter
   schemas; §8.3 cross-adapter rules permit it.
2. **Corpus location for `divergence_summary`.** Embedded as adapter
   fixtures (must ship in `da` binary), pulled from a project's
   `testdata/`, or resolved per-run? Affects what "the parity test
   runs in CI" means — §11.6 currently says "100 commits from
   `master`" without naming `master` *of what*.
3. **Replace §11.4's "3 consecutive weeks of CI green" with a numeric
   `parity_score >= τ for τ weeks`?** Numeric is real signal; boolean
   is flaky-test-vulnerable.
4. **`communities` parity — before or after summarization step?**
   The `community_summaries.purpose` / `key_symbols` / `risk` fields
   are LLM-derived; parity on them is unachievable. Spec should say
   "partitions equivalent; summary fields explicitly excluded."
5. **`flows` `ORDER BY` workaround.** Two options:
   - Store `position` on the `:HAS_MEMBER` edge, sort post-fetch in
     the planner (writes load-bearing planner behavior into the
     contract).
   - Treat as a §12.4 v1 blocker (the new fast-path) and add
     `ORDER BY` to the v1 grammar.
   The first avoids §12 signaling but the second is cleaner.
6. **Bulk-export carve-out.** `ReadNodes(0)` / `ReadEdges(0)` already
   note they violate the uniform bounds contract (`crg.go:907-917`).
   The kg-native bootstrap will need an equivalent. Is this "spec
   to gcc3 to resolve" or does t4 force the choice (paged read with
   cursor vs. carve-out documented as `bulk: true` operation type)?

---

## Real query intents from payout — for t4 testdata corpus

Three concrete review-time intents an agent would issue, with their
DSL mapping + the gaps each exposes. Use these as **seeds** for
`testdata/crg-parity/symbols.json` (the 100-changed-symbol corpus
§11.6 calls for) — they're real, not synthetic.

### Intent A: "which tests cover the manager-native-app token-refresh path"
- Bridge: `impact-radius(files=[manager-native-app/src/data/repositories/Token*.ts])` → filter `is_test=true`.
- DSL: `impact_radius` named query with `(:Function)-[:CALLS|TESTED_BY*1..max_depth]->(:Test)`. **Works.**
- Gap: reviewer wants the **call chain**, not just nodes — addition E above.

### Intent B: "which client-ui components depend on the same auth helper across client-se and client-ui"
- Bridge: no single tool — `semantic_search_nodes_tool` + per-candidate `callers_of`.
- DSL: cross-adapter or filtered `file_path LIKE '%client-ui%'`. **Blocked** until §5.1 WHERE enumerates LIKE/STARTS_WITH — addition F.

### Intent C: "what changed in payment-flow between two commits, and which of those touched a top-10-criticality flow"
- Bridge: `detect-changes(base=$base) → AffectedFlows[]` + `ChangedFunctions[]`.
- DSL: needs both `flows` (precomputed) and `source_mutation` driver. **Fully dependent on §11.1 `flows` committing to materialized-view path** — addition B.

---

## Where this ties back into the workflow

- **graph-backend-adapter-contract spec v6** already documents the
  reconciliation theme (§0 v6 entry). The seven additions above
  belong in a v6.1 follow-on commit OR as part of t4-crg-dual-read's
  in-flight work, depending on how much the maintainer accepts.
- **t4-crg-dual-read notes** should be updated with the testdata
  corpus shape: `testdata/crg-parity/commits.txt` (pinned shas),
  `testdata/crg-parity/symbols.json` (the 100-changed-symbol corpus
  including the three payout intents above), and
  `testdata/crg-parity/expected/<sha>.json` (snapshot per row).
- **gcc3 / gcc4** will resolve some of the open questions
  (bulk-export carve-out, parent-ctx threading); coordinate so this
  parity work doesn't make commitments gcc3/gcc4 then have to break.

The full subagent transcript is available at
`/private/tmp/claude-502/-Users-nikashp-Documents-dot-agents/<session>/
tasks/a3b92fafdb7ba86fd.output` for verbatim review; this proposal
is the distilled, decision-shaped version.
