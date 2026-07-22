# Iteration Close — Workflow

## Detect Environment

Check which project is active and resolve the binary path:

**dot-agents repo** (`/Users/nikashp/Documents/dot-agents` or wherever `.agentsrc.json` has `"project": "dot-agents"`):
```bash
# Use installed binary or go run from repo root
which da 2>/dev/null || echo "go run ./cmd/da"
```
Run commands as: `da workflow ...` (or `go run ./cmd/da workflow ...`)

**payout repo** (`/Users/nikashp/Documents/payout` or wherever `.agentsrc.json` has `"project": "payout"`):
```bash
# Build dev binary from sibling repo if not already fresh
go -C ../dot-agents build -o /tmp/dot-agents-dev ./cmd/da
```
Run commands as: `/tmp/dot-agents-dev workflow ...`

Confirm the binary resolves before running any commands. A missing binary silently fails — see gotchas.

---

## Write Stop-Gate Sentinel

Write the sentinel **once**, immediately after you know the project, the
`<task_id>`, and whether this is a delegated or direct closeout — and **before**
any verify-record, checkpoint, advance, or merge-back action. The Stop /
SubagentStop gate reads the latest `iteration-close` sentinel and validates the
declared `--expect` artifacts before allowing the agent to stop. No sentinel
means the gate has nothing to enforce this turn.

Pick a filename-safe `--run-id` (a UTC timestamp such as
`$(date -u +%Y%m%dT%H%M%SZ)` works) and set `--agent-type loop-worker` when the
closeout runs inside a delegated subagent, or `main` for direct work. Declare the
artifacts this iteration is contracted to produce as repeatable `--expect`
flags:

```bash
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"

# Delegated closeout (subagent) — expects the merge-back artifact
da workflow hook-sentinel write iteration-close \
  --run-id "$RUN_ID" \
  --plan <plan-id> \
  --task <task_id> \
  --agent-type loop-worker \
  --expect ".agents/active/iteration-log/iter-<N>.yaml" \
  --expect ".agents/active/merge-back/<task_id>.md"

# Direct closeout — no merge-back; the iter-log is the closeout artifact
da workflow hook-sentinel write iteration-close \
  --run-id "$RUN_ID" \
  --plan <plan-id> \
  --task <task_id> \
  --agent-type main \
  --expect ".agents/active/iteration-log/iter-<N>.yaml"
```

Only declare `--expect` artifacts this iteration actually owns. If the review
trio is being skipped for a non-code iteration (see § Invoke Self-Review), do
not list `review-decision.yaml` — the sentinel must describe the real closeout
contract, not an aspirational one. The gate validates artifact presence and
declared sentinel data; it does not require hard remediation on transcript-only
facts unless the hook is given a verified trace.

---

## Record Verification (test)

Run `workflow verify record --kind test` once after all tests pass (or fail) for this iteration:

```bash
# Pass case
da workflow verify record \
  --kind test \
  --status pass \
  --summary "go test ./... — N tests, 0 failures. <focused package>: pass."

# Partial case (some tiers not run)
da workflow verify record \
  --kind test \
  --status partial \
  --summary "Focused: pass. Integration: not-run. Acceptance: not-run."

# Fail case (log but don't close iteration as done)
da workflow verify record \
  --kind test \
  --status fail \
  --summary "go test ./...: FAIL — <error>. Do not advance tasks."
```

The `--summary` should match the test commands actually run in this iteration (e.g., `go test ./internal/platform/...`).

> A test-status `fail` short-circuits the chain: skip Self-Review, skip the review-record / checkpoint --role review pair, and route to fold-back or remediation. `partial` and `pass` continue to Self-Review.

---

## Invoke Self-Review

