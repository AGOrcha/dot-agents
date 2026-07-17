---
schema_version: 1
task_id: t8-oci-publish
parent_plan_id: package-artifact-install
title: 'OCI publish producer on the unified surface + round-trip byte-parity smoke'
summary: 'da config publish on the canonical unified config surface (NOT the retired da packages) packs a resource tree into a typed +tar+gzip artifact-bundle via the shared H1 normalizer (PackTree, the producer counterpart of UntarBundle), pushes it to an OCI registry reusing t7 auth, and emits the sha256. Wired the real OCI push + live pull (replacing the ociPull stub), populating ManifestDigest (verified distinct from the payload digest) and stamping a schema version on the oci-type sidecar. Round-trip byte-parity smoke against an in-process fake registry proves recursive source-vs-materialized parity (R8) + H5 tampered-blob + H6 wrong-media-type rejection + frozen-lock no-op. Two H12 credential-handling defects found by cross-harness (Codex) review and closed: blob-upload Location pinned to the registry origin, and a redirect policy that refuses any push redirect + any cleartext pull redirect.'
files_changed:
    - commands/config/publish.go
    - commands/config/cmd.go
    - internal/config/artifact_bundle.go
    - internal/config/fetcher_oci.go
    - internal/config/fetcher_oci_test.go
    - commands/internal/lifecycle/packages_pass2_test.go
    - scripts/verify.sh
verification_result:
    status: pass
    summary: 'go build ./... + go vet + go test ./internal/config/... -race + ./commands/config/... + lifecycle green. Cross-harness Codex gpt-5.6-sol: publish/pull round-trip clean; live-pull rewrite preserved all t6 gates; two H12 fixes (Location origin guard + push-redirect policy) confirmed pass, raw output read by orchestrator.'
integration_notes: 'Commits dcbd011a (base) + 79e6a226 (Location origin guard) + eb080fbe (redirect policy) cherry-picked onto feat. Closes wire-protocol residuals oci-pin-manifest-digest-deadcode (ManifestDigest populated) + oci-sidecar-schema-versioning. Deferred: oci-artifact-nongzip-early-reject (LOW, conflicts with existing non-gzip test fixtures) + oci-auth-trusted-realm-allowlist (opt-in DiD). Last feature task; t9 (perf) now unblocked.'
created_at: "2026-07-16T04:00:00Z"
---

## Summary

OCI publish producer + real push/pull wire protocol + round-trip byte-parity, on the unified `da config publish` surface. Hardened through cross-harness review (two H12 credential-handling fixes). See summary frontmatter. Base commit authored by the t8 worker; the two H12 fixes authored by the orchestrator after the worker's resume failed to re-engage, each cross-harness-confirmed.
