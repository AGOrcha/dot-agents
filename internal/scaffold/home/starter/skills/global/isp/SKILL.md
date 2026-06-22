---
name: "isp"
description: "Interactive staged pipeline orchestrator. Assumes orchestrator-session-start has already run eligible + orientation. Steps: task selection from pre-gathered output → direct vs fanout decision → fanout with evidence-aware context → staged runtime (impl → verifier → review → parent gate). Use in repos with .agents/workflow/ and active.loop.md present."
argument-hint: "[--plan <plan-id>[,<plan-id>...]] [eligible-json-output-path]"
---

# ISP — Interactive Staged Pipeline

Counterpart to the scripted `ralph-pipeline`. Assumes **`orchestrator-session-start` has already run** and produced a `workflow eligible --json` snapshot. Load that output rather than re-running orientation.

## Workflow

1. **Load orientation context**
   Load -> `instructions/orientation.md`
   Read the pre-gathered eligible output from orchestrator-session-start. If it is absent or stale, re-run `workflow eligible --json --plan <scope>`.

2. **Write the stop-gate sentinel** *(required — after consuming the eligible/orientation input, before selecting or dispatching work)*
   Load -> `instructions/orientation.md` § Write Stop-Gate Sentinel
   Run `da workflow hook-sentinel write isp` once, recording the plan, task/run ID, whether the eligible snapshot was loaded from orchestrator-session-start (`--eligible-snapshot-loaded`), and the declared `--max-batch`. The isp gate reads this sentinel to confirm fanout discipline held — no sentinel means no enforcement.

3. **Select work**
   Load -> `instructions/task-selection.md`
   Use `max_batch` from eligible output as the fanout set. Parallel mode trigger: `max_batch > 1` AND no active delegations.

4. **Decide direct work vs fanout**
   Load -> `instructions/direct-vs-fanout.md`

5. **Fanout the delegated task(s)**
   Load -> `instructions/fanout.md`
   Parallel mode: one bundle per task in `max_batch`. Load evidence sidecar context for tasks with `evidence_confidence: medium|high`.

6. **Drive the staged runtime**
   Load -> `instructions/staged-runtime.md`
   Chain: `impl → verifier(s) → review → parent gate`.
