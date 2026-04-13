# Delegation bundles (Phase 8)

Repo-local YAML files model the **per-delegation** worker handoff (profile reference, prompts, context files, verification metadata, closeout expectations). Schema: `schemas/workflow-delegation-bundle.schema.json` at the repository root.

The **`loop-worker`** label in `worker.profile` refers to the global habits documented at **`~/.agents/profiles/loop-worker.md`** (not loaded automatically by the CLI — agents read it). Repo-specific guidance goes in **`--project-overlay`** files (e.g. `.agents/active/*.loop.md`).

## Naming

- One file per delegation: `.agents/active/delegation-bundles/<delegation_id>.yaml`.
- **`delegation_id` must match the contract’s `id` field** inside `.agents/active/delegation/<parent_task_id>.yaml` (the contract **filename** is the canonical `parent_task_id`, not the delegation id).
- After `workflow fanout`, copy the new contract’s `id` into both the bundle filename stem and the `delegation_id` YAML field so the bundle stays paired with that contract.

`workflow fanout` **creates** the bundle automatically (Phase 8). You can still edit the YAML by hand afterward for edge cases the flags do not cover.

## Minimal example (valid against the schema)

```yaml
schema_version: 1
delegation_id: del-example-task-1710000000
plan_id: example-plan
task_id: example-task
owner: worker
worker:
  profile: loop-worker
scope:
  write_scope:
    - commands/
prompt: {}
context: {}
verification:
  feedback_goal: Smoke verification passes
closeout: {}
```

`prompt` and `context` may be empty objects; `closeout` may be empty. Optional sections such as `selection`, `slice_id`, and nested `evidence_policy` are omitted here but allowed by the schema when you need them.
