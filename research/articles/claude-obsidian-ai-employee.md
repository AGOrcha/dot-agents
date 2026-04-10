# Claude + Obsidian = A True AI Employee

**Author:** Fraser Cottrell (@sourfraser)
**Source:** https://x.com/sourfraser/status/2035454870204100810
**Published:** March 21, 2026
**Follow-up:** https://x.com/sourfraser/status/2039756047162671220 (full system walkthrough, April 2)

---

There's a difference between using AI and having an AI employee. Fraser Cottrell built a Claude + Obsidian system in an afternoon that costs almost nothing to run and now runs half his business. The single biggest unlock he's found as a founder.

He forgot about a client's shipping decision made three weeks prior. When he asked Claude about that client's status, it pulled up the decision from a meeting transcript with the exact context of why they made it. That's the difference — Claude had been reading every meeting note, every Slack message, every client update for weeks. It knew more about the business state than he did.

## The System

The vault is set up with a Memory file, a Home page, and folders for whatever you track. The key insight: **Claude reads the vault at the start of every session**, so it knows more every time you talk to it.

### Core Components

**Client Roster** — Active clients with key details, health status, and responsibility assignments. Claude can cross-reference client history across meetings, Slack, and email to give status reports on demand.

**Action Tracker** — Open tasks, owners, and due dates. Updated by Claude after every meeting transcript ingestion. Surfaces what's overdue without anyone asking.

**Library of Frameworks** — Sales process, production workflow, and org structure documented as reference material. Claude uses these to make suggestions consistent with how the business actually operates.

**Templates** — Call notes, follow-up emails, proposals, and daily briefs. Claude fills these in automatically based on context from recent conversations and meetings.

## How It Works

The magic is **Claude Cowork** running on the desktop, connected to actual tools through MCP (Model Context Protocol):

- **Slack** — Claude reads channels and can give cross-client status reports
- **Google Calendar** — Knows what meetings are coming, what happened in past ones
- **Gmail** — Reads email threads for client communication context
- **Google Drive** — Pulls up shared documents and proposals
- **ClickUp** — Task management integration

You can say "check my Slack and tell me what's going on across clients" and get a full status report in minutes. Claude isn't just reading your vault — it's reading your Slack channels, checking your calendar, pulling up your Drive files, and cross-referencing everything.

## The Compounding Effect

The vault grows every day. By week four, Claude knows your clients, team dynamics, processes, communication preferences, and outcomes of previous conversations. It onboards itself a little more every day.

This is the same compounding knowledge pattern described in the [Claude + Obsidian memory stack](claude-obsidian-memory-stack.md) — but applied to business operations rather than software development. The three-layer architecture (session memory, knowledge graph, ingestion pipeline) maps directly:

- **Session memory** = CLAUDE.md with business context and preferences
- **Knowledge graph** = The Obsidian vault with client rosters, action trackers, frameworks
- **Ingestion pipeline** = Meeting transcripts, Slack messages, email threads flowing in via MCP

## Connection to dot-agents

This system demonstrates the [agent-as-operator](../AGENT_AS_OPERATOR_RESEARCH.md) pattern in a business context:

1. **Orient** — Claude reads the vault at session start (the orient hook)
2. **Persist** — Meeting notes, action items, and client updates are saved back to the vault
3. **Propose** — Claude surfaces insights, flags overdue items, suggests follow-ups

The human doesn't operate the system. The human steers — reviewing what Claude surfaces, approving follow-up emails, making strategic decisions. Everything else is automated.

The multi-tool MCP integration also reinforces why dot-agents needs to manage MCP configurations centrally. Fraser's system requires wiring up 5+ MCP servers — the kind of setup that should be portable across machines and shareable with team members.

## Key Insight

> "There's a difference between using AI and having an AI employee."

The difference is persistence. An AI tool answers questions. An AI employee accumulates context, tracks state, notices patterns, and gets better at its job every day. The vault is what makes this possible — without externalized memory, every session starts from zero.
