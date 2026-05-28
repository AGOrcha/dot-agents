# Bundle → execution (loop-worker + project loop)

Use this when a **delegation bundle already exists** (for example after `workflow fanout` or a harness script). This is the **implementation worker** path — not the orchestrator "pick the next task" path.

## The three layers (read in order)

Do not improvise handoff from chat. The bundle points at the same stack `workflow fanout` records:

| Layer | Typical path | Role |
|-------|----------------|------|
| **1. Bundle (task truth)** | `.agents/active/delegation-bundles/<delegation_id>.yaml` | `scope.write_scope`, `prompt`, `context.required_files`, `verification`, `closeout.worker_must` |
| **2. Global worker profile** | `~/.agents/profiles/loop-worker.md` | Cross-repo habits: respect `write_scope`, tests-first, merge-back discipline |
| **3. Project loop overlay** | `.agents/active/active.loop.md` (and any paths under `worker.project_overlay_files` / `prompt.prompt_files` in the bundle) | Repo dogfood rhythm, loop-state, CLI inventory |

Also read **`.agents/active/delegation/<task-id>.yaml`** for contract title, success criteria, and `may_mutate_workflow_state`.

The canonical plan and task definitions live at `.agents/workflow/plans/<plan-id>/` (`PLAN.yaml` + `TASKS.yaml` + `<plan-id>.plan.md`). Read the matching task entry to pick up any `notes` the orchestrator wrote — those are guaranteed to be load-bearing.

## Pre-implementation checks (worker)

Before touching code, validate the bundle's assumptions against current HEAD — otherwise you may waste a spawn on already-shipped work:

1. **PR / commit search for the task ID:**
   ```bash
   gh pr list --state merged --search "<task-id>" --limit 5
   git log --oneline --all | grep -i "<task-id>" | head -5
   ```
   If the work already shipped, STOP. Do not push. Report a no-op closeout to the parent so they can run `workflow delegation closeout --decision accept` instead of re-fanning out.

2. **Write-scope sanity:** confirm every file in `scope.write_scope` exists on HEAD. Missing files signal a stale bundle — escalate via `pending_intent: escalation_notice` rather than guessing at a typo correction.

3. **Premise check:** if the bundle's `feedback_goal` rests on a premise (e.g. "dedup these duplicates"), confirm the premise still holds. If not, escalate; do not silently expand scope.

## Multi-worktree discipline

If the bundle directs you to work in a worktree (or your project uses `.agents/worktrees/<name>` / `.claude/worktrees/<name>`), **never `cd` to the worktree** for git commands. Always use `git -C /absolute/path/to/worktree <subcommand>`. A single `cd` persists `pwd` across subsequent Bash calls and silently lands branches, commits, and pushes in the wrong worktree.

For build/test commands that genuinely need cwd inside a worktree, use a subshell: `(cd "$WORKTREE" && go test ./...)` — the parentheses prevent pwd leak.

## Execution checklist (worker)

1. Open the **bundle YAML** and note `plan_id`, `task_id`, `delegation_id`, and every path under `prompt`, `context`, and `scope`.
2. Load **`loop-worker.md`** and the **project overlay** (`active.loop.md` or bundle-listed files) before editing code.
3. Implement **only** under `scope.write_scope`. If something requires changes outside it, stop and escalate (parent must re-scope or fan out again). Do NOT add new files to a project's coverage-exceptions / lint-allowlist as a way to dodge a gate — own coverage at ship time.
4. Run **focused tests** for touched packages first, then broader checks as the overlay prescribes.
5. **Close out as the worker** (not as the parent orchestrator):
   - `da workflow verify record` (outcome + summary)
   - `da workflow checkpoint` (iteration message + verification status)
   - `da workflow merge-back --task <task-id> --summary "..." --verification-status pass --commit-state`
   Do **not** run `workflow advance` yourself — that is parent-only for direct work. Delegated work gets advanced by `workflow delegation closeout` on the parent's side.

## PR-readiness loop

If the project registers a `pr-ci` (or equivalent) verifier_profile, you exit cleanly at merge-back and the verifier owns the PR-CI-watch loop. Check the bundle's `verification` block — if a verifier sequence is set, do not also poll CI yourself.

If no verifier_profile is registered (fallback mode), the worker owns the readiness loop:
- Poll `gh pr checks <n>` + the project's SAST/coverage gate to terminal state (60-90s interval).
- Auto-fix mechanical issues: CI test failures, per-file coverage <gate (add focused tests), lint cog complexity, dup literals.
- Never silence a new-file coverage gap by allowlisting; add the tests.
- Only after the PR is genuinely review-ready, run merge-back.

## Parent (after merge-back)

The parent reviews the merge-back artifact, optionally runs `workflow delegation gate` for an evidence-based recommendation, then runs `workflow delegation closeout --decision accept|reject|escalate`. Closeout auto-advances task status — the parent does NOT also call `workflow advance` for delegated work.

See `instructions/workflow.md` for full command examples.

## Relationship to other skills

- **`iteration-close`** — same verify → checkpoint → merge-back ordering; use when persisting loop state after implementation.
- **`orchestrator-session-start`** — chooses work and may **create** fanout; it is not the implementation pass after the bundle exists.
- **`provider-consumer-pair`** — when this bundle is half of a paired-wave slice, sequence per that skill.
