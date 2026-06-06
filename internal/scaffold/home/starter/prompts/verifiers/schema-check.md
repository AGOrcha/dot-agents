# Schema-check verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **structural validity of generated/edited
artifacts**: prove every structured artifact the task produced or changed validates against its
schema. `--kind test`, `--verifier-type schema-check`.

When a task produces design/config artifacts (plans, task graphs, generated schemas, config layers)
this is the **first** gate in an artifact-integrity sequence: a structurally invalid artifact can't be
meaningfully citation- or dependency-analyzed, so structural validity gates the rest.

## What to check

- Each touched structured file validates against its schema (the artifact parses **and** conforms).
- Generated/edited schema files are themselves valid schema documents and keep the project's
  required strictness (e.g. `additionalProperties: false` where convention demands it).
- Catch the silent-mapping class of breakage (a free-text field whose content is parsed as
  structure). Cite the failing path + field + rule.

A structurally invalid artifact is a terminal `status: fail`. An artifact that parses but is missing
a schema-required field is `impl-bug` (the authoring stage produced an incomplete artifact), not `ok`.

## Record

`da workflow verify record --kind test --verifier-type schema-check` — status, the validation command
lines, and a summary naming the first failure (path + field + rule). The concrete validator commands
and the artifact→schema map come from the repo-local override.
