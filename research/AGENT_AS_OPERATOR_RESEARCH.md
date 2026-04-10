# Agent-as-Operator: dot-agents Managed by Agents, Not Humans

Status: Exploratory
Last updated: 2026-04-03

## The Feedback

> "A new tool + learning all about agentic config / workflows may be overload. Would make more sense if the agent is the one running the dot-agents commands / managing the config."

This reframes the entire product surface. The previous research (AUTONOMOUS_WORKFLOW_MANAGEMENT_RESEARCH.md) designed commands that humans would invoke. This document explores what changes when **the agent is the primary operator** and the human is a reviewer/approver.

## The Shift

Previous framing:
```
Human learns dot-agents → Human runs commands → Agent benefits from managed state
```

New framing:
```
Agent runs dot-agents autonomously → Human reviews/approves changes → Workflow stays healthy
```

This is the Hermes pattern (from openclaw-hermes-supervisor-pattern.md) applied inward: the agent supervises its own infrastructure.

## What the New Research Tells Us

### 1. Supervisor Pattern (Graeme / OpenClaw + Hermes)

Key takeaway: **a structured protocol with explicit markers prevents infinite loops and drift.** Two agents coordinate through four intent markers ([STATUS_REQUEST], [REVIEW_REQUEST], [ESCALATION_NOTICE], [ACK]) with strict termination logic.

Applicable to dot-agents:
- Agent manages config/skills/rules and uses a similar marker protocol to communicate changes to the human
- Changes that are safe (updating a plan checkpoint, persisting a lesson) → auto-apply with [ACK]
- Changes that affect shared state (modifying rules, adding skills, changing workflow config) → [REVIEW_REQUEST] to human
- Issues that require judgment (conflicting rules across team members, stale config) → [ESCALATION_NOTICE]
- The human never needs to run `dot-agents` commands directly — the agent handles it and surfaces decisions

### 2. Orchestrator/Subagent Context Engineering (am.will / Codex Swarms)

Key takeaway: **front-loading context saves tokens and prevents drift.** The orchestrator knows who/what/where/when/why/how and prompts subagents with full context templates.

Applicable to dot-agents:
- dot-agents becomes the **context source** that agents query, not a CLI humans operate
- When an agent starts a session, dot-agents provides the resume context (active plan, last checkpoint, verification state, recent lessons)
- When an agent spawns a subagent, dot-agents provides the scoped context (relevant rules, skills, preferences for that task)
- The "front-loading" pattern means dot-agents should emit structured context bundles, not require agents to discover state via file reads

### 3. Self-Improving Knowledge Graph (Nyk / Claude + Obsidian)

Key takeaway: **agents should maintain and evolve their own operational infrastructure.** The graph improves itself — agents notice contradictions, flag stale state, propose structural changes.

Applicable to dot-agents:
- Agent notices when a rule conflicts with observed behavior → proposes rule update
- Agent notices when a skill is repeatedly modified → proposes skill refactor
- Agent notices when config diverges across repos → flags drift to human
- Agent notices when a workflow pattern repeats → creates a new skill for it
- The "orient → work → persist" rhythm becomes: dot-agents provides orientation, agent does work, dot-agents persists observations

## What "Agent as Operator" Means Concretely

### Dot-agents Managed Resources

These are the things the agent manages on behalf of the team:

| Resource | Agent Creates/Updates | Human Reviews |
|----------|----------------------|---------------|
| Rules (CLAUDE.md, .cursorrules, etc.) | Agent proposes edits when patterns emerge | Human approves rule changes |
| Skills | Agent creates skills when repetitive patterns detected | Human approves new skills |
| Lessons | Agent writes lessons after corrections — **already happening** | Human can review/prune |
| Plans | Agent creates/updates plans as work progresses | Human approves scope |
| Checkpoints | Agent auto-persists at natural breakpoints | No review needed |
| Handoffs | Agent packages context at session end | No review needed |
| Verification state | Agent persists test/lint/build results | No review needed |
| Preferences | Agent learns and persists workflow preferences | Human confirms |

### The Approval Gradient

Not all changes need human review. The key design question is the **approval gradient**:

**Auto-apply (no human needed):**
- Checkpointing current state
- Persisting verification results
- Writing lessons after corrections
- Updating plan progress
- Packaging handoffs

**Propose-and-apply (human confirms or overrides):**
- Adding new rules
- Modifying existing rules
- Creating new skills
- Changing workflow config
- Updating preferences that affect other team members

**Escalate (human decides):**
- Conflicting rules across team members
- Stale config that may affect production
- Workflow drift across repos that needs strategic decision
- Skill deletions or major refactors

This maps directly to the intent marker protocol from Graeme's article:
- Auto-apply = [ACK] (agent handles it, logs it)
- Propose-and-apply = [REVIEW_REQUEST] (agent presents change, waits)
- Escalate = [ESCALATION_NOTICE] (agent flags issue, human decides)

## How This Changes the dot-agents Architecture

### Before: CLI-First

```
dot-agents sync         # human runs
dot-agents refresh      # human runs
dot-agents doctor       # human runs
dot-agents workflow resume  # human runs
```

### After: Agent-First with CLI Escape Hatch

```
# Agent runs these automatically via hooks/MCP:
dot-agents orient       # at session start — load context
dot-agents persist      # at natural breakpoints — save state
dot-agents propose      # when changes detected — queue for review
dot-agents sweep        # periodically — check health across repos

# Human can still run:
dot-agents status       # see what the agent has been doing
dot-agents review       # approve/reject pending proposals
dot-agents override     # force a specific state
```