Per [ADR-0003](../../../../docs/adr/0003-self-review-fire-ordering.md), self-review fires **AFTER** `verify record --kind test` and **BEFORE** `workflow checkpoint`. The chain has three sub-steps: invoke the skill, record the review verdict via the verify-record CLI, then merge it into the iter-log via the existing checkpoint `--role review` path. Anti-scope: this step **calls** `mergeReviewIterLog` and the verify-record review writer; it does **not** redesign them, and adds no new flags to `workflow checkpoint` — the existing `--role review` path is what we use.

### Step 1 — Invoke `/self-review`

```text
/self-review
```

The self-review skill writes `.agents/active/verification/<task_id>/review-decision.yaml` per [ADR-0002](../../../../docs/adr/0002-self-review-output-schema.md). Resolve `<task_id>` from the active delegation contract (`.agents/active/delegation/<task_id>.yaml`) or from `da workflow status` if no delegation is active. Confirm the file exists before continuing:

```bash
ls .agents/active/verification/<task_id>/review-decision.yaml
```

### Step 2 — Record the review verdict (`verify record --kind review`)

Read the `phase1_decision`, `phase2_decision`, `overall_decision`, and (when escalating) `escalation_reason` out of the YAML self-review just wrote, and pass them through:

```bash
da workflow verify record \
  --kind review \
  --task <task_id> \
  --phase1-decision <accept|reject|escalate> \
  --phase2-decision <accept|reject|escalate> \
  --summary "<one-line review summary, traceable to reviewer_notes>"
```

Pass `--escalation-reason "<text>"` whenever the consolidated decision is `escalate`. Pass `--failed-gate <slug>` (repeatable) for any failed verifier gates listed in the YAML. Do **not** pass `--overall-decision` — the CLI derives it from the phase decisions and rejects mismatches; rely on the derived value. The CLI re-validates against `schemas/verification-decision.schema.json` and appends to `.agents/active/verification/log.jsonl`.

### Step 3 — Populate the iter-log review block (`checkpoint --role review`)

Determine the iteration number `<N>` (use the next unused `iter-N.yaml` slot under `.agents/active/iteration-log/`), then:

```bash
da workflow checkpoint \
  --log-to-iter <N> \
  --role review
```

This is the path that has been dead-coded since the iter-log v2 schema landed. With `--role review`, `mergeReviewIterLog` reads `.agents/active/verification/<task_id>/review-decision.yaml` and populates `iter-<N>.yaml`'s `review:` block (`phase_1_decision`, `phase_2_decision`, `overall_decision`, `failed_gates`, `escalation_reason`, `reviewer_notes`, `decision_artifact`). The iter-log review block remaining empty after this step is the canonical "false positive" the test for this skill watches for — the most common cause is omitting `--role review` (the iter-log gains a checkpoint entry but the review block stays at zero values).

Verify by inspecting the produced file:

```bash
sed -n '/^review:/,/^[a-z_]\+:/p' .agents/active/iteration-log/iter-<N>.yaml
```

`review.overall_decision` should be non-empty and `review.reviewer_notes` should be traceable to the `review-decision.yaml` written by self-review.

### Failure modes — what each `overall_decision` means for closeout

| `overall_decision` | What iteration-close does next |
|---|---|
| **`accept`** | Proceed: run § Write the narrative checkpoint, then § Close the iteration (direct) or § Merge-back (delegated). |
| **`reject`** | **Halt closeout.** Do not run merge-back or advance. Fix the rejected items per `reviewer_notes`, re-run focused tests, then restart the chain from § Record Verification (test). The review-decision.yaml from the rejected pass remains on disk as audit history; the rerun overwrites it. |
| **`escalate`** | Route to fold-back. Run `da workflow fold-back create --plan <plan-id> --observation '[review-escalate]: <escalation_reason verbatim>' --propose`. Do **not** run merge-back or advance. The verify-record review entry already captured the escalation; the fold-back surfaces it for orchestrator scheduling. |

### Skipping the review chain (allowed only for non-code iterations)

