---
name: "loop-worker"
description: "Bounded implementation worker for a delegated task. Reads a delegation bundle, implements write_scope, runs /iteration-close. Cold-start capable — only the bundle path is needed. Use in repos with .agents/workflow/ and active.loop.md present."
argument-hint: "<bundle_path>"
---

# Loop Worker

Execute a single delegated task: read the bundle, implement write_scope, close out via `/iteration-close`. Role-pure — never selects tasks, never updates orchestrator state.

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
   Worker-specific failure modes: scope creep, wrong closeout command, Current Position ownership.

3. **Load project overlay**
   Load -> `.agents/active/active.loop.md`
   Repo-specific implementation rules, CLI inventory (worker subset), and safety guardrails.

4. **Implement write_scope task**
   Implement the single task within write_scope. One item per iteration. Run tests (positive + negative). Commit.

5. **Close out**
   Load -> `iteration-close` skill
   verify record → checkpoint → merge-back (delegated path). Do NOT run `workflow advance` — parent owns that.
