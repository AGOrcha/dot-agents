# context-scan — Phase 1 Lessons and Spec Overlap Rules

Rules for Phase 1 lessons + spec overlap check:

- Read .agents/lessons/index.md in full; pull any lesson file whose title or tags
  could apply to the topic (err on the side of inclusion).
- **Preflight**: check that .agents/workflow/specs/ exists before scanning.
  If absent, note "no prior specs directory found" in the briefing — do not error or create it.
- Run: ls .agents/workflow/specs/ — for each spec directory, read the first 20 lines of
  design.md to check for scope overlap. Flag if found; do not read full spec files.
- Also list .agents/workflow/plans/ — flag prior plans that share scope, so Phase 3 can
  reuse task patterns and cross-plan deps rather than duplicating them.
- Canonical spec root is always .agents/workflow/specs/ — never workflow/specs/ (bare).
