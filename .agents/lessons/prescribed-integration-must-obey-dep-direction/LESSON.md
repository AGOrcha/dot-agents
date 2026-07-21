# A bundle-prescribed integration that crosses the package DAG the wrong way is impossible — re-derive the cycle-free equivalent, don't force it

## Pattern (t2c-codex-import-seam, package-artifact-install, 2026-07-16)

The t2c bundle told the worker to "route unmarked-collision codex renders into the import
seam" — concretely, to call `commands/import.go`'s `importCandidate` /
`writeImportConflictReviewNote` machinery from the low-level renderer
`writeCodexAgentTomlFile` in `internal/platform/codex.go`.

That call is **architecturally impossible**: `commands` already imports `internal/platform`,
so a call from `platform` back into `commands` closes an import cycle. The bundle prescribed
an integration that runs *against* the dependency DAG. A worker that took the instruction
literally would either not compile or smear logic into the wrong layer to dodge the cycle.

## Root cause

A delegation bundle describes intent at the *behavioral* level ("reuse the import seam") but
often names a *concrete call site* to reuse. The named call site lives in a higher layer than
the code being edited. The bundle author didn't check the import direction between the edit
target's package and the reuse target's package. Package dependency edges are a hard DAG — the
compiler enforces them — so a prescribed call in the forbidden direction has **no valid
implementation**, only workarounds that damage layering.

## Rule

1. **Before writing a bundle that says "call X from Y", check the import direction.** If `Y`'s
   package would have to import `X`'s package and that edge doesn't already exist (or reverses
   an existing one), the prescribed call is invalid. Prescribe the *behavior*, not the call site.

2. **As the worker, when a prescribed integration would violate the DAG, do not force it** (no
   back-channel globals, no moving the callee down a layer to "make it fit", no duplicating the
   whole high-level flow into the low-level function). Re-derive the cycle-free equivalent:
   - Make the low-level function **self-contained** — derive everything it needs from the args
     it already receives (t2c: `writeCodexAgentTomlFile` derived collision resolution from `dst`
     alone, since its only call site passes no `project`), rather than reaching up for context.
   - Where a genuine cross-layer touch is unavoidable, cross **only in the legal direction**:
     export a small predicate/helper from the low layer and consume it from the high layer
     (t2c: exported `platform.IsManagedCodexAgentTomlFile`, called from `commands/import.go` —
     `commands`→`platform` is the one cycle-free direction).
   - Duplicate a *small* format-compatible struct locally rather than import a type across the
     forbidden edge (t2c wrote a same-YAML-tags review-note struct in `codex.go` instead of
     importing `commands.importConflictReviewNote`).

3. **Flag the re-scope in the merge-back** so the parent knows the literal bundle instruction
   was infeasible and what the cycle-free substitute was — the parent may want to thread a real
   parameter (t2c flagged that a friendlier review-note `Scope` would need `config.Load()` in a
   low-level renderer, deferred as a follow-up rather than smuggled in).

## Cross-links

Sibling of [[build-tagged-test-import-cycle]] (an OS-tagged *test* closing an invisible import
cycle) and [[bundle-scope-via-code-graph]] (pre-validate the bundle's file/caller scope against
the actual code graph — the same "author bundles against the real graph, not the prose" spine,
one level up at the *dependency-direction* granularity). Re-scope discipline sibling of
[[validate-bundle-against-head]].
