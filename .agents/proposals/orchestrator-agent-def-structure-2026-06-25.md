# Orchestrator AGENT.md structure vs the reviewer-family pattern

**Date:** 2026-06-25
**Scope:** project-local (markdown, per `proposal-routing.md` — targets a file specific to this repo's scaffold)
**Target file:** `internal/scaffold/home/starter/agents/global/orchestrator/AGENT.md`
**Status:** comparison + recommended restructure; the behavior-preserving slice is applied to the target file in the same branch.

## Why this matters for performance

An agent definition is a prompt the model reads top-to-bottom on every turn. The reviewer
family converges on the same behavior across four separate lenses because its defs are
*scannable and contract-shaped*: a model can locate "what is my output," "when do I stop,"
and "what must I never do" in fixed places. The orchestrator def carries the same load-bearing
rules but spreads each one across two or three sections, so the highest-stakes constraints
(the pre-fanout gate, the closeout branch, the never-self-edit refusal) are restated partially
in multiple places instead of pinned once. Diffuse rules degrade adherence: the model has to
reconstruct the contract from scattered prose rather than read it off a fixed slot.

## The reviewer-family pattern (what works)

Reference files:
- `internal/scaffold/home/starter/prompts/reviewers/reviewer.base.md` (shared base)
- `internal/scaffold/home/starter/agents/global/adversarial-reviewer/AGENT.md`
- `internal/scaffold/home/starter/agents/global/acceptance-invariants-reviewer/AGENT.md`
- `internal/scaffold/home/starter/agents/global/architecture-standards-reviewer/AGENT.md`
- `internal/scaffold/home/starter/agents/global/cross-harness-adversarial-reviewer/AGENT.md`
- `docs/VERIFIER_REVIEWER_TEMPLATES.md` (authoring conventions / composition model)

Structural traits that make the family effective:

1. **Shared base + thin role-specific overlay.** `reviewer.base.md` owns the generic contract
   ("how to behave and how to record a verdict"); each per-lens AGENT.md owns only "what to
   look for." `VERIFIER_REVIEWER_TEMPLATES.md` formalizes this as the two-axis composition
   model (composition × scope) resolvable with `da workflow resolve-prompt`.
2. **Identical sectioning across every def.** Every reviewer AGENT.md is
   `# Role → # Startup → # Review execution → # Findings format → # Closeout → # Guardrails`,
   in that order, every time. A reader (human or model) knows exactly where each concern lives.
3. **A pinned output contract.** The findings block and verdict line appear verbatim, flagged
   "identical across all lenses" (`reviewer.base.md` §"Findings format"). The deliverable is
   unambiguous and copy-pasteable.
4. **Numbered, copy-pasteable startup steps** (`# Startup` Steps 1–3/4 in each reviewer def).
5. **Concentrated refusal rules.** Stop/never rules live together in `# Guardrails`
   (e.g. "Do NOT call `workflow advance` / `delegation closeout` / `orient` — orchestrator-scope").
6. **One numbered closeout sequence** (`# Closeout`: verify record → checkpoint → merge-back),
   stated once, not re-derived.

## Structural gaps in the orchestrator def

Citing `internal/scaffold/home/starter/agents/global/orchestrator/AGENT.md` by section:

- **G1 — No pinned output/handoff contract.** The reviewer's verbatim findings+verdict block
  has no orchestrator analogue. The orchestrator's actual deliverable — the delegation **bundle**
  plus the load-bearing **constraints written into TASKS.yaml `notes`** before fanout — is
  described in prose scattered across the arc step 4 ("a bundle (the source of truth …) and any
  load-bearing constraints written into the task's TASKS.yaml `notes`") and the Toolset table.
  It is never pinned as a named contract the way reviewers pin their findings format.

- **G2 — Closeout decision criteria stated three times, each partial.** "delegated → `delegation
  closeout` (don't also advance); direct → `advance`" appears in arc step 2, in `# Closeout`, and
  again in `# Guardrails` ("Closeout path depends on direct vs delegated"). Reviewers state their
  closeout sequence exactly once. Triplication invites drift between the three copies.

- **G3 — Refusal/stop rules are scattered, not concentrated.** The orchestrator's hard refusals —
  never self-edit a slice, EXPAND-vs-REFUSE on a non-empty coverage delta, never re-fanout an
  active bundle, don't poll CI when a verifier owns the watch — are spread across `# Role`
  (the no-Write block), the arc steps 3 and 5, and `# Guardrails`. Reviewers concentrate the
  equivalent rules in `# Guardrails`. The orchestrator already *has* a `# Guardrails` section but
  it duplicates rather than centralizes.

- **G4 — The pre-fanout gate (arc step 3) is prose-dense, not a checklist.** This is the
  orchestrator's single highest-leverage gate (HEAD-validate write_scope, caller walk,
  coverage-delta forecast, EXPAND-vs-REFUSE). Reviewers render their highest-stakes procedure as
  numbered steps; here it is one dense paragraph plus a sub-bullet split. It is the hardest part
  of the def to scan and the part where a missed step is most expensive.

- **G5 — Section vocabulary diverges from the family.** Reviewers use a stable, shared section
  set. The orchestrator uses `# Role → # Startup → # The orchestration arc → # Closeout →
  # Toolset → # Guardrails → # Reference`. "The orchestration arc" bundles orient, reconcile,
  the pre-fanout gate, fanout, and monitor under one heading — five distinct concerns the
  reviewer pattern would separate. The orchestrator legitimately needs an "arc" (it is a
  multi-step turn, not a single-pass review), so the fix is not to delete the arc but to make
  the *contract-shaped* concerns (output contract, closeout branch, refusals) addressable on
  their own the way reviewers make them addressable.

## What does NOT transfer (and must be preserved)

The orchestrator is structurally different from a reviewer in ways the restructure must respect:

- It is **pure orchestration — no `Edit`/`Write`/`NotebookEdit`** (frontmatter `tools:` and the
  `# Role` hard-rule block). Every state mutation routes through a `da workflow` subcommand. The
  restructure must not introduce any phrasing that implies a write tool.
- Its turn is a **multi-step arc**, not a single-pass review — so a literal one-to-one section
  copy of the reviewer template is wrong. The arc stays; we add contract-shaped slots around it.
- It carries **multi-remote orient discipline** (arc step 1: identify the active-line remote via
  `git remote -v`, cross-check both refs, derive eligible from the active line only) and **named
  closeout verbs** (`delegation closeout --decision …` vs `advance`). These are semantics, not
  formatting — preserve verbatim.
- The **§0 pre-fanout gate** is consolidated in `orchestrator-session-start` / `delegation-lifecycle`;
  the AGENT.md references it. Keep the reference; do not relocate the gate's authority.

## Recommended restructure (behavior-preserving)

Goal: make the orchestrator def scan like the reviewer family **without changing a single
semantic**. Concretely:

1. **Add a pinned `# Output contract` section (closes G1).** A short, fixed block naming the
   orchestrator's two deliverables before fanout — the **bundle** (source of truth, never
   reconstructed from chat) and the **constraints written into TASKS.yaml `notes`** — plus the
   single closeout decision rule. This is the orchestrator's analogue of the reviewer findings
   block: one place a model reads "what must I produce." Pull the wording from existing prose in
   arc step 4 and `# Closeout`; do not invent new obligations.

