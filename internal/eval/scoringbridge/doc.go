// Package scoringbridge translates a completed eval run into the R1 scoring
// pipeline: it emits an R1-shaped iteration record and scores it with the
// production rubric, persisting both under the run's eval-namespaced
// iteration-log directory.
//
// Two invariants from the R4 spec
// (.agents/workflow/specs/r4-code-task-generation-eval/design.md) anchor this
// package:
//
//   - D4.4 — eval outcomes feed R1 unchanged. The bridge scores with
//     scoring.DefaultRubric() through the same AssembleSignalSet →
//     Rubric.Score → WriteIterationScore path production scoring uses. There
//     is no eval-specific rubric, no parallel scoring path, and no new
//     signal; eval is just another input source, so eval scores stay
//     comparable to production scores under the same RubricVersion.
//
//   - D4.6 — eval-namespaced iter-log root. Eval runs write ONLY under
//     .agents/eval/runs/<run-id>/iteration-log/, never into the active
//     orchestration log (.agents/active/iteration-log/): eval iterations are
//     not real wave iterations, and mixing them would pollute orchestration
//     history. Eval and production share the rubric but not the iter-log
//     space. The bridge enforces the separation structurally — it derives
//     the iteration-log directory from the run directory it is handed and
//     never accepts an arbitrary log-dir path.
//
// Per spec OQ2 (recommendation applied) v1 scoring is 1-shot: one eval run
// produces exactly one iteration entry (iter-1.yaml) and one score sidecar
// (iter-1.score.yaml); aggregation across repeated runs of the same TaskSpec
// is a v2 concern that wraps this package rather than living inside it.
//
// The emitted record is schema_version 2 of the workflow iter-log schema
// (schemas/workflow-iter-log.schema.json), so the existing loaders —
// scoring.LoadIterationLog and the `da score iteration` CLI — read the eval
// space without modification (spec requirement R5).
package scoringbridge
