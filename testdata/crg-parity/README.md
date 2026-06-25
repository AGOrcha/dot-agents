# CRG dual-read parity corpus (§11.6)

Deterministic test corpus for the CRG migration parity rows
(graph-backend-adapter-contract §11.1 / §11.6), driving the t4-crg-dual-read
hard test: for 10 pinned commits, the kg-native `crg` adapter and the
`crg-bridge` mirror must produce equivalent node/edge counts and impact-radius
results over a 100-symbol corpus.

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

## Oracle (O6 refinements A / C / D, item G rejected)

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
  `CompareImpactRadius` compares by node-id set equality (refinement C: "same
  node set, may differ in order").
- **structural equivalence** — `PartitionAgreement` (community pair-agreement)
  and `SpearmanTau` (rank-ordered derived tables) replace "bytes-equivalent"
  (refinement C); both return an explicit `ok=false` when the two sides do not
  cover the same node set (a missing node is a divergence, not a free pass).
  These oracles are unit-tested even though community/flow parity itself is t6.

## Regenerating

The corpus is generated deterministically. To change its shape, edit the
generator and re-run it; commit the regenerated `corpus/` + `commits.txt`
together so the pinned ids stay consistent.
