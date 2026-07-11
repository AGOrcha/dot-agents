# Plan: da recipe scripts

**Spec:** `.agents/workflow/specs/da-recipe-scripts/design.md`
**Status:** completed · **Created:** 2026-07-03 · **Shipped:** 2026-07-10

## Goal

Give `da` a first-class, deterministic, cross-platform **recipe** format so the meta-loop's
mechanical `da`-command sequences (iteration-close spine, gate-flush) become versionable
artifacts instead of brittle bash or re-improvised agent steps. `da run <file>` executes a
line-oriented recipe; `#!/usr/bin/env -S da run` makes a `.da` file directly executable.

## Approach (traces to spec decisions)

1. **p1** — `da run <file>`: read → strip shebang/comment/blank → tokenize (quoted-field, no
   shell) → dispatch each line through the `da` root command in-process (D2/D5, cross-platform).
2. **p2** — fail-fast: first failing step aborts with `step N ("<line>") failed`, non-zero exit
   (D4/R3).
3. **p3** — env-var substitution `$VAR`/`${VAR}` per line (D6/R6); resolves OQ1 (env-only v1).
4. **p4** — shebang + cross-platform acceptance test mirroring yq #1851; runs every target OS
   (R4/R5).
5. **p5** — dogfood a real mechanical meta-loop sequence as a shipped `.da` recipe; resolves OQ2
   (recipe home + convention) and validates the D3 skills/recipe boundary in practice.

## Shipped recipes (`src/share/recipes/*.da`, per OQ2)

- `checkpoint-advance.da` — p5's dogfood: the iteration-close checkpoint→advance spine.
- `scaffold-plan.da` — a second dogfood added post-p5 covering the motivating plan/task-creation
  use case: the `plan create` → `task add` → `plan update --status active --focus` bootstrap
  chain from `docs/concepts/workflow-artifact-model.md`'s "Author plan" flow. Validated with
  `da run` against a scratch repo (see merge-back evidence).

## Boundary (D3, load-bearing)

Recipes are **mechanical only**. Anything needing judgment/branching stays a **skill** (a skill
may call a recipe for its spine). Distinct from the JS **Workflow** engine (agent fan-out).

## Open questions carried from the spec

- OQ1 (variable scope) — resolved in p3 notes (env-only v1).
- OQ2 (recipe home/convention) — resolved in p5 (proposed `src/share/recipes/*.da`).
- OQ3 (vs Workflow engine) — documented in p1 notes.

## Success

Per PLAN.yaml `success_criteria` — ordered execution, fail-fast, comment/shebang skipping, env
substitution, a directly-executable `.da` on every OS, and one dogfooded recipe.
