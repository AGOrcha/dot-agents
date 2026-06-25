# Eligible Orientation: Run Before Task Selection

Run this step after pre-flight checks and before `workflow next` or task selection. It replaces the bare `workflow next` call with a richer snapshot that the ISP skill can consume directly.

## Command

```bash
da --json workflow eligible --plan <scope>
```

Or without scope filter to see all active plans:

```bash
da --json workflow eligible
```

`<scope>` is a comma-separated list of plan IDs the user wants to focus on. Plans live at `.agents/workflow/plans/<plan-id>/` — the CLI reads `PLAN.yaml` / `TASKS.yaml` there as the source of truth (NOT the markdown).

## What to extract and present

From the JSON output:

| Field | Meaning |
|---|---|
| `total_eligible` | How many tasks are unblocked right now |
| `max_batch` | Task IDs that can run in parallel (no write_scope conflicts) |
| `max_parallel` | Size of max_batch |
| per-task `evidence_confidence` | `none\|low\|medium\|high` — readiness for delegation |
| per-task `has_evidence` | Whether a scope sidecar exists |
| per-task `write_scope_declared` | Whether write_scope is set (false = caution) |
| per-task `conflicts_with` | Which tasks conflict with this one |

## Orientation summary to present

```
Eligible: <total_eligible> tasks  |  Max batch: <max_parallel> tasks
Max batch: [<task_id_1>, <task_id_2>, ...]

Per task:
  [<plan_id>/<task_id>] <title>  evidence: <evidence_confidence>  scope: <write_scope>
  ...

Active delegations: <count from ls .agents/active/delegation-bundles/ | grep -v README>
```

If `total_eligible == 0`: surface the lock/pause state from `workflow orient` and stop.

## Stale-status drift check (before announcing the batch)

`workflow eligible` reports TASKS.yaml `status`, which drifts behind merged PRs after parallel batches. Before you announce a `max_batch`, drop any task whose work already shipped — this is check **0a** of the canonical pre-fanout gate (**`delegation-lifecycle` → `instructions/workflow.md` § 0**); a shipped task gets `workflow delegation closeout --decision accept`, not a batch slot. Spot-check each `max_batch` task with `gh pr list --state merged --search "<task-id>" --limit 3` here so the announced batch is already reconciled.

## Parallel fanout trigger

Announce whether parallel mode is active:
- `max_batch > 1` AND no active delegations → **parallel fanout mode**
- Otherwise → **serialized mode** (one task at a time)

## Sidecar context loading (medium/high confidence tasks)

For each task in `max_batch` where `evidence_confidence` is `medium` or `high`, load the scope-evidence sidecar before chaining to ISP:

```bash
cat .agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml
```

Extract and surface to ISP:
- `required_reads` — files the worker must read before implementing (pass as `--context-file` entries in the fanout bundle)
- `decision_locks` — constraints that must not be violated (surface as locked decisions in the bundle `--prompt`)
- `confidence` — confirm it matches the eligible output value

Pass the sidecar path to ISP as an additional context artifact. ISP's fanout step will include it as `--context-file` when building the delegation bundle.

For `low` or `none` confidence: skip sidecar loading. Note thin context for the worker in the orientation summary; consider running `da workflow plan derive-scope <plan-id> <task-id>` to author a sidecar before fanout.

## Validate write_scope against HEAD

Scope validation (file-exists-on-HEAD, caller walk, coverage-delta forecast) is checks **0b–0d** of the canonical pre-fanout gate (**`delegation-lifecycle` → `instructions/workflow.md` § 0**). Run them per `max_batch` task before passing it downstream, and note any scope expansion in the orientation summary so ISP's fanout step uses the corrected scope, not the stale plan-notes one.

## Chain to ISP skill

After presenting the summary (and loading any sidecars for medium/high tasks), load the `isp` skill with the eligible JSON output and sidecar content as pre-gathered context. The ISP skill's orientation step reads this output rather than re-running eligible.
