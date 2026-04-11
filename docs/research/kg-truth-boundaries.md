# KG Truth Boundaries

**Date:** 2026-04-10
**Phase:** KG Phase 6 Research
**Status:** Draft — awaiting RFC review

---

## Summary

Four distinct truth domains exist in the dot-agents + KG system. Each domain has a single authoritative store. Cross-domain reads are permitted; cross-domain writes must go through explicit integration points. Conflating domains causes state corruption and invalidates agent reasoning.

---

## Truth Domains

### 1. Graph Truth

**What it owns:** Curated knowledge — entities, decisions, synthesis, source summaries, cross-links, confidence levels.

**Authoritative store:** `KG_HOME/notes/` — markdown files with YAML frontmatter.

**Properties:**
- Long-lived (notes survive across sessions and projects)
- Intentionally curated (not auto-generated without review)
- Cross-referenced (notes link to other notes via `links` field and `source_refs`)
- Provenance-tracked (every note has `source_refs` pointing back to raw sources)

**Invariants:**
- A note's `id` is immutable once created
- `created_at` is immutable; only `updated_at` changes on update
- `status` can only advance, not regress (active → stale, not stale → active)
- Source notes in `notes/sources/` cannot be deleted, only archived

**Interaction with other domains:**
- Reads from raw sources (via ingest) — one-way import
- Provides context to workflow agents via bridge query — read-only export
- Does not read or write workflow truth directly

### 2. Workflow Truth

**What it owns:** Plan state, task status, checkpoints, proposals, verification records, health snapshots, preferences.

**Authoritative store:** `.agents/active/` (plans, tasks), `~/.agents/context/<project>/` (checkpoints, health, verification, preferences).

**Properties:**
- Project-scoped (tied to a specific repo)
- Operational (changes frequently as work progresses)
- Proposal-gated for shared mutations (preferences, canonical plans)

**Invariants:**
- Canonical plan IDs are stable once written
- Task status follows a defined lifecycle (pending → in_progress → completed/blocked)
- Checkpoints are append-only (never overwritten)
- Health snapshots are computed, not authored — derived from observed state

**Interaction with other domains:**
- May query graph truth for context (via bridge) — read-only
- Does not write to `KG_HOME` directly
- May export artifacts to graph truth via explicit ingest (e.g., ingest a plan spec as a KG source)

### 3. Coordination Truth

**What it owns:** Delegation contracts, merge-back summaries, coordination intents between agents.

**Authoritative store:** `.agents/active/delegation/` and `.agents/active/merge-back/`.

**Properties:**
- Ephemeral (scoped to a single task delegation cycle)
- Write-scope-bounded (contracts declare which files the delegate may touch)
- Resolution-targeted (always ends with merge-back or cancellation)

**Invariants:**
- A delegation contract's `write_scope` is immutable after creation
- Only one active delegation per task at a time
- Merge-back artifacts are produced by the delegate; consumed by the parent — never edited after creation
- Coordination intents are transport-neutral (no chat syntax, no `@mentions`)

**Interaction with other domains:**
- Reads workflow truth (plan/task state) to create delegation contracts
- Produces merge-back summaries that update workflow truth (task status to completed)
- Does not write to graph truth

### 4. Session Truth

**What it owns:** Active agent context, conversation state, ephemeral scratchpad, CLAUDE.md instructions.

**Authoritative store:** CLAUDE.md files, `.claude/` directories, conversation context (in-memory).

**Properties:**
- Highly ephemeral (session-scoped)
- Not persisted to disk (or persisted only for continuity across sessions)
- Agent-specific (different agents may have different views)

**Invariants:**
- Session truth does not override graph truth or workflow truth — it annotates it
- CLAUDE.md instructions apply globally; project-local CLAUDE.md narrows scope
- Lessons (`.agents/lessons/`) live at the boundary of session and workflow truth — written during sessions but persist across sessions

**Interaction with other domains:**
- Reads from all other domains at session start (orient, graph health, preferences)
- Writes lessons to workflow truth (`lessons/`)
- Does not directly write graph truth or coordination truth during normal operation

---

## Cross-Domain Interaction Map

```
Session Truth
  → reads: Graph Truth (bridge query), Workflow Truth (orient/status), Coordination Truth (delegation status)
  → writes: Workflow Truth (lessons, checkpoints via commands)

Workflow Truth
  → reads: Graph Truth (bridge query for context)
  → writes: Coordination Truth (fanout creates delegation contracts)

Coordination Truth
  → reads: Workflow Truth (plan/task state)
  → writes: Workflow Truth (task status updates via merge-back)

Graph Truth
  → reads: raw sources (inbox ingest) [one-way import]
  → provides: query surface to Workflow Truth and Session Truth [read-only export]
```

---

## Risk: Domain Conflation

The highest-risk conflation scenarios:

1. **Agent writes checkpoint into KG notes** — mixes operational workflow state with curated knowledge. Fix: enforce that `kg ingest` is the only path from workflow artifacts to graph truth, and it produces a source note (not a decision note).

2. **Agent stores delegation contract in KG** — coordination truth does not belong in graph truth. Fix: delegation contracts live in `.agents/active/delegation/`, never in `KG_HOME`.

3. **Bridge query result directly modifies workflow state** — graph truth should inform, not drive, workflow decisions. Fix: bridge query is read-only; agents must explicitly create proposals or checkpoints based on query results.

4. **Session truth (CLAUDE.md) overrides canonical preferences** — the preferences system (Wave 4) should be authoritative for shared preferences. Fix: CLAUDE.md instructions apply to agent behavior, not to stored preferences; they are lower precedence than explicit `preferences.yaml`.

---

## Invariants Across All Domains

1. **No circular writes**: Graph truth → Workflow truth → Coordination truth is one-directional. No domain writes to a higher-authority domain without an explicit integration point.
2. **ID stability**: Once a note ID, plan ID, or delegation contract ID is created, it is not renamed (archives instead).
3. **Append-only logs**: `notes/log.md`, `verification-log.jsonl`, and `sweep-log.jsonl` are append-only. No deletion.
4. **Proposal gating**: Mutations that affect shared state (shared preferences, canonical plans) require proposal review before applying.
