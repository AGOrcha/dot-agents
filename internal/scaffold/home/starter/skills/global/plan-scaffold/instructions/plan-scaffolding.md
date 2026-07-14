# Plan Scaffolding Instructions

Rules for Phase 3 plan scaffolding (plan-scaffold molecule).

## Step 10 — Concurrency decision (mandatory, recorded)

Apply the `workflow-artifact-model` heuristic "Spec before plan… unless the work is
purely mechanical." **Sequential is the default.** Choose concurrent/folded only when
there are no open questions whose answers change task ordering or write-scopes. State
the mode and rationale in the plan notes.

Signs that sequential is required:
- The spec has open questions that could split a task into two or collapse two into one.
- Any write-scope depends on a spec decision not yet locked.
- Phase 2 step 6 (gap→open question) produced items that remain unresolved.

Signs that concurrent/folded may apply:
- All spec decisions are ratified; no open questions remain.
- The work is purely mechanical (a refactor with a clear change surface, a doc update,
  an additive feature with no behavioral forks).
- An open question from Phase 2 was already answered mechanically by the briefing
  (the gap→open-question signal from Phase 2 `spec-scaffold`).

The concurrency decision is itself recorded in the plan notes (block scalar form — see
YAML colon-space rule below).

## Step 11 — Task breakdown + dependency ordering

Load → `instructions/task-breakdown.md`

## Step 12 — Write-scopes per task

Load → `instructions/write-scope-derive.md`

## Step 13 — Verification strategy per task

Each task must name how it will be verified:
- Verification must trace back to one or more of the spec's done criteria (Phase 2 step 8).
- Do not invent verification criteria that contradict or ignore the spec's done criteria.
- Acceptable verification forms: `go test ./...`, `da workflow verify record`, CI pass,
  file exists check, smoke test against a known input/output pair.
- Record the verification intent in the task `notes` field (block scalar).
  Set `verification_required: true` (boolean) — there is no free-text `verification` field.

## Step 14 — Write the plan

Author the plan via the canonical CLI surface:

```
da workflow plan create <id>
```

Then keep PLAN.yaml / TASKS.yaml / <id>.plan.md in sync via:
- `da workflow advance <plan-id> --task <task-id> --status <status>` — never hand-edit
  task status.
- `da workflow merge-back` — for delegated task closeout.

Load → `templates/plan-output.md` for the PLAN.yaml + TASKS.yaml structure.

## YAML colon-space rule

Free-text YAML fields (`notes`, `summary`, `rationale`, `description`) that may contain
`: ` MUST use block scalar syntax (`|-`), per the YAML plain-scalar colon-space rule.

```yaml
# WRONG — parse error or mapping misinterpretation
notes: Implements two-lens: phase 1 review gate

# CORRECT — block scalar preserves literal text
notes: |-
  Implements two-lens (phase 1) review gate
```

Common fields that need this treatment: `notes`, `summary`, `feedback_goal`,
`observation`, `rationale`, `description`.

## Canonical paths

- Spec root: `.agents/workflow/specs/` — never the bare `workflow/specs/` path.
- Plan root: `.agents/workflow/plans/`
- Cross-plan dep format: `<plan-id>/<task-id>` (a dep is cross-plan iff it contains `/`).