### The Agent Interface

Instead of CLI commands, the agent interacts with dot-agents through:

1. **Hooks** — session start/end, pre-commit, post-test, on-error
2. **MCP tools** — query state, propose changes, persist observations
3. **File conventions** — read/write canonical artifacts at known paths

The hook approach (from Nyk's article — LACP hooks for session orientation and quality gates) is the most natural fit because:
- Hooks fire at the right moments without the agent needing to remember
- They're already a pattern in Claude Code, Cursor, and Codex
- They can be platform-specific while the artifacts they produce are canonical

### What dot-agents Provides to Agents

At session start (orient):
```yaml
# .agents/context/orient.yaml — generated by dot-agents
active_plan: .agents/active/go-modular-monolith-migration.plan.md
last_checkpoint: 2026-04-02T14:30:00Z
verification:
  last_run: 2026-04-02T14:28:00Z
  status: pass
  failures: []
recent_lessons:
  - preserve-existing-secret-names
  - no-indexes-in-application-code
pending_reviews: 2
drift_warnings:
  - po-core-api-se rules diverged from template
next_suggested_action: "Continue with Phase 3, Task 5 of migration plan"
```

This is the "front-loading" pattern from am.will's article — give the agent everything it needs up front so it doesn't waste tokens discovering state.

### What Agents Persist Back

At natural breakpoints:
```yaml
# .agents/context/checkpoint.yaml — written by agent, managed by dot-agents
session_id: claude-2026-04-03-1430
files_touched:
  - po-core-api-se/internal/transport/http/auth/oauth.go
  - po-core-api-se/internal/transport/http/auth/oauth_test.go
verification:
  tests_run: true
  tests_passed: true
  lint_clean: true
observations:
  - "OAuth test pattern uses fakeOAuthStore — consistent with existing test patterns"
  - "TestConfigWithJWT helper is well-established — use it for all auth tests"
next_action: "Wire callback handler and add integration test"
```

## Workflow Hygiene: Agent-Managed

The previous research identified workflow hygiene practices worth supporting. In the agent-as-operator model, these become **automated behaviors**, not commands:

| Hygiene Practice | How Agent Manages It |
|-----------------|---------------------|
| Config stays up-to-date as updates are pushed | Agent detects drift on session start, proposes sync |
| Rules reflect actual team patterns | Agent proposes rule updates when corrections accumulate |
| Skills stay relevant | Agent flags unused skills, proposes new ones from repetitive patterns |
| Plans stay current | Agent updates plan progress automatically |
| Lessons get captured | Agent writes lessons after corrections — already happening |
| Handoffs are clean | Agent packages state at session end via hook |
| Verification is tracked | Agent persists test/lint results after each run |

### Multi-Member Workflow

When different team members push updates:
1. Agent detects changes on session start (git pull + dot-agents orient)
2. If changes are compatible → auto-merge, log it
3. If changes conflict → [ESCALATION_NOTICE] to human with specific conflict
4. Agent tracks which member last modified each resource for attribution

## What This Means for the dot-agents MVP

### Previous MVP (from AUTONOMOUS_WORKFLOW_MANAGEMENT_RESEARCH.md):
1. `dot-agents workflow resume`
2. `dot-agents workflow checkpoint`
3. `dot-agents workflow verify`

### Revised MVP (agent-as-operator):
1. **Orient hook** — fires at session start, emits context bundle
2. **Persist hook** — fires at natural breakpoints, saves state
3. **Propose mechanism** — agent queues changes, human reviews via `dot-agents review`

Why this is better:
- Zero new commands for humans to learn
- Agent does the work it's already trying to do, but with canonical persistence
- The only human-facing command is `review` — approve or reject what the agent proposes
- Existing `dot-agents sync/refresh/doctor` still work as escape hatches

## Open Questions

1. **Hook registration**: Should dot-agents register hooks in each platform's native format, or use a unified hook runner that platforms call?
2. **MCP vs hooks**: For runtime queries (e.g., "what are the active rules?"), MCP tools are better than hooks. Should dot-agents ship an MCP server?
3. **Proposal queue**: Where do pending proposals live? `.agents/proposals/` with one file per proposal? A single queue file?
4. **Multi-agent coordination**: When multiple agents operate in the same repo (e.g., swarm pattern), how do they avoid conflicting dot-agents writes? The intent marker protocol (one message per turn, ACK is terminal) is one option.
5. **Trust boundary**: Which auto-apply actions are truly safe? Checkpointing seems safe. But auto-persisting "observations" that later influence rules needs thought.
6. **Rollback**: If a proposed rule change is approved and later causes problems, how does the human revert? Git history on the `.agents/` directory?

## Key Insight

The three articles converge on one principle:

**Agents should manage their own operational infrastructure. Humans should steer, not operate.**

- Graeme: Human makes one decision, back to building
- am.will: You are the architect, agents are the builders
- Nyk: The graph improves itself, agents refactor their own instructions

dot-agents should be the operational layer that agents use to stay organized — not a new tool that humans need to master.

## Recommended Next Step

Design the orient/persist/propose protocol:
- What exactly goes in the orient context bundle?
- What triggers a persist (time? git commit? test run? session end?)
- What's the proposal format and review UX?
- How does this integrate with existing `.agents/` directory conventions?
