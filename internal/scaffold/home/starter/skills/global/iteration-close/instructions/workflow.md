# Iteration Close — Workflow

## Detect Environment

Check which project is active and resolve the binary path:

**dot-agents repo** (`/Users/nikashp/Documents/dot-agents` or wherever `.agentsrc.json` has `"project": "dot-agents"`):
```bash
# Use installed binary or go run from repo root
which da 2>/dev/null || echo "go run ./cmd/dot-agents"
```
Run commands as: `da workflow ...` (or `go run ./cmd/dot-agents workflow ...`)

**payout repo** (`/Users/nikashp/Documents/payout` or wherever `.agentsrc.json` has `"project": "payout"`):
```bash
# Build dev binary from sibling repo if not already fresh
go -C ../dot-agents build -o /tmp/dot-agents-dev ./cmd/dot-agents
```
Run commands as: `/tmp/dot-agents-dev workflow ...`

Confirm the binary resolves before running any commands. A missing binary silently fails — see gotchas.

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
| **`accept`** | Proceed: run § Write Checkpoint (impl), then § Merge-back (delegated) or § Advance Task (direct). |
| **`reject`** | **Halt closeout.** Do not run merge-back or advance. Fix the rejected items per `reviewer_notes`, re-run focused tests, then restart the chain from § Record Verification (test). The review-decision.yaml from the rejected pass remains on disk as audit history; the rerun overwrites it. |
| **`escalate`** | Route to fold-back. Run `da workflow fold-back create --plan <plan-id> --observation '[review-escalate]: <escalation_reason verbatim>' --propose`. Do **not** run merge-back or advance. The verify-record review entry already captured the escalation; the fold-back surfaces it for orchestrator scheduling. |

### Skipping the review chain (allowed only for non-code iterations)

For docs-only iterations or iterations smaller than the self-review heuristic, you may skip Step 1–3 entirely. Document the skip in the impl checkpoint message (e.g., `--message "Docs-only — self-review skipped"`). The iter-log review block then stays at zero values, which is the canonical signal for "review not applicable this iteration." Do **not** run `checkpoint --role review` without a corresponding `verify record --kind review` — `mergeReviewIterLog` would find no review-decision.yaml and zero out the block on disk.

---

## Write Checkpoint

Run `workflow checkpoint` after the review trio has completed (or has been documented as skipped):

```bash
da workflow checkpoint \
  --message "<iteration summary — what was built and why>" \
  --verification-status pass \
  --verification-summary "Tests: N pass, 0 fail. <scope>."
```

The `--message` should be a 1–2 sentence summary of the iteration outcome — same language you would write in `loop-state.md`'s `summary:` field.

For partial or fail status, adjust `--verification-status` accordingly:
- `pass` — all intended tiers ran and passed
- `partial` — some tiers not run (e.g., integration skipped)
- `fail` — tests failed; use this to document the failure state, not to mark the iteration done

If the iteration log is being assembled (an `iter-N.yaml` is in flight), pair this with the impl role merge so all three blocks of the iter-log are populated:

```bash
da workflow checkpoint \
  --log-to-iter <N> \
  --role impl
```

This populates the iter-log impl block; the review block was already populated in § Invoke Self-Review Step 3. A separate verifier-role merge (`--role verifier --verifier-type unit`) is run by the verifier when applicable. Together the three roles fill out the full v2 iter-log.

After running, verify the checkpoint was written:
```bash
da workflow status
# Confirm the "Next action" / checkpoint text is now current (not 2026-04-11 stale text)
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

The loop orchestrator model (see `docs/LOOP_ORCHESTRATION_SPEC.md`, `loop-orchestrator-layer.plan.md`) splits **worker** vs **parent** responsibilities:

| Role | After verify + checkpoint | Who moves canonical task to `completed` |
|------|---------------------------|----------------------------------------|
| **Delegated worker** (fanout created a contract + bundle) | `workflow merge-back` | Parent — `workflow delegation closeout` after review; accepted closeout completes the delegated task |
| **Direct implementer** (no active delegation on this task) | `workflow advance` when work is done | You, in the same session |

**How to tell:** If `.agents/active/delegation/<task-id>.yaml` exists with `status: active` for the task you implemented, you are in the **delegated** path — use **Merge-back**, not Advance.

Optional: open `.agents/active/delegation-bundles/<delegation_id>.yaml` and read `closeout.worker_must` / `closeout.parent_must` — they list the same commands as the table in the spec (**Delegation bundle workflow**).

---

## Merge-back (delegated worker)

Run **instead of** `workflow advance` when returning work to a parent agent:

```bash
da workflow merge-back \
  --task <task-id> \
  --summary "<what you implemented and how>" \
  --verification-status pass \
  --integration-notes "<merge/conflict notes for parent>"
```

The parent reviews `.agents/active/merge-back/<task-id>.md`, then runs
`workflow delegation closeout` as appropriate. Accepted delegation closeout
reconciles canonical completion, so it is not followed by `workflow advance`.
Do not advance the canonical task yourself; `workflow advance` is reserved
for direct, non-delegated completion.

---

## Advance Task (direct work only)

Only run when **no** active delegation contract applies to this task (you own the slice end-to-end).

Check the current task state first:
```bash
da workflow tasks <plan-id>
# e.g.: da workflow tasks resource-intent-centralization
# e.g.: da workflow tasks crg-kg-integration
```

If a task is `in_progress` and the iteration fully resolved it:
```bash
da workflow advance <plan-id> --task <task-id> --status completed
# e.g.: da workflow advance resource-intent-centralization --task phase-6-verification --status completed
```

Do NOT advance unless:
- The iteration's tests prove the task's acceptance criteria
- The task's dependent tasks are not blocked by remaining work
- The markdown plan's matching checklist item is also checked

If uncertain, leave the task as `in_progress` and note `aligned_with_canonical_tasks: partial` in the loop-state.

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

If the `PATH` entry still points at an older wrapper or stale build, note that explicitly and keep using `go run ./cmd/dot-agents ...` until the shell/runtime wiring is corrected.

---

## Notes

- Full chain (per ADR-0003): **verify record (test) → /self-review → verify record (review) → checkpoint --log-to-iter --role review → checkpoint (impl) → merge-back** (delegated) or **… → advance** (direct). Never skip verify record even if tests are trivially passing — the log builds audit history. Skip the review trio only for non-code iterations (see § Invoke Self-Review).
- Review-decision.yaml ↔ iter-log: the `--role review` checkpoint call is what closes the dead-coded loop between self-review's output and the iter-log review block. Omitting `--role review` is the documented false-positive — the iter-log gains a checkpoint entry but the review block stays empty.
- **Fold-back** (`workflow fold-back create`) is for recording orchestrator observations into TASKS/plan notes or `~/.agents/proposals/` — not a substitute for verify/checkpoint/merge-back on an implementation slice.
- The checkpoint message persists as `workflow status` "Next action" text — make it forward-looking, not backward-looking
- For payout: rebuild the dev binary only when `../dot-agents` has new commits; skip the rebuild if `/tmp/dot-agents-dev` is already fresh
- `make build-prod` is a separate stability step after major milestones, not part of the minimal per-iteration closeout loop
