# Finding: layered reconciliation model for the git-ref coordination backend

Feeds: git-ref-work-backend spec §9 (D9/D10), workflow-store-concurrency-safe-writes proposal,
Phase-2 swarm scheduler.

## Versioned optimistic concurrency (CAS), two layers
1. MECHANICAL (automatic — git/CAS):
   - Ref-level CAS: `git update-ref refs/agents/state <new> <old>` — `<old>` = expected version;
     stale base ⇒ update fails ⇒ re-read + retry. (NOT bare `update-ref <new>` = last-writer-wins.)
   - PER-RECORD state files (D9 per-task files): semantically-UNRELATED concurrent updates land in
     DIFFERENT files ⇒ no mechanical conflict ⇒ clean union merge, no reconcile. The common case;
     must be automatic. Per-record version/generation counter (or base content-hash) catches a
     genuinely stale same-record base.
2. SEMANTIC (judgment — only when related): a clean TEXTUAL merge can still be semantically wrong
   (two writers advanced a state only one should own). Version/CAS says "base moved"; the semantic
   reconcile decides the merged MEANING. Required only when updates touch the same concern.

## Mechanical conflicts = a HEALTH SIGNAL (rare by design)
With per-record granularity, mechanical conflicts should be rare. When one occurs, ROOT-CAUSE it
(don't just resolve the merge) to one of:
- state layout too coarse (a shared file many writers append to) → split into per-record/per-task files;
- tool/primitive gap (write path not granular/atomic) → the update-ref CAS + WorkStore record API;
- operating instructions (agents writing a shared file instead of their own record; under-declared
  write_scope) → fix the instruction/declaration.

## Already-present signal in this system
PlansBreakdownDoc found 9 write_scope-OVERLAP pairs = the PRE-DETECTED mechanical-conflict risks.
⇒ Phase-2 scheduler rule: NEVER run write_scope-overlapping tasks in the same parallel wave (keeps
mechanical conflicts rare by construction). If a conflict slips through anyway ⇒ a write_scope was
UNDER-DECLARED ⇒ fix the declaration (system/instructions), not the merge.

## This run
Sequential (one writer) ⇒ no stale base. Switch ready_gate's ref commit to CAS form
(`update-ref <new> <old>`) so a stray concurrent writer is still caught; last-writer-wins was the gap.