For docs-only iterations or iterations smaller than the self-review heuristic, you may skip Step 1–3 entirely. Document the skip in the impl checkpoint message (e.g., `--message "Docs-only — self-review skipped"`). The iter-log review block then stays at zero values, which is the canonical signal for "review not applicable this iteration." Do **not** run `checkpoint --role review` without a corresponding `verify record --kind review` — `mergeReviewIterLog` would find no review-decision.yaml and zero out the block on disk.

---

## Write the narrative checkpoint

After the review trio completes (or is documented as skipped), write a narrative
checkpoint so `workflow log` / `status` show the current iteration outcome:

```bash
da workflow checkpoint \
  --message "<iteration summary — what was built and why>" \
  --verification-status pass \
  --verification-summary "Tests: N pass, 0 fail. <scope>."
```

The `--message` should be a 1–2 sentence summary of the iteration outcome — the
same language you would write in `loop-state.md`'s `summary:` field. For partial
or fail status adjust `--verification-status` (`pass` = all intended tiers ran and
passed; `partial` = some tiers not run; `fail` documents the failure state, it
does not mark the iteration done).

The iter-log `impl` block is **not** written here — it is written in § Close the
iteration: by `da workflow close-task` on the direct path, or by an explicit
`workflow checkpoint --log-to-iter <N> --role impl` on the delegated path. (The
review block was already populated in § Invoke Self-Review Step 3; a separate
`--role verifier --verifier-type unit` merge is run by the verifier when applicable.)

After running, verify the checkpoint landed:
```bash
da workflow status
# Confirm the "Next action" / checkpoint text is now current
```

---

## Auto-escalate tool-bugs

If any `[tool-bug]` was logged this iteration, fold it back immediately:

```bash
da workflow fold-back create \
  --plan <active-plan-id> \
  --observation '[tool-bug]: <detail — command, error, reproduction steps>' \
  --propose
```

This routes the bug into the proposal queue for orchestrator scheduling rather than leaving it as perpetual baseline noise. One fold-back per distinct tool-bug per iteration. The active plan ID is the plan the current iteration was working on.

> **Motivating example:** The `pgx/v5` dependency missing from `go.mod` caused `go test ./...` failures for 6+ consecutive iterations. It was documented as `[tool-bug]` but never escalated for scheduling — fold-back would have surfaced it in one step.

---

## Delegation vs direct closeout

The loop orchestrator model (see `.agents/workflow/specs/workflow-parallel-orchestration/design.md`) splits **worker** vs **parent** responsibilities:

| Role | After verify + checkpoint | Who moves canonical task to `completed` |
|------|---------------------------|----------------------------------------|
| **Delegated worker** (fanout created a contract + bundle) | `workflow checkpoint --log-to-iter N --role impl` → `workflow merge-back` | Parent — `workflow delegation closeout --decision accept` after review (the accepted closeout already sets the canonical task to `completed`; no separate `workflow advance` needed) |
| **Direct implementer** (no active delegation on this task) | `workflow close-task` (checkpoint → score → advance → focus → commit) | You, in the same session — `close-task` advances it |

**How to tell:** If `.agents/active/delegation/<task-id>.yaml` exists with `status: active` for the task you implemented, you are in the **delegated** path — use **Merge-back**, not Advance.

Optional: open `.agents/active/delegation-bundles/<delegation_id>.yaml` and read `closeout.worker_must` / `closeout.parent_must` — they list the same commands as the table in the spec (**Delegation bundle workflow**).

---

## Merge-back (delegated worker)

