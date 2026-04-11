# RFC: KG Shared Memory Layer

**Date:** 2026-04-10
**Status:** Draft — requires review before any implementation
**Authors:** dot-agents system (KG Phase 6)
**Depends on:** `docs/research/kg-verification-models.md`, `docs/research/kg-shared-memory-evaluation.md`, `docs/research/kg-truth-boundaries.md`

---

## Problem Statement

The knowledge graph (KG) is currently a local-first, single-machine system. There is no mechanism for:
1. Multiple agents writing to the same graph concurrently without conflict
2. Synchronizing the graph across multiple machines
3. Verifying note integrity against out-of-git modifications

This RFC proposes a minimal, phased approach to address these concerns without abandoning the local-first, human-readable markdown design.

---

## Non-Goals

This RFC explicitly does not propose:

- A real-time collaborative editing system
- A graph database backend (replacing filesystem)
- CRDTs for markdown content
- A continuously running sync daemon
- Multi-author trust attestation via signature chains

These are out of scope for the near term. Any future work in these areas requires a separate RFC.

---

## Proposed Approach

### Phase A: Content Hash Manifest (deferred to Phase 4 extension)

**Scope:** Lightweight tamper detection for local graph integrity.

**What:** Add `ops/integrity/manifest.json` mapping note IDs to SHA-256 hashes of note bodies (excluding frontmatter to avoid self-referential hashing). Update manifest in `updateGraphNote`. Check manifest in `kg lint` as a new `integrity` check (severity: `warn`).

**Why now:** Low implementation cost (~2 hours), catches out-of-git modifications, adds no operational complexity.

**Design:**
```json
{
  "schema_version": 1,
  "updated_at": "2026-04-10T...",
  "notes": {
    "dec-001": { "hash": "sha256:abc123...", "updated_at": "..." },
    ...
  }
}
```

**Acceptance criteria:**
- `kg lint` reports `integrity_violation` for notes edited outside of kg commands
- `updateGraphNote` updates manifest atomically (write new manifest after note write)
- `kg setup` initializes an empty manifest

---

### Phase B: Version Counter (implement now as reserved field)

**Scope:** Enabling future sync without breaking existing notes.

**What:** Add `version int` to `GraphNote` frontmatter (default 0). Increment in `updateGraphNote`. No sync logic yet — this is infrastructure.

**Why now:** Adding a version field retroactively requires migrating all notes. Doing it now is free. The field is transparent to existing queries and lint checks.

**Design:** `version: 0` in frontmatter. Incremented as `version: version + 1` on every `updateGraphNote` call.

**Acceptance criteria:**
- New notes created with `version: 0`
- `updateGraphNote` increments version
- Existing notes without `version` field are treated as `version: 0` (backward compatible)

---

### Phase C: Git Push/Pull Sync (current recommendation for multi-machine)

**Scope:** Multi-machine sync for single-author use.

**What:** Document the git-based sync workflow. Provide `kg sync` as a thin wrapper around `git pull` + `kg lint` (to validate after pull).

**Design:**
```bash
dot-agents kg sync          # git pull + kg lint
dot-agents kg sync --push   # git push
```

**Acceptance criteria:**
- `kg sync` pulls latest, runs lint, reports conflicts (lint findings that indicate content drift)
- `kg sync --push` pushes current state
- Conflicts in frontmatter fields (e.g., `updated_at` on same note) surface as `integrity_violation` lint findings, not hard errors

---

### Phase D: Custom LWW Sync Protocol (deferred — implement when team use emerges)

**Trigger:** When a second human author writes to the same graph, or when two machines consistently conflict on the same notes.

**What:** A note-level last-write-wins protocol using version counters. Sync coordinator reads version fields, takes higher-versioned note, creates forked notes on version tie.

**Not to implement before:** At least one concrete conflict scenario is observed in real use. The research (kg-shared-memory-evaluation.md) shows expected conflict rate is near zero for the primary use case.

---

## Truth Boundaries Preserved

This RFC preserves all invariants from `docs/research/kg-truth-boundaries.md`:

- Graph truth remains authoritative for note content
- Workflow truth is not written into `KG_HOME` except via explicit `kg ingest`
- Coordination contracts remain in `.agents/active/delegation/`
- No circular writes between domains

---

## Phased Implementation Plan

| Phase | What | When | Complexity |
|-------|------|------|-----------|
| A | Content hash manifest | Next implementation sprint | Low (~2h) |
| B | Version counter field | Next implementation sprint | Trivial (~30min) |
| C | `kg sync` wrapper | Next implementation sprint | Low (~1h) |
| D | LWW sync protocol | When team use emerges | Medium (~1 week) |

Phases A, B, C can ship together as a single PR. Phase D requires its own RFC when triggered.

---

## Blocking Risks

1. **Version counter drift** — if two agents write the same note with version N simultaneously, both become version N+1. The sync protocol in Phase D must detect this (same version, different content = fork).

2. **Manifest staleness** — if `updateGraphNote` fails partway, the manifest may be out of sync with the note. `kg lint` should re-derive the expected hash rather than trusting a cached manifest value.

3. **Git rebase / history rewrite** — git rebase changes commit SHAs and invalidates git-based verification. Document: rebase is not permitted on the KG branch; use merge commits only.

---

## Decision Required

**To proceed with Phase A + B + C**, this RFC needs acknowledgment that:

1. The non-goals list is accepted (no CRDT, no graph DB)
2. The truth boundary invariants are accepted
3. Git push/pull is accepted as the sync mechanism for the near term
4. Phase D will not be implemented without a new RFC and observed conflict scenario

**Status:** Awaiting review.
