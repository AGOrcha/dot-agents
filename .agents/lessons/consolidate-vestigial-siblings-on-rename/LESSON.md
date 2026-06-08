# A "rename/generalize this primitive" request usually means "consolidate the duplication around it"

## Pattern

Asked to rename `VerifierProfile → StageProfile` (so one type represents
executor/verifier/reviewer/orchestrator), I first scoped to the literal rename + a back-compat
alias. The maintainer corrected: also retire the **duplicated** `reviewer_profiles` and the
**vestigial** `app_type_verifier_map` — "all of those need to be transitioned into the new format so
it works as intended." The real intent was *consolidation*, not a rename.

## Root cause

A primitive rarely drifts alone. When a type is being generalized, its siblings are usually:
- **duplicates** — a second map/field with the identical shape (`reviewer_profiles` mirrored
  `verifier_profiles`), or
- **superseded-but-not-retired** — a surface a newer one already replaced *in design* but whose
  *consumer was never migrated*, so the old surface stays load-bearing. Here
  `execution_profile.topology` literally documented that it "supersedes the flat
  app_type_verifier_map," yet `delegation.go` still read the flat map — that lagging consumer was the
  only thing keeping it alive.

Scoping to the named rename leaves the duplication/vestige in place and misses the point.

## Rule

When asked to rename, generalize, or "make X the intended form":
1. **Audit for siblings before planning.** Grep for fields/keys with the same shape (duplicates) and
   for comments/docs saying "supersedes / replaces / successor to / moves here" (superseded surfaces).
2. **Follow the consumers.** A surface is only vestigial-in-practice if every consumer has migrated.
   Grep each legacy key's read sites; a superseded surface with a live consumer is the actual work.
3. **Check for byte-level redundancy.** A legacy key whose data already exists verbatim in the
   successor (the live `app_type_verifier_map` exactly duplicated `topology.verifier_sequence`) can be
   *deleted*, not just aliased — confirm with a diff before assuming you must preserve it.
4. **Propose the consolidation, surface the scope jump, and ask** (delivery vehicle + hard-cut vs
   back-compat) rather than silently doing the minimum.

## How to apply

Before writing the plan for a rename/generalize task:
- list the sibling surfaces (duplicate shapes + superseded surfaces) and their live consumers;
- decide per sibling: fold-in, retire, or out-of-scope (and say which);
- treat back-compat as read-fold + canonical-write + migrate-now + deprecate, not permanent aliasing.

## Cross-references

- `.agents/workflow/specs/stage-profile-and-routing-consolidation/design.md` — the consolidation this
  lesson came from (PR #45, supersedes #40).
- [`additive-state-fields`](../additive-state-fields/LESSON.md) — the inverse case (extend, don't
  overload); here the call was to *unify*, not extend.
