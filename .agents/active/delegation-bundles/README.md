# Delegation bundles (Phase 8)

Repo-local YAML files model the **per-delegation** worker handoff (profile reference, prompts, context files, verification metadata, closeout expectations). Schema: `schemas/workflow-delegation-bundle.schema.json` at the repository root.

## Naming

- One file per delegation: `.agents/active/delegation-bundles/<delegation_id>.yaml`.
- **`delegation_id` must match the contract’s `id` field** inside `.agents/active/delegation/<parent_task_id>.yaml` (the contract **filename** is the canonical `parent_task_id`, not the delegation id).
- After `workflow fanout`, copy the new contract’s `id` into both the bundle filename stem and the `delegation_id` YAML field so the bundle stays paired with that contract.

The Go CLI does **not** yet write these files from `workflow fanout` (no `--prompt-file` or related flags). Create or update bundles manually until that wiring exists.

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
