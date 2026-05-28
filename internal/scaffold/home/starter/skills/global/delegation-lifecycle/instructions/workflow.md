# Workflow: Delegation Lifecycle

Use this skill when delegating a task to a sub-agent with a bounded write scope and integrating the result back into the canonical plan.

## 0. Pre-fanout checks (parent / orchestrator)

Run these **before** every `workflow fanout` — they prevent wasted spawns and stale-bundle worker churn.

### 0a. Cross-check task status vs. shipped PRs

`da workflow eligible` reports tasks by their TASKS.yaml `status` field, which drifts behind merged PRs after parallel-worker batches. For each task you plan to fanout:

```bash
gh pr list --state merged --search "<task-id>" --json number,title,mergedAt --limit 5
git log --oneline --all | grep -iE "(<task-id>|<task-keyword>)" | head -5
```

If the work already shipped, do NOT fanout. Run:

```bash
da workflow delegation closeout --plan <plan-id> --task <task-id> --decision accept
```

This both archives the delegation artifacts AND auto-advances the task status. Do not also call `workflow advance` — closeout handles it.

### 0b. Validate the bundle's `--write-scope` against current HEAD

Bundle write_scope decays as the tree moves under it. Before fanout:

1. Confirm every file in your proposed `--write-scope` exists on HEAD.
2. Enumerate callers of any symbol you expect to move with a code-graph query and a `grep -rln '<symbol>\b'` pass. Add missed-caller files to write_scope upfront instead of forcing the worker into a fold-back.
3. If the task notes mention "dedup these duplicates" or similar premise-dependent work, confirm the premise still holds on HEAD before fanning out.

### 0c. Confirm no overlapping active delegation

```bash
ls .agents/active/delegation-bundles/ 2>/dev/null
ls .agents/active/delegation/ 2>/dev/null
```

If a bundle for this task already exists, **do not re-fanout** — skip to bundle-to-execution with the existing bundle path.

## 1. Fanout

Run from the parent agent to create the delegation contract and bundle in one step:

```bash
da workflow fanout \
  --plan <plan-id> \
  --task <task-id> \
  --write-scope "<scope-prefix-1>/,<scope-prefix-2>/" \
  --owner "<worker-name>" \
  --delegate-profile loop-worker \
  --project-overlay .agents/active/active.loop.md \
  --feedback-goal "<concrete question evidence must answer>" \
  --prompt-file .agents/prompts/loop-worker.project.md \
  --context-file <path/to/spec-or-context-doc> \
  --selection-reason "<why this task now>"
```

Useful additional flags:
- `--scenario-tag <tag>` — repeatable; surfaces in the verifier scoring matrix
- `--regression-artifact <path>` — repeatable; e.g. a testing matrix the verifier should cross-check
- `--validation-queue <path>` — higher-layer validation queue the worker must drain
- `--verifier-sequence <id1,id2>` — override the staged verifier chain when the project's `app_type_verifier_map` default doesn't fit
- `--verifier-retry-max <n>` — cap how many auto-fix iterations the verifier may run before escalation
- `--require-negative-coverage` — force verifier to demand a regression test for the bug being fixed
- `--skip-evidence-check` — only when intentionally proceeding without a scope sidecar

Expected effects of fanout:
- creates `.agents/active/delegation/<task-id>.yaml` (the contract)
- creates `.agents/active/delegation-bundles/<delegation-id>.yaml` (the bundle the worker reads)
- validates that the plan and task exist
- rejects overlapping active write scopes
- advances the task to `in_progress`

For direct (non-delegated) orchestrator work that still needs a contract for the audit trail, use:

```bash
da workflow contract create --plan <plan-id> --task <task-id> --direct --write-scope <...>
```

## 2. Bundle handoff (orchestrator → worker)

After fanout, the success box prints `Bundle: .agents/active/delegation-bundles/<delegation_id>.yaml`. That file is the **source of truth** for the worker. Do not reconstruct the handoff from chat memory.

