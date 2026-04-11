# KG Shared Memory Evaluation

**Date:** 2026-04-10
**Phase:** KG Phase 6 Research
**Status:** Draft — awaiting RFC review

---

## Summary Recommendation

Git push/pull is sufficient for multi-machine sync in the near term. CRDTs for markdown are not yet mature enough for production use without significant custom work. Defer shared-memory implementation until a concrete multi-agent conflict scenario is observed in real use.

---

## Research Questions

1. How can multiple agents or users share graph state without corruption?
2. What are realistic conflict rates and resolution strategies?
3. Is multi-machine sync a real requirement or can git suffice?

---

## Survey of Approaches

### 1. Git Push/Pull (current baseline)

The graph lives in a git repository. Multiple machines sync via `git push` and `git pull`. Conflicts are detected at the file level and resolved via standard git merge.

**Conflict rate analysis:** In a single-author graph, conflicts are zero. In a multi-author team graph, conflicts arise when two agents update the same note concurrently. Since notes are small markdown files, git's line-level diff usually resolves conflicts automatically. Manual resolution is needed for frontmatter-only edits (e.g., two agents updating `updated_at` simultaneously).

**Resolution:** Conflicts on `updated_at` and `status` fields are last-write-wins at the YAML field level — acceptable for ephemeral metadata. Content conflicts in the body require human review.

**Fit:** Excellent for async workflows (one agent writes, another reads after a sync cycle). Poor for real-time concurrent writes.

**Recommendation:** Use git as the primary sync mechanism. Treat concurrent writes as a race condition to avoid rather than a system to resolve — enforce single-writer-per-note contracts.

### 2. CRDTs for Markdown

CRDT (Conflict-free Replicated Data Type) approaches for text include Yjs, Automerge, and Diamond Types. They enable concurrent edits without conflicts by representing document state as a DAG of operations.

**Fit assessment:**
- CRDT libraries for Go are immature (Yjs is JS/Rust, Automerge has Go bindings but is complex)
- Frontmatter fields could be modeled as LWW-Map CRDTs (last-write-wins registers)
- Markdown bodies could use text CRDT but output is a character-position DAG, not readable markdown

**Key problems:**
- CRDT history grows unboundedly without compaction
- Human-readable markdown is a poor fit for character-level CRDT (whitespace, paragraph breaks become ambiguous)
- Merging YAML frontmatter via CRDTs requires custom data types per field

**Recommendation:** Do not implement. The complexity cost exceeds the value for a local-first, primarily single-author system.

### 3. Custom Sync Protocol (note-level last-write-wins with tombstones)

Define a sync protocol where:
- Each note has a `version` (monotonic counter) in its frontmatter
- A sync operation compares version counters and takes the higher-versioned note
- Deleted notes are replaced with tombstones (status: `archived`, `deleted_at` field)
- Conflicts (same version, different content) trigger a lint warning and create a forked note

**Fit:** Simple to implement, predictable behavior, compatible with git-based storage.

**Gaps:**
- Version counters only work if writers are coordinated (no two agents write the same note simultaneously without bumping version)
- Requires a sync coordinator or timestamps (susceptible to clock skew)
- Does not handle split-brain scenarios (two graphs diverge for a long period)

**Recommendation:** If shared-memory is needed, implement this as the first approach. Design version field into `GraphNote` as `version int` (default 0, increment on update). Build sync as a separate `kg sync` command, not baked into writes.

### 4. Dedicated Graph Database (e.g., DGraph, Weaviate, Neo4j)

Replace the filesystem graph with a proper graph database that handles concurrent writes natively.

**Fit:** Excellent concurrency, rich query support, mature replication.

**Gaps:**
- Eliminates the "local-first, human-readable markdown" property that makes the KG inspectable and git-versionable
- Introduces an operational dependency (database process, backup, upgrade path)
- Severe breaking change to the existing Phase 1-5 architecture

**Recommendation:** Out of scope for this project. The local-first file-based design is a core property, not an implementation detail.

---

## Conflict Rate Estimation

For the primary use case (single developer, multiple Claude Code sessions on one machine):
- **Expected conflict rate:** Near zero. Sessions are sequential, not concurrent.

For secondary use cases (same developer, multiple machines synced via git):
- **Expected conflict rate:** Low. Git push/pull creates a happens-before ordering. Conflicts only when two machines are edited offline simultaneously.

For tertiary use cases (team with multiple developers):
- **Expected conflict rate:** Medium. The single-writer-per-note contract breaks down. Requires explicit coordination.

**Conclusion:** The near-zero conflict rate for the primary use case does not justify CRDT implementation cost. Git provides sufficient concurrency guarantees for the expected usage pattern.

---

## Decision

| Approach | Complexity | Conflict handling | Human-readable | Recommendation |
|----------|-----------|-------------------|----------------|----------------|
| Git push/pull | None | Last-write-wins (file level) | Yes | ✅ Use now |
| CRDTs | Very High | Automatic | Degraded | ❌ Defer indefinitely |
| Custom LWW + version | Medium | Explicit version race | Yes | Consider if team use emerges |
| Graph DB | High + operational | Native | No | ❌ Out of scope |

---

## Recommendation

1. **Use git push/pull for multi-machine sync.** Document the single-writer-per-note expectation.
2. **Add `version int` to `GraphNote` frontmatter** as a reserved field, defaulting to 0. Increment in `updateGraphNote`. This costs nothing and enables a sync protocol later.
3. **Do not build a sync coordinator now.** If team use becomes a real requirement, the custom LWW approach with version counters is the implementation path.
4. **Revisit at KG Phase 7** (not yet planned) if shared-graph use cases emerge in practice.
