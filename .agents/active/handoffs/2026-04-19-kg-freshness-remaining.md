# Handoff: KG Freshness Remaining Work

**Created:** 2026-04-19
**Author:** Claude Code session
**For:** AI Agent
**Status:** Ready to execute

---

## Summary

This session completed the scoped `error-message-compliance` plan end-to-end under the staged ISP runtime, then reloaded the updated `.agents/prompts/isp.prompt.md` and resumed the remaining scoped work. The current live task is `kg-command-surface-readiness / kg-freshness-impl`: the audit is complete, a canonical delegation bundle exists, and the impl stage has already finished with uncommitted code plus `impl-handoff.yaml`; the next agent should start at verifier/review/parent-gate for that bundle.

## Project Context

`dot-agents` is a Go CLI with Cobra entrypoints in `cmd/dot-agents` and command implementations in `commands/`. Repo-local workflow control lives in `.agents/workflow/plans/<plan-id>/`, active delegation contracts live in `.agents/active/delegation/` and `.agents/active/delegation-bundles/`, and stage artifacts live under `.agents/active/verification/` and `.agents/active/merge-back/`. The current orchestrator model is the updated ISP prompt in `.agents/prompts/isp.prompt.md`, which treats ISP as the interactive orchestrator counterpart to the scripted pipeline: scoped completion, task selection, bundle-first fanout, then `impl -> verifier -> review -> parent gate`.

## The Plan

Primary active plan in scope:

`kg-command-surface-readiness`

- `kg-freshness-audit` is completed.
- `kg-freshness-impl` is currently `in_progress`.
- downstream tasks remain pending and should not be pulled forward until `kg-freshness-impl` is closed out.

Driving active plan note from `.agents/active/isp-prompt-orchestrator.plan.md`:

```md
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
```

Canonical task state for `kg-command-surface-readiness` at handoff time:

- `kg-freshness-audit` — completed
- `kg-freshness-impl` — in progress
- `kg-change-impact-audit` — pending
- `kg-change-impact-impl` — pending
- `kg-advanced-surfaces-audit` — pending
- `kg-mcp-transport-audit` — pending
- `kg-mcp-transport-impl` — pending

## Key Files

| File | Why It Matters |
|------|----------------|
| [.agents/prompts/isp.prompt.md](/Users/nikashp/Documents/dot-agents/.agents/prompts/isp.prompt.md:1) | Updated ISP runtime shape the session resumed under |
| [.agents/workflow/plans/kg-command-surface-readiness/TASKS.yaml](/Users/nikashp/Documents/dot-agents/.agents/workflow/plans/kg-command-surface-readiness/TASKS.yaml:1) | Canonical task state and notes for the current plan |
| [docs/research/kg-freshness-audit.md](/Users/nikashp/Documents/dot-agents/docs/research/kg-freshness-audit.md:1) | Completed audit and graph-ready contract driving the current implementation |
| [.agents/active/delegation-bundles/del-kg-freshness-impl-1776636903.yaml](/Users/nikashp/Documents/dot-agents/.agents/active/delegation-bundles/del-kg-freshness-impl-1776636903.yaml:1) | Canonical execution contract for the live implementation slice |
| [.agents/active/verification/kg-freshness-impl/impl-handoff.yaml](/Users/nikashp/Documents/dot-agents/.agents/active/verification/kg-freshness-impl/impl-handoff.yaml:1) | Impl-stage done artifact; start verifier from here |
| [commands/kg/cmd.go](/Users/nikashp/Documents/dot-agents/commands/kg/cmd.go:1) | KG command wiring changed during the current implementation slice |
| [commands/kg/kg.go](/Users/nikashp/Documents/dot-agents/commands/kg/kg.go:1) | KG health/config behavior; must remain note-store health only |
| [commands/kg/sync_code_warm_link.go](/Users/nikashp/Documents/dot-agents/commands/kg/sync_code_warm_link.go:1) | `kg build`, `kg update`, and `kg code-status` logic |
| [internal/graphstore/crg.go](/Users/nikashp/Documents/dot-agents/internal/graphstore/crg.go:1) | CRG status/build/update plumbing and error classification |
| [commands/kg/kg_test.go](/Users/nikashp/Documents/dot-agents/commands/kg/kg_test.go:1) | New tests added for freshness states and output behavior |

## Current State

**Done:**
- Updated `.agents/prompts/isp.prompt.md` and resumed remaining scoped work under the new runtime.
- Completed `error-message-compliance` plan end-to-end, including staged implementation and regression coverage.
- Completed `kg-freshness-audit` directly and recorded findings in `docs/research/kg-freshness-audit.md`.
- Updated canonical notes for `kg-freshness-audit` and `kg-freshness-impl`.
- Fanned out `kg-freshness-impl` as a bounded delegation bundle.
- Impl stage for `kg-freshness-impl` completed and wrote `impl-handoff.yaml`.

**In Progress:**
- `kg-freshness-impl`
  - Impl worker reported these scoped changes:
    - `commands/kg/cmd.go`
    - `commands/kg/kg.go`
    - `commands/kg/sync_code_warm_link.go`
    - `internal/graphstore/crg.go`
    - `commands/kg/kg_test.go`
  - Impl worker claims:
    - `kg code-status` now reads persisted CRG SQLite directly
    - JSON output and explicit readiness state were added
    - build/update classification now distinguishes `no_diff`, `no_mutation`, and busy/locked failures
  - Verification has **not** yet been run under the orchestrator for this bundle.

