# Loop Agent Pipeline Plan

Status: Active

## Outcome

Keep the already-landed role surfaces and typed artifacts, then finish the missing runtime work so the live `ralph-*` pipeline actually runs:

- `impl`
- `verifier(s)`
- `review`

instead of collapsing everything into a single legacy loop-worker path.
Use the new terminology:

- `I_S_P` = interactive staged pipeline; the slightly interactive/manual counterpart to the scripted staged pipeline
- `legacy loop-worker` = the older single-worker merge-back path

The revived plan now also covers the missing parent-side runtime gate: after each task finishes, the orchestrator should review the task outcome before the same plan is allowed to continue.

## Resurrection Note

This plan was previously archived as completed. The archive overstated the live runtime state.

What is actually true:

- repo-owned impl/verifier/review prompt surfaces exist
- typed verifier and review artifacts exist
- `workflow fanout` can persist `app_type` and `verifier_sequence`
- `workflow bundle stages` expands one bundle into `impl`, ordered verifier stages, and `review`
- `ralph-worker` now loads stage-specific prompt surfaces instead of treating every role as the same loop-worker prompt
- the repo should stop using `Pattern E` as the name for the interactive staged runtime; `I_S_P` is the new term, while the old single-worker path is `legacy loop-worker`

What is still not true:

- the post-task review gate is still lighter than the spec wants and still leans on auto-accept heuristics in auto mode
- parent-side reasoning still sits too far toward closeout instead of a richer per-task orchestrator review/apply step
- the runtime cannot yet drive a scoped plan set to completion while pausing on planning / architectural decisions
- some docs and historical notes still use `Pattern E` when they really mean either `I_S_P` or `legacy loop-worker`
- legacy `.agents/active/*.plan.md` artifacts that were created under older repo rules should keep being migrated into canonical `.agents/workflow/plans/` or `.agents/workflow/specs/` bundles so `I_S_P` and scripted runtime work evolve against one canonical artifact layout

## Reopened Tasks

### `p8-orchestrator-awareness`

Reopened. The missing work is runtime role-aware dispatch:

- orchestrator must emit stage-appropriate handoffs instead of loop-worker-only bundles
- pipeline must stop treating every bundle as a single `ralph-worker` worker
- worker/runtime prompt selection must match the spec's impl/verifier/review surfaces

### `p6-fanout-dispatch`

Reopened. The missing work is runtime consumption of the data already carried in plan schema, `.agentsrc`, and delegation bundles:

- `app_type` and `verifier_sequence` must drive stage execution, not just bundle contents
- fanout/bundle tests must prove the runtime chain, not only serialization
- verifier stages should reuse an extended `workflow verify record` surface instead of introducing a near-duplicate command

### `p7-post-closeout`

Reopened. The missing work is not just "end-of-session closeout". The runtime needs a post-task orchestrator review gate that runs immediately after merge-back:

- accept or reject the task
- review typed verifier/review artifacts
- archive the per-task verification directory and remove the active task directory after accept
- reconcile fold-backs and proposal-worthy findings
- decide whether the same plan may continue automatically

### `p11-plan-completion-mode`

Added. The runtime needs a scoped plan-completion loop:

- take a single plan id or a comma-separated plan filter
- continue task by task within that scoped plan set only
- when a worker dies for a resumable terminal reason, start a fresh agent on the same bundle/stage and continue from repo + artifact state
- when the failure is a usage / rate limit, prefer a different configured agent bin or model path instead of rerunning the exact same capped runtime
- pause delegation for that plan when analysis, architectural review, and/or human assisted planning is required
- resume only after the planning lock is cleared

## Runtime Boundary

Future runtime work should keep the non-deterministic parts in agents and move deterministic mechanics into `dot-agents`.

For implementation fanout, "bounded by `write_scope`" is necessary but not sufficient. The delegated bundle should act like an execution contract that also carries:

- one concrete task goal
- locked decisions and invariants the worker must preserve
- exact required reads
- a focused verification target
- explicit exclusions and stop conditions

If those fields are missing, the runtime is forcing workers to reconstruct planner intent instead of executing it.

Agent-owned:

- implementation choices inside `write_scope`
- verifier evidence gathering and pass/fail judgment
- review acceptance / rejection / escalation reasoning
- deciding whether a cross-cutting observation belongs in fold-back, proposal review, or a planning pause