First populate the iter-log `impl` block (close-task is **not** used on the
delegated path — it advances the task to `completed`, which is the parent's call),
then record the merge-back **instead of** `workflow advance`:

```bash
da workflow checkpoint --log-to-iter <N> --role impl

da workflow merge-back \
  --task <task-id> \
  --summary "<what you implemented and how>" \
  --verification-status pass \
  --integration-notes "<merge/conflict notes for parent>"
```

The parent reviews `.agents/active/merge-back/<task-id>.md`, then runs
`workflow delegation closeout --decision accept|reject` as appropriate.
Accepted delegation closeout already sets the canonical task to `completed`
via `applyCloseoutDecisionToTasks` (`commands/workflow/delegation.go`), so a
separate `workflow advance` call is redundant. Do not advance the canonical
task yourself — that would violate the parent/child split the plan describes.
`workflow advance` is reserved
for direct, non-delegated completion.

---

## Close the iteration (direct work only)

Only run when **no** active delegation contract applies to this task (you own the
slice end-to-end). One command runs the whole direct-path close: it writes the
iter-log `impl` block, scores the iteration, advances the task to `completed`,
refocuses the plan to the next eligible task, and commits the workflow state.

Check the current task state first:
```bash
da workflow tasks <plan-id>
# e.g.: da workflow tasks resource-intent-centralization
```

If the task is `in_progress` and the iteration fully resolved it:
```bash
da workflow close-task <plan-id> --task <task-id> --json
# e.g.: da workflow close-task resource-intent-centralization --task phase-6-verification --json
```

`close-task` composes the end-of-iteration primitives —
`checkpoint --log-to-iter N --role impl` → `score iteration N` →
`advance --status completed` → `plan update --focus <next-eligible>` →
`workflow commit` — and emits the result (iteration N, score value + band, sidecar
path, next focus). Render the score back to the operator so the feedback loop
closes while the context is hot; read the `iter-N.score.yaml` sidecar (or
`da score iteration <N>`) for the per-signal breakdown. Useful flags: `--no-commit`
(batch the commit elsewhere) and `--next-focus <task>` (override the auto-pick).
`--score-recompute` takes only `current` today (the default; `recent-N`/`all` are
reserved but rejected until wired).

Do NOT run `close-task` unless:
- The iteration's tests prove the task's acceptance criteria
- The task's dependent tasks are not blocked by remaining work
- The markdown plan's matching checklist item is also checked

If uncertain, leave the task `in_progress` (skip `close-task`, keep the narrative
checkpoint) and note `aligned_with_canonical_tasks: partial` in the loop-state.

---

## Refresh Production Binary

Run this only after a major section or feature is complete, tests are already green, and you expect to rely on the repo-local production binary soon.

```bash
make build-prod
```

Use this as a stability checkpoint, not as a per-iteration default:
- appropriate after closing a canonical task cluster, feature slice, or merge-ready section
- not required for small red/green loops or documentation-only edits
- especially useful when `da` on `PATH` is expected to expose newer Go CLI commands such as `workflow`

After running, verify the binary you expect is now the one you are calling:
```bash
command -v da
da --help
```

If the `PATH` entry still points at an older wrapper or stale build, note that explicitly and keep using `go run ./cmd/da ...` until the shell/runtime wiring is corrected.

---

## Notes

- Full chain (per ADR-0003): **verify record (test) → /self-review → verify record (review) → checkpoint --log-to-iter --role review → narrative checkpoint → close-task** (direct) or **… → checkpoint --role impl → merge-back** (delegated). Never skip verify record even if tests are trivially passing — the log builds audit history. Skip the review trio only for non-code iterations (see § Invoke Self-Review).
- Review-decision.yaml ↔ iter-log: the `--role review` checkpoint call is what closes the dead-coded loop between self-review's output and the iter-log review block. Omitting `--role review` is the documented false-positive — the iter-log gains a checkpoint entry but the review block stays empty.
- **Fold-back** (`workflow fold-back create`) is for recording orchestrator observations into TASKS/plan notes or `~/.agents/proposals/` — not a substitute for verify/checkpoint/merge-back on an implementation slice.
- The checkpoint message persists as `workflow status` "Next action" text — make it forward-looking, not backward-looking
- For payout: rebuild the dev binary only when `../dot-agents` has new commits; skip the rebuild if `/tmp/dot-agents-dev` is already fresh
- `make build-prod` is a separate stability step after major milestones, not part of the minimal per-iteration closeout loop
