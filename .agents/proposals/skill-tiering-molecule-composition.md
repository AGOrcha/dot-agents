# Proposal: tiering contract refinement — dispatch DEPTH governs composition, not tier-adjacency

**ID:** skill-tiering-molecule-composition
**Scope:** project-local (markdown per `proposal-routing` — amends a repo spec, not a shared
`~/.agents/` resource)
**Status:** proposed (delta against `skill-tiering-contract`; owner-ruled — fold in on review)
**Created:** 2026-06-26
**Author:** agent-proposed, capturing an owner ruling
**Targets:**
- `.agents/workflow/specs/skill-tiering-contract/design.md` — §3 tier table intent column,
  §4 tier invariants (T0/T1/T2 Composition bullets), §5 done-criteria (lint fixtures).
**Motivated by:**
- `.agents/workflow/specs/ideation-system-composition/design.md` (OQ1) — the
  `kg-ideate → spec-scaffold → ideation-cycle → kg-brief` chain exposed the over-strictness.
- `internal/scaffold/home/starter/skills/global/ideation-cycle/` — a molecule that calls a
  molecule (`kg-brief`), legal only under this refinement.

---

## Why this is a delta, not a rewrite

`skill-tiering-contract` is the canonical tier spec. Per `proposal-routing`, a change to it
is **proposed as a delta**; the owner folds it in. This proposal captures the owner ruling
that composition is governed by dispatch DEPTH, not tier-adjacency.

## The problem with the current contract

The current contract restricts `calls:` by **tier-adjacency**:

- **T0 atom** (§4) — "declares no downstream skill, molecule, or compound calls."
- **T1 molecule** (§4) — "`calls:` lists the **atoms** it invokes"; judgment "bounded to
  picking among declared **atoms**."
- **T2 compound** (§4) — "`calls:` lists the **molecules** it orchestrates."

This implies a molecule may call only atoms and a compound only molecules. That adjacency
rule is stricter than the research it cites. The reliability finding (§1.2, shivsakhuja /
`the_smart_ape`) is about dispatch **DEPTH** — agents reliably dispatch through **1–2 hops**
and degrade past that — **not** about which tier may call which. Forcing a structural hoist
purely to satisfy adjacency (e.g. bouncing a hard-fork dispatch back up to the compound so a
molecule never calls a molecule) creates redundant indirection that buys no reliability.

## The ruling (the delta)

### D-refine-1 — Composition is governed by dispatch DEPTH, not tier-adjacency

- The composition invariant is: **≤2 reliable skill-to-skill dispatch hops on any runtime
  path.** Depth is what degrades; depth is what the contract bounds.
- **A molecule MAY call molecules (and atoms). A compound MAY call compounds, molecules, and
  atoms.** Tier no longer constrains WHICH tier you may call.
- **Tier continues to denote AUTONOMY semantics only:**
  - **atom** — indivisible; declares **no downstream calls** (unchanged — an atom is a leaf
    by definition).
  - **molecule** — bounded runtime judgment over its **declared `calls:` set** (the set may
    now include molecules, not only atoms).
  - **compound** — unbounded runtime judgment within its declared `calls:` set.
- A `reuses:`/terminal-leaf call (a downstream skill that itself calls nothing, and may be
  satisfied by artifact reuse rather than re-dispatch) does **not** extend dispatch depth —
  it is a leaf on every runtime path. (Cf. `ideation-cycle` calling `kg-brief`.)

### D-refine-2 — Lint change

- **DROP** any "molecule with a molecule-call → error" (and the analogous
  compound-calls-compound error). Tier-adjacency is no longer a lint rule.
- **ADD** "static call-graph **depth > 2** → **warning**" — the depth bound is what the lint
  enforces now. Depth is measured over dispatch edges; terminal-leaf / reuse-by-artifact
  edges count as a leaf (depth contribution 0 beyond their own node).
- **KEEP** the existing checks unchanged: atom-with-downstream-calls → error;
  molecule/compound-without-verifier → error; compound-without-review-or-attendance → error;
  **per-skill > 10 children → warning**.

### D-refine-3 — Edits to the spec text

- §3 tier table "Intent" column for T1: change "runtime agent judgment bounded to picking
  among declared **atoms**" → "...among declared **calls (atoms or molecules)**."
- §4 T1 Composition: "`calls:` lists the **atoms** it invokes" → "`calls:` lists the
  **atoms or molecules** it invokes"; drop the atom-only judgment phrasing.
- §4 keep T0 as-is (atom = leaf, no downstream calls).
- §5 done-criteria lint fixtures: replace the implicit adjacency fixture with the
  depth-fixtures: "lint catches call-graph depth >2 (warning) and atom-with-calls (error);
  a molecule-calls-molecule edge at depth ≤2 PASSES."

## Rationale

Forcing a hoist purely to satisfy adjacency creates redundancy without buying reliability.
The cited research's reliability finding was about DEPTH, not adjacency. Bounding depth
directly (and letting a terminal-leaf/reuse edge be a leaf) captures the real constraint and
removes a class of spurious indirection — concretely, it lets `ideation-cycle` (molecule)
run `kg-brief` (molecule, terminal leaf) as its grounding step inside the 2-hop
`kg-ideate → spec-scaffold → ideation-cycle` path, with no hoist.

## What stays the same

- Tier still self-declared + lint-verified (D3 of the contract).
- Atom is still a leaf (no downstream calls).
- molecule/compound verifier + compound review-gate requirements unchanged.
- The >10-children warning is unchanged.
- This is additive: no existing artifact's tier changes; only the `calls:` adjacency
  restriction is lifted and the lint swaps adjacency-check → depth-check.

## Verification (once folded + the lint exists)

- A fixture molecule whose `calls:` includes a molecule, at call-graph depth ≤2, **passes**
  lint (previously would have been an adjacency violation).
- A fixture chain at static call-graph depth >2 raises a **warning**.
- An atom declaring any downstream call still raises an **error**.
- The >10-children warning still fires.
