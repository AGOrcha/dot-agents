---
schema_version: 1
task_id: cl-cache-keys-consume
parent_plan_id: config-v2-coherence
title: Consume cache_keys + per-kind staleness in fetcher/resolver (cc was inert)
summary: Wired cache_keys + per-kind cache-key default consumption end to end. Fetchers (git/http/local/oci/http-pkg) now surface CacheKeyInputs facts on FetchedLayer/FetchedArtifact; resolver derives EffectiveCacheKey (gathering live env/dir override facts, backfilling kind fact from resolved SHA), records it as LockedLayer.cache_key, and the offline-serve path refuses to serve when a force escape (--refresh via WithRefresh, or source always_revalidate) demands revalidation. Added staleness.go CacheKeyStale + resolver CacheKeyStaleForLayer (the non-test consumer) and cache_keys.go SourceKindOf. EffectiveCacheKey/DefaultCacheKey/CacheKeys are no longer inert.
files_changed:
    - .agents/active/delegation/.retired-stale/cg-project-local-overlay.yaml
    - .agents/active/delegation/.retired-stale/p3-starter-promotion.yaml
    - .agents/workflow/plans/config-v2-coherence/TASKS.yaml
    - .agents/workflow/plans/cross-platform-test-skips-audit/TASKS.yaml
    - .agents/workflow/plans/layered-pr-fanout/TASKS.yaml
    - .agents/workflow/plans/loop-discipline-stop-hooks/TASKS.yaml
    - .agents/workflow/plans/platform-driven-diagnostics/TASKS.yaml
    - infra/azure/codesigning/README.md
    - infra/azure/codesigning/cert-profile.json
    - infra/azure/codesigning/create-cert-profile.sh
verification_result:
    status: pass
    summary: 'Commit b97868ce on branch worktree-agent-a428de77563990d46 (NOT pushed). IMPORTANT: this worktree branch was stale (PR-3 era, lacked cc''s cache_keys.go); I reset it onto fix/refresh-inexact-flag (contains cc f508c455) before working, so the diff is exactly the 8 write_scope files. Parent should integrate b97868ce onto the live config-v2-coherence line. Tests: go test ./internal/config/... green, full go test ./... green, gofmt/vet clean.'
integration_notes: 'Commit b97868ce on branch worktree-agent-a428de77563990d46 (NOT pushed). IMPORTANT: this worktree branch was stale (PR-3 era, lacked cc''s cache_keys.go); I reset it onto fix/refresh-inexact-flag (contains cc f508c455) before working, so the diff is exactly the 8 write_scope files. Parent should integrate b97868ce onto the live config-v2-coherence line. Tests: go test ./internal/config/... green, full go test ./... green, gofmt/vet clean.'
created_at: "2026-06-22T13:25:48Z"
---

## Summary

Wired cache_keys + per-kind cache-key default consumption end to end. Fetchers (git/http/local/oci/http-pkg) now surface CacheKeyInputs facts on FetchedLayer/FetchedArtifact; resolver derives EffectiveCacheKey (gathering live env/dir override facts, backfilling kind fact from resolved SHA), records it as LockedLayer.cache_key, and the offline-serve path refuses to serve when a force escape (--refresh via WithRefresh, or source always_revalidate) demands revalidation. Added staleness.go CacheKeyStale + resolver CacheKeyStaleForLayer (the non-test consumer) and cache_keys.go SourceKindOf. EffectiveCacheKey/DefaultCacheKey/CacheKeys are no longer inert.

## Integration Notes

Commit b97868ce on branch worktree-agent-a428de77563990d46 (NOT pushed). IMPORTANT: this worktree branch was stale (PR-3 era, lacked cc's cache_keys.go); I reset it onto fix/refresh-inexact-flag (contains cc f508c455) before working, so the diff is exactly the 8 write_scope files. Parent should integrate b97868ce onto the live config-v2-coherence line. Tests: go test ./internal/config/... green, full go test ./... green, gofmt/vet clean.
