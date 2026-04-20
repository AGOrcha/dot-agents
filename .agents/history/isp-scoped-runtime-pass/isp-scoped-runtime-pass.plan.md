Status: Active

# Scoped ISP Runtime Pass

## Scope

- `loop-agent-pipeline`
- `kg-command-surface-readiness`
- `ci-smoke-suite-hardening`

## Goal

Run the scoped canonical plans to completion using Pattern `I_S_P` as the interactive orchestrator counterpart to the scripted staged pipeline.

## Execution Order

- Probe scoped completion with `workflow complete --json --plan <ids>`
- Select the next canonical task with `workflow next --plan <ids>`
- For bounded write-scope tasks, create or reuse one delegation bundle, then drive `impl -> verifier(s) -> review -> parent gate`
- Close out accepted work with `workflow delegation closeout` before any canonical advance
- Re-enter scoped completion and continue until the scoped result becomes `drained`, `paused`, or `locked`

## Current Pass

- First canonical task selected: `loop-agent-pipeline/p12-review-gate-hardening`
- Current focus: remove heuristic review-gate auto-accept behavior, keep `I_S_P` semantics aligned, and preserve per-task gate/closeout separation

## References

- `.agents/prompts/isp.prompt.md`
- `.agents/active/loop-state.md`
- `.agents/workflow/plans/loop-agent-pipeline/TASKS.yaml`
- `.agents/workflow/plans/kg-command-surface-readiness/TASKS.yaml`
- `docs/LOOP_ORCHESTRATION_SPEC.md`
