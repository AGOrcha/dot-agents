# Schema Follow-Ups

This repo now has a repo-local JSON Schema for `.agentsrc.json` at `schemas/agentsrc.schema.json`.

Deferred until `.agents/active/resource-intent-centralization.plan.md` is complete:

- canonical `HOOK.yaml` schema file
- schema-backed validation for other introduced canonical files and bundles
- deciding which schema families remain repo-local versus moving into exported/public schema paths

The current rule is to keep the repo-local `.agentsrc.json` schema aligned with the Go `AgentsRC` model and reject unknown top-level fields.
