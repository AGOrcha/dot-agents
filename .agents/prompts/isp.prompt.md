Run this session using Pattern I_S_P (interactive staged pipeline) as the interactive orchestrator counterpart to the scripted pipeline.

Scope context:
- Plan scope: `<plan_id>[,<plan_id>...]`
- Active task: `<task_id>` once selected
- Active task set: multiple eligible tasks when the runtime is in parallel fanout mode
- Delegation bundle: `<absolute_bundle_path>` once fanout creates or reuses it

Top-level operating model:
- Act as the orchestrator first, not as a collapsed worker
- Stay inside the scoped plan set only; do not jump to plans outside `--plan <id>[,<id>...]`
- Use command-owned workflow surfaces for deterministic control flow and typed artifacts
- Use the staged runtime shape `impl -> verifier(s) -> review -> parent gate` beneath one canonical delegated task contract
- Treat `I_S_P` as the interactive/manual counterpart to `ralph-pipeline`, not as `legacy loop-worker`

Orchestrator startup discipline:
- Load the `orchestrator-session-start` skill when available
- Read `.agents/active/loop-state.md` before selecting work
- Prefer `dot-agents workflow orient` plus `dot-agents workflow next --plan <id>[,<id>...]` as the authoritative control-plane read
- When multiple active plans compete and priority is unclear, use the `plan-wave-picker` skill instead of guessing

Step 1: Probe scoped completion state
- Read the scoped plan list from `--plan <id>[,<id>...]`
- Use `dot-agents workflow complete --json --plan <id>[,<id>...]` to determine whether the scope is `actionable`, `locked`, `paused`, or `drained`
- If the scoped result is `paused` or `locked`, stop delegation for that scope and surface the planning or architectural pause clearly
- If the scoped result is `drained`, report that no actionable task remains in scope and stop

Step 2: Select the next scoped task
- Use `dot-agents workflow next --plan <id>[,<id>...]` to select the next actionable canonical task inside the scoped plan set
- Prefer canonical task state over ad hoc notes or checkpoint hints
- If no task is returned, treat the scope as unavailable rather than searching outside the scoped plan set
- If the runtime is not in scoped-completion mode and multiple eligible tasks remain, select every non-overlapping task the pipeline is allowed to fan out in this pass, up to the `max_parallel_workers` preference limit

Step 3: Decide direct work vs fanout
- Work directly only when the task is research, planning, architecture, or interactive user collaboration with no bounded write_scope
- Otherwise create or reuse bounded delegations for the selected task set, the same way the pipeline orchestrator does
- Keep one canonical delegated task / contract per task; do not split impl, verifier, and review into separate top-level workflow tasks
- In parallel fanout mode, one orchestrator pass may create multiple non-overlapping bundles; in scoped completion mode, stay serialized to one scoped task per pass

Step 4: Fanout the delegated task
- Use `dot-agents workflow fanout` to create the bounded contract and bundle for each selected task
- Pass the canonical plan id, task id, owner, write_scope, project overlay, prompt text, prompt files, context files, and verification controls
- Default orchestrator-side files should mirror the scripted path when they exist:
  - project overlay: `.agents/active/active.loop.md`
  - context file: `.agents/active/loop-state.md`
  - context file: `.agents/workflow/plans/<plan_id>/TASKS.yaml`
- Keep `--project-overlay` and per-delegation prompt files distinct bundle fields; do not pass the same file as both
- Reuse an existing active delegation bundle for the same task when one already exists instead of inventing a second contract
- Treat the bundle as the execution contract carrying task goal, decision locks, required reads, verification focus, exclusions, and stop conditions
- Keep bundle write_scopes non-overlapping when the pass is fanning out multiple tasks in parallel; otherwise defer the conflicting task to a later pass

Step 5: Drive the staged runtime for that bundle
- Expand the delegated task into the staged chain `impl -> verifier(s) -> review -> parent gate`
- Use bundle metadata such as `verifier_sequence` and `app_type` to determine the verifier stages
- Keep stage roles separate; a worker stage does not continue orchestrating the broader plan set
- Treat each delegated stage as a separate session, not one agent changing hats mid-run
- The orchestrator stays in the parent session and spawns a fresh subagent for each stage
- The default session model is:
  - spawn one impl subagent
  - then spawn one fresh subagent per verifier stage in `verifier_sequence`
  - then spawn one fresh review subagent
  - then return to the parent orchestrator / closeout gate for the decision
