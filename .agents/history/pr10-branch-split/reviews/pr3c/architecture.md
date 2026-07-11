# Architecture Review — pr3c/kg (PR#18)

_Lens 1 of 3 (architecture + standards), general-purpose reviewer, read-only.
Range reviewed: `pr3b-rebased..HEAD` (pr3c-own changes only)._

## Verdict
**approve**

## Critical findings
None.

## Major findings
None.

## Minor findings / nits

1. **`16cb85b` commit message is stale post-rebase.** It still describes a
   `buildAdjacencyMaps`/`loadEdgePairs` graphstore extraction, but the rebased
   diff is only two kg files (`kg_test.go`, `query_lint_maintain.go`) — the
   graphstore dedup was correctly dropped for pr3b's canonical
   `buildEdgeAdjacency`. No graphstore residue in pr3c's diff. Cosmetic
   (history prose only); resolution rationale documented in `d39c6cb`. Optional
   amend if history is rewritten.

2. **`persistReweavedNote` swallows read/parse errors silently** (bare `return`
   on readErr/parseErr). Pre-existing best-effort-repair behavior, not
   introduced by `f06ab87`; acceptable for a repair path. Worth a one-line
   "intentional silence" comment; not blocking.

## Notes
- **Cohesion & boundaries:** Excellent. Acyclic `commands` → `commands/kg` →
  `internal/graphstore`/`config`/`ui`. The `commands/kg.go` shim + `Deps`/
  `GlobalFlags` DI struct breaks the import cycle without leaking `commands`
  internals. kg consumes graphstore's exported surface only; bridge.go is a
  clean translation seam. Concrete `*graphstore.SQLiteStore` binding is the
  established graphstore convention, not a regression.
- **`NewKGCmd()`/Deps:** consistent with the pr3a/pr3b subpackage pattern.
  **Scope correction:** `explain.go`/`review.go` are NOT modified in pr3c
  (confirmed via `git diff --stat`); only `root.go` (one `AddCommand`) + the
  new `kg.go` shim wire kg. Minimal, additive.
- **Cleanup `d39c6cb`/`bf3fde8`:** faithful. `curationState`+13-step preserves
  ordered-pipeline + abort-on-first-failure exactly (`if !t.Run{...}{return}`
  mirrors original `t.Fatalf` stop semantics; shared state bound to parent t).
  `findKGSub`/`dispatchKG*` hoist sound. `assertNeighborYieldsNothing`
  semantically equivalent (ctx param preserves the granularity).
- **`f06ab87`:** correct fix at the right layer — old empty-body happy-path
  early-return left the body-preserving fallback unreachable, wiping note
  bodies on reweave; fix reads+parses body off disk so reweave only rewrites
  links. Reweave correctly owns body persistence.
- **Breaking changes:** none — entire `kg` surface is new/additive.
- **Stack hygiene:** clean. No pr3b/graphstore/`internal/` files in pr3c's
  diff; `16cb85b` collision correctly resolved kg-only during rebase. The one
  workflow touch (`graph_test.go` in `0eb4d80`) is a test-only Windows fix
  (marshal real `GraphBridgeConfig` vs backslash-corrupting string-concat
  YAML) enabling pr3b's previously-skipping graph-bridge tests; aligns with
  the project YAML-scalar lesson.
