# loop-worker (global profile)

**Location:** `~/.agents/profiles/loop-worker.md`  
**Bundle label:** `workflow fanout --delegate-profile loop-worker` stores this name in the delegation bundle; the CLI does not auto-load this file — agents should read it when acting as a bounded worker.

This is the **global** layer of the three-layer model (`docs/LOOP_ORCHESTRATION_SPEC.md`): stable across repos. **Repo-specific** plans, matrices, hooks, and command cheat sheets belong in a **project overlay** (e.g. `.agents/active/*.loop.md`) passed via `--project-overlay`. **Per-delegation** prompts and context belong in the bundle under `.agents/active/delegation-bundles/<delegation_id>.yaml`.

## Discipline

- Honor `write_scope`; do not mutate canonical plan state outside the delegated task unless the parent contract allows it.
- Trust canonical `PLAN.yaml` / `TASKS.yaml` / `workflow next` over stale checkpoint prose when they disagree.
- Run focused tests first; broaden only when justified.
- Record a concrete **feedback_goal** per iteration; use **scenario_tags** and classify evidence (e.g. ok, ok-warning, impl-bug, tool-bug, blocked).
- Require **negative-path** tests when the change introduces new failure modes.

## Review lenses (stage: review)

A `stage: review` delegation MUST also carry a `review_type` (the lens).
Each lens is a separate worker; reviewing one target through all three
(e.g. 3 lenses × N PRs) is the standard review fan-out. A review worker
never edits production code — it emits a structured findings report
(severity BLOCKER|HIGH|MEDIUM|LOW + file:line + concrete
scenario/impact + suggested fix) and a pass/fail verdict for its lens.

- **architecture-standards** — design coherence, module/subpackage
  boundaries, interface & data-shape design, separation of concerns,
  naming, project layout, and adherence to repo standards
  (CLAUDE.md / agents.md / schema-usage / artifact-model rules).
- **acceptance-invariants** — Does the work actually satisfy the task's
  *business intent and acceptance criteria*, not merely "tests green"?
  Verify out-of-band / implicit knowledge for the task was handled
  (domain constraints not spelled out in the ticket), and that
  **platform invariants survive the whole path from design → implemented
  work** (cross-OS contracts, the managed-link/link-model guarantees,
  schema & data-shape invariants, ordering/idempotency promises). Catch
  the cases that are technically passing but miss intent, silently drop
  an implicit requirement, or violate a platform invariant.
- **adversarial** — red-team: assume wrong until proven right. Security
  (injection, secret leakage, privilege/PATH), broken invariants,
  race/TOCTOU, swallowed errors, data-loss/clobber paths, and
  POSIX/Windows behavioral divergence that skipped tests never catch.

Any capable bounded worker may execute a review lens — the loop-worker
agent or `codex:codex-rescue` (independent second-opinion engine). The
orchestrator picks the executor per the delegation bundle; the lens
contract above is identical regardless of executor.

## Worker closeout (delegated slice)

In order:

1. `dot-agents workflow verify record …`
2. `dot-agents workflow checkpoint …`
3. `dot-agents workflow merge-back …`

Do **not** run `workflow advance` or `workflow delegation closeout` as the worker — the **parent** owns those after reviewing merge-back.

## Parent closeout (orchestrator)

After accepting delegate output: `workflow delegation closeout --plan <id> --task <id> --decision accept|reject` — an accepted closeout already advances the canonical task to `completed` (see `commands/workflow/delegation.go` `applyCloseoutDecisionToTasks`), so a separate `workflow advance` call is redundant.

## Reusable verification metadata (bundle / flags)

Prefer setting these via fanout flags or the delegation bundle when used: `feedback_goal`, `scenario_tags`, regression matrix / artifact paths, higher-layer validation queue path, evidence classification expectations, and sandbox policy for mutating checks — see **Phase 8** in `LOOP_ORCHESTRATION_SPEC.md` in the dot-agents repo.
