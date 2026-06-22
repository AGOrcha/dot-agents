---
title: Demo Diagrams
description: Two diagrams plus talk tracks for a short leadership walkthrough of dot-agents.
sidebar:
  order: 6
---

# dot-agents — Leadership Demo Diagrams

**Audience:** Leadership / tech leads evaluating an agent-platform investment.
**Companion docs:** [`DEMO_INDEX.md`](./DEMO_INDEX.md) ·
[`LOOP_ORCHESTRATION_SPEC.md`](./LOOP_ORCHESTRATION_SPEC.md) ·
[`RESOURCE_COMMAND_CONTRACT.md`](./RESOURCE_COMMAND_CONTRACT.md)

Two diagrams + talk tracks suitable for a 5–10 minute live walkthrough:

- **Diagram 1** — *what* dot-agents is: one canonical home that fans out to every
  AI coding agent the team uses.
- **Diagram 2** — *how* it automates development work: the orient → act → persist
  → delegate → propose loop that lets agents resume, verify, hand off, and
  propose their own improvements without losing context.

---

## 1. Architecture — one canonical home, every agent

```mermaid
%%{init: {"flowchart": {"defaultRenderer": "elk"}} }%%
flowchart TB
    Dev["👤 Developer / Tech Lead"]

    subgraph Canonical["~/.agents/ — single source of truth"]
        direction LR
        Rules["rules/<br/><i>always-on<br/>guidelines</i>"]
        Skills["skills/<br/><i>on-demand<br/>procedures</i>"]
        Agents["agents/<br/><i>subagent<br/>definitions</i>"]
        Hooks["hooks/<br/><i>lifecycle<br/>automation</i>"]
        Settings["settings/ · mcp/<br/><i>per-platform<br/>config</i>"]
    end

    CLI["<b>da</b> CLI &nbsp;·&nbsp; init · add · refresh · doctor · install"]

    subgraph Projects["Repo-local projections (auto-linked)"]
        direction LR
        P1[".cursor/rules/<br/><i>hard links</i>"]
        P2["CLAUDE.md<br/>.claude/<br/><i>symlinks</i>"]
        P3["AGENTS.md<br/>.codex/<br/><i>symlinks</i>"]
        P4[".github/<br/>copilot · skills<br/>· agents"]
    end

    subgraph AI["AI coding agents"]
        direction LR
        Cursor["Cursor"]
        Claude["Claude Code"]
        Codex["Codex"]
        Copilot["GitHub Copilot"]
        Other["OpenCode"]
    end

    Dev -->|edits once| Canonical
    Canonical --> CLI
    CLI -->|projects| Projects
    Projects --> AI

    classDef hub fill:#1f2937,stroke:#0ea5e9,stroke-width:2px,color:#f9fafb
    classDef cli fill:#0ea5e9,stroke:#0369a1,color:#f9fafb
    classDef proj fill:#f1f5f9,stroke:#64748b,color:#0f172a
    classDef tool fill:#fef3c7,stroke:#d97706,color:#78350f
    class Canonical hub
    class CLI cli
    class Projects proj
    class AI tool
```

### Talk track (≤60 seconds)

- Today, every AI coding agent has its own config format — Cursor uses
  `.cursor/rules/`, Claude Code uses `CLAUDE.md`, Codex uses `AGENTS.md`,
  Copilot uses `.github/`. Rules get duplicated per repo, drift across machines,
  and no one has a single source of truth.
- `dot-agents` consolidates all of it into **one canonical home** at
  `~/.agents/`. You write a rule, skill, or agent definition **once**.
