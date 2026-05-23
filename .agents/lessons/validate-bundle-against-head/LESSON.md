# Validate delegation bundle assumptions against current HEAD

## Pattern

When the orchestrator authors a delegation bundle from a TASKS.yaml
entry, **the entry's `status`, `notes`, and `write_scope` are
write-time snapshots** — they decay every time the tree moves under
them. A bundle authored against a stale snapshot will either:

1. spawn a worker on work that is already on master (no-op spawn —
   wasted loop budget), or
2. ship the wrong files (typo'd write_scope that doesn't exist in
   current HEAD — worker either deviates from scope to salvage
   intent, or aborts).

Two instances surfaced in a single session (2026-05-23,
`go-test-fixture-extraction` plan):

- **t7-extract-skills-and-agents-tests** — TASKS.yaml said
  `in_progress`. The actual migration had landed in commit
  `c7f1780e` and was folded into `1c6f3b76` during the 2026-05-22
  identity history rewrite. The status was never advanced. Worker
  confirmed 39+15+0 testutil sites already on master and returned
  a clean no-op closeout. Spawn was wasted.
- **t5-extract-commands-root-tests** — bundle declared
  `commands/mcp_settings_test.go` as a target. That single-file
  layout exists in `internal/platform/` but not in `commands/` root,
  which splits into `mcp_test.go` + `settings_test.go`. Worker
  treated it as a typo and touched both files. Defensible but
  technically a scope deviation.

## Root cause

Two contributing factors:

1. **No HEAD-validation step in the orchestrator's pre-fanout
   checklist.** `workflow eligible` reports the schema-level state
   (status, depends_on, conflicts) but does not check that the
   write_scope files exist or that the task's premise (e.g.
   "dedup these duplicates") still holds.
2. **History rewrites silently break task currency.** A `git filter`
   that folds or rewrites commits leaves TASKS.yaml status untouched
   while changing the commits that proved a task's premise. There is
   no link from "rewrite happened" to "re-audit in_progress tasks".

## Rule

Before calling `da workflow fanout`, the orchestrator MUST run a
two-step HEAD-validation:

```bash
# 1. Every file in --write-scope must exist on the target branch.
for f in $(echo "$WRITE_SCOPE" | tr ',' '\n'); do
  test -e "$f" || echo "MISSING: $f"; done

# 2. The task's premise (when stated as "dedup X" / "extract Y" /
#    "raise coverage of Z") must still match HEAD. For dedup/refactor
#    tasks, a quick grep against the target files for the helper
#    name(s) the task promises to migrate to is sufficient:
grep -c "testutil\." $WRITE_SCOPE   # if non-zero across the board,
                                    # the task may already be done
```

If either step shows drift, **do not fan out**. Reconcile TASKS.yaml
first (typo correction, status advance, or both), then re-author the
bundle.

## Why this matters more than it looks

The deeper bug surfaced by both cases is that a stale task can pass
the orchestrator's "eligible" filter and look spawn-ready. A spawn-
ready task with stale premises silently consumes loop budget, fills
the active delegation slot, and forces an awkward closeout
(no-PR / scope-deviation / re-author) that is hard to triage cleanly
after the worker has run. Catching it pre-fanout is order-of-magnitude
cheaper than catching it in worker review.

## How to apply

- **Routine:** make HEAD-validation the last orchestrator step before
  `workflow fanout`, alongside the existing pre-flight (proposals,
  active bundles, loop-state).
- **After a history rewrite:** treat *every* `in_progress` task as
  potentially stale until re-audited. Run the HEAD-validation script
  across the matching `write_scope` for each in_progress task.
  Reference: `[[identity-history-rewrite-2026-05-22]]` for the rewrite
  that caused t7's lag.
- **When authoring TASKS.yaml originally:** include a one-line
  "premise" field (or note prefix) the orchestrator can grep for
  later — "premise: replace local writeMCPConfig with
  testutil.WriteScopeFile" makes the pre-spawn check mechanical.

## Cross-references

- Resolution artifacts:
  `.agents/history/archived-delegations/2026-05-23/t7-no-op/RESOLUTION.md`
- `[[identity-history-rewrite-2026-05-22]]` — the rewrite that caused
  t7's status lag
- `[[stale-local-master-ref]]` — the sibling lesson about verifying
  against origin/master rather than local refs
