---
schema_version: 1
task_id: pr5-followup-verifier-xref
parent_plan_id: pr10-branch-split
title: 'PR5-followup-B: enforce isp.prompt.md ↔ verifier_surface cross-reference'
summary: 'PR #140 merged. Added TestStarterVerifierSurfaceCrossReference in internal/scaffold/home/copy_test.go — walks starter/skills/global/ prompt files, extracts verifier_profile IDs referenced, asserts each resolves to an entry in the starterVerifierSurface set. Mirrors PR #122 reviewer-lens enforcement pattern. Follow-up commit (80f3bebd) extracted helpers to clear S3776 cog complexity. 11/11 checks pass + SonarCloud gate OK.'
files_changed:
    - internal/scaffold/home/copy_test.go
verification_result:
    status: pass
    summary: PR #140 merged via squash; all CI green; 0 OPEN Sonar issues
integration_notes: Closes the loop opened by PR #122 — both stranded agent refs AND stranded verifier_profile refs are now enforcement-tested at scaffold-build time.
created_at: "2026-05-28T00:00:00Z"
---

## Summary

PR #140 merged. Enforcement test prevents scaffold drift from stranding verifier_sequence references in isp.prompt.md (and sibling skill prompts under starter/skills/global/).

## Integration Notes

Companion to PR #122 (reviewer-lens cross-reference). Together they form the scaffold-drift defense: agents AND verifier_profiles referenced by prompts must resolve to actual registry entries.