- Do not reuse one worker chat/session across multiple stages unless the runtime explicitly requires that fallback
- Cross-stage handoff must happen through the bundle and typed artifacts, not through assumed chat memory
- In interactive subagent mode, delegated workers should load the `loop-worker` skill when the bundle or runtime handoff expects it
- Use `/iteration-close` only in worker-scope closeout, never as a substitute for orchestrator task selection or parent gating

Subagent spawn discipline:
- Every spawned stage worker gets only the task-scoped inputs it needs:
  - delegation bundle path
  - stage assignment (`impl`, specific verifier type, or `review`)
  - role-specific prompt surface
  - required artifact paths from prior stages
- The parent orchestrator is responsible for waiting on stage completion, checking that the stage's done artifact exists, and only then spawning the next stage
- If a stage fails for a resumable reason, replace that stage with a fresh subagent on the same bundle/stage rather than trying to continue inside the failed session
- If multiple bundles were fanned out in parallel, each bundle runs its own staged chain independently; the orchestrator may wait on all of them before parent closeout

Impl stage:
- Read the delegation bundle and required context files
- Load `.agents/prompts/impl-agent.project.md`
- Run as its own dedicated subagent session (cheaper agent)
- Implement only inside bundle write_scope unless the bundle explicitly widens scope
- Write `.agents/active/verification/<task_id>/impl-handoff.yaml` with:
  - `task_id`
  - `commit_sha`
  - `write_scope_touched`
  - `ready_for_verification`
  - `tests_unchanged_justified` when applicable
  - `impl_notes`
- Stop after implementation and hand off to verifier stages

Verifier stage:
- Read `.agents/active/verification/<task_id>/impl-handoff.yaml`
- Run each verifier as its own dedicated subagent session (cheap)
- Run verifier stages in the bundle's `verifier_sequence` order
- Use the matching repo verifier prompt surface:
  - `unit`: `.agents/prompts/verifiers/unit.project.md`
  - `api`: `.agents/prompts/verifiers/api.project.md`
  - `ui-e2e`: `.agents/prompts/verifiers/ui-e2e.project.md`
  - `batch`: `.agents/prompts/verifiers/batch.project.md`
  - `streaming`: `.agents/prompts/verifiers/streaming.project.md`
- Follow scoped-first verification: start from `write_scope_touched`, then broaden only when the scoped slice is green and the plan calls for it
- Each verifier writes `.agents/active/verification/<task_id>/<verifier>.result.yaml`
- Do not implement product code in verifier stages unless the bundle explicitly allows it

Review stage:
- Load `.agents/prompts/review-agent.project.md`
- Run as its own dedicated subagent session (medium)
- Review verifier artifacts and `impl-handoff.yaml` using the two-lens contract:
  - phase 1: product, domain, stability
  - phase 2: tech-lead, architecture, standards
- Persist the decision with `dot-agents workflow verify record --kind review`
- Write merge-back for the task via `dot-agents workflow merge-back ...`
- Do not hand-author `review-decision.yaml` unless there is an emergency
- Produce `accept`, `reject`, or `escalate` from the staged evidence, then stop after merge-back is written

Parent gate:
- The orchestrator or review gate reads the review decision, verifier artifacts, and merge-back
- If the staged evidence is not acceptable, fail the gate before closeout rather than silently continuing
- Closeout then processes accepted or rejected merge-backs by running `dot-agents workflow delegation closeout --plan <plan_id> --task <task_id> --decision accept|reject`
- After accepted closeout, run canonical advancement for that task; do not advance before closeout
- Only after parent acceptance should archival, cleanup, and continuation logic proceed
- If the review exposes unresolved planning or architecture questions, pause the scoped completion run instead of continuing automatically

Continuation rules:
- After one task finishes, re-enter scoped completion mode and choose the next actionable task from the same plan scope only
- Ignore bundles, merge-backs, or closeout work that fall outside the active scoped plan set during this run
- If post-closeout fold-back audit is enabled, review `workflow fold-back list --plan <plan_id>` output without letting it expand the current plan scope
- Do not collapse the orchestrator and worker roles into one undifferentiated prompt
- Prefer typed artifacts over chat-summary handoff
- Keep outputs factual and concise
- Surface blockers immediately when verification focus, write_scope, or architectural intent is unclear