- The **`da` CLI** projects it into every repo and every platform — hard links
  for Cursor (which can't follow symlinks) and symlinks for everything else.
  Edit the canonical file once and every project picks it up.
- Net effect: write a coding standard once, every agent in every repo enforces
  it. No more copy-paste, no more drift.

---

## 2. Workflow automation — the agent operating loop

```mermaid
flowchart TB
    Start(["🟢 Session start"])

    subgraph Loop["dot-agents workflow loop"]
        direction LR
        Orient["<b>Orient</b><br/>da workflow orient<br/><i>Load active plan,<br/>last checkpoint,<br/>verification state</i>"]
        Act["<b>Act</b><br/>Agent does work<br/><i>edit · test · build</i>"]
        Persist["<b>Persist</b><br/>checkpoint · verify · advance<br/><i>Save progress, results,<br/>task state</i>"]
        Delegate["<b>Delegate</b><br/>fanout → merge-back → closeout<br/><i>Bounded sub-agents<br/>with write-scope limits</i>"]
        Propose["<b>Propose</b><br/>da review<br/><i>Queue rule/skill/config<br/>changes for human approval</i>"]
    end

    KG[("📚 Knowledge Graph<br/>da kg<br/><br/>structured notes ·<br/>code graph ·<br/>decision history")]

    Human["👤 Human review<br/>approve · reject"]

    Start --> Orient
    Orient --> Act
    Act --> Persist
    Persist --> Act
    Act --> Delegate
    Delegate --> Persist
    Persist --> Propose
    Propose --> Human
    Human -->|approved| Canonical2["~/.agents/<br/>updated"]
    Canonical2 -.->|next session| Orient

    KG <-->|context in,<br/>learnings out| Orient
    KG <-->|impact analysis| Act

    classDef step fill:#0ea5e9,stroke:#0369a1,color:#f9fafb
    classDef store fill:#1f2937,stroke:#0ea5e9,color:#f9fafb
    classDef human fill:#fef3c7,stroke:#d97706,color:#78350f
    class Orient,Act,Persist,Delegate,Propose step
    class KG,Canonical2 store
    class Human human
```

### Talk track (≤90 seconds)

The loop turns AI coding agents into something closer to **operators** than chat
assistants:

1. **Orient** — Every session starts with `da workflow orient`. The agent loads
   the active plan, the last checkpoint, what's already been verified, and
   relevant context from the knowledge graph. **No more 30–40 minutes per
   session re-explaining yesterday's work.**
2. **Act** — The agent works against a canonical plan with explicit tasks. The
   knowledge graph's code graph tells it the **blast radius** of any change, so
   it knows what it's about to break before it breaks it.
3. **Persist** — Checkpoints, verification results, and task progress get
   written to repo-local state. The agent **stops rediscovering what's broken
   vs. what it just caused**.
4. **Delegate** — Large work fans out to sub-agents with bounded write scopes,
   then merges back with structured summaries. A `pr-ci` verifier profile owns
   the PR-readiness loop (CI + SonarQube auto-fix) so the impl agent exits
   cleanly at merge-back. Parent runs `workflow delegation closeout` to archive
   the artifact and auto-advance the task — no extra `workflow advance` needed.
5. **Propose** — When the agent notices a pattern worth codifying (a new rule,
   missing skill, config drift), it queues a proposal via `da review`. **Humans
   steer**; agents handle the mechanical work.

Approved proposals flow back into `~/.agents/`, so **the system gets smarter
every session**. The knowledge graph accumulates decisions, entities, and code
structure — making the next orient even faster.

### Business value at a glance

| Pain today | What dot-agents changes |
|---|---|
| 30–40 min/session re-explaining context | `orient` resumes in seconds from canonical state |
| Rules duplicated across N repos × M agents | Write once in `~/.agents/`, link everywhere |
| Agents rediscover what's broken every session | Verification state persists across sessions |
| No safe way to run parallel agents | Bounded `fanout` with explicit write-scopes |
| Tribal knowledge stays tribal | Knowledge graph captures decisions + code structure |
| Agents can't improve their own setup | `propose → review → approve` closes the loop |

---

## Demo script (5 minutes)

1. **`da status`** — show one repo already managed; point out linked files.
2. **`da workflow orient`** — show the structured context an agent gets at
   session start.
3. Edit a rule in `~/.agents/rules/global/` — show it instantly reflected in
   `.cursor/rules/` and `CLAUDE.md` via the link.
4. **`da workflow checkpoint`** + **`da workflow verify record`** — demonstrate
   persisted state.
5. **`da kg query --intent decision_lookup "why postgres"`** — show structured
   memory recall.
6. **`da workflow fanout` → `merge-back` → `delegation closeout`** — bounded
   sub-agent lifecycle in one minute (optional if time allows).
7. **`da review`** — show a pending proposal an agent queued, approve it, watch
   it land in `~/.agents/`.

Close with the roadmap slide: **agent-as-operator** — agents run `da`
themselves, surfacing only the decisions humans need to make. The current
design pass (see
[`codex-019e6245-examination-and-sequenced-plan`](../.agents/proposals/codex-019e6245-examination-and-sequenced-plan.md))
sequences the remaining work: typed staged dispatch, per-stage native agents,
two-tier config distribution, scope-routed `da review`.
