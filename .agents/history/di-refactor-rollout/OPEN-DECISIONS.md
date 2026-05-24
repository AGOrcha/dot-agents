# di-refactor-rollout — open decisions (resolve before executing the 6 tasks)

## OD-1: package-level mutable `deps` singleton (the reference pattern itself)

Surfaced by PR #16 review (Low-2) + maintainer analysis 2026-05-17.

**Finding.** This plan propagates *"the workflow DI pattern … Mirror the
workflow-di-refactor reference implementation"* to 6 packages. The
reference is `commands/workflow/deps.go` — which uses a **package-level
mutable `var deps Deps`** set by `NewCmd(...)` / `InitTestDeps(...)`.
The plan therefore does **not eliminate** the package-global; it
*standardizes on it* (replacing the older `osXxx`/`yamlMarshal`
function-var seams with this singleton). The review's Low-2 is not
resolved by this plan — it would be replicated ×6.

**Why it (currently) doesn't bite.** `da` is a single-shot process; the
singleton is set once at startup. No concurrency, no observed defect.
The reviewer rated it Low / "irrelevant unless used concurrently."

**Decision required BEFORE the 6 tasks run (plan is still `draft`):**

- **(A) Accept & document.** Keep the package-singleton as the
  intentional pattern for a single-process CLI; add an explicit
  rationale comment to the reference (`commands/workflow/deps.go`) so
  the propagated copies are a *justified* choice, not unexamined. Cheap;
  preserves the plan as-is.
- **(B) Revise the reference first.** Thread `Deps` explicitly (param /
  context) instead of a mutable package var, then propagate the better
  shape. Larger; MUST precede propagation or 7 packages get
  reworked twice.

**RESOLUTION (2026-05-17): defer to the graphstore contract spec.**
OD-1 is resolved by `workflow/specs/graphstore-concurrency-contract/`
(decision: Hybrid — contract-first, provider owns concurrency). Under
that contract the `var deps Deps` singleton is the **(A) accept &
document** path *with teeth*: it is acceptable only as a holder of a
**contract-typed handle whose provider owns pooling/serialization** —
the singleton is no longer the concurrency story. di-refactor-rollout
must therefore propagate the *contract-bound* Deps shape, not the
current incidental singleton.

**Sequencing constraint:** di-refactor-rollout's 6 tasks **must not
start until the graphstore contract spec graduates to a plan and pins
the Deps boundary semantics** — otherwise we propagate the unbounded
pattern ×6 and rework it. di-refactor stays `draft`, blocked on the
contract spec. No code changed pending that.
