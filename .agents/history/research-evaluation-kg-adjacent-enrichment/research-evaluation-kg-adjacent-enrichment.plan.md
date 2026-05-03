# Research Evaluation KG Adjacent Enrichment Plan

## Goal

Enrich `research/evaluations/` and `research/articles-evaluation-kg-and-adjacent.md` after inventorying:

- `research/articles/`
- `research/evaluations/`
- `.agents/workflow/specs/`
- `.agents/workflow/plans/`

## Tasks

- [x] Inventory source articles, evaluation docs, workflow specs, and workflow plans.
- [x] Identify domain gaps, logical flaws, and platform-specific assumptions in the current synthesis.
- [x] Patch evaluation docs with cross-cutting findings that connect article claims to current workflow specs/plans.
- [x] Patch `research/articles-evaluation-kg-and-adjacent.md` with an updated inventory, gap analysis, and enriched recommendations.
- [x] Review diffs for accidental rewrites or conflicts with existing user changes.
- [x] Second-pass enrichment after re-reading specs end-to-end: append Part F sections to all six evaluation docs covering contract-level couplings (verifier evolution governance, cell vs compound audit shapes, reweave ↔ KG-propagation unification, same-scope vs cross-scope contradiction split, one-schema-three-projections, additive `open_questions`, public-scope ingest, hook capability matrix, recursive accountability via the `research` profile).

## Notes

- Preserve existing uncommitted user edits in the target research article map.
- Treat new untracked article/spec files as source material, not scratch output.
- Second-pass enrichment (2026-04-27) lives in Part F of each evaluation doc and a "Second-pass findings" section in `research/evaluations/workflow-spec-plan-inventory.md`. No code or spec changes; research-only.