**Not Started:**
- Verifier stage for `kg-freshness-impl`
- Review stage for `kg-freshness-impl`
- Parent closeout for `kg-freshness-impl`
- all downstream `kg-command-surface-readiness` tasks

## Decisions Made

- **Use ISP as a real orchestrator, not a collapsed worker prompt** — after the prompt update, the session stayed in scoped completion mode, did direct work only for audit/contract tasks, and switched back to bundle-first fanout for implementation tasks.
- **Treat `kg code-status` as the freshness probe, not `kg health`** — the audit reproduced that `kg health` can be `healthy` with an absent code graph, so readiness must be anchored on `code-status`.
- **Audit on a clean checkout before defining the freshness contract** — a clean detached worktree in `/tmp/kg-freshness-clean` was necessary to separate real CRG behavior from workspace lock noise.
- **Preserve `kg health` as note-store health only** — the implementation prompt explicitly told the worker not to overload `kg health` with code-graph readiness semantics.
- **Do not widen this slice into `kg changes`, MCP parity, or Go-native CRG replacement** — those remain separate canonical tasks/plans.

## Important Context

- A clean detached worktree was created at `/tmp/kg-freshness-clean` for the audit. It is useful if you need to re-run fresh-graph repros without the dirty live repo.
- During audit, a stray live `kg update` process caused a real `database is locked` failure for `kg build`. That process was explicitly killed before handoff.
- The clean-checkout audit found that `kg code-status --json` still rendered prose before the current impl slice; this is a concrete behavior to verify now.
- The clean-checkout audit also found a mismatch between successful `kg build` summary counts and the immediately following `kg code-status` persisted counts. The current implementation claims to address freshness semantics, but you still need to verify whether this count mismatch is resolved or explicitly documented.
- Current worktree is intentionally dirty. Do not treat that as accidental drift; it includes the completed error-message plan, the KG audit, and the in-progress freshness implementation.
- There are no active delegations other than `kg-freshness-impl` in the remaining scoped lane.

## Session Review / Feedback

- The staged ISP runtime worked well once the prompt update was in place. The session stayed disciplined about direct-vs-fanout boundaries and used parent closeout correctly for `error-message-compliance`.
- The strongest part of the session was evidence-first control flow: `workflow complete`, `workflow next`, direct audit when the task was research, then canonical fanout once a bounded implementation slice existed.
- The weakest part was environmental noise during the KG audit. Two different failures were initially conflated:
  - sandbox-only Python semaphore permission failure
  - real graph DB lock from a concurrent `kg update`
  The audit got back on track only after separating those causes.
- The current `kg-freshness-impl` worker report is promising, but it is still only an impl-stage claim. Do not trust it until the verifier confirms:
  - `--json` actually emits JSON
  - readiness states are stable
  - `kg health` remains semantically narrow

## Self-Reflection Notes

- Good call: switching to a clean worktree for KG freshness repros. That avoided writing a misleading audit based on the dirty live repo.
- Good call: re-prompting against the updated ISP prompt and continuing from scoped completion instead of restarting the session or improvising a new flow.
- Missed once: an earlier `kg update` process was left running long enough to create lock contention. The next session should be more aggressive about checking for lingering long-running graph commands after exploratory repros.
- Caution for next time: when `--json` appears ignored, verify both root-flag and subcommand-flag forms before writing the conclusion. That was done here eventually, but later than ideal.
- Another caution: the current worktree contains completed plan changes plus a live in-progress bundle. Avoid casually rebasing or cleaning until `kg-freshness-impl` is either accepted or rejected through the parent gate.

## Next Steps

1. **Start at the verifier stage for `kg-freshness-impl`** — use the existing bundle and `impl-handoff.yaml`; do not create a second delegation.
   Definition of done: write `.agents/active/verification/kg-freshness-impl/unit.result.yaml` via `workflow verify record`.

2. **Verify the specific freshness-contract claims, not just generic tests** — at minimum confirm:
   - `go run ./cmd/dot-agents --json kg code-status --repo .` emits valid JSON
   - readiness states cover `unbuilt`, `ready`, `busy_or_locked`, and `error` in observable output
   - `kg health` remains note-store health and is not recast as code-graph readiness
   - build/update summaries distinguish `no_diff` and `no_mutation`

3. **Run review stage for `kg-freshness-impl`** — accept only if the implementation matches `docs/research/kg-freshness-audit.md` and does not widen semantics.

4. **Parent closeout for `kg-freshness-impl`** — if accepted:
   - `workflow delegation closeout --plan kg-command-surface-readiness --task kg-freshness-impl --decision accept`
   - `workflow advance kg-command-surface-readiness --task kg-freshness-impl --status completed`
   - `workflow plan update kg-command-surface-readiness --focus kg-change-impact-audit`

5. **Re-enter scoped completion** — the expected next task after closeout is `kg-change-impact-audit`.

## Constraints

- Stay inside the scoped plan set that was active this session: `graph-bridge-command-readiness,error-message-compliance,kg-command-surface-readiness`.
- Do not reopen completed `error-message-compliance` work unless verification proves a regression in the current KG changes.
- Reuse the existing bundle for `kg-freshness-impl`; do not re-fanout the same task.
- Keep role separation intact: verifier is not the reviewer, reviewer is not the parent gate.
- Do not widen `kg-freshness-impl` into `kg changes`, MCP parity, or architectural CRG replacement.
- Do not clean the working tree or create commits as part of closeout unless explicitly asked; current staged progress is still uncommitted.
