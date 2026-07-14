# Gotchas: kg-brief

Common failure points:

## `.agents/lessons/index.md` Has No Preflight

- `context-scan.md` explicitly preflights `.agents/workflow/specs/` (note absence,
  don't error), but it does NOT preflight `.agents/lessons/index.md` — the instructions
  just say "Read .agents/lessons/index.md in full." On a fresh repo with no lessons
  captured yet, this file may not exist and there is no documented fallback; don't let
  that stall the whole briefing, and don't fabricate lesson content to fill the gap.

## A Flagged §A Summary Is Not Citable As-Is

- `corpus-scan.md`'s escalation rule: a §A article evaluation entry carrying a
  citation-presence, freshness, or rubric-check flag (Part G) must NOT be promoted as
  spec-decision rationale until you've read the underlying `research/articles/<file>.md`.
  Quoting the flagged summary directly and treating it as authoritative skips the one
  check the flag exists to force.

## `get_impact_radius_tool` Is Skipped Unless the Topic Names Code

- It only runs when the topic explicitly names a file, function, or module. For a vague
  or purely conceptual topic, Phase 3's write-scope derivation has no impact radius to
  ground on — `write-scope-derive.md` falls back to guessing at that point. Flag this
  explicitly in the briefing's Impact Radius section (`[none]`) rather than leaving it
  ambiguous.

## The Adapter Preflight Gates Phase 2, Not Phase 1

- `kg-queries.md`'s `contradicting_claims` preflight (`da graph query --list-queries`)
  only decides whether Phase 2 step 7 runs structurally or falls back — it has no effect
  on Phase 1 itself. Forgetting to run it here silently breaks Phase 2's ability to
  choose correctly later; it cannot be deferred to Phase 2 because Phase 2's own
  instructions point back here for the check.
