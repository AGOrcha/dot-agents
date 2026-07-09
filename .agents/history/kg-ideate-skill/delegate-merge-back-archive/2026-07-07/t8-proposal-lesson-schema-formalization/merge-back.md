---
schema_version: 1
task_id: t8-proposal-lesson-schema-formalization
parent_plan_id: kg-ideate-skill
title: Formalize canonical proposal.schema.json + lesson.schema.json (single-source)
summary: |-
  Attempted schema drafting produced initial proposal.schema.json and
  lesson.schema.json files, but the delegated write scope is insufficient to
  complete the task contract. The schemas are not wired into
  schemas/schemas.go, the active proposal/lesson loaders still use older
  hardcoded shapes, and there is no evidence-backed validation against
  existing proposal and lesson artifacts.
files_changed:
  - schemas/proposal.schema.json
  - schemas/lesson.schema.json
verification_result:
  status: partial
  summary: |-
    go test ./schemas/... passes, but consumer wiring and real artifact
    validation required by the task remain incomplete.
integration_notes: |-
  Parent should reject and rescope this task. A valid completion needs either
  expanded write_scope or a narrower task contract: wire proposal/lesson
  schema consumers to a single canonical source of truth and validate against
  existing proposal and lesson artifacts before delegating again.
created_at: "2026-07-07T13:05:00Z"
---

## Summary

Attempted schema drafting produced initial proposal.schema.json and lesson.schema.json files, but the delegated write scope is insufficient to complete the task contract. The schemas are not wired into schemas/schemas.go, the active proposal/lesson loaders still use older hardcoded shapes, and there is no evidence-backed validation against existing proposal and lesson artifacts.

## Integration Notes

Parent should reject and rescope this task. A valid completion needs either expanded write_scope or a narrower task contract: wire proposal/lesson schema consumers to a single canonical source of truth and validate against existing proposal and lesson artifacts before delegating again.