2. **Render the pre-fanout gate as a numbered checklist (closes G4).** Keep every clause from arc
   step 3 (HEAD existence, caller walk, coverage-delta two shapes, EXPAND-vs-REFUSE) but as
   numbered steps. No new requirements; same gate, scannable.

3. **State the closeout branch exactly once (closes G2).** Keep it in the new `# Output contract`
   (or in `# Closeout`), and replace the two other copies with a one-line pointer rather than a
   restated rule. The named verbs stay verbatim.

4. **Leave `# Guardrails` as the single home for refusals (closes G3)**, and trim the in-arc
   restatements to pointers. The no-self-edit hard rule stays in `# Role` (it is the role's
   defining constraint) but the *operational* refusals (re-fanout, CI-poll, EXPAND-vs-REFUSE)
   concentrate in `# Guardrails`.

5. **Keep the `# The orchestration arc` heading (G5 is addressed, not by renaming the arc, but by
   giving the contract concerns their own sections around it).** The arc remains the execution
   narrative; the contract-shaped concerns become first-class sections like the reviewer family.

### Risk assessment

Low. Every change is relocation + numbering of existing text, plus one new section assembled
entirely from sentences already in the file. No new tool, no widened allow-list, no changed verb,
no altered gate logic. The scaffold ships this file as content; the breaking test for scaffold
changes is the manifest/embedded-content test, which asserts on file presence/tree, not on the
prose body — so re-sectioning does not break the manifest assertion. (If a content-hash assertion
exists it will flag the edit as an intended content change, reviewable in the diff.)

### Applied vs deferred

The **behavior-preserving re-sectioning (items 1–5) is applied** to the target AGENT.md in this
branch, because it is pure relocation/numbering of existing text with no semantic change.

**Not applied (deferred, out of scope for a behavior-preserving pass):** factoring the orchestrator
def into a shared `orchestrator.base.md` + thin overlay the way reviewers split base/lens. The
reviewer split pays off because there are *four* lenses sharing one contract; there is exactly one
orchestrator, so a base/overlay split adds a composition layer with no second consumer today. If a
second orchestration profile is ever added (e.g. a release-orchestrator), revisit the split then —
it would be the same move `VERIFIER_REVIEWER_TEMPLATES.md` describes, applied to orchestration.
