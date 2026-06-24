# Your Agent Does Not Need One Summary. It Needs a Compaction Plan.

- **Author:** Anshul (@luhsnaa)
- **Source tweet:** https://x.com/luhsnaa/status/2068709582537654775
- **X long-form article:** https://x.com/i/article/2068053717925117952 (body paywalled / HTTP 402)
- **Project / canonical source:** https://github.com/anshulluhsna/compaction-orchestrator
- **Author site:** https://arbityr.live
- **Published:** ~2026-06-21
- **Retrieved:** 2026-06-24
- **Status:** RETRIEVED — tweet had no prose (link card only); the X article body is paywalled, but the GitHub README is the full, public substance and is reproduced verbatim below. The article thesis excerpt was recoverable from the article card.

## Article thesis (from the X article card)

> "A summary asks: what happened? Compaction should ask: what must survive for the next turn to work?"

> "A user constraint should not be compressed like an old search result; an active typecheck error should not be treated like completed exploration."

---

## Source content (GitHub README, verbatim)

### Your Agent Does Not Need One Summary. It Needs a Compaction Plan.

Compaction Orchestrator is an open-source compaction control layer for custom AI agents.

Long-running agents eventually hit the same wall: too much context, too much tool output, too many decisions, too many half-useful logs.

The default answer is usually trimming, rolling summaries, or one generic summary.

That is the wrong primitive.

A user constraint should not be compressed like an old search result. An active typecheck error should not be treated like completed exploration. A customer escalation should not disappear because it happened ten turns ago.

Compaction Orchestrator gives the agent a plan.

It stores the raw session, classifies the context, chooses a strategy for each segment in the current turn, and returns a smaller runtime context view.

### The Core Idea

Compaction is not one operation. It is a routing decision.

```
raw session events
-> segment classification
-> per-segment strategy routing
-> compaction plan
-> runtime context view
```

One turn can mix strategies:

```
user constraint      -> keep_verbatim
current failure      -> extract_active_error
large tool output    -> externalize_for_retrieval
completed work       -> structured_summary
```

The original session is not overwritten. The canonical event log stays intact. Every compaction creates a derived context view with an inspectable plan.

### Why This Exists

Most agent memory systems optimize for smaller context. That is not enough.

The real question is not: "How do we summarize this conversation?"

The real question is: "What should each piece of context become before the next model call?"

That is the difference between a summarizer and a control layer.

### ACCS Evaluation Results

| Use case | Generic summary ACCS | rolling_summary_recent ACCS | Compaction Orchestrator ACCS |
|----------|----------------------|----------------------------|------------------------------|
| Coding agent | 0.548 | 0.698 | 0.836 |
| Customer support | 0.410 | 0.474 | 0.773 |
| Voice agent | 0.430 | 0.767 | 0.886 |

The customer-support fixture also has a live DeepSeek probe: DeepSeek recovered `5/6` facts from the generic summary and `6/6` from the orchestrated context.

### What Is Built

SDK (`compact()` inside an agent loop), CLI (compacts a JSON session from file/stdin), HTTP API (persists sessions, returns context views), SQLite store (sessions, events, plans, views, externalized content), Web UI, OpenAPI spec. Root package is SDK-first; persistence is explicit (`SqliteStore`).

### SDK usage

```javascript
import { compact } from "@anshulluhsna/compaction-orchestrator";

const result = compact({
  messages,
  objective: "Prepare context for the next agent turn.",
  policy: { mode: "balanced", preserveUserMessagesVerbatim: true, allowExternalRetrieval: true }
});

console.log(result.contextView.content);
console.log(result.plan.segments.map((segment) => segment.operation));
```

The important output is not only `contextView`. It is `plan` — what the agent chose to keep, extract, externalize, or summarize. Policy is deterministic in the current alpha. `cost_first` picks the supported strategy with the highest estimated token savings; other modes use a default safety ordering (preserve user instructions, extract active errors when allowed, then first supported strategy). The strategy interface is pluggable.

It can import real Claude Code (`~/.claude/projects/**/*.jsonl`) and Codex sessions into the fixture shape.

### Strategy Matrix

| Context type | Default risk | Strategy |
|--------------|--------------|----------|
| User instruction | Exact wording gets lost | `keep_verbatim` |
| Active error | Debugging signal gets blurred | `extract_active_error` |
| Large tool output | Context window gets flooded | `externalize_for_retrieval` |
| Completed exploration | Old work takes too much space | `structured_summary` |
| Support escalation | Operational state gets dropped | use-case package |
| Voice turn state | Latency optimization drops slots/consent | cost-first plan with preserved exact state |

### API Shape

- One-shot compaction: `POST /v1/compact`
- Persisted session compaction: `POST /v1/sessions/:sessionId/compact`
- Customer-support package: `POST /v1/sessions/:sessionId/context-package`
- OpenAPI contract: `docs/openapi.yaml`

### Current Status

Launchable as an alpha/demo repo: working SDK, CLI, API, SQLite persistence, UI demo, examples, tests, docs, OpenAPI spec, CI, MIT license, npm metadata. Not production infra yet; current eval is on curated fixtures rather than broad real-world traces. Next work: optional LLM-powered strategies behind the existing interface, broader real-world eval traces, production persistence option. License: MIT.

> Note: ACCS numbers and strategy names are self-reported by the project README, not independently verified.
