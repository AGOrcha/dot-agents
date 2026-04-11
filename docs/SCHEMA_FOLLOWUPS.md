# Schema Follow-Ups

This repo now has repo-local JSON Schemas under `schemas/`:

| Schema | Path | Target artifact |
|--------|------|-----------------|
| `.agentsrc.json` | `schemas/agentsrc.schema.json` | Project manifest (aligned with Go `AgentsRC`; rejects unknown top-level fields) |
| `HOOK.yaml` | `schemas/hook.schema.json` | Canonical hook bundle manifest under `~/.agents/hooks/<scope>/<name>/` |
| Workflow plan | `schemas/workflow-plan.schema.json` | `.agents/workflow/plans/<id>/PLAN.yaml` |
| Workflow tasks | `schemas/workflow-tasks.schema.json` | `.agents/workflow/plans/<id>/TASKS.yaml` |

Editor validation: point YAML language servers at these `$id` paths (see `# yaml-language-server: $schema=...` comments in generated bundles).

**Still deferred (not blocking schema files):**

- Wire schema-backed **runtime** validation in the Go CLI for `HOOK.yaml`, `PLAN.yaml`, and `TASKS.yaml` (optional; unmarshaling already enforces structure for workflow files).
- Deciding which schema families remain repo-local versus moving into exported/public schema paths for downstream consumers.

The former deferral tied to `resource-intent-centralization` for *authoring* `HOOK.yaml` schema is satisfied: the schema file exists; command-layer validation remains a separate follow-up.
