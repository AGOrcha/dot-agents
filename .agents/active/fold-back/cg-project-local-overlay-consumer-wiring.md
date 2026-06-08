# Fold-back: project-local overlay resolves in the canonical Resolver but two consumers bypass it

## Observation

Task `config-v2-coherence/cg-project-local-overlay` added the 8th scope (the
gitignored `.agentsrc.local.json` overlay, §7A.1 Axis A) to the canonical
`internal/config` resolver. The bundle write_scope was exactly:

- `internal/config/resolver.go`
- `internal/config/overlay.go`
- `internal/config/overlay_test.go`

The overlay now merges above repo-local committed in BOTH `FlatResolver.Resolve`
and `LayeredResolver.Resolve`. The inputs_digest hashing side was already done by
`cb-content-hash-staleness` (it provisioned the `project-local` slot in
`localScopeManifests` / `ComputeInputsDigest`). So the canonical seam is complete.

Two CONSUMERS of resolution do not flow through the updated stack, and both are
outside this task's write_scope:

1. `internal/config/resolve_locked.go` — `LayeredResolver.ResolveLocked` (the
   offline read seam `da config explain` and `da workflow app-types` use). Its
   flat-degrade branch (`len(rc.Extends)==0`) delegates to `r.flat.Resolve`, so
   FLAT projects DO pick up the overlay (verified via `da workflow app-types`).
   But its extends-present branch (lines ~49-63) assembles its own stack
   (product-defaults → user-local → imported → repo-local) and never appends the
   overlay. So on a project that uses `extends`, the offline explain/app-types
   path would not reflect the overlay.

2. `commands/config/explain.go` — `loadFlatSnapshot` is a SECOND, hand-rolled
   layer-merge implementation private to the command. It only walks three layers
   (product-defaults / user-local / repo-local) and does not consume
   `internal/config.Resolver` at all. `da config explain` therefore never shows
   the overlay (confirmed: skills + features.staged_fanout + repo_id all ignore
   `.agentsrc.local.json`).

## Impact

The overlay is correctly resolvable through the canonical `Resolver` (the contract
this task owns), but the operator-facing `da config explain` surface and the
extends-present offline path do not yet reflect it. No behavior regresses; this is
missing follow-on wiring, not a defect in the delivered seam.

## Recommendation

- Add a one-line `loadProjectLocalOverlay(projectPath)` append to the
  extends-present stack in `resolve_locked.go` (mirrors the two appends added to
  `resolver.go` this task). Trivial, but out of this task's write_scope.
- Retire `commands/config/explain.go`'s private `loadFlatSnapshot` in favor of the
  canonical `internal/config` resolver (or, minimally, teach it the overlay layer)
  so explain stops maintaining a parallel merge implementation. This is the larger
  divergence — explain reimplementing layering — and is its own task.

Suggested home: a `ch`/`ci`-adjacent follow-on under config-v2-coherence, or fold
into whichever task next touches `resolve_locked.go` / `explain.go`.

## Action taken in this iteration

Stayed within write_scope. Delivered the overlay merge + 100%-covered tests in
`internal/config`. Verified the live seam through `da workflow app-types` (flat
project, overlay override + no-overlay negative). Did NOT edit `resolve_locked.go`
or `explain.go`. Recorded this fold-back so the parent can route the consumer
wiring.
