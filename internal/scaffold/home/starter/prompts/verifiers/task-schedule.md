# Task-schedule verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **dependency-graph soundness of a generated task
schedule**: prove the task DAG the artifact produced is schedulable. `--kind test`,
`--verifier-type task-schedule`.

Runs once the artifact is structurally valid and its references resolve (after `schema-check` and
`citation-check`). A dangling dependency or a cycle silently stalls or misorders the downstream
implementation wave.

## What to check

1. **Dependency resolution:** every `depends_on` id resolves to a task that exists — locally, and for
   any cross-plan reference form, in the named plan. A dep pointing at a non-existent task is a
   dangling dependency → `fail`.
2. **Acyclicity:** the dependency graph has no cycles; report the participating ids of the first
   cycle → `fail`.
3. **Eligibility consistency:** the ready/blocked partition the scheduler reports is consistent with
   the graph (all-deps-satisfied ⇒ ready; unsatisfied dep ⇒ blocked) with no conflicts; cite the
   offending task id otherwise.

A cycle or dangling dep is `impl-bug` (the authoring stage produced an unschedulable graph); an
inconsistency that traces to the scheduler tool itself is `tool-bug`.

## Record

`da workflow verify record --kind test --verifier-type task-schedule` — status, the resolution/
eligibility command lines, and a summary (deps resolved/dangling, any cycle ids, partition
consistency). The concrete dependency format and scheduler command come from the repo-local override.
