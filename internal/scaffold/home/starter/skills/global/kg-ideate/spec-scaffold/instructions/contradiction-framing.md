# Contradiction Framing — Phase 2 Step 7

**Status: adapter-conditional** — execute Phase 2 step 7 only when the active profile's
adapter ships a `contradicting_claims` named query (preflight in kg-brief's kg-queries.md).
The built-in `dotagents-builtin:graph/citation@^1.0` adapter ships this query today
(contract §13.4); contradiction handling is available for citation-backed projects now.

This step degrades gracefully: when the adapter is absent, the skill skips structural
contradiction traversal and uses the competing-decisions fallback. No other phase depends
on it, so the spec/plan/execution pipeline ships and is useful regardless.

Contradictions are represented as `claim -[:contradicts]-> claim` edges, not as a
separate NoteType. The preflight check is adapter capability, not schema NoteType.

## Rules when adapter-enabled

- For each load-bearing decision in the briefing, run:
  `da graph query contradicting_claims --param claim_id=<id>`
  to retrieve claims that structurally contradict it.
- Each returned contradiction must become an explicit decision point in the spec.
  Do not silently pick one side.
- Frame as: "The citation graph surfaces a contradiction on X: [claim A] vs [claim B].
  The spec must resolve this. Which approach does this spec take and why?"
- Record the user's answer as a decision entry with rationale.
- If the user defers: add it as an open question, not a decision.

## Fallback when adapter lacks `contradicting_claims`

Present competing decisions surfaced during Phase 1 (two decision nodes with conflicting
rationale) in Phase 2 step 5 as "competing decisions — choose one" rather than as
structured contradictions.
