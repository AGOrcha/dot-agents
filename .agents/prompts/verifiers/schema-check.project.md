# Schema-check verifier — dot-agents repo overlay

Repo-local committed layer. Composes **after** `verifiers/verifier.base.md` (the contract) and
`verifiers/schema-check.md` (the kind: structural validity gates the rest). This file adds **only**
the dot-agents validation matrix.

## Commands

1. **Config layers (when `.agentsrc.json` or a config layer changed):** `da config verify`
   — validates `.agentsrc.json` against `schemas/agentsrc.schema.json` and the layer coherence rules.
   A failure here is a terminal `--status fail`.
2. **Plan/task artifacts:** validate each touched `PLAN.yaml` against `schemas/workflow-plan.schema.json`
   and each `TASKS.yaml` against `schemas/workflow-tasks.schema.json`. Explicitly catch the YAML
   colon-space class (a `notes:`/`summary:` free-text field containing `: ` silently parsed as a
   mapping). Cite the failing path + field.
3. **Introduced schemas:** a touched `schemas/*.schema.json` is itself valid JSON Schema and keeps
   `additionalProperties: false` on nested objects where the repo convention requires it.

`--kind test`, `--verifier-type schema-check`. If a required validation fails, you may skip the rest
but record `--status fail` naming the first failing artifact + rule.