Allowed implementation autonomy should stay local: helper extraction, assertion wording, and small fixture details inside the bounded task.

Command-owned:

- scoped plan selection and loop-break checks
- plan and delegation lock enforcement
- expansion of bundle metadata into a concrete staged runtime plan
- typed verifier / review artifact writes
- task and plan state reconciliation after the parent decision is made
- task-scoped archival / cleanup
- replacement-worker scheduling for resumable bundle/stage failures
- rate-limit aware runtime fallback selection for replacement workers
- rejecting or pausing tasks that still hide major architectural decisions or undefined verification targets

## Runtime Agent Briefing

These facts should be treated as current audited reality so a follow-on runtime agent does not need to rediscover them:

- `bin/tests/ralph-pipeline` still runs a three-phase shell flow: orchestrate, one `ralph-worker` per bundle, then `ralph-closeout`.
- `bin/tests/ralph-orchestrate` still creates loop-worker bundles and emits `RALPH_BUNDLE: <path>` lines, but it does not drive a distinct impl/verifier/review chain.
- `bin/tests/ralph-worker` still contains the legacy loop-worker fallback when called without `--stage`, even though the staged runtime path is now the target for both `ralph-pipeline` and `I_S_P`.
- `commands/workflow/delegation.go` already persists `app_type` and `verifier_sequence`, so the missing behavior is runtime consumption, not schema invention.
- `commands/workflow/verification.go` already uses `workflow verify record --kind review` as a structured writer for `review-decision.yaml`; non-review kinds still only append a global log row.
- the review contract is two-lens by design: phase 1 is broad product/domain/stability review, phase 2 is tech-lead / architecture review, but the live runtime still does not execute that review stage distinctly.
- `commands/workflow/cmd.go` already exposes `workflow next --plan <id>` and fold-back commands, so scoped completion and post-task review should build on existing CLI surfaces rather than replacing them with shell parsing.

## Design Constraints

These constraints are now part of the active plan and should not be re-litigated unless code reality forces it:

- Keep one canonical delegated task / contract per task. Do not explode impl, verifier, and review into separate top-level workflow tasks or separate delegation contracts.
- Extend `workflow verify record` for typed verifier result writing instead of introducing a near-duplicate command.
- Move deterministic state-machine behavior into `dot-agents` commands; keep agent prompts focused on non-deterministic reasoning and execution.
- Treat post-task orchestrator review as a required runtime gate after merge-back, distinct from end-of-session cleanup.
- Treat "take plan(s) to completion" as scoped to one id or a comma-separated filter, never as a broad scan of all active plans.
- Treat "worker is ready to implement" as meaning no major product or architecture decision is left implicit inside the slice.
- Make provider-consumer contracts explicit when one slice defines a flag, schema field, prompt contract, or artifact that another slice consumes.
- Prefer explicit `required_reads`, `decision_locks`, `verification_focus`, and `stop_conditions` over expecting workers to rediscover them from repo state.

## Native Command Candidates

These are the highest-value sections of the current `ralph-*` shell flow to evaluate for native `dot-agents workflow` commands:

- scoped completion driver for one id or a comma-separated plan set
- replacement-agent / same-stage resume helper for resumable terminal worker failures
- fallback-runtime selection that avoids reusing the same capped agent bin after usage-limit failures
- bundle/stage-plan expansion from persisted bundle metadata
- extended `workflow verify record` support for verifier result artifacts
- parent-review apply/finalize helper after the orchestrator decides what to do
- explicit plan-lock helpers if existing task/delegation state is too weak to encode planning pauses

## Completed But Not Reopened

These tasks remain complete because they landed real surface or contract work even though the runtime chain is still missing:

- `p2-impl-agent-surface`
- `p3a-result-schema`
- `p3b-unit-verifier`
- `p3c-api-verifier`
- `p3d-ui-verifier`
- `p3e-batch-verifier`
- `p3f-streaming-verifier`
- `p4-review-agent`
- `p5-iter-log-v2`
- `p9-sources-design-fork`
- `p10-workflow-command-decomposition`

## Exit Condition

The plan is complete when the live `ralph-*` path matches the spec instead of only documenting it: role-specific dispatch is real, `verifier_sequence` is consumed by runtime stages, verifier artifacts flow through the existing verify-record surface, post-task orchestrator review is real, and a scoped plan-completion mode can continue work safely without running past planning or architectural pause points.