Before handing off, write any constraints, risks, or session-context that does NOT fit the bundle YAML fields into the matching task's `notes` field in `.agents/workflow/plans/<plan-id>/TASKS.yaml`. The worker reads `workflow tasks <plan-id>` at startup; notes there are guaranteed to be seen.

Spawn the worker with the bundle path. Patterns:

- **Native subagent (Claude Code):**
  ```
  Agent(
    description="Implement <task_id> in <plan_id>",
    subagent_type="loop-worker",
    prompt="Delegation bundle: <absolute_bundle_path>",
    mode="auto"
  )
  ```
  The `loop-worker` subagent loads its own profile — the prompt only needs the bundle path. Pick `loop-worker` ONLY when a bundle exists; for cross-cutting hygiene work without a canonical task, use `general-purpose` instead.

- **Headless loop script** (long-running batch): hand the worker the bundle path via env or arg; suitable for many-iteration runs without an interactive session.

The worker then follows `instructions/bundle-to-execution.md`.

## 3. Verifier turn (between worker and parent closeout)

If the project registers a verifier_profile (e.g. `pr-ci`) in `.agentsrc.json`'s `verifier_profiles` and `app_type_verifier_map`, the verifier runs automatically after the worker's `merge-back` and before the parent's closeout. The verifier:

1. Polls the PR / CI / SAST surface to terminal state.
2. Auto-fixes mechanical issues (coverage <gate, cog complexity, dup literals, stale allowlist entries, missing focused tests).
3. Writes `.agents/active/verification/<task-id>/<profile>.result.yaml` with a terminal `READY` or fold-back signal.
4. Returns a structured fix-up brief to the impl worker for anything architectural/security/spec — never autofixes intent.

The worker exits cleanly at merge-back; the verifier owns the PR-readiness loop end-to-end. The parent sees a single terminal signal rather than a stream of mechanical-fix round trips.

If the project has **not** registered a verifier_profile, the impl worker owns the readiness loop itself (fallback mode — poll `gh pr checks` + SonarCloud / equivalent gate, auto-fix mechanical issues, do not allowlist new code).

## 4. Merge-back (worker)

Run after delegated implementation + verification finish:

```bash
da workflow merge-back \
  --task <task-id> \
  --summary "Implemented X by doing Y; tests added at Z" \
  --verification-status pass \
  --integration-notes "No conflicts, parent should accept closeout" \
  --commit-state
```

Expected effects:
- creates `.agents/active/merge-back/<task-id>.md`
- records git-diffed changed files
- marks the delegation as `merge-back-complete`
- `--commit-state` stages and commits the workflow-state mutation (iteration-close integration)

**Merge-back alone does NOT advance task status.** The parent's closeout step does.

## 5. Closeout (parent)

Run from the parent after reviewing the merge-back artifact:

```bash
da workflow delegation closeout \
  --plan <plan-id> \
  --task <task-id> \
  --decision accept   # or reject | escalate
```

Closeout BOTH advances task status to `completed` AND archives the delegation contract + bundle + merge-back. Do not also call `workflow advance` for delegated work — that path is for direct (non-delegated) iteration.

For a higher-confidence accept/reject decision, run the gate command first:

```bash
da workflow delegation gate --plan <plan-id> --task <task-id>
```

This evaluates the task-local verification evidence (verifier output + any review-lens results) into a recommended outcome the parent can rubber-stamp.

## 6. Orient / inspect delegation state

At any point:

```bash
da workflow orient
da workflow status
```

These surface the `# Delegations` section, active counts, pending intents, and merge-back queue.

## Coordination intents

If a worker needs to signal the parent mid-execution, set `pending_intent` on the delegation contract. Valid values:

- `status_request` — worker is blocked and wants direction
- `review_request` — worker wants a human/parent review pass before continuing
- `escalation_notice` — worker is surfacing scope or correctness drift
- `ack` — worker acknowledges parent guidance

The parent reads pending intents via `workflow orient` / `workflow status` and responds by editing the contract or chaining the next skill (`iteration-close`, lens-reviewer, etc.).
