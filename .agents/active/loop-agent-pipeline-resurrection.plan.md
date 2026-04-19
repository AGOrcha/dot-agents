# Loop Agent Pipeline Resurrection

Status: Active

Objective: restore `loop-agent-pipeline` as an active workflow plan because the archived completion state overstated the live runtime.

Actions:

- recreate the canonical workflow plan under `.agents/workflow/plans/loop-agent-pipeline/`
- reopen `p8-orchestrator-awareness` for actual role-aware runtime dispatch
- reopen `p6-fanout-dispatch` for runtime consumption of `app_type` and `verifier_sequence`
- reopen `p7-post-closeout` for a required post-task orchestrator review gate after merge-back
- add `p11-plan-completion-mode` for scoped plan completion, planning locks, and comma-separated plan filters
- keep prompt/schema surface tasks completed, but clarify that they are not yet wired through the live `ralph-*` execution path
- capture the runtime audit, current code anchors, and command-vs-agent boundary directly in the active plan so future runtime agents do not need chat history to understand the remaining work
