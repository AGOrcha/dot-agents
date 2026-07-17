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

## Isolated worktree per delegated slice

The orchestrator provisions each delegated slice as its own managed worktree
with `da worktree create` — never raw `git worktree add` / `git branch`, and
never `git merge-base`:

```bash
da worktree create \
  --name <slice-name> \                    # [a-zA-Z0-9-]+ — derive from the task-id
  --path .agents/worktrees/<slice-name> \  # directory for the new linked worktree
  --base-branch <parent-branch> \          # its CURRENT tip is recorded as the base
  --purpose "<plan-id>/<task-id>"          # free-form registry note (optional --parent-pr <n>)
```

This forks the slice branch, checks it out at `--path` with an **isolated
index**, and records the parent tip as the slice's immutable base — so
concurrent workers cannot cross-stage and the merge-back boundary is fixed at
create time, not re-derived later. The worker's cwd is that worktree, so
`git status` / commits already scope to the slice branch. If a single session
ever touches more than one worktree, use `git -C /abs/path <cmd>` and run
build/test in a subshell (`(cd "$WORKTREE" && go test ./...)`) so a stray `cd`
never leaks `pwd` into a sibling worktree.

## Execution checklist (worker)

1. Open the **bundle YAML** and note `plan_id`, `task_id`, `delegation_id`, and every path under `prompt`, `context`, and `scope`.
2. Load **`loop-worker.md`** and the **project overlay** (`active.loop.md` or bundle-listed files) before editing code.
3. Implement **only** under `scope.write_scope`. If something requires changes outside it, stop and escalate (parent must re-scope or fan out again). Do NOT add new files to a project's coverage-exceptions / lint-allowlist as a way to dodge a gate — own coverage at ship time by adding tests (`[[no-lazy-allowlist-tech-debt]]`). Note `write_scope` often OMITS the tests your change breaks — the **Worker self-gate** below re-derives them.
4. Run **focused tests** for touched packages first, then broader checks as the overlay prescribes.
5. **Close out as the worker** (not as the parent orchestrator):
   - `da workflow verify record` (outcome + summary)
   - `da workflow checkpoint` (iteration message + verification status)
   - `da workflow merge-back --task <task-id> --summary "..." --verification-status pass --commit-state`
   Because you are inside the isolated worktree, any workflow-state commit uses
   `da workflow commit` (deterministic scoped path set — never `git add -A`);
   once `commit-2-cli-scoped-mode` ships this becomes `da workflow commit
   --scope task`. You never merge the slice branch yourself — the parent does
   that via `da worktree merge-back` after closeout review.
   Do **not** run `workflow advance` yourself — that is parent-only for direct work. Delegated work gets advanced by `workflow delegation closeout` on the parent's side.

## Worker self-gate (before push / merge-back)

You are the last agent to touch the diff. Do NOT trust that the bundle's
`write_scope` is complete or that a green PR check means clean — re-derive the
gate against your actual diff. Each rule is the worker-execution counterpart of
the orchestrator's pre-fanout gate (`instructions/workflow.md` § 0 / § "Brief-template
defaults"); links go to the owning lesson, not a restated rule. See
`references/executor-prompt-retro.md` for the full defect→prevention rationale.

1. **Re-derive the coverage-delta — `write_scope` may have omitted broken tests.**
   Before you finish, list the tests your change breaks or makes assert-fail:
   - **Go scope** → walk `*_test.go` callers of every symbol you changed/deleted.
   - **Non-Go scope (docs / config / scaffold prose, e.g. `internal/scaffold`)** →
     the breaking tests are manifest/snapshot tests asserting on the file tree,
     file existence/counts, or embedded content (e.g. a scaffold `copy_test.go`).
   If a broken asserter lives **outside** `write_scope`, escalate via
   `pending_intent: escalation_notice` — do not silently expand scope, and do not
   push with a known-red cross-package test (`[[bundle-scope-via-code-graph]]`,
   `[[tests-for-each-slice]]`, `[[validate-bundle-against-head]]`).
