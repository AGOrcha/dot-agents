# Implementation Results

## 1. Planner Mini-Doc

- Added a planner-facing mini-doc at `.agents/active/planner-resource-write-safety.md`.
- Captured the current write-safety model for MCP, hooks, and shared repo-local projection targets.
- Documented the deliberate exception for review-stage skill outputs that should not yet be auto-imported back into managed resources.
- Explicitly noted that future promotion of those skills should go through the `skill-architect` path before they are treated as canonical managed skills.
