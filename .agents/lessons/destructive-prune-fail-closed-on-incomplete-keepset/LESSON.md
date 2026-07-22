# A prune/GC that deletes the complement of a keep-set must fail closed when the keep-set is incomplete

**Captured:** 2026-07-15
**Triggered by:** t3b-invocation-time-integrity CAS orphan GC (package-artifact-install). Cross-harness
review found a data-loss BLOCKER: `LiveArtifactDigests()` silently `continue`d past any bound project
whose lock wouldn't read ("best-effort union, skip"), and `GCOrphanedArtifactStore` deletes every
store entry **absent** from that union — so a transient read error on project A's lock dropped A's
live digests from the keep-set and GC `RemoveAll`'d a live artifact A still referenced.

## The mistake

Building a destructive operation as *"delete everything not in the keep-set,"* then computing the
keep-set **best-effort** — skipping any source that errors. The author labeled the skip "conservative
under-collect, never over-delete." **That reasoning is inverted.** For a delete-the-complement
operation, under-collecting the *keep* set means *more* things look deletable, i.e. **over-delete**.
"Conservative" for a delete means *keep more, delete less* — which requires the keep-set to be
**complete or the whole operation to abort**, not partial.

Every gate on the *delete path* (digest-shape allowlist, `assertUnderCASRoot` containment,
abort-on-remove-error) was present and correct — and none of them protect against an **incomplete
input set**. The delete logic trusts the keep-set; the keep-set was silently partial.

## Why it happens

The keep-set builder and the deleter have separate error models. The builder treats "couldn't read a
source" as a local skip (best-effort aggregation, a sensible default for a *read*). The deleter treats
the set it's handed as authoritative. Composed, a benign-looking read skip becomes silent data loss —
and it's invisible to a green test suite because the tests populate readable sources.

## The rule

For any prune / GC / sweep that removes the **complement** of a computed keep-set:

1. **The keep-set is complete-or-error, never partial.** Distinguish "this source legitimately
   contributes nothing" (e.g. `os.IsNotExist` — a project with no lock has no live digests → safe
   empty) from "this source could not be read" (parse error, IO error, permission) → **propagate the
   error; do not skip.**
2. **The deleter aborts on an incomplete keep-set.** If the keep-set couldn't be fully computed, delete
   nothing. Make this a contract: the delete function's doc/signature states the keep-set MUST be a
   non-error result, and every caller short-circuits before any `RemoveAll`.
3. **The completeness guarantee lives in the keep-set's error channel** — the deleter physically
   cannot tell "no one references digest D" from "the project that references D failed to load," so it
   must be *given* a set it can trust.
4. **Test the failure, not just the happy path.** Add a case where a source is present-but-unreadable
   and assert (a) the keep-set computation errors and (b) the referenced entry **survives** because the
   caller aborted. A green delete-the-orphans test with all sources readable proves nothing about the
   dangerous case.

## How to apply

- Grep any `RemoveAll`/prune for the set it trusts; trace back to where that set is built; confirm a
  read failure there propagates rather than `continue`s.
- Mirrors the review discipline in [[tests-must-drive-the-production-path]] and was caught by the same
  cross-harness gate ([[cross-harness-read-raw-not-reconciler]]) — a same-model self-review read the
  "conservative under-collect" comment and accepted it.
