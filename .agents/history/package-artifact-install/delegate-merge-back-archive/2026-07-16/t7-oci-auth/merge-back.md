---
schema_version: 1
task_id: t7-oci-auth
parent_plan_id: package-artifact-install
title: 'OCI registry auth (bearer + credential-helper providers)'
summary: 'Bearer + credential-helper OCI auth providers with the standard registry token flow, wired as the ociAuthHeaderForRef seam for t8 live pull. H12 secret non-disclosure — auth config carries secret references only; a resolved credential lives only in an Authorization header. Three cross-harness (Codex gpt-5.6-sol) review rounds closed real credential-leak defects: token-endpoint realm https-enforced + all redirects refused (no cleartext/wrong-endpoint credential exfiltration); redactSecrets rewritten as single-pass merged-interval (no fragment leak under equal-length overlap); redaction moved to the error boundary (redactedError, no Unwrap escape); credential-helper hardened (stdin-only, argv=get, io.Discard stderr, allowlisted env, CommandContext, bounded stdout).'
files_changed:
    - internal/config/oci_auth.go
    - internal/config/oci_auth_test.go
    - internal/config/fetcher_oci.go
    - internal/config/fetcher_test.go
    - internal/config/audit.go
    - internal/config/audit_test.go
verification_result:
    status: pass
    summary: 'go test ./internal/config/... -race + go build ./... + go vet ./... green. Cross-harness Codex gpt-5.6-sol round-3 confirmation = pass (all escalations demoted to LOW after call-site/stdlib verification; raw output read by orchestrator).'
integration_notes: 'Commits d45c7582 (round-2) + 7b737a35 (round-3) cherry-picked onto feat. ociPull live wire protocol still stubbed (t8 wires it + reuses this auth seam). Residuals tracked as fold-backs — oci-pin-manifest-digest-deadcode, oci-sidecar-schema-versioning, oci-auth-trusted-realm-allowlist (opt-in defense-in-depth). Three cross-harness review rounds.'
created_at: "2026-07-16T00:00:00Z"
---

## Summary

OCI registry auth providers (bearer + credential-helper), H12 secret non-disclosure, hardened across three cross-harness Codex review rounds. See summary frontmatter. Commits d45c7582 + 7b737a35 on the worktree branches; recovered + finalized after a mid-session process restart.
