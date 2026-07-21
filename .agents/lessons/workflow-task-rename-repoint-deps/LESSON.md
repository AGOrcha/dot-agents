# Renaming/superseding a workflow task must repoint its dependents

`da workflow task` has no `rename`/`supersede` operation — only `add` and `update`
(notes/write-scope/title). So a task rename is done by hand: add the new task, mark
the old one done/removed. The hazard: **dependents still point `depends_on` at the
OLD task id**, and if the old id lingers as a separate `pending` task, the dependent
is silently blocked forever.

## What happened

The obs plan's `o1` was renamed `o1-obs-deploy-spec` → `o1-obs-deploy-spec-and-contracts`.
The rename left the OLD `o1-obs-deploy-spec` entry in TASKS.yaml (still `pending`)
AND `o4`'s `depends_on` still listed `o1-obs-deploy-spec`. Result: `o1-…-and-contracts`
completed, but `o4` stayed blocked on the stale-pending duplicate — it never became
eligible even though its real prerequisite was done. `da workflow eligible` obeyed the
canonical graph correctly; the graph itself was wrong.

## Rule

When you rename or supersede a task:
1. **Remove** the old entry (don't leave a duplicate pending task shadowing the new one).
2. **Repoint every dependent's `depends_on`** from the old id to the new id —
   `grep <old-id>` across the plan's TASKS.yaml; a lone `- <old-id>` under some other
   task's `depends_on` is the tell.
3. Re-run `da workflow eligible --plan <plan>` and confirm the expected tasks unblocked
   (a dependent that DIDN'T appear is the symptom).

`da workflow task update` has no `--depends-on` flag and there's no `task rm`, so the
dedup + repoint is a careful hand-edit of TASKS.yaml (structural, not a status edit —
status still goes through `advance`). Sibling of
[[consolidate-vestigial-siblings-on-rename]] (rename = consolidate the duplication) and
[[validate-bundle-against-head]] (the task graph decays between snapshots).

## Durable fix (follow-up)

A `da workflow task rename <plan> --from <old> --to <new>` that atomically renames the
entry and rewrites every dependent's `depends_on` (+ a `--supersede` that removes the
old and repoints) would make this safe. Tracked as a proposal
(`~/.agents/proposals/workflow-task-rename-repoints-deps.md`).
