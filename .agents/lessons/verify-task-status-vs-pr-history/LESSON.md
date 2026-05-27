# Verify task status against PR history before fanout

## Pattern

`da workflow eligible` reports tasks whose `status: pending|in_progress` in TASKS.yaml. It does NOT cross-check whether the work already shipped via a merged PR. After parallel-worker batches, this status field commonly drifts — tasks land as merged PRs but the orchestrator's `delegation closeout` step is missed.

## Root cause

`da workflow merge-back` is the worker's exit; it writes the merge-back artifact but does NOT advance status. Per `commands/workflow/delegation.go:1608-1649` (`applyCloseoutDecisionToTasks`), it is `da workflow delegation closeout --decision accept` that BOTH advances task status to `completed` AND archives the delegation artifacts.

Parallel-worker waves complete faster than the orchestrator can manually `delegation closeout` each one. The merge-back artifact lives in `.agents/active/merge-back/<task>.md` but until closeout runs, TASKS.yaml's `status` field is the eligibility source-of-truth and reports stale.

**Symptom:** spawning a worker for "eligible" work, the worker reports "PR #N already merged, byte-identical to master, no action needed" after ~5 minutes of investigation. Burns ~5-10k tokens of worker context and ~30-60k orchestrator tokens for nothing. Worse, a less careful worker could PUSH a stale branch and revert merged work — `[[parallel-worker-branch-drift]]` territory.

Symptoms observed in session 151d7271 (2026-05-26 to 2026-05-27):
- `p1a-mapper-extensions`: TASKS.yaml said pending, PR #88 had merged 4+ hours earlier
- `t1-capture-outcomes`: TASKS.yaml said in_progress, PR #91 merged; dependents t1b/t2/t-archival-policy/t-docs all marked completed — logically impossible without t1 done
- `t8-archive`: advance failed because the plan dir didn't even exist locally (already archived)

## Rule (bake into pre-fanout step for EVERY task before `da workflow fanout`)

1. **Search merged PRs for the task ID** before spawning:
   ```bash
   gh pr list --state merged --search "<task-id>" --json number,title,mergedAt --limit 5
   ```
   Or grep recent commit subjects:
   ```bash
   git log --oneline --all | grep -iE "(<task-id>|<task-keyword>)" | head -5
   ```

2. **If found:** the work has shipped. Skip fanout. Instead run:
   - `da workflow delegation closeout --plan <plan> --task <task-id> --decision accept` (preferred — also archives the delegation contract + bundle)
   - OR if the delegation files were already cleaned up: `da workflow advance <plan> --task <task-id> --status completed` (fallback)

3. **If not found OR ambiguous:** spawn the worker BUT brief it explicitly with the pre-check: "Before any code changes, run `gh pr list --state merged --search '<task-id>'` and `git log --all --grep '<task-id>'`. If the work appears already merged, STOP and report — do NOT push."

4. **For dependent tasks logically signaling completion:** if every task that `depends_on: <X>` is marked `completed`, then X is almost certainly also completed (just status-stale). Strong heuristic; verify with merge search, then closeout.

5. **After EACH merged delegated PR, run `delegation closeout` — NOT `workflow advance`:**
   ```bash
   da workflow delegation closeout --plan <plan> --task <task-id> --decision accept
   ```
   Accepted closeout already sets the task's `status` to `completed` AND archives the delegation artifacts. Running `workflow advance` afterwards is redundant (idempotent but noisy). `workflow advance` is for **direct (non-delegated) completion** OR for repairing **stale status** when closeout was never run.

## Cross-references

- `[[parallel-worker-branch-drift]]` — stale branches PR'd or pushed can revert merged work
- `[[worker-owns-pr-readiness-loop]]` — workers exit at merge-back; the orchestrator's `delegation closeout` is the missing step that keeps eligibility honest
- `[[bundle-scope-via-code-graph]]` — pre-fanout verification with grep/file_summary should now also include the merge-history check
- `[[validate-bundle-against-head]]` — sibling lesson on bundle-time HEAD validation; this lesson is its merge-state complement
