# ISP Step 5: Drive the Staged Runtime

After fanout, drive the staged chain: `impl → verifier(s) → review → parent gate`.

Do not load the legacy `loop-worker` profile, agent, or
`.agents/active/active.loop.md` into these typed stages. The parent injects
the role-specific prompt plus an explicit stage-safe product/project overlay
when one is configured.

## Impl stage

- Read the delegation bundle and required context files.
- Load `.agents/prompts/impl-agent.project.md`.
- Run as a dedicated subagent session (cheaper agent).
- Implement only inside bundle `write_scope` unless the bundle explicitly widens scope.
- The slice runs inside the `da worktree create`-provisioned linked worktree
  (see `fanout.md` § Isolated worktree per delegated slice): its cwd is that
  worktree, with an isolated index, so its commits land only on the slice
  branch. Commit workflow-state with `da workflow commit` (deterministic scoped
  path set — never `git add -A`); once `commit-2-cli-scoped-mode` ships, this
  becomes `da workflow commit --scope task`.
- Write `.agents/active/verification/<task_id>/impl-handoff.yaml` with:
  - `task_id`, `commit_sha`, `write_scope_touched`, `ready_for_verification`
  - `tests_unchanged_justified` when applicable
  - `impl_notes`
- Stop after implementation and hand off.

## Verifier stage(s)

- Read `.agents/active/verification/<task_id>/impl-handoff.yaml`.
- Run each verifier as its own dedicated subagent session (cheap).
- Verifier sequence order from bundle `verifier_sequence`.
- Verifier prompt surfaces: `unit`, `api`, `ui-e2e`, `batch`, `streaming` → `.agents/prompts/verifiers/<type>.project.md`
- Scoped-first verification: start from `write_scope_touched`, broaden only when green.
- Each verifier writes `.agents/active/verification/<task_id>/<verifier>.result.yaml`.
- Do not implement product code in verifier stages.

## Review stage (current consolidated compatibility stage)

- Load `.agents/prompts/review-agent.project.md`.
- Run as its own dedicated subagent session (medium).
- Four-lens contract: spawn one reviewer per lens — `architecture-standards-reviewer`, `acceptance-invariants-reviewer`, `adversarial-reviewer`, `cross-harness-adversarial-reviewer` (see `~/.agents/agents/global/<lens>-reviewer/AGENT.md` and the lens definitions in `~/.agents/profiles/loop-worker.md` § "Review lenses"). Each lens emits its own findings + pass/fail verdict; the parent aggregates.
- `cross-harness-adversarial-reviewer` is not like the other three: still spawn it on every host, but it degrades to a documented non-blocking skip (its own AGENT.md `## Graceful skip` section) when no alternate agent harness — codex/cursor/opencode/copilot — is installed, returning `verdict: pass ... [SKIPPED: no alternate harness]` instead of a real cross-engine pass. Record that verdict as evidence for the lens, not as a missing lens.
- Distinguish two different "skip" reasons; do not conflate them. (a) **Not wired up**: `da workflow resolve-prompt --kind reviewer --slug <lens>` reports `matched: false` (no `stage_profiles.reviewer.<lens>` entry in the effective config) — a fanout-configuration gap, not a reviewer verdict; do not spawn that lens, fix the missing profile entry instead of proceeding. (b) **Wired up, nothing to route to**: the profile is `matched: true` but `cross-harness-adversarial-reviewer`'s own alternate-harness probe finds nothing — spawn it as normal and accept the `[SKIPPED: no alternate harness]` verdict (previous bullet) as that lens's result.
- Persist decision: `da workflow verify record --kind review`.
- Write the workflow merge-back artifact: `da workflow merge-back ...` (this
  records the merge-back doc + delegation state; it does NOT integrate git —
  the parent's `da worktree merge-back` below does that).
- Produce `accept`, `reject`, or `escalate`, then stop.

## Parent gate

- Read review decision, verifier artifacts, and merge-back.
- If evidence is not acceptable, fail the gate before closeout.
- On accept, integrate the slice branch into its parent using the recorded
  base — never raw `git merge` / `git merge-base`:
  ```bash
  da worktree merge-back --name <slice-name> --onto <parent-branch>
  ```
  `merge-back` reads the recorded base and fails loudly (`ErrStaleBase`) if the
  parent advanced or was force-pushed since `create`, then verifies the slice
  HEAD did not drift underneath — so a stale-base merge is caught, not silently
  rebased onto the wrong commit.
- Run: `da workflow delegation closeout --plan <plan_id> --task <task_id> --decision accept|reject` — Accepted delegation closeout completes the delegated task by setting the canonical task to `completed` (see `commands/workflow/delegation.go` `applyCloseoutDecisionToTasks`); do not also call `workflow advance`. Advancement remains for direct, non-delegated work.
- After acceptance: archival, cleanup, and continuation logic.
- If review exposes unresolved planning/architecture questions, pause and do not auto-continue.

## Subagent spawn discipline

- Every spawned stage worker gets only the task-scoped inputs it needs.
- Parent orchestrator waits on stage completion before spawning the next stage.
- If a stage fails for a resumable reason, spawn a fresh subagent on the same bundle/stage.
- Cross-stage handoff happens through the bundle and typed artifacts, not chat memory.
- Use Pattern E (Agent tool) for write_scope ≤ 5 files in interactive Claude Code sessions.
- Use `/iteration-close` only in worker-scope closeout, never for orchestrator task selection.

## Continuation

After one task finishes, re-enter scoped completion mode from Step 2. Select the next actionable task from the same plan scope only.
