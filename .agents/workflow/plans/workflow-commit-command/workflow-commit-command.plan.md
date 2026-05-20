# workflow-commit-command — plan (non-normative)

Spec: `.agents/workflow/specs/workflow-commit-command/design.md` (the
contract). Graduates Part 2 of the (applied) global proposal
`workflow-state-commit-coupling`.

## Ordering rationale

`wc-path-derivation` first — the deterministic, scope-disciplined path
set is the correctness keystone (the whole risk is "never stage what we
didn't derive"); everything else consumes it. The subcommand wraps it;
prefs opt-out gates both the subcommand and the iteration-close hook, so
it precedes `wc-iteration-close`. `wc-verify-close` proves the spec
done-criteria as one gate.

## Sequencing / dependency

- **Hard dep: `pr10-branch-split/pr3b-workflow`** (cross-plan). The
  workflow subpackage must exist; this builds on it.
- **Timing: follow-up after 0.3.1 ships** (post-#16 merge). Explicitly
  NOT folded into the approved/green #16 — additive new command, clean
  0.3.x follow-up. The active Part 1 operator rule covers the gap until
  this lands.

## Deferred

- Code-deliverable auto-commit (this is workflow-state only).
- Any daemon/watcher; cross-repo orchestration.
- Independent of `graphstore-concurrency-contract` and
  `di-refactor-rollout`.

## Note on the tracking gap this closes

The originating proposal's disposition claimed Part 2 was "FOLDED into
pr10-branch-split as task workflow-commit-state-coupling" — it never
was (no such task in any canonical plan). This plan is the actual,
correct landing of that work.
