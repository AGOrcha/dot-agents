# KG Verification Models

**Date:** 2026-04-10
**Phase:** KG Phase 6 Research
**Status:** Draft — awaiting RFC review

---

## Summary Recommendation

Git-based verification (commit SHAs + signed commits) is sufficient for the local-first graph. Content hashing for individual notes adds value as a lightweight tamper-detection layer. Full Merkle DAG or signature chains are premature — implement if multi-author shared graphs become a requirement.

---

## Research Questions

1. What level of provenance verification is needed beyond `source_refs`?
2. Which verification model fits a local-first markdown graph best?
3. What is the implementation complexity vs. risk reduction tradeoff?

---

## Survey of Approaches

### 1. Git-Based Verification (current baseline)

Git tracks every change with SHA1/SHA256 hashes. Signed commits (GPG/SSH) provide author attribution. For a local-first, single-author graph, git already provides:

- Immutable history via commit SHAs
- Author attribution via commit metadata
- Tamper detection via `git fsck`
- Rollback via `git revert`

**Fit:** Excellent for local-first, single-author or small-team scenarios. Git is already a dependency of dot-agents workflows.

**Gaps:**
- Does not protect individual note files against out-of-git modification (e.g., direct filesystem writes)
- No per-note integrity check without reading full git history

### 2. Content-Addressable Note Hashing

Store a SHA-256 hash of each note file in its YAML frontmatter (`content_hash` field) or in a separate manifest (`ops/integrity/manifest.json`).

On `kg lint`, verify each note's hash matches its content. Mismatches surface as `integrity_violation` lint findings.

**Fit:** Low implementation cost (~50 LOC). Detects out-of-git edits. Does not prevent corruption but surfaces it quickly.

**Gaps:**
- Hash is self-referential if stored in frontmatter (must hash body only, not frontmatter)
- Requires disciplined hash update on every note write (must be part of `createGraphNote` / `updateGraphNote`)

**Recommendation:** Implement as Phase 4 extension (add to lint). Hash body only; store in `ops/integrity/manifest.json` keyed by note ID.

### 3. Signature Chains (author attestation)

Each note write includes a cryptographic signature from the author. Enables multi-author attribution and tamper detection even for notes that have passed through multiple hands.

**Fit:** Needed only if the graph has multiple untrusted contributors. For single-author local graphs, git signed commits provide equivalent guarantees.

**Gaps:**
- High implementation complexity (key management, signature validation)
- Poor UX for solo developers
- Premature for current use case

**Recommendation:** Do not implement. Revisit if shared-memory graphs become a requirement.

### 4. Merkle Tree / DAG Integrity

Build a Merkle DAG over the note graph: each note's hash includes hashes of its linked notes. The root hash represents the entire graph state.

**Fit:** Useful for partial graph sync (transfer only subtrees that diverge). Core requirement of DKG-like shared memory.

**Gaps:**
- High implementation complexity
- Requires rebuilding the Merkle DAG on every note write (or caching with invalidation)
- Overkill for local-first single-machine graphs

**Recommendation:** Do not implement now. Design the `GraphHealth` struct to include a `graph_root_hash` field as a reserved placeholder. Implement if and when shared-memory sync is built.

---

## Decision Matrix

| Approach | Complexity | Value (local) | Value (shared) | Recommendation |
|----------|-----------|---------------|----------------|----------------|
| Git-based (current) | None (free) | High | Medium | Keep as baseline |
| Content hash manifest | Low (~50 LOC) | Medium | Medium | Implement in Phase 4 extension |
| Signature chains | High | Low | High | Defer to shared-memory phase |
| Merkle DAG | Very High | Low | Very High | Defer; reserve field in health |

---

## Recommendation

1. **Keep git as the primary verification layer.** It provides history, attribution, and tamper detection at no additional cost.
2. **Add a content hash manifest** (`ops/integrity/manifest.json`) as an extension to `kg lint` to catch out-of-git modifications. Implement as a non-blocking `info` lint finding initially.
3. **Reserve `graph_root_hash` in `GraphHealth`** for future Merkle-based sync.
4. **Do not implement signature chains** until there is a concrete multi-author requirement.

---

## Implementation Estimate

Content hash manifest: ~2 hours — new lint check + `updateGraphNote` hook to update manifest.

No other changes needed for the local-first phase.
