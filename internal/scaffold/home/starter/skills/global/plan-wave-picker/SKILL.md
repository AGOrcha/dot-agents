---
name: plan-wave-picker
description: "Use when multiple active workflow or KG plan files exist and you need to choose the next wave or phase to work on without manually rereading every plan. Use ONLY once orchestrator-session-start's pre-flight has already run this session, or the repo has no .agents/workflow/ + active.loop.md loop-orchestration surface at all — otherwise run orchestrator-session-start first; its pre-flight and eligible-orientation steps supersede this skill's eligible-surface and drift-check logic."
---

# Plan Wave Picker

Choose the next active wave or phase from the current plan set.

## Workflow

1. **Load the selection workflow**
   Load → `instructions/workflow.md`
   Gather plan status, dependency ordering, and any existing in-progress work before choosing the next wave.

2. **Review failure points**
   Load → `instructions/gotchas.md`
   Check the common plan-selection mistakes before committing to a wave or phase.
