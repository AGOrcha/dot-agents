# Design the loop skeleton, routing, and lifecycle

Operationalizes **§1–§3** of the public guide `docs/full-loop-pipeline-craft.md`.
Load when creating or altering the loop skeleton, a stage profile, a model route, or a
lifecycle/recovery contract.

**Deep dive:** [`references/design-loop-routing.md`](../references/design-loop-routing.md) carries
the full mechanism (wave algorithm, prompt-precedence chain, cross-family binding, and the four
failure-reconciliation modes). This file is a concise loader — read the reference before editing
config.

---

## A. Deterministic loop skeleton (§1)

Two-owner split: the workflow engine owns selection, slots, fanout, and lifecycle; the harness
owns only agent execution. Nothing about *which* task runs, *how many*, or *what transition* a
result triggers may be left to model prose.

- **Compute selection, slot budget, and transitions from canonical engine state.** NEVER let an
  agent choose its own next task or declare its own status.
- **Gate every dispatch on the max-batch set:** dispatch only write-scope-disjoint tasks in one
  wave (two tasks conflict iff any write-scope path is a prefix of the other).
- **Treat `available == 0` and `total_eligible == 0` as clean quiescent stops**, never
  busy-waits.
- **Use the typed predicates:** a slot is held only by in-progress / awaiting-agent-review; a
  dependency is satisfied by completed / awaiting-owner-review, not in-progress. The slot budget
  defaults to a machine-derived value (available parallelism minus a reserve), not a constant.
- **Enforce a wave barrier:** wait on all inner pipelines, then run exactly one serialized
  reconcile that MUST emit `RECONCILED` before the next wave; abort if it does not.
- **Bound the run with an explicit max-waves ceiling;** the live protocol requires it.

**Config/command surface:** the driver reads state through the engine's slots, eligible, and
fanout commands. Do not reimplement selection in prompt text.

---

## B. Stage / profile / model routing (§2)

Routing is **typed config**, not prompt text. Stage profiles are a two-level map:
stage (executor / verifier / reviewer / orchestrator) → slug → profile{label, model,
model_family, prompt_files, precondition_policy} — one routing surface, four consumers.

- **Express every stage as a typed profile with an explicit model AND model family;** refuse to
  emit or dispatch a matched stage whose model or model family is empty.
- **Resolve every stage prompt through the single resolve seam** (base-first, scope-merged);
  NEVER inline duplicate prompt prose into the projection.
- **Preserve prompt precedence:** repo-local prompts override the shared-home starter for the
  same filename (absolute → repo-local project scope → repo-local prompts scope → shared-home
  starter → unresolved).
- **Bind cross-family diversity to the named adversarial lens** and assert
  `reviewer.family != executor.family`; NEVER bind it to a numeric reviewer slot or list order.
- **Keep the model family open-ended** (identity comparison, no closed vendor allowlist), so a
  new tier is a config edit, not a code change.

**Config/command surface:** edit the stage profiles in config; verify each route with the resolve
seam (returns matched flag, model, model family, and the scope-resolved composition). Resolve
topology/lenses per app-type. Legacy per-stage keys fold into the canonical routing map — migrate,
never re-emit the legacy keys.

---

## C. Lifecycle + recovery contracts (§3)

A task moves `fanout → bundle → worker → merge-back → closeout`, all engine-owned. Every failure
mode routes back through reconcile, never to abandonment.

- **Bound inner re-entry with a target count;** a retryable verifier/lens rejection re-enters the
  executor **inside the same active delegation** and NEVER re-fans an active task. Terminal
  fold-back is the result only after that bounded budget is exhausted.
- **Route crash / non-zero / missing-exit through reconcile** as a recoverable lifecycle failure:
  record an idempotent fold-back, persist the failed artifact, close out reject, free the slot —
  never claim success or orphan an in-progress delegation.
- **Make the driver lock pid-aware:** recover a dead owner's lock, refuse a live owner's, and
  reconcile any wave missing `RECONCILED` on startup unless a live pid still owns it.
- **On fanout refusal, write an explicit fold-back for that task** so a successful sibling
  delegation is never stranded.
- **Give each spawned pipeline its own process group and trap signals** to co-terminate the whole
  tree; checkpoint before signal-class kills.
- **Cover crash, stale-lock, and fanout-refusal with explicit recovery tests before shipping.**

**Config/command surface:** reconcile drives lifecycle with the engine's fold-back and closeout
commands and re-runs slots + eligible after every canonical write. Never edit canonical plan/task
state by hand.
