Status: Active

# I_S_P Prompt Orchestrator Update

## Goal

Update `.agents/prompts/isp.prompt.md` so Pattern `I_S_P` is framed as the interactive orchestrator counterpart to `ralph-pipeline`, not just a stage-execution worker prompt.

## Required Changes

- Add scoped multi-plan support at the top via `--plan <id>[,<id>...]`
- Make the prompt start from orchestrator control flow: probe scope, select next task, decide direct work vs fanout, then drive the staged chain
- Keep `impl -> verifier(s) -> review -> parent gate` as the delegated runtime shape under orchestrator control
- Keep terminology aligned with `I_S_P` vs `legacy loop-worker`

## References

- `docs/LOOP_ORCHESTRATION_SPEC.md`
- `.agents/workflow/plans/loop-agent-pipeline/loop-agent-pipeline.plan.md`
- `.agents/active/orchestrator.loop.md`
- `bin/tests/ralph-orchestrate`

## Verification

- Read back `.agents/prompts/isp.prompt.md`
- Confirm scoped plan completion, `workflow next --plan`, `workflow fanout`, staged dispatch, and parent gate are all represented
