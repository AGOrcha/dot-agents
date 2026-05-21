# Audit target: cg6b-b3-workflow-helpers

**Flagged:** 2026-05-20 (orchestrator session triage)
**Not archived with the 20 sibling 2026-05-20 archival batch** because no evidence the work was ever performed.

## What the contract says

The contract was **authored as spawn-gated** on PR #35 merge — the workflow notes (TASKS.yaml) say *"B3 spawns only post-#35-merge"*. The B3 task is one slice of the `cg6b-ratchet-loop` task on the `coverage-gate-per-file` plan.

## What the evidence shows

- PR #35 has merged.
- No delegation bundle for `cg6b-b3-workflow-helpers` exists in `.agents/active/delegation-bundles/` (the orchestrator did not spawn it).
- No commit on master grep-matches `cg6b-b3` or the b3 helper-coverage shape the contract describes.
- The `coverage-gate-per-file` plan's `cg6b-ratchet-loop` task on master shows B1 and B2 as the only landed slices (PR #26 and PR #35 respectively).

## Possible explanations (open question)

1. **The spawn was forgotten.** Most likely. After PR #35 merged, the orchestrator never came back to spawn B3 even though the gate had cleared. The B3 work (workflow-helpers allowlist shrink) is still genuinely needed if the plan's intent is to ratchet the allowlist to zero.
2. **The contract is stale and B3's content has been absorbed elsewhere.** Possible if the allowlist's workflow-helpers entries were pruned via a non-cg6b mechanism (e.g. a different PR's incidental coverage gain).
3. **B3 was intentionally deferred.** No evidence of explicit deferral in `coverage-gate-per-file`'s TASKS.yaml.

## Recommended next action (for a human or a future orchestrator)

- Check `coverage-gate-per-file`'s allowlist file on master for current workflow-helpers entries.
- If entries remain that the contract intended to prune: spawn the bundle now. The contract describes the work clearly enough to re-author the bundle from.
- If entries are already at zero (somehow), update `coverage-gate-per-file` TASKS.yaml to mark B3 explicitly skipped with the actual route the helpers took, and then archive this file to `.agents/history/archived-delegations/<date>/`.

## Cross-reference

- `.agents/history/archived-delegations/2026-05-20/MANIFEST.md` — the 20-contract archival this audit target was carved out from.
- `coverage-gate-per-file` plan in `da workflow plan show coverage-gate-per-file`.
