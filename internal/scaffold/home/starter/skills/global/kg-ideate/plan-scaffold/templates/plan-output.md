# Plan Output Template

Plan scaffolding template. Authored via `da workflow plan create <id>`; PLAN.yaml +
TASKS.yaml + `<id>.plan.md` are kept in sync via the CLI. The plan references the spec
but does not duplicate it; its success criteria trace back to the spec's done criteria.

---

## PLAN.yaml structure

```yaml
spec_ref: .agents/workflow/specs/<id>/design.md
concurrency_mode: sequential   # or: concurrent-folded (Phase 3 step 10 decision)
concurrency_rationale: |-
  <why this mode; which open questions, if any, block sequential→concurrent>
```

`concurrency_mode` and `concurrency_rationale` record the Phase 3 step 10 decision.
Use block scalar (`|-`) for `concurrency_rationale` because it may contain `: ` text.

---

## TASKS.yaml structure

Each task entry:

```yaml
- id: <task-id>
  title: <short task title>
  status: pending
  write_scope:
    - <path derived from Phase 1 impact radius or spec decisions>
  depends_on:
    - <local-task-id>                     # same-plan dep
    - <plan-id>/<task-id>                 # cross-plan dep (contains "/")
  blocks: []
  verification_required: true
  notes: |-
    <block scalar — safe for ": " free text; records task-scoping rationale and
    how this task is verified (traces to spec done criterion)>
```

Rules:
- `write_scope` entries are file or directory paths, not intent descriptions.
- `depends_on` entries use `<plan-id>/<task-id>` form for cross-plan deps; a dep is
  cross-plan iff it contains `/`.
- `notes` (and other free-text fields) MUST use `|-` block scalar when the value may contain `: `.
- `verification_required` is a boolean (`true`/`false`) — NOT a block scalar.
- `status` starts at `pending`; advance via `da workflow advance`, never by hand-editing.

---

## Example TASKS.yaml

```yaml
- id: p0-skill-md
  title: Author plan-scaffold SKILL.md
  status: pending
  write_scope:
    - internal/scaffold/home/starter/skills/global/kg-ideate/plan-scaffold/SKILL.md
  depends_on: []
  blocks: []
  verification_required: true
  notes: |-
    Authors the plan-scaffold molecule SKILL.md (Phase 3 of kg-ideate).
    Sequential mode — spec was fully stabilized before this task was created.
    Verified: file exists at write_scope path; go test ./internal/scaffold/... passes.

- id: p1-instructions
  title: Author plan-scaffold instruction files
  status: pending
  write_scope:
    - internal/scaffold/home/starter/skills/global/kg-ideate/plan-scaffold/instructions/
  depends_on:
    - p0-skill-md
  blocks: []
  verification_required: true
  notes: |-
    Authors instruction files for the plan-scaffold molecule.
    Verified: instruction files exist at write_scope path; content matches proposal body.

- id: p2-templates
  title: Author plan-scaffold templates
  status: pending
  write_scope:
    - internal/scaffold/home/starter/skills/global/kg-ideate/plan-scaffold/templates/
  depends_on:
    - p0-skill-md
  blocks: []
  verification_required: true
  notes: |-
    Authors templates/plan-output.md for the plan-scaffold molecule.
    Cross-plan dep example: kg-ideate-skill/p0-compound-skill
    Verified: plan-output.md exists; YAML colon-space rule demonstrated in template body.
```
