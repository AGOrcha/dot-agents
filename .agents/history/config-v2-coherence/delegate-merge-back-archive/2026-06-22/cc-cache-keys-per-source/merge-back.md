---
schema_version: 1
task_id: cc-cache-keys-per-source
parent_plan_id: config-v2-coherence
title: Per-source cache_keys (uv model) + per-kind staleness defaults
summary: 'Per-source cache_keys (uv model) + per-kind staleness defaults. Adds Source.cache_keys (file/glob, git{commit,tags}, env, dir, always_revalidate) to internal/config/agentsrc.go + schemas/agentsrc.schema.json (additionalProperties:false). New internal/config/cache_keys.go: pure-policy DefaultCacheKey per kind (git->commit; local->commit+worktree; http->ETag/Last-Modified else digest; oci->digest) and EffectiveCacheKey composing force-escapes (--refresh / always_revalidate) > selector override > kind default. Verified: go test ./internal/config ./commands ./internal/links green; cache_keys.go per-file coverage 100% (gate 95%); 16 test funcs (positive + negative: type-mismatch + schema unknown-key reject); gofmt -l empty; go vet clean; da config explain sources round-trips cache_keys end-to-end.'
files_changed:
    - .agents/workflow/plans/config-v2-coherence/TASKS.yaml
verification_result:
    status: pass
    summary: 'Branch feature/cc-cache-keys-per-source (org), commit f508c455, already pushed; PR #35 OPEN: https://github.com/AGOrcha/dot-agents/pull/35. Branched off master (deps_in_flight empty). Land-it-once target was PR #209 (p5) but p5 already merged to master, so this stands alone on master. No new commit created this run: worktree files verified byte-identical to the pushed PR head. Cross-plan dep config-v2-migration/p5-source-types-http-oci is present on master.'
integration_notes: 'Branch feature/cc-cache-keys-per-source (org), commit f508c455, already pushed; PR #35 OPEN: https://github.com/AGOrcha/dot-agents/pull/35. Branched off master (deps_in_flight empty). Land-it-once target was PR #209 (p5) but p5 already merged to master, so this stands alone on master. No new commit created this run: worktree files verified byte-identical to the pushed PR head. Cross-plan dep config-v2-migration/p5-source-types-http-oci is present on master.'
created_at: "2026-06-06T22:15:36Z"
---

## Summary

Per-source cache_keys (uv model) + per-kind staleness defaults. Adds Source.cache_keys (file/glob, git{commit,tags}, env, dir, always_revalidate) to internal/config/agentsrc.go + schemas/agentsrc.schema.json (additionalProperties:false). New internal/config/cache_keys.go: pure-policy DefaultCacheKey per kind (git->commit; local->commit+worktree; http->ETag/Last-Modified else digest; oci->digest) and EffectiveCacheKey composing force-escapes (--refresh / always_revalidate) > selector override > kind default. Verified: go test ./internal/config ./commands ./internal/links green; cache_keys.go per-file coverage 100% (gate 95%); 16 test funcs (positive + negative: type-mismatch + schema unknown-key reject); gofmt -l empty; go vet clean; da config explain sources round-trips cache_keys end-to-end.

## Integration Notes

Branch feature/cc-cache-keys-per-source (org), commit f508c455, already pushed; PR #35 OPEN: https://github.com/AGOrcha/dot-agents/pull/35. Branched off master (deps_in_flight empty). Land-it-once target was PR #209 (p5) but p5 already merged to master, so this stands alone on master. No new commit created this run: worktree files verified byte-identical to the pushed PR head. Cross-plan dep config-v2-migration/p5-source-types-http-oci is present on master.
