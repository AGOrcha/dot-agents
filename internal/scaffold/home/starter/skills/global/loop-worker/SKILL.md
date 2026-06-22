---
name: "loop-worker"
description: "Legacy/full-slice bounded implementation worker for a delegated task. Reads a delegation bundle, implements write_scope, runs /iteration-close, and returns merge-back. Do not use for typed ISP stages."
argument-hint: "<bundle_path>"
---

# Loop Worker

Execute a single legacy/full-slice delegated task: read the bundle, implement
`write_scope`, and close out via `/iteration-close`. Role-pure: never selects
tasks, never updates orchestrator state, and never substitutes for typed ISP
stage agents.

## Workflow

0. **Load global worker discipline**
   Load -> `~/.agents/profiles/loop-worker.md`
   Establishes discipline rules and the canonical verify → checkpoint → merge-back closeout sequence.

1. **Cold-start orientation**
   Load -> `instructions/startup.md`
   3-step startup: read bundle → `workflow tasks <plan_id>` → `git status --short`.
   Do NOT run `workflow orient`, `workflow next`, or `workflow status`.

2. **Review failure points**
   Load -> `instructions/gotchas.md`
   Worker-specific failure modes: typed-stage misrouting, scope creep, wrong closeout command, Current Position ownership.

3. **Load project overlay**
   Load -> `.agents/active/active.loop.md`
   Legacy/full-slice repo-specific implementation rules, CLI inventory
   (worker subset), and safety guardrails. Typed ISP stages must use a
   separately resolved stage-safe overlay instead.

4. **Write the stop-gate sentinel** *(required — after reading the bundle, before any scoped edit or closeout command)*
   Load -> `instructions/startup.md` § Write Stop-Gate Sentinel
   Run `da workflow hook-sentinel write loop-worker` once, recording the
   bundle's plan, task, a run ID, `--agent-type loop-worker`, and every
   delegated `write_scope` path. The SubagentStop gate diffs your edits against
   the sentinel's `write_scope` — no sentinel means no scope enforcement.

5. **Implement write_scope task**
   Implement the single task within write_scope. One item per iteration. Run tests (positive + negative). Commit.

6. **Close out**
   Load -> `iteration-close` skill
   verify record → checkpoint → merge-back (delegated path). Accepted
   parent-run delegation closeout completes the delegated task. Do NOT run
   `workflow advance`; it is for direct non-delegated work.
