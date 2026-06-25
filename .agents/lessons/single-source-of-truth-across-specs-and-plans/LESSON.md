---
name: single-source-of-truth-across-specs-and-plans
description: Keep each concern in ONE canonical spec/plan; sibling specs/plans that touch the same area must POINT to it, never re-state it — split copies drift, and a task carrying a stale copy makes a worker implement a superseded design and burn a cycle.
type: lesson
---

# Single source of truth across specs and plans (no split-drift)

## Pattern observed

When one concern or decision is **split or duplicated across multiple
specs or plans that touch the same area**, the copies drift. The two
copies start identical, then one side is refreshed (a new idea folded
in, a detail reconciled against shipped code) and the other is left
behind. Now there are two "truths" for the same concern and nothing
flags which is current.

Concrete instance: `config-v2-migration` and `config-v2-coherence`
both described the same config-v2 concern. The detail was edited on one
side and left stale on the other. A task then carried the **stale spec
detail** — a description of a *superseded* design — embedded in its
notes. A worker picked up the task, implemented faithfully against the
stale copy, and burned a cycle building to a design that had already
been replaced. The agent got confused reconciling "what the task says"
against "what the code actually does" before someone caught it and
re-pointed the work.

## Root cause

Two contributing factors:

1. **The same concern lived in two places.** Sibling specs/plans that
   overlap on an area each re-stated the shared concern in full prose
   instead of one owning it and the others pointing at it. Duplicated
   prose has no mechanism to stay in sync — every edit is a chance to
   drift.
2. **A task embedded a drift-prone copy of the spec detail** instead of
   referencing the canonical spec. The task froze a snapshot of one
   side at authoring time; when the canonical design moved, the task's
   embedded copy silently became a description of a dead design. The
   worker had no signal that its instructions were stale.

This is the design-phase sibling of the stale-ref failures: the same
content existing in more than one place, and a downstream consumer
trusting the stale copy.

## Rule

Keep each concern in **ONE canonical doc — a single source of truth.**

- Cross-spec / cross-plan references to the same concern must **POINT
  to the canonical doc, not re-state it.** A sibling that overlaps says
  "see `<canonical-spec>#section`," not a full re-description that will
  drift.
- When **refreshing or activating** a design-phase spec or plan,
  **reconcile every detail against what is ACTUALLY IMPLEMENTED** —
  read the code / shipped behavior, not the other spec's prose. Fold
  in new ideas, and make the edit in the **single canonical doc**.
  Never scatter the same updated content across siblings.
- Tasks reference the canonical spec; they do not embed drift-prone
  copies of spec details into their notes/write-scope.

## How to apply (design-phase activation)

Before activating overlapping draft plans, run a **coherence
reconciliation** across the area:

1. **Verify each detail vs current code / implementation.** For every
   detail a spec/plan asserts, confirm it against shipped behavior
   (read the code, the contract, the tests) — not against a sibling
   doc. Where the doc lags the code, the doc is wrong; fix the doc.
2. **Single-source any concern that's duplicated.** Pick the canonical
   owner for each concern, and replace every sibling copy with a
   pointer to it. After this step, exactly one doc describes each
   concern in full.
3. **Fix stale spec-details embedded in TASKS.** Tasks must reference
   the canonical spec, not embed copies that drift. Replace any
   inline re-statement in a task's notes with a pointer to the
   canonical section, and reconcile the premise against HEAD.
4. **Fold new ideas into the canonical doc.** New design thinking goes
   into the single canonical owner for that concern — never as a fresh
   copy in a sibling spec/plan.

If two activated plans still each own a slice of the same concern after
this pass, that's a smell — consolidate to one owner before fanning out
work against either.

## Why this matters more than it looks

A stale embedded spec-detail passes every schema-level eligibility
check: the task looks well-formed, in-scope, and spawn-ready. The drift
is invisible until a worker has already implemented against it. Catching
it at design-phase activation (a single reconciliation pass over the
area) is far cheaper than catching it after a worker has burned a cycle
building a superseded design and the review has to unwind it.

## Cross-references

- `[[validate-bundle-against-head]]` — the per-task version of the same
  failure: bundle premise/notes decay; HEAD-validate before fanout.
  Single-sourcing prevents the drift; HEAD-validation catches it.
- `[[stale-local-master-ref]]` / `[[stale-local-checkout-mass-drift]]` —
  the runtime-ref siblings: trusting a stale copy of "what's current"
  (a ref vs a spec) leads a downstream consumer to act on dead state.
- `[[consolidate-vestigial-siblings-on-rename]]` — sibling-consolidation
  on rename; the same "one canonical owner, retire the duplicate"
  discipline applied to types/fields instead of specs.
