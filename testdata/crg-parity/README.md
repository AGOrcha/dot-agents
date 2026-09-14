# CRG dual-read parity corpus (§11.6)

Deterministic test corpus for the CRG migration parity rows
(graph-backend-adapter-contract §11.1 / §11.6), driving the t4-crg-dual-read
hard test: for 10 pinned commits, the kg-native `crg` adapter and the
`crg-bridge` mirror must produce equivalent node/edge counts, upsert tuples and
impact-radius results over a 100-symbol corpus.

**Scope, and what moved out of it.** This corpus covers the three rows where
the oracle is a comparison between the native adapter and the BRIDGE — build /
status, update, impact-radius. It no longer covers the derived views (flows,
communities, the summary tables, FTS). Those are compared against upstream
code-review-graph v2.3.8's own recorded output instead; see
[the derived views](#derived-views-live-in-the-release-contract-not-here) below.

## Layout

- `commits.txt` — the 10 pinned commit ids, in order. One id per line; `#`
  comment lines and blanks are ignored.
- `corpus/<NN>-<commit>.json` — the normalized Tree-sitter ingestion output for
  one commit: `{commit, symbols[], references[]}`. Both adapters ingest the
  identical file (§11.2 requires the mirror to MATCH the same shapes), so any
  parity divergence comes from the comparison oracle, not the input.

## Why synthetic + deterministic

The legacy bridge is a live Python subprocess over a repo's real
`.code-review-graph/graph.db`; a CI parity test cannot depend on that. The
corpus is the normalized ingestion both adapters would produce, pinned so the
test is hermetic and reproducible (gates-must-be-locally-reproducible). The
symbol set is stable across commits (same 100 qualified names) while a rotating
window changes `content_hash` each commit and one symbol is added / one removed
from commit 1 onward — so consecutive-commit diffs exercise insert / update /
delete upsert tuples (O6 refinement D).

## Verified from storage readback (not corpus arithmetic)

The parity surfaces are computed by READING BACK the notes and edges actually
persisted into each namespace (`kg_crg.*` vs `kg_crg-bridge.*`) through the
Store seam — never from the input corpus. The kg-native adapter ingests through
the SDK; the legacy bridge state is seeded INDEPENDENTLY (the hard test models
the external Python process with a legacy seeder), and the `crg-bridge` adapter
exposes it read-only via `MirrorSnapshot`. Equivalence is therefore *verified*,
not guaranteed by construction: `TestHardTest_CatchesDivergence` proves the
comparison FAILS when the bridge drops a kind the kg-native side keeps.

## Oracle (O6 refinements A / D, plus impact-radius set equality)

The comparison is computed in Go over the readback data both adapters expose
through the Store seam (`internal/graphstore/parity.go` +
`internal/adapters/builtin/crg/readback.go`) — never via adapter-authored SQL
views (O6 item G rejected: it would break the §2.2/§5.2 no-raw-SQL invariant):

- **build / status** — `SnapshotFromStore` reads the namespace back and
  `CompareSnapshots` applies per-kind ±tolerance on `nodes.kind`,
  `nodes.language`, `edges.kind` with an exact file count (refinement A).
  Dangling references dropped at write time never reach storage, so the readback
  snapshot does not count them.
- **update** — `DiffFromStore` diffs the prior persisted notes against the
  current persisted notes; `CompareUpserts` compares the structured
  `(qualified_name, kind, file_path, line_start, op)` tuples, replacing the
  bridge's free-text `parseCRGMutationSummary` regex (refinement D).
- **impact-radius** — `ImpactRadiusFromStore` expands the persisted edge graph;
  `CompareImpactRadius` compares by node-id set equality ("same node set, may
  differ in order").

A tolerance-based oracle is the right instrument for these three and only
these three: their inputs are COUNTS and MUTATION TUPLES produced by two
genuinely independent implementations, and there is no canonical recorded form
to compare against instead.

## Derived views live in the release contract, not here

`flows`, `flow_memberships`, `communities`, `community_summaries`,
`flow_snapshots`, `risk_index` and `nodes_fts` are pinned by
`testdata/crg-release/v2.3.8/graph.json` — upstream's own rows for the fixture
repository in `testdata/crg-release/v2.3.8/repo/`, captured from a real v2.3.8
install. `internal/adapters/builtin/crg/postprocess_test.go` seeds a native
store with the fixture's nodes and edges, runs the derivations, and compares
every produced row FIELD FOR FIELD, including the 4-decimal criticality and
cohesion values.

Four oracles used to live in this file and are **deleted**, not relocated:
`flow_memberships` set equality, community partition equivalence via
`PartitionAgreement`, `risk_index` Spearman rank correlation via `SpearmanTau`,
and FTS token-set equality via `CompareFTS`.

They were removed because they could not fail on the bug they were guarding.
Each compared two NATIVE computations to each other under a loose statistical
predicate, and the native algorithms were wrong in ways upstream's recorded
values expose immediately:

- communities were the weakly-connected components of CALLS ∪ IMPORTS, where
  upstream groups by DIRECTORY with generated `<dir>-<keyword>` names,
  per-community cohesion and a dominant language;
- `risk_index` was degree centrality, where upstream scores
  caller-count bands plus a test-coverage gap plus a name-based security
  keyword hit;
- FTS was a slice of qualified-name tokens, where upstream maintains a real
  FTS5 external-content index over `(name, qualified_name, file_path,
  signature)`;
- and `IMPORTS` was not even an edge kind the scanner emits — the term was
  silently always empty.

A pair-agreement score of 1.0 between two copies of the same mistake reports
perfect parity. Direct comparison against recorded upstream rows has no
tolerance to tune and no way to pass on a divergent algorithm, so it is the
only oracle that catches this class of defect.

The release contract also pins the LIFECYCLE semantics the derived views sit
inside (`testdata/crg-release/v2.3.8/lifecycle.json`): a standalone
`postprocess` rebuilds flows, communities and FTS but must NOT recompute the
three summary tables. See `internal/graphstore/CONTRACT.md` for the storage
side of that split, including why the code-graph foreign keys are declared but
not enforced.

## Regenerating

This corpus is generated deterministically. To change its shape, edit the
generator and re-run it; commit the regenerated `corpus/` + `commits.txt`
together so the pinned ids stay consistent.

The release contract under `testdata/crg-release/v2.3.8/` is generated
separately, from a real upstream install:

```
tools/crgrelease/generate_release_contract.py \
  --out testdata/crg-release/v2.3.8 \
  --crg-bin /path/to/code-review-graph
```

Volatile values are normalized to `${REPO_ROOT}`, `${HEAD_SHA}`, `${BASE_SHA}`,
`${TIMESTAMP}`, `${AGE_SECONDS}` and `${DURATION}`; tests substitute them and
must never hardcode a real path, sha or time. `flows.created_at`,
`flows.updated_at` and `risk_index.last_computed` are omitted outright rather
than recorded as an unmatchable stamp.
