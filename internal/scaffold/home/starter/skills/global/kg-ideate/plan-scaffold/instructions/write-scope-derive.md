# Write-Scope Derivation — Phase 3 Step 12

Rules for deriving each task's `write_scope`:

- **Prefer the Phase 1 impact radius.** When Phase 1 ran `get_impact_radius_tool` (because
  the topic named a file, function, or module), use those results to ground write-scopes
  rather than guessing.
- A task with an unknowable write-scope is a signal the spec still has an open question.
  Flag it and resolve the spec gap before locking the task.
- Write-scopes are paths (files or directories), not intent. Example:
  `write_scope: [internal/scaffold/home/starter/skills/global/kg-ideate/plan-scaffold/]`
- Molecule sibling names for cross-referencing: `kg-brief`, `spec-scaffold`, `plan-scaffold`,
  `staged-execution-handoff`.
