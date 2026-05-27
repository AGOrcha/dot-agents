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
- Three-lens contract: spawn one reviewer per lens — `architecture-standards-reviewer`, `acceptance-invariants-reviewer`, `adversarial-reviewer` (see `~/.agents/agents/global/<lens>-reviewer/AGENT.md` and the lens definitions in `~/.agents/profiles/loop-worker.md` § "Review lenses"). Each lens emits its own findings + pass/fail verdict; the parent aggregates.
- Persist decision: `da workflow verify record --kind review`.
- Write merge-back: `da workflow merge-back ...`
- Produce `accept`, `reject`, or `escalate`, then stop.

## Parent gate

- Read review decision, verifier artifacts, and merge-back.
- If evidence is not acceptable, fail the gate before closeout.
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
