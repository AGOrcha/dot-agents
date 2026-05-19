---
schema_version: 1
task_id: phase-6-release-and-docs
parent_plan_id: typescript-port
title: Phase 6 — Package, document, and validate the Windows-friendly release path
summary: 'Phase 6 release/docs: root README TypeScript subsection + Requirements note; ports/typescript README install/run (npm ci, build, npm link, Windows); package.json engines>=20, bin dot-agents-ts, start script; docs/TYPESCRIPT_PORT_BOUNDARY Phase 6 install path; CLI --help clarifies non-parity + README pointer; boundary help requires ports/typescript/README.md substring; fixed readdir Dirent[] / string[] typings so npm run build succeeds on @types/node 22.'
files_changed:
    - .agents/active/loop-state.md
    - .agents/workflow/plans/command-surface-decomposition/TASKS.yaml
    - .agents/workflow/plans/typescript-port/TASKS.yaml
verification_result:
    status: pass
    summary: 'Parent: advance typescript-port phase-6-release-and-docs + delegation closeout. Unstaged edits remain in .agents/ (orchestrator) outside this bundle write_scope.'
integration_notes: 'Parent: advance typescript-port phase-6-release-and-docs + delegation closeout. Unstaged edits remain in .agents/ (orchestrator) outside this bundle write_scope.'
created_at: "2026-04-18T21:19:56Z"
---

## Summary

Phase 6 release/docs: root README TypeScript subsection + Requirements note; ports/typescript README install/run (npm ci, build, npm link, Windows); package.json engines>=20, bin dot-agents-ts, start script; docs/TYPESCRIPT_PORT_BOUNDARY Phase 6 install path; CLI --help clarifies non-parity + README pointer; boundary help requires ports/typescript/README.md substring; fixed readdir Dirent[] / string[] typings so npm run build succeeds on @types/node 22.

## Integration Notes

Parent: advance typescript-port phase-6-release-and-docs + delegation closeout. Unstaged edits remain in .agents/ (orchestrator) outside this bundle write_scope.
