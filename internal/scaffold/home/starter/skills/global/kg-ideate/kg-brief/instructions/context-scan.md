# Context Scan

Rules for Phase 1 lessons and artifact-overlap checks:

- Read `.agents/lessons/index.md` in full.
- Pull any lesson whose title or tags could apply to the topic; err on the side of inclusion.
- **Preflight**: check whether `.agents/workflow/specs/` exists before scanning. If it does
  not, note `no prior specs directory found` in the briefing. Do not error and do not create
  the directory.
- List `.agents/workflow/specs/`; for each spec directory, read the first 20 lines of
  `design.md` to check for scope overlap. Flag overlap when present, but do not read full spec
  files in this phase.
- Also list `.agents/workflow/plans/` and flag prior plans with shared scope so downstream
  planning can reuse task patterns or cross-plan dependencies instead of duplicating them.
- Canonical spec root is always `.agents/workflow/specs/`; never use bare `workflow/specs/`.
