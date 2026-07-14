# worker-bundle-lessons

The dot-agents **code** work to auto-inject relevance-selected lessons into
delegation/worker bundles as `required_reads`. Extracted from the payout plan
`worker-bundle-authoring` (the `load-lessons-*` chain) and the dot-agents
proposal `.agents/proposals/load-lessons-into-worker-bundles-2026-06-25.md`.
Ideation ✅ (in payout); this is the canonical execution home for the code.

## Goal
When a task is delegated, the bundle should carry the lessons that actually
apply to it (by app_type + touched write_scope + tags), single-sourced as
`required_reads` — so workers stop re-learning captured gotchas.

## Predecessor (shipped)
The base `lessons` relevance facet already shipped (`688c9813`): `da config
relevance --filter lessons` reads repo-local `.agents/lessons/` frontmatter
(`filterLessons` + `lessonsFacet` in `commands/config/relevance_lessons.go`).

## Task spine
1. `load-lessons-1-schema-selection-metadata` — **focus**. `lesson.schema.json`
   gains selection metadata (`applies_to_app_types`, touched-path/subject globs,
   scope discriminator repo/user/team/org/public); extend the selector.
2. `load-lessons-2-relevance-lessons-facet` — `--lesson-tag` selection + schema-
   driven app_type/write_scope/tag matching + superseded auto-resolve (fail loud
   on missing replacement). **CROSS-PLAN DEP:** must read the layered resolver →
   `config-transitive-layering:layered-consumers-relevance-verify` must land first.
3. `load-lessons-3-bundle-required-reads` — fanout/bundle generation runs the
   lessons relevance query and attaches matching `LESSON.md` as `required_reads`
   (single-sourced, not inlined). Interim: filesystem match over `.agents/lessons/`;
   reuse the required-read precedent (`plan_task.go:40-65,460-485`). **This is the
   actual injection-into-bundles step.**
4. `load-lessons-4-kg-scoped-view` — end-state: replace the filesystem scan with
   the scoped-lessons KG view for scope-aware selection; loud fallback when KG
   absent. Deferred until KG scoped-lessons ships.

Design detail: `~/proj-docs/payout/.agents/workflow/plans/worker-bundle-authoring/`
(`load-lessons.design.md`) + the dot-agents proposal above.

## Relationship to sibling work
- Depends on `config-transitive-layering:layered-consumers-relevance-verify`
  (task 2 onward) — coordinate; both migrate relevance onto the layered resolver.
- The sibling **commit-scope** / KG-CRG-scope tasks in payout
  `worker-bundle-authoring` (`commit-1-task-pathset`, `commit-2-cli-scoped-mode`,
  `kgcrg-1..4`) are the *task-state-commit* thread — tracked separately (see the
  `read-task-state-from-master-source` proposal); intentionally NOT in this plan.