2. **Reason about GOOS before push.** Never hand-join paths or string-munge them
   (e.g. `strings.Replace(p,"~",home)` mishandles the Windows 8.3 short path like
   `RUNNER~1`) — route OS-divergent fs behavior through the project's fs helpers,
   never branch on `runtime.GOOS` ad-hoc. A `t.Skip`/build-tagged test that passes
   locally proves nothing about the skipped platform: treat it as UNVERIFIED until
   its CI shard is green, and mirror the package's existing Windows-skip convention
   (`[[leverage-cross-platform-fs-helpers]]`, `[[match-ci-test-flags-locally]]`).
3. **Run the project's quality gate LOCALLY — a green forge check is not proof.**
   The new-issues signal can stale-read and is bundled into the "Coverage gate"
   check, so green-on-the-PR ≠ clean. Drive the local gate to terminal state and
   fix mechanical findings yourself (`[[sonar-rating-gate-misses-new-issues]]`):
   - Per-file coverage below gate → add focused tests, **never** allowlist new code.
   - Go S3776 cognitive-complexity ≤ 15 (nesting-weighted, ≠ `gocognit`) → extract
     nested bodies into helpers; pass S3776, not the local linter.
   - Shell `.sh` you touched → S7679 (positional `$1`→`local`), S1192 (repeated
     literal→`readonly` const, but data-drive tabular literals to avoid CPD —
     `[[const-extraction-triggers-cpd-on-tables]]`), S7688 (`[`→`[[`).
4. **Validate against FRESH `origin/master` (the active line).** PRs that are green
   individually go red once combined on master. Sync/rebase onto fresh
   `origin/master` before declaring review-ready; never validate against a stale
   local base (`[[stale-local-master-ref]]`, `[[stale-local-checkout-mass-drift]]`,
   `[[parallel-worker-branch-drift]]`).
5. **Name the active-line remote explicitly on `gh`.** In a fork clone, `gh pr
   create` / `gh pr list` default to the wrong remote. Always pass the active-line
   repo explicitly (the concrete repo name is project-overlay config, not a starter
   default) (`[[stale-local-master-ref]]`).

## PR-readiness loop

If the project registers a `pr-ci` (or equivalent) verifier_profile, you exit cleanly at merge-back and the verifier owns the PR-CI-watch loop — the self-gate above then belongs to the verifier, not you. Check the bundle's `verification` block — if a verifier sequence is set, do not also poll CI yourself.

If no verifier_profile is registered (fallback mode), the worker owns the readiness loop — run the **Worker self-gate** above end-to-end:
- Poll `gh pr checks <n>` + the project's SAST/coverage gate to terminal state (60-90s interval), and run the gate LOCALLY (a green PR check stale-reads / bundles into "Coverage gate" — `[[sonar-rating-gate-misses-new-issues]]`).
- Auto-fix mechanical issues: CI test failures, per-file coverage <gate (add focused tests), S3776 cog complexity, S1192 dup literals, shell S7679/S7688.
- Never silence a new-file coverage gap by allowlisting; add the tests (`[[no-lazy-allowlist-tech-debt]]`).
- Only after the PR is genuinely review-ready, run merge-back.

## Parent (after merge-back)

The parent reviews the merge-back artifact, optionally runs `workflow delegation gate` for an evidence-based recommendation, then integrates the slice branch into its parent using the recorded base — never raw `git merge` / `git merge-base`:

```bash
da worktree merge-back --name <slice-name> --onto <parent-branch>
```

`merge-back` reads the base recorded at `create` time and fails loudly (`ErrStaleBase`) if the parent advanced or was force-pushed, then verifies the slice HEAD did not drift underneath. After a clean integration the parent runs `workflow delegation closeout --decision accept|reject|escalate`. Closeout auto-advances task status — the parent does NOT also call `workflow advance` for delegated work.

See `instructions/workflow.md` for full command examples.

## Relationship to other skills

- **`iteration-close`** — same verify → checkpoint → merge-back ordering; use when persisting loop state after implementation.
- **`orchestrator-session-start`** — chooses work and may **create** fanout; it is not the implementation pass after the bundle exists.
- **`provider-consumer-pair`** — when this bundle is half of a paired-wave slice, sequence per that skill.
