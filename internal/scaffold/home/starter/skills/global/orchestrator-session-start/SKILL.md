---
name: "orchestrator-session-start"
description: "Orchestrator turn (not the worker): pre-flight → eligible-orientation (workflow eligible --json) → orient/next → pick task → KG readback → decide (fanout or direct) → if fanout, write constraints into TASKS.yaml notes and produce bundle, then chain ISP skill for staged runtime. The orchestrator does not implement the delegated slice. Use when starting a session in repos with .agents/workflow/ and active.loop.md present — run this first; once its pre-flight and eligible-orientation steps have completed this session, subsequent wave/phase picks can use the lighter plan-wave-picker skill instead of repeating this flow."
argument-hint: "[--plan <plan-id>[,<plan-id>...]] [--task <task-id>]"
---

# Orchestrator Session Start

Start a loop orchestration pass **above** the focused worker agent, gather eligible task context, then hand off to the `isp` skill for staged runtime. The orchestrator's job ends when the bundle exists and TASKS.yaml notes are up to date.

Once pre-flight (step 0) and eligible orientation (step 2) have run this session, `plan-wave-picker` is the lighter-weight tool for any subsequent wave/phase pick in the same session — the two skills are sequenced, not competing alternatives.

## Workflow

0. **Pre-flight checks**
   Load -> `instructions/preflight.md`
   Check pending proposals, active bundles, and worker loop context before running any workflow commands. If a bundle already exists for the task `workflow next` will select, skip to step 3.

1. **Review failure points**
   Load -> `instructions/gotchas.md`
   Check the task-selection, write-scope, and fold-back pitfalls **before** running the orchestration flow. This fires early so the "Do Not Turn The Orchestrator Into A Worker" rule is in context before the fanout decision.

2. **Eligible orientation**
   Load -> `instructions/eligible-orientation.md`
   Run `da workflow eligible --json --plan <scope>` to get the full annotated task set. Extract `max_batch`, `total_eligible`, and per-task `evidence_confidence`. Present the orientation summary. Determine whether parallel fanout mode is active (`max_batch > 1` AND no active delegations).

3. **Load the orchestration flow**
   Load -> `instructions/workflow.md`
   Run orient, next-task selection, graph-first readback, and the fanout (or direct) decision in order. Use the eligible output from step 2 as the authoritative task set — `workflow next` is a cross-check, not the primary selector.

4. **Chain ISP skill for staged runtime**
   Load -> **`isp`** skill with the eligible JSON output as pre-gathered context.
   The ISP skill's Step 1 skips re-running orientation since it was already done here. Pass `max_batch`, `eligible_tasks`, and active delegation state as context so ISP can proceed directly to task selection and fanout.

   If you ran `workflow fanout` (or a bundle already exists for the chosen task):
   Load -> **`delegation-lifecycle`** → **`instructions/bundle-to-execution.md`**
   That is the **worker / subagent** turn: profile prompts (`~/.agents/profiles/loop-worker.md`, `.agents/active/active.loop.md`), bundle YAML, implementation inside `write_scope`, then verify / checkpoint / merge-back. The orchestrator briefs the worker with the bundle path and the updated TASKS.yaml notes; it does not replace the worker.
