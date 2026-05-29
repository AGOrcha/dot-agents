---
schema_version: 1
task_id: pr8a-cmd-da-rename
parent_plan_id: pr10-branch-split
title: 'PR 8a: rename cmd/dot-agents → cmd/da (Go convention)'
summary: PR #139 merged via squash. Follow-ups tracked - pr8a-stale-refs-sweep (lessons/proposals/plans/specs/docs/ralph harnesses/scaffold templates), signing-native-mac-windows (Apple Dev ID + Authenticode extension on top of #138 cosign block).
files_changed: [cmd/da/, .goreleaser.yaml, Makefile, .github/workflows/test.yml, .github/workflows/auto-release.yml, scripts/verify.sh, README.md, commands/root.go]
verification_result:
    status: pass
    summary: 11/11 checks pass; SonarCloud gate OK; 0 issues
integration_notes: Unblocks signing-native-mac-windows (no longer conflicts with #138 cosign block on .goreleaser.yaml).
created_at: "2026-05-28T00:00:00Z"
---

## Summary

PR #139 merged. cmd/dot-agents → cmd/da. Follow-up tasks filed for the stale-refs sweep and the native OS signing extension.
