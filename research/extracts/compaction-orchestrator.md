## Extract: Compaction Orchestrator (@luhsnaa)

**Source**: https://x.com/luhsnaa/status/2068709582537654775 (body: github.com/anshulluhsna/compaction-orchestrator)
**Author**: Anshul (@luhsnaa)
**Date**: 2026-06-21
**Method**: Playwright (tweet) + WebFetch (project README)
**Raw archive**: research/articles/compaction-orchestrator.md

### Summary

Compaction is a routing decision, not one summary: classify each context segment, route to a per-segment strategy, emit an inspectable plan + derived view over an immutable raw event log. Maps onto our derivation model and §3A typed-view projection.

### Key Quotes

> "Compaction is not one operation. It is a routing decision."

> "What should each piece of context become before the next model call?"

---

## Key claims
- One generic summary is the wrong primitive for agent context compaction. Compaction is a **routing decision**, not a single operation: classify each segment, route it to a strategy, emit an inspectable **plan** + a derived runtime view.
- The raw/canonical session event log is **never overwritten**; the compacted view is a *derivation* of it with an auditable plan attached.
- Per-segment strategy matrix: `keep_verbatim` (user instructions), `extract_active_error` (live debug signal), `externalize_for_retrieval` (large tool output, moved out-of-band with pointers), `structured_summary` (completed work).
- Policy-driven and reproducible (`balanced` / `cost_first` modes); strategy interface is pluggable.
- Self-reported ACCS metric (does each critical fact survive compaction?) beats rolling-summary baselines across coding / support / voice fixtures.

## Techniques
- 4-stage pipeline: raw events -> segment classification -> per-segment routing -> compaction plan -> runtime view.
- Typed, domain-specific output packages (e.g. a customer-support handoff carrying identity / escalation / policy constraints / next action) instead of free-text summaries.
- SQLite store for sessions, events, plans, views, externalized content; importers for Claude Code & Codex JSONL traces.

## What's novel
- The **plan itself is the auditable artifact** — separating "what survives" from "the summary."
- **Externalize-for-retrieval** as a first-class strategy: move bulky content out of the working window with a pointer back to canonical storage.

## Mapping to our work
- **scoped-knowledge-graphs / derivation model:** Directly mirrors our "canonical source is never destroyed; derived views carry provenance" stance. The compaction plan == a provenance-bearing derivation (`{reason, because[], fired_at}`-shaped) over an immutable event log. Strong validation of event-log-as-SOT + derived-view design.
- **work-tracking-storage-abstraction §3A typed views:** `keep_verbatim / extract_active_error / externalize / structured_summary` is essentially a per-segment routing of raw episodic events into working/operational/semantic shapes — concrete prior art for how our episodic->semantic projection could be policy-driven rather than ad hoc.
- **session-handoff journal (memory theme):** The Claude Code JSONL importer + immutable event log is exactly our append-only journal substrate; their `externalize_for_retrieval` pointer pattern is a candidate mechanism for keeping the journal lean while the KG holds the externalized payload.
- **Concrete proposal idea:** a `compaction-plan` note type in the episodic view — when the agent compacts, write the plan as a KG node (edges: `derived_from` -> raw events, `preserves` -> the surviving facts) so handoff/recovery can replay *what was deliberately kept vs dropped*, not just a summary.

## Caveats
- Alpha; ACCS numbers are self-reported on curated fixtures, not independent. Policy is deterministic-only today (LLM strategies are future work).
