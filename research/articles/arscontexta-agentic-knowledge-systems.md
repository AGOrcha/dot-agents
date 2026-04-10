# Ars Contexta: Claude Code Plugin for Agentic Knowledge Systems

**Author:** Heinrich (@arscontexta)
**Source:** https://x.com/arscontexta/status/2023957499183829467
**Published:** February 18, 2026
**GitHub:** https://github.com/agenticnotetaking/arscontexta
**Stats:** 3,081 stars, 197 forks

---

Claude now builds itself a second brain. Ars Contexta is a Claude Code plugin that generates a complete knowledge system from conversation. You describe how you think and work, have a conversation, and get a complete second brain as markdown files you own.

The engine derives a cognitive architecture — folder structure, context files, processing pipeline, hooks, navigation maps, and note templates — tailored to your domain and backed by 249 research claims.

## The Core Idea

Most "second brain" tools start with templates. Ars Contexta starts with conversation. You install it, run `/arscontexta:setup`, and have a 20-minute conversation about what you need. The system generates a complete memory architecture in your project folder, grounded in actual research about how knowledge systems work.

The output is plain markdown files connected by wiki links, forming a traversable knowledge graph. No database, no cloud, no lock-in. The agent traverses its own knowledge the same way it traverses code.

## Three-Space Architecture

All generated systems separate content into three invariant spaces:

| Space | Purpose | Growth Pattern |
|-------|---------|----------------|
| `self/` | Agent identity, methodology, goals | Slow (tens of files) |
| `notes/` | Primary knowledge graph | Steady (10-50/week) |
| `ops/` | Queue state, sessions, coordination | Fluctuating |

The space names adapt to your domain — `notes/` might become `reflections/`, `claims/`, or `decisions/` — but the three-way separation is invariant.

## The 6 Rs Processing Pipeline

Extends the Cornell Note-Taking methodology into an agent-native pipeline:

1. **Record** — Manual inbox capture
2. **Reduce** — Extract insights via `/reduce`
3. **Reflect** — Find connections via `/reflect`
4. **Reweave** — Backward pass updating context via `/reweave`
5. **Verify** — Quality checks via `/verify`
6. **Rethink** — Challenge assumptions via `/rethink`

Each phase spawns a fresh subagent to maintain optimal context windows, preventing the attention degradation that comes from long-running sessions.

## The /ralph Orchestrator

The `/ralph` command orchestrates the pipeline:

1. Reads queue for next unblocked task
2. Spawns subagent with fresh context window
3. Subagent runs skill, updates task file, returns handoff
4. Captures learnings, advances queue phase
5. Repeats for specified number of tasks

This is the same "front-loading context" pattern described in the [Codex multi-agent swarms playbook](codex-multi-agent-swarms-playbook.md) — each subagent gets exactly the context it needs, nothing more.

## Four Automation Hooks

| Hook | Trigger | Function |
|------|---------|----------|
| Session Orient | Start | Workspace injection, identity load |
| Write Validate | PostToolUse (Write) | Schema enforcement |
| Auto Commit | PostToolUse async | Non-blocking git commits |
| Session Capture | Stop | Persist state to `ops/sessions/` |

The session orient hook maps directly to the "orient" primitive described in the [agent-as-operator research](../AGENT_AS_OPERATOR_RESEARCH.md) — loading context at session start so the agent doesn't waste tokens rediscovering state.

The session capture hook maps to the "persist" primitive — saving state at session end so the next session can pick up where this one left off.

## Plugin & Generated Commands

**Plugin-level (always available):**
- `/arscontexta:setup` — Conversational onboarding
- `/arscontexta:help` — Contextual guidance
- `/arscontexta:tutorial` — Interactive walkthrough
- `/arscontexta:ask` — Query research graph
- `/arscontexta:health` — Diagnostic checks
- `/arscontexta:recommend` — Architecture advice
- `/arscontexta:architect` — Evolution guidance
- `/arscontexta:add-domain` — Multi-domain extension
- `/arscontexta:reseed` — Re-derive from principles
- `/arscontexta:upgrade` — Apply knowledge updates

**Generated (post-setup):**
- Processing: `/reduce`, `/reflect`, `/reweave`, `/verify`, `/validate`, `/seed`
- Orchestration: `/ralph`, `/pipeline`, `/tasks`
- Analysis: `/stats`, `/graph`, `/next`
- Development: `/learn`, `/remember`, `/rethink`, `/refactor`

## Research Foundation

The `methodology/` directory contains 249 interconnected claims synthesizing:
- Zettelkasten & Evergreen Notes practices
- Cornell Note-Taking & PARA systems
- Memory palace techniques
- Cognitive science (extended mind, spreading activation, generation effect)
- Network theory (small-world topology, betweenness centrality)
- Agent architecture patterns (context windows, multi-agent spawning)

Every primitive includes `cognitive_grounding` linking to specific research — e.g., MOC hierarchy is grounded in context-switching cost research.

## The Self-Improving Pattern

This connects directly to the pattern from the [Claude + Obsidian memory stack](claude-obsidian-memory-stack.md):

> Agents don't get bored with maintenance. The thing that killed every wiki is the exact thing agents are built for.

The agent notices contradictions between notes, flags stale state, and proposes structural changes. It refactors its own instructions and evolves its own architecture when the current one creates too much drag.

## Installation

```bash
# Add marketplace
/plugin marketplace add agenticnotetaking/arscontexta

# Install
/plugin install arscontexta@agenticnotetaking

# Restart Claude Code, then:
/arscontexta:setup

# Answer 2-4 domain questions (~20 minutes, one-time)
# Restart again to activate generated hooks
/arscontexta:help
```

**Prerequisites:** Claude Code v1.0.33+, `tree`, `ripgrep` (`rg`), optionally `qmd` for semantic search.

## The Name

Named for a historical tradition: *Ars Combinatoria*, *Ars Memoria*, *Ars Contexta* — the art of context. Llull and Bruno created external thinking systems. The missing piece was human traversal. With LLMs, "the wheels can spin again."
