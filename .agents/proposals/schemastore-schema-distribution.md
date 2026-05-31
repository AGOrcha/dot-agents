# Proposal: distribute dot-agents JSON schemas via SchemaStore

**Status:** idea / backlog
**Logged:** 2026-05-30
**Scope:** project-local (this repo's `schemas/*.schema.json`)

## Idea
Publish our JSON schemas (`schemas/agentsrc.schema.json`, and the TASKS/PLAN/
delegation/hook-outcome schemas under `commands/workflow/static/` etc.) to
[SchemaStore](https://github.com/SchemaStore/schemastore) so editors (VS Code,
JetBrains, etc.) auto-associate and validate `.agentsrc.json`, `TASKS.yaml`,
`PLAN.yaml`, delegation bundles, and `.agentsrc.lock` out of the box — no
per-user config. Easy, well-adopted distribution channel.

## How (per CONTRIBUTING)
https://github.com/SchemaStore/schemastore/blob/master/CONTRIBUTING.md#how-to-add-a-json-schema-thats-hosted-in-this-repository
- Schemas stay **hosted in this repo** (raw GitHub URL or a stable
  `$id`); SchemaStore's catalog just points to them + declares fileMatch globs.
- Add an entry to `schemastore`'s `api/json/catalog.json` with `name`,
  `description`, `fileMatch` (e.g. `.agentsrc.json`, `**/TASKS.yaml`,
  `**/PLAN.yaml`, `.agentsrc.lock`), and `url` → our raw schema.
- Each schema needs a stable `$id` and `$schema` (draft) — audit ours for that.
- PR to SchemaStore; they run their test suite (schema must be valid + have
  positive/negative test fixtures under `test/`).

## Open questions / prerequisites
- Pin schema URLs to a tag/release (not `master`) so the catalog points at a
  stable, versioned schema — ties into the `lock_version`/RubricVersion
  versioning discipline.
- fileMatch for `TASKS.yaml`/`PLAN.yaml` is generic — risk of matching other
  projects' files. Consider a more specific glob (`**/.agents/**/TASKS.yaml`).
- Decide which schemas are public-contract worthy vs internal-only.

## Next step
When picked up: audit `schemas/*.schema.json` for stable `$id` + draft decl,
add test fixtures, then open the SchemaStore catalog PR. Could graduate to a
`workflow/specs/` entry if it grows.
