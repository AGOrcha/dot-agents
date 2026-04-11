---
name: delegation-lifecycle
description: "Use when you need to delegate a bounded write-scope task to a sub-agent, track its lifecycle, and merge the result back into the canonical plan."
---

# Delegation Lifecycle

Drive the delegation fanout, merge-back, and orient loop for bounded sub-agent work.

## Workflow

1. **Load the delegation flow**
   Load → `instructions/workflow.md`
   Follow the fanout, merge-back, and orient sequence for a delegated task.

2. **Review failure points**
   Load → `instructions/gotchas.md`
   Check the write-scope, coordination, and cleanup pitfalls before using the flow.
