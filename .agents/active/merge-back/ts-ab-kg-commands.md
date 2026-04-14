---
schema_version: 1
task_id: ts-ab-kg-commands
parent_plan_id: typescript-port
title: AB-test B — Implement read-only kg commands in TS port (health, query stub)
summary: Implemented runKgHealth (KG_HOME + notes/ dir) and runKgQuery stub per phase-4 boundary; kg.test.ts 8 cases; vitest 74/74; go test ./... ok.
files_changed: []
verification_result:
    status: pass
    summary: cd ports/typescript && npm test; go test ./...
integration_notes: cd ports/typescript && npm test; go test ./...
created_at: "2026-04-14T19:25:23Z"
---

## Summary

Implemented runKgHealth (KG_HOME + notes/ dir) and runKgQuery stub per phase-4 boundary; kg.test.ts 8 cases; vitest 74/74; go test ./... ok.

## Integration Notes

cd ports/typescript && npm test; go test ./...
