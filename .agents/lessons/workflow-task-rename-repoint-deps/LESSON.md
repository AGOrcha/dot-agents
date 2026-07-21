# Renaming/superseding a workflow task must repoint its dependents

Use `da workflow task rename` or `da workflow task supersede` whenever a canonical
task ID changes. These commands atomically update the task set and repoint every
other task's `depends_on` and `blocks` references. A hand rename can leave the old
ID as a separate `pending` task and silently block dependents forever.

## What happened

The obs plan's `o1` was renamed `o1-obs-deploy-spec` → `o1-obs-deploy-spec-and-contracts`.
The rename left the OLD `o1-obs-deploy-spec` entry in TASKS.yaml (still `pending`)
AND `o4`'s `depends_on` still listed `o1-obs-deploy-spec`. Result: `o1-…-and-contracts`
completed, but `o4` stayed blocked on the stale-pending duplicate — it never became
eligible even though its real prerequisite was done. `da workflow eligible` obeyed the
canonical graph correctly; the graph itself was wrong.

## Rule

Preview the affected dependents, then perform the structural mutation:

```sh
da workflow task rename <plan> --from <old-id> --to <new-id> --dry-run
da workflow task rename <plan> --from <old-id> --to <new-id>

# When the replacement task already exists:
da workflow task supersede <plan> --old <old-id> --new <new-id> --dry-run
da workflow task supersede <plan> --old <old-id> --new <new-id>
```

`rename` changes the task ID and task-keyed `(fb:*)` note tags. `supersede` removes
the old entry. Both repoint all `depends_on` and `blocks` references through the
canonical write path, including the `refs/agents/state` mirror when the git-ref
backend is active. Re-run `da workflow eligible --plan <plan>` and confirm the
expected task graph.

If the installed `da` predates these commands, use the manual fallback carefully:
edit TASKS.yaml once, remove or rename the old entry, grep the full file for
`<old-id>`, repoint every `depends_on` and `blocks` occurrence, update task-keyed
`(fb:*)` tags, and re-run `eligible`. Do not leave a pending duplicate. This is a
sibling of [[consolidate-vestigial-siblings-on-rename]] (rename = consolidate the
duplication) and [[validate-bundle-against-head]] (the task graph decays between
snapshots).
