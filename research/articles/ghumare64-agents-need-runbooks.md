## Rohit Ghumare — Agents Need Runbooks, Not Longer Chats

**Source**: https://x.com/ghumare64/status/2052825541057626258
**Author**: Rohit Ghumare (@ghumare64) — Verified
**Date**: 2026-05-08
**Method**: Playwright
**Word count**: ~1,500 words
**Engagement**: 0 replies, 2 reposts, 19 likes, 26 bookmarks, 1.2K views

---

### Summary

Production agents become reliable through operational scaffolding — runbooks, permissions, logs, rollback, verification — not better prompts. The article draws a DevOps analogy: before DevOps, deployment knowledge lived in people's heads; now it's encoded in CI/CD, runbooks, and health checks. Agents are repeating the same mistake. Two layers are required: a knowledge layer (docs, skills, memory) and a control layer (state machine, tool policy, checks, approvals, rollback). Most teams over-invest in the first and under-build the second. The better abstraction is "agent as production worker" not "agent as employee."

---

### Body

**Thesis:** Production agents will not become reliable because we write better prompts. They will become reliable when we give them the same operational scaffolding we gave humans: runbooks, permissions, logs, rollback, and verification.

If your agent workflow depends on phrases like "IMPORTANT:", "DO NOT SKIP THIS STEP", "MAKE SURE YOU VERIFY" — you are not building an agent system. You are negotiating with a stochastic intern. That is fine for demos. It breaks in production.

Real work is an operating loop: read the issue, inspect the codebase, understand convention, make a change, run tests, debug failures, update the plan, check the diff, ask for approval, create a PR, monitor CI, respond to review, rollback if needed. Operating loops need runbooks. Not vibes.

**We already learned this lesson in DevOps.** Before DevOps matured, infrastructure work lived inside people's heads. Deployments were tribal knowledge. Incident response was Slack chaos. Then we moved the work into systems: runbooks, CI/CD pipelines, health checks, alerts, dashboards, logs, rollback scripts, infrastructure as code, postmortems. The lesson: if the process matters, encode it. Now we are repeating the same mistake with agents — putting all operational knowledge into a giant prompt.

**A prompt is not a runbook.** A prompt describes what should happen. A runbook controls what can happen.

Prompt: "Run tests before finalizing."
Runbook:
```
step: run_tests
command: pytest
required: true
on_failure: stop_and_debug
success_condition: exit_code == 0
```

Prompt: "Be careful with production data."
Runbook:
```
permissions:
  read: [staging_db]
  write: [none]
  production: [denied]
```

One is advice. The other is infrastructure.

**The problem is not that agents are dumb.** The problem is we give agents human-shaped instructions for machine-shaped workflows. Humans recover from ambiguity using memory, judgment, fear, context, and social pressure. Agents need: state, constraints, typed inputs, retries, checkpoints, logs, permissions, tests, rollback, deterministic control flow.

**The "agent loop" is becoming the new deployment pipeline.** Every serious agent system needs answers to: Permissions (what tools can the agent call?), State (what does it know right now, what has it tried?), Verification (which checks are mandatory?), Observability (what did it read/modify?), Rollback (can we undo the change?). These are platform-engineering problems, not prompt-engineering problems.

**"Just ask the agent to verify" is not enough.** Asking the same model to do the work and verify the work is like asking a deploy script if production is healthy without checking metrics. Verification has to become external and programmatic: tests must actually run, screenshots must actually be captured, API responses must actually be checked, files must actually exist, diffs must actually be inspected, commands must return real exit codes.

**The future agent stack looks boring.** That is the point. The next generation will look less like magic and more like DevOps: task queues, worker boundaries, tool registries, memory scopes, approval gates, logs, traces, policy files, evals, CI checks, rollback hooks. The agent will be a worker inside a controlled runtime. The system decides what step comes next, what tools are available, what counts as success, what happens on failure, when a human must approve.

**The wrong abstraction is "agent as employee."** The better abstraction is agent as production worker. A worker has a job, a queue, permissions, inputs, outputs, logs, retries, failure states, escalation paths. You do not ask a worker to "be careful." You constrain the environment so carelessness cannot destroy the system.

**CLAUDE.md is not enough.** It can tell an agent how the project works. It cannot guarantee the agent follows the release process. A real agent system needs both:
1. Knowledge layer: docs, skills, memory, conventions
2. Control layer: state machine, tool policy, checks, approvals, rollback

Most teams are over-investing in the first. They are under-building the second.

---

### Key Quotes

> "A prompt is not a runbook. A prompt can describe what should happen. A runbook controls what can happen."

> "If the process matters, encode it. Do not hope someone remembers it at 2 AM."

> "The agent is not replacing the platform. The agent is becoming a client of the platform."

> "CLAUDE.md is not enough. It can tell an agent how the project works. It cannot guarantee the agent follows the release process."
