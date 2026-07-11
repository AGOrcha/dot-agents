# dot-agents inner-loop plan breakdown — ALL active plans

_Generated 2026-07-11 by PlansBreakdownDoc. Source of truth: `~/proj-docs/dot-agents/.agents/workflow/plans/<plan-id>/{PLAN,TASKS}.yaml`. Readiness computed DIRECTLY from `TASKS.yaml` statuses (NOT `da workflow eligible`, which is unreliable on the shipped 0.4.2 binary)._

## Scope & method

- **22 ACTIVE plans** (PLAN.yaml `status: active`). Non-active plans excluded from the ready/critical-path analysis but listed at the end for completeness.
- Per task the canonical fields are: `id`, `title`, `status`, `depends_on`, `blocks`, `owner`, `write_scope`, `verification_required`, `app_type`, `notes`. Structured fields quoted verbatim; `notes` reduced to a gist.
- **Done statuses** (satisfy a downstream dep): `completed`, `complete`, `done`, `cancelled`, `superseded`, `archived`. **Not-done**: `pending`, `in_progress`, `blocked`, `deferred`, `ready`, `awaiting_review`, `awaiting_agent_review`, `awaiting_owner_review`.
- A task is **READY** iff it is not-done AND every `depends_on` entry resolves to a done task (deps may be same-plan bare ids or `plan/task` cross-plan refs). Empty `depends_on` ⇒ ready.
- Per the layered-pr-fanout spec, `awaiting_owner_review` also satisfies downstream deps and frees a slot; `in_progress`/`awaiting_agent_review` hold a slot. The strict READY set below uses done-only for dep-satisfaction and flags `awaiting_owner_review` tasks separately (they are impl-complete, human-merge-pending).

## Active-plan inventory

| plan id | title | status | current_focus_task | not-done / total | TASKS.yaml |
|---|---|---|---|---|---|
| `agent-ops-hardening` | Agent-ops hardening (retro of config-v2/0.4.0 + recent sessions) | active | — | 4/14 | `.agents/workflow/plans/agent-ops-hardening/TASKS.yaml` |
| `config-transitive-layering` | Transitive org→team→repo config layering (resolver + scope schema + layered consumers) | active | org-config-spec-transitive-scope | 4/4 | `.agents/workflow/plans/config-transitive-layering/TASKS.yaml` |
| `config-v2-migration` | v1->v2 config-distribution-model migration | active | — | 1/21 | `.agents/workflow/plans/config-v2-migration/TASKS.yaml` |
| `cross-platform-test-skips-audit` | Audit Windows test skips; replace OS-dependent shortcuts with cross-platform abstractions | active | Prune stale allowlist entries the migration unblocks | 0/8 | `.agents/workflow/plans/cross-platform-test-skips-audit/TASKS.yaml` |
| `git-ref-work-backend` | git-ref coordination-state backend + read-from-master shim (decouple state from the code branch) | active | read-from-master-shim | 6/7 | `.agents/workflow/plans/git-ref-work-backend/TASKS.yaml` |
| `graph-backend-adapter-contract` | Graph Backend Adapter Contract | active | Bridge decommissioning - close §11.4 gate | 3/10 | `.agents/workflow/plans/graph-backend-adapter-contract/TASKS.yaml` |
| `kg-ideate-skill` | KG-Ideate Skill — Build + Schedule | active | — | 1/8 | `.agents/workflow/plans/kg-ideate-skill/TASKS.yaml` |
| `layered-pr-fanout` | Layered PR fanout: awaiting_review status + slot/dep accounting + blocked-on | active | — | 0/7 | `.agents/workflow/plans/layered-pr-fanout/TASKS.yaml` |
| `loop-discipline-stop-hooks` | Loop-Discipline Stop Hooks | active | Final migration of legacy downstream discipline-skill overrides | 4/17 | `.agents/workflow/plans/loop-discipline-stop-hooks/TASKS.yaml` |
| `managed-gitignore-autofill` | Wire D14/R8 managed-gitignore auto-fill for per-platform generated outputs | active | wire-managed-gitignore-autofill | 1/1 | `.agents/workflow/plans/managed-gitignore-autofill/TASKS.yaml` |
| `platform-driven-diagnostics` | Platform-driven diagnostics (doctor + status delegate to internal/platform sister interfaces) | active | — | 0/8 | `.agents/workflow/plans/platform-driven-diagnostics/TASKS.yaml` |
| `r1-5-hook-enforcement-telemetry` | R1.5: Hook enforcement telemetry | active | Release: bump VERSION + finalize CHANGELOG for the hook-telemetry minor | 1/12 | `.agents/workflow/plans/r1-5-hook-enforcement-telemetry/TASKS.yaml` |
| `r2-observability-dashboard` | R2: Real-time observability and evaluation dashboard | active | Wire /api/v1/observability/events SSE handler to broker | 11/18 | `.agents/workflow/plans/r2-observability-dashboard/TASKS.yaml` |
| `r3-background-worker-service` | R3: Background-worker service | active | `da service run\|status\|stop` cobra subcommand | 3/11 | `.agents/workflow/plans/r3-background-worker-service/TASKS.yaml` |
| `r4-code-task-generation-eval` | R4: Code-task generation and evaluation harness | active | Release: bump VERSION + finalize CHANGELOG for the eval-harness minor | 1/19 | `.agents/workflow/plans/r4-code-task-generation-eval/TASKS.yaml` |
| `r5-review-labeling-access` | R5: Human-in-the-loop review, labeling, and access layer | active | Review surface in R2 (list, inspect, label form, role-aware controls) | 4/11 | `.agents/workflow/plans/r5-review-labeling-access/TASKS.yaml` |
| `release-patch-train` | Release patch train (cadence) | active | release-docs-refresh | 2/2 | `.agents/workflow/plans/release-patch-train/TASKS.yaml` |
| `root-command-decomposition` | Root commands package decomposition | active | — | 1/24 | `.agents/workflow/plans/root-command-decomposition/TASKS.yaml` |
| `seam-interface-di-migration` | Migrate func-var test seams to interface-DI | active | — | 0/7 | `.agents/workflow/plans/seam-interface-di-migration/TASKS.yaml` |
| `shared-target-projection-wiring` | Shared-target projection wiring | active | Final doc reconcile + pr10 cross-ref + close | 1/6 | `.agents/workflow/plans/shared-target-projection-wiring/TASKS.yaml` |
| `worker-bundle-lessons` | Lesson preload/injection into worker bundles (relevance-selected required_reads) | active | load-lessons-1-schema-selection-metadata | 4/4 | `.agents/workflow/plans/worker-bundle-lessons/TASKS.yaml` |
| `worktree-platform` | Managed worktree platform for delegation/branch isolation | active | Rich worktree metadata registry + auto-prune-if-unchanged (layered on wt1's Prune/BaseRef) | 6/9 | `.agents/workflow/plans/worktree-platform/TASKS.yaml` |

## Per-plan task breakdown

### `agent-ops-hardening` — Agent-ops hardening (retro of config-v2/0.4.0 + recent sessions)

- status: **active** · current_focus_task: (none set) · tasks: 14 (4 not-done) · path: `.agents/workflow/plans/agent-ops-hardening/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `p0-codex-gocache-sandbox` | P0: pre-set GOCACHE/GOTMPDIR/TMPDIR + da-write perms in the review-agent sandbox | completed | ∅ | config | Y | Grant the existing Go cache (~/Library/Caches/go-build + ~/go/pkg/mod) as writable_roots in the Codex sandbox config.toml, killing the biggest GOCACHE=/tmp tax. DONE. |
| `p0-sonar-scanner-exclusions` | P0: exclude dist/ + .scannerwork from sonar-scanner (stop pre-push flake) | completed | ∅ | config | Y | Add dist/ and .scannerwork to sonar.exclusions so the pre-push scanner stops flaking and workers stop using --no-verify. Landed via PR #121. |
| `p0-move-repo-out-of-documents` | P0: migration to move repo out of ~/Documents (preserve primary-agent history) | completed | ∅ | ops | n | Migration script to move the repo out of TCC-protected ~/Documents (to ~/dot-agents), carrying Claude transcripts/memory and merging project config keys. Landed via PR #129. |
| `p1-safe-push-skill` | P1: extend precommit-mandate.sh + safe-push lesson (SSH keepalive + ref-land verify) | completed | p0-sonar-scanner-exclusions | config | Y | Extend precommit-mandate.sh with SSH keepalive + post-push ref-land verification and capture a safe-push lesson (no new skill). Landed via PR #131. |
| `p1-delegation-brief-hardening-skill` | P1: operationalize pre-fanout bundle hardening (make validate-bundle-against-head + bundle-scope-via-code-graph mandatory in orchestrator-session-start/delegation-lifecycle) | awaiting_owner_review | ∅ | skill | Y | Make validate-bundle-against-head + bundle-scope-via-code-graph mandatory pre-fanout, plus a coverage-delta-forecast bullet. Impl done, PR #134 awaiting owner merge. |
| `p1-release-cut-skill` | P1 skill (the one new standalone skill): release-cut-monitor-retry + split-out pin-toolchain CI step | awaiting_owner_review | ∅ | skill | Y | New standalone release-cut skill (preflight pin-check signing toolchain, cut, monitor auto-release.yml, retry/classify infra failures); split the pin-toolchain guard out as a CI step. PR #135 awaiting merge. |
| `p1-reconcile-eligible-and-preload-tools` | P1: fold reconcile bullet into orchestrator-session-start preflight (guarded git fetch) + preload toolset via tools: frontmatter | awaiting_owner_review | ∅ | config | Y | Add a guarded git fetch bullet to orchestrator-session-start preflight and preload the orchestrator toolset via tools: frontmatter. Delivered with PR #134. |
| `p1-helper-skills` | P1: helper scripts / verifier-profile content (NOT skills) | completed | ∅ | docs | n | Helper scripts/verifier content (not skills): pr-gate-wait, locate-and-tail-claude-session, and a sonar-issue-fetch (use MCP not curl) CI note. Landed via PR #130. |
| `p2-lessons` | P2: capture the 3 un-captured lessons | completed | ∅ | docs | n | Capture 3 lessons (pin-release-toolchain, hermetic-home-for-state-resolving-tests, gates-must-be-locally-reproducible) and update the index. Landed via PR #125. |
| `p2-brief-template-hardening` | P2: harden the delegation brief/bundle template | awaiting_owner_review | ∅ | docs | n | Bake hardening rules into the standard delegation bundle/brief (Sonar complexity <15, tests in write_scope, read-only boundary, no --no-verify, Windows path sanitization). Delivered with PR #134. |
| `p2-scrub-stealth-leak` | P2: scrub agent-tool session-author leak in worktree handoffs | cancelled | ∅ | docs | n | Cancelled: policy relaxed to allow factual AI references; harness/model/created_by are legitimate operational metadata. PR #122 closed unmerged. |
| `p3-tool-routing-doc` | P3: document primary-agent vs secondary-review-agent work routing | completed | ∅ | docs | n | Short doc routing work between primary agent (heavy Go build-test loops) and secondary review agent (adversarial review, cross-tool resume, bounded delegation). Landed via PR #137. |
| `p4-fanout-gate-enforcement` | P4: mechanical refuse-check in da workflow fanout for out-of-scope asserting test callers | completed | p1-delegation-brief-hardening-skill | go-cli | Y | Add a mechanical refuse-check to da workflow fanout rejecting bundles whose out-of-scope asserting *_test.go callers of changed symbols fall outside write_scope. Code enforcement of PR #134's prose. |
| `p5-routing-doc-cross-harness-evidence` | Update AGENT_TOOL_ROUTING.md with cross-harness wave evidence + blocking-gate policy | completed | ∅ | docs | Y | Update AGENT_TOOL_ROUTING.md with cross-harness wave evidence (Codex=blocking review, Claude=impl+publish/gate-running) and blocking-gate policy. |

**Not-done task write_scope & readiness:**

- `p1-delegation-brief-hardening-skill` [awaiting_owner_review] **READY** [impl-complete; human-merge-pending]: `internal/scaffold/home/starter/skills/global/orchestrator-session-start/`; `internal/scaffold/home/starter/skills/global/delegation-lifecycle/`
- `p1-release-cut-skill` [awaiting_owner_review] **READY** [impl-complete; human-merge-pending]: `internal/scaffold/home/starter/skills/global/release-cut/`; `.github/workflows/`
- `p1-reconcile-eligible-and-preload-tools` [awaiting_owner_review] **READY** [impl-complete; human-merge-pending]: `internal/scaffold/home/starter/skills/global/orchestrator-session-start/`; `.agents/active/orchestrator.loop.md`
- `p2-brief-template-hardening` [awaiting_owner_review] **READY** [impl-complete; human-merge-pending]: `internal/scaffold/home/starter/skills/global/delegation-lifecycle/`; `internal/scaffold/home/starter/skills/global/orchestrator-session-start/`

### `config-transitive-layering` — Transitive org→team→repo config layering (resolver + scope schema + layered consumers)

- status: **active** · current_focus_task: `org-config-spec-transitive-scope` · tasks: 4 (4 not-done) · path: `.agents/workflow/plans/config-transitive-layering/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `org-config-spec-transitive-scope` | Spec org→team→repo transitive extends + source scope/owner routing | pending | ∅ | — | Y | Spec repo→team→org transitive extends with post-order precedence and define sources[].scope/owner as routing metadata (not authority). Includes the PA/Roos worked example. |
| `config-source-scope-schema` | Add Source.Scope/Owner typed schema surface | pending | org-config-spec-transitive-scope | — | Y | Add typed Source.Scope (enum public/org/team/repo) + Source.Owner routing fields to agentsrc.go and the schema; ensure v1 byte-stable round-trip. |
| `config-transitive-extends-resolver` | CORE: resolve layer-declared sources/extends transitively | pending | config-source-scope-schema | — | Y | CORE gap: add resolveExtendsGraph to decode each fetched layer's own sources/extends recursively, return children-first (org before team before repo), dedupe/cycle-detect, and lock all transitive units for offline reproduction. |
| `layered-consumers-relevance-verify` | Make relevance/verify/verifier dispatch read layered snapshots | pending | config-transitive-extends-resolver | — | Y | Migrate da config relevance + verifier dispatch off the flat snapshot onto the layered/locked resolver so org-through-team app_types stop showing matched:false. Shared prereq for worker-bundle-lessons. |

**Not-done task write_scope & readiness:**

- `org-config-spec-transitive-scope` [pending] **READY**: `.agents/workflow/specs/org-config-resolution/design.md`; `.agents/workflow/specs/config-distribution-model/design.md`; `docs/LAYERED_CONFIG_GUIDE.md`
- `config-source-scope-schema` [pending] **BLOCKED — waits on config-transitive-layering/org-config-spec-transitive-scope[pending]**: `internal/config/agentsrc.go`; `schemas/agentsrc.schema.json`; `internal/config/agentsrc_test.go`; `internal/config/schema_roundtrip_test.go`
- `config-transitive-extends-resolver` [pending] **BLOCKED — waits on config-transitive-layering/config-source-scope-schema[pending]**: `internal/config/resolver.go`; `internal/config/resolver_test.go`; `internal/config/resolver_extra_test.go`; `internal/config/lock_units.go`; `internal/config/units_lock_wiring_test.go`
- `layered-consumers-relevance-verify` [pending] **BLOCKED — waits on config-transitive-layering/config-transitive-extends-resolver[pending]**: `commands/config/relevance.go`; `commands/config/explain.go`; `commands/workflow/`; `internal/config/precondition_resolve.go`; `docs/CONFIG_RELEVANCE.md`

### `config-v2-migration` — v1->v2 config-distribution-model migration

- status: **active** · current_focus_task: (none set) · tasks: 21 (1 not-done) · path: `.agents/workflow/plans/config-v2-migration/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `p0-schema-additive-extension` | Phase 0: additive v2 schema extension (preserves v1 compat) | completed | ∅ | go-cli | Y | Additively add v2 schema fields (Source.id/cache_ttl/auth, Extends, Packages, RepoID, Features; type enum local/git/http/oci) while keeping v1 loadable and validated. |
| `p0b-repo-id-bootstrap` | Phase 0b: repo_id field + da init auto-populate from git remote | completed | p0-schema-additive-extension | go-cli | Y | Wire da init to auto-derive repo_id from the git remote (normalize host/org/repo, strip .git); no remote leaves it empty with a doctor warning. |
| `p1-resolver-core-flat` | Phase 1: effective-config snapshot API (flat scope first) | completed | p0-schema-additive-extension | go-cli | Y | Introduce snapshot.go with a Resolver interface and a FlatResolver producing an effective config + per-field provenance across product/user-local/repo-local (no fetch yet). |
| `p1b-resolver-extends-tier1` | Phase 1b: resolver tier-1 extends (git+http+local layer fetch) | completed | p1-resolver-core-flat | go-cli | Y | Extend to LayeredResolver: fetch git/http/local extends layers, validate layer schema, write the lockfile config section with content-addressed caching, TTL re-check, and offline fallback. |
| `p2-lockfile-doctor-status` | Phase 2: lockfile drift surfaces (da doctor + da status) | completed | p1b-resolver-extends-tier1 | go-cli | Y | Surface lockfile drift: da doctor warns/errors on expired TTL, undeclared layers/packages, and protection violations; da status shows resolved SHAs and package digests. |
| `p3-audit-events` | Phase 3: audit event taxonomy (config.* events) | completed | p1b-resolver-extends-tier1 | go-cli | Y | Emit structured config.* audit events (source.fetch, layer.resolve, field.overridden, field.protection_violation, import.failed, effective.produced) with trace_id correlation via a pluggable Sink. |
| `p4-config-explain-cli` | Phase 4: da config explain command + da config subtree | completed | p1-resolver-core-flat | go-cli | Y | Net-new da config subtree with da config explain [field-path] (--all/--flags/--value-only/--origin-only/--json) reporting effective value + winning layer + full stack, with spec §10 exit codes. |
| `p4b-app-types-snapshot-refactor` | Phase 4b: refactor workflow app-types to consume snapshot API | completed | p4-config-explain-cli | go-cli | Y | Refactor workflow app-types to read the snapshot API instead of .agentsrc.json directly, preserving the user-facing contract while reporting layered app types. |
| `p4c-config-sync-lint` | Phase 4c: da config sync + da config lint + da config verify commands | completed | p4-config-explain-cli, p1b-resolver-extends-tier1, p2-lockfile-doctor-status | go-cli | Y | Add da config sync (force re-fetch + update lock), da config lint (validate layer files), and da config verify (repo setup checks, no fetch); keep da refresh distinct. |
| `p5-source-types-http-oci` | Phase 5: source-type expansion (http+oci) + signing posture stub | completed | p1b-resolver-extends-tier1 | go-cli | Y | Add http and read-only oci fetchers with digest-pinning + a permissive Verifier signing stub; also folds in consolidating install clone onto go-git (fb:consolidate-clone-on-gogit). |
| `p6-packages-pass2-cli` | Phase 6: Tier 2 packages resolution (pass 2) + da packages subtree | cancelled | p5-source-types-http-oci, p4c-config-sync-lint | go-cli | Y | Cancelled/superseded by config-distribution-model §15: the packages tier collapsed into the units model; pass-2 artifact resolution moved to config-v2-coherence/ce. No code written. |
| `p7-v1-deprecation-warnings` | Phase 7: v1 surface deprecation warnings (post-soak) | completed | p4b-app-types-snapshot-refactor, p4f-units-lock-7a-wiring | go-cli | Y | Post-soak v1 deprecation: one-time load notice, da config migrate v1->v2, doctor warning, da init writes v2 by default; keep v1 loading. Landed via PR #123. |
| `p1c-verifier-profile-source-aware` | Phase 1c (optional): source-aware verifier_profile.prompt_files | completed | p1b-resolver-extends-tier1 | go-cli | Y | Q1 Option B (critical-path): force typed prompt_files objects everywhere, bulk-migrate flat [string]->[{source,path,version}] across repo + starter, update resolver lookups + fixtures. |
| `p7b-v1-auto-migrator` | Phase 7b: v1→v2 auto-migrator with 1-day sunset | completed | p7-v1-deprecation-warnings | go-cli | Y | Ship da config migrate (opt-in v1->v2 with .v1.bak backup, folds legacy keys into stage_profiles/execution_profile, idempotent, --dry-run). Landed via PR #138 in 0.4.1. |
| `p4d-da-install-repurpose` | Phase 4d: repurpose da install per spec §13.3 | cancelled | p4-config-explain-cli | go-cli | Y | Cancelled/deferred out of the v2 critical path; da install repurpose is a §15 D12 concern to re-file as its own plan. No code written. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the config-v2 minor | completed | p0-schema-additive-extension, p0b-repo-id-bootstrap, p1-resolver-core-flat, p1b-resolver-extends-tier1, p2-lockfile-doctor-status, p3-audit-events, p4-config-explain-cli, p4b-app-types-snapshot-refactor, p4c-config-sync-lint, p5-source-types-http-oci, p4f-units-lock-7a-wiring, p1c-verifier-profile-source-aware, p4g-da-install-units-sync, p4h-agentslock-interprocess-lock, config-v2-coherence/ce-unified-artifact-sourcing, config-v2-coherence/ck-command-reshape | release | Y | Release-gated tail: bump VERSION + finalize CHANGELOG for the complete config-v2 §15 minor (0.4.0). Landed: 0.4.0 published via PR #114. |
| `p4f-units-lock-7a-wiring` | Phase 4f: wire §7A units lock (inputs_digest + units) into resolve/sync; verify tracks local-scope drift | completed | p4-config-explain-cli, config-v2-coherence/ca-units-lock-schema, config-v2-coherence/cd-ensure-resolved-seam | go-cli | Y | Wire the §7A units lock (inputs_digest + units section) into resolve/sync/install as a clean cutover (retire the §7 config lock), with three adversarial-review correctness fixes and verify staleness tracking. |
| `p4g-da-install-units-sync` | Phase 4g: re-scope da install onto EnsureResolved + exact/prune projection (§15) | completed | p4f-units-lock-7a-wiring, p4c-config-sync-lint | go-cli | Y | Re-scope da install onto EnsureResolved + exact/prune projection, record the resolution stamp in the lock (not .agentsrc.json), honoring Codex F-007/F-008 constraints. Landed via PR #92. |
| `p4h-agentslock-interprocess-lock` | Phase 4h: agentslock interprocess lost-update protection (.agentsrc.lock) | completed | p4f-units-lock-7a-wiring | go-cli | Y | Fix the agentslock lost-update defect (only in-process mutex today) with interprocess advisory lock or compare-and-merge-retry so concurrent da writers don't drop sibling sections. Codex F-006, gated into 0.4.0. |
| `lint-validate-locked-remote-layers` | Fast-follow: da config lint validates locked+cached remote layers (not blanket-skip) | completed | p4c-config-sync-lint, p4f-units-lock-7a-wiring | go-cli | Y | Fast-follow F-005: da config lint validates locked+cached remote layers at their locked digest instead of blanket-skipping non-local sources; export a cache-read-by-digest helper. Landed via PR #127. |
| `p8-payout-wrk-v2-upgrade` | Payout-wrk v1->v2 upgrade + fresh install-from-git validation (0.4.1) | ready | p7b-v1-auto-migrator | go-cli | Y | 0.4.1 payout upgrade readiness: migrate payout-wrk v1->v2, validate fresh install-from-git, and diagnose the Windows 'failed to get the local' error (likely git-binary/PATH; go-git fold-back candidate). Live execution runs on the Windows box. |

**Not-done task write_scope & readiness:**

- `p8-payout-wrk-v2-upgrade` [ready] **READY**: `.agents/history/config-v2-migration/payout-wrk-v2-upgrade-readback.md`

### `cross-platform-test-skips-audit` — Audit Windows test skips; replace OS-dependent shortcuts with cross-platform abstractions

- status: **active** · current_focus_task: `Prune stale allowlist entries the migration unblocks` · tasks: 8 (0 not-done) · path: `.agents/workflow/plans/cross-platform-test-skips-audit/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `catalogue` | Catalogue every Windows skip and classify each | completed | ∅ | go-cli | n | Walk grep for runtime.GOOS == "windows" skips and produce findings.md classifying each site (shortcut-permission/mode/symlink vs genuine-posix); classification drives which task adopts it. |
| `perms-dir` | Extend testutil with MakeDirUnreadable (POSIX chmod + Windows ACL/DACL) | completed | catalogue | go-cli | Y | Add testutil.MakeDirUnreadable (POSIX chmod 0 + Windows deny-DACL) for cannot-stat/list assertions, restoring access on cleanup. |
| `perms-readonly` | Extend testutil with MakeFileReadOnly (works on both platforms) | completed | catalogue | go-cli | Y | Add testutil.MakeFileReadOnly via chmod 0o444, which Go maps to FILE_ATTRIBUTE_READONLY on Windows - the genuinely cross-platform case. |
| `symlinks` | testutil.SymlinkOrSkip - gate symlink tests on capability, not OS | completed | catalogue | go-cli | Y | Add testutil.SymlinkOrSkip gating on a symlink-capability probe rather than OS, so Dev-Mode Windows runs the tests. |
| `exec-bit` | Decide on exec-bit tests - POSIX-only behaviour, document deliberately | completed | catalogue | go-cli | Y | Keep the exec-bit test skip (genuine POSIX 0o755 semantics) but document why it cannot be abstracted. |
| `fsops-windows-tests` | Add internal/fsops/fsops_windows_test.go (6 untested Windows-build-tagged functions) | completed | perms-dir | go-cli | Y | Add a //go:build windows test file covering the 6 untested Windows-tagged fsops functions (0% Windows coverage today); test-only addition. |
| `migrate-sites` | Migrate every shortcut-class skip to the new helpers | completed | perms-dir, perms-readonly, symlinks, exec-bit | go-cli | Y | Apply the new helpers to every shortcut-class skip so no t.Skip fires on windows-latest and per-file Windows coverage rises. |
| `cg6-prune` | Prune stale allowlist entries the migration unblocks | completed | migrate-sites | go-cli | Y | Prune stale coverage-allowlist entries the migration lifts above threshold; found none stale at HEAD (all 23 files still sub-95%). |

### `git-ref-work-backend` — git-ref coordination-state backend + read-from-master shim (decouple state from the code branch)

- status: **active** · current_focus_task: `read-from-master-shim` · tasks: 7 (6 not-done) · path: `.agents/workflow/plans/git-ref-work-backend/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `spec-git-ref-backend` | Spec: git-ref backend + read-from-master (amend storage-abstraction §9) | completed | ∅ | — | n | Appended §9 (D9 git-ref WorkStore, D10 no-merge-to-default, read-from-master shim, commit-scope) to the storage-abstraction design; additions only. |
| `read-from-master-shim` | read-from-master shim: resolve task-state from the canonical ref, not the worktree copy | pending | spec-git-ref-backend | go-cli | Y | Add work_tracking.read_from = worktree\|master so canonical task-state resolves from the ref / origin default branch, killing the worktree-isolation re-dispatch storm; read-side only. |
| `git-ref-state-ref-write` | Write path: commit transitions to refs/agents/state via update-ref CAS | pending | read-from-master-shim | go-cli | Y | Commit status transitions to refs/agents/state via atomic update-ref CAS with retry-on-mismatch (interprocess-safe RMW); no merge to default branch. |
| `per-task-state-files` | Split coordination status into per-task state files under the ref | pending | git-ref-state-ref-write | go-cli | Y | Split status into per-task state files under the ref so concurrent workers on different tasks never line-conflict; keep the projected TASKS.yaml regenerable. |
| `decouple-coordination-commits` | Stop staging .agents/workflow/** into code-branch commits (fold commit-scope) | pending | git-ref-state-ref-write | go-cli | Y | Stop staging .agents/workflow/** into code-branch commits now that state lives on the ref; supersedes worker-bundle-authoring commit-scope tasks. |
| `workstore-git-ref-backend` | Expose git-ref as a WorkStore backend + work_tracking.backend config value | pending | per-task-state-files, decouple-coordination-commits | go-cli | Y | Expose git-ref behind the WorkStore interface and add work_tracking.backend to the scope ladder (local\|git-ref\|kg\|cloudflare-do\|jira\|linear). |
| `document-and-default-git-ref` | Adoption: document + recommend git-ref, surface the active backend so consumers know to use it | pending | workstore-git-ref-backend | go-cli | Y | Document/recommend git-ref + read_from=master, surface the active backend in da workflow status/health, and seed the starter default. |

**Not-done task write_scope & readiness:**

- `read-from-master-shim` [pending] **READY**: `commands/workflow/plan_task.go`; `commands/workflow/eligible_accounting.go`; `commands/workflow/next.go`; `internal/config/agentsrc.go`; `commands/config/`
- `git-ref-state-ref-write` [pending] **BLOCKED — waits on git-ref-work-backend/read-from-master-shim[pending]**: `commands/workflow/plan_task.go`; `internal/gitwt/`; `internal/agentslock/`
- `per-task-state-files` [pending] **BLOCKED — waits on git-ref-work-backend/git-ref-state-ref-write[pending]**: `commands/workflow/plan_task.go`; `commands/workflow/delegation.go`
- `decouple-coordination-commits` [pending] **BLOCKED — waits on git-ref-work-backend/git-ref-state-ref-write[pending]**: `commands/workflow/commit_cmd.go`; `commands/workflow/commit_pathset.go`
- `workstore-git-ref-backend` [pending] **BLOCKED — waits on git-ref-work-backend/per-task-state-files[pending], git-ref-work-backend/decouple-coordination-commits[pending]**: `internal/config/agentsrc.go`; `schemas/agentsrc.schema.json`; `commands/config/`; `commands/workflow/`
- `document-and-default-git-ref` [pending] **BLOCKED — waits on git-ref-work-backend/workstore-git-ref-backend[pending]**: `docs/LAYERED_CONFIG_GUIDE.md`; `docs/WORKFLOW.md`; `commands/workflow/status.go`; `internal/scaffold/home/starter/`

### `graph-backend-adapter-contract` — Graph Backend Adapter Contract

- status: **active** · current_focus_task: `Bridge decommissioning - close §11.4 gate` · tasks: 10 (3 not-done) · path: `.agents/workflow/plans/graph-backend-adapter-contract/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `t1-none-adapter-end-to-end` | Built-in `none` adapter end-to-end pipeline | completed | ∅ | — | Y | Built-in none null adapter exercising the whole pipeline (registry, ref resolver, lockfile state machine, query routing); shipped in the #136 merge wave. |
| `t2-compliance-read-only` | Compliance register adapter (read-only / stub bootstrap) | completed | t1-none-adapter-end-to-end | — | Y | Read-only compliance-register adapter delivering the first full v1 DSL parser (ref-joins, env_predicates, materialized views) with conformance tests T6-T31. |
| `t3-ttrpg-dogfood` | TTRPG dogfood adapter - first real adapter author UX | completed | t1-none-adapter-end-to-end | — | Y | TTRPG dogfood adapter with an OCI-packaged bootstrap skill and the Go adapter SDK - first real adapter-author UX and live DM dogfood. |
| `t4-crg-dual-read` | CRG migration - dual-read mode with crg-bridge adapter | completed | t1-none-adapter-end-to-end | — | Y | CRG migration dual-read: kg-native CRG adapter + read-only crg-bridge mirror adapter, plus a 100-commit parity test corpus. |
| `t5-cross-adapter-reads-from` | Cross-adapter `reads_from` - compliance view from CRG | completed | t2-compliance-read-only, t4-crg-dual-read | — | Y | Compliance view reading from CRG via reads_from - validates cross-namespace token derivation, multi-namespace query plans, and lockfile cutover. |
| `t6-bridge-decommission` | Bridge decommissioning - close §11.4 gate | pending | t4-crg-dual-read | — | Y | Flip core to the new CRG namespace and delete the Python-subprocess bridge once the §11.4 parity gate (3 consecutive weeks green) passes. |
| `t7-app-type-profiles-wiring` | Wire graph_backend adapter-ref selection into app-type-profiles | completed | t1-none-adapter-end-to-end | — | Y | Replace the closed graph_backend enum with an adapter-ref resolving via sources/extends and lockfile pin; wires the none adapter through the profile path. |
| `t8-tasks-schema-app-type-drift` | Fix workflow-tasks.schema.json app_type drift (schema rejects a field code uses) | completed | ∅ | — | Y | Add the optional open-string app_type property to workflow-tasks.schema.json so it stops rejecting a field the code and live plans already write. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the graph-backend minor | pending | t1-none-adapter-end-to-end, t2-compliance-read-only, t3-ttrpg-dogfood, t4-crg-dual-read, t5-cross-adapter-reads-from, t6-bridge-decommission, t7-app-type-profiles-wiring | release | Y | Bump VERSION to the recomputed next minor and finalize the CHANGELOG for the graph-backend minor (internal backend swap -> MINOR, not MAJOR). |
| `t9-sdk-materializeview-readsfrom-gate` | Wire reads_from enforcement into SDK.MaterializeView | pending | t4-crg-dual-read | go-cli | Y | Wire reads_from enforcement (registry consult) into SDK.MaterializeView so a view reading a migration_only adapter is rejected; add integration + mutation test. |

**Not-done task write_scope & readiness:**

- `t6-bridge-decommission` [pending] **READY**: `internal/graphstore/ (deletions)`; `internal/adapters/builtin/crg/ (full feature parity)`; `.github/workflows/ci.yml (parity test promotion)`
- `release-minor` [pending] **BLOCKED — waits on graph-backend-adapter-contract/t6-bridge-decommission[pending]**: `VERSION`; `CHANGELOG.md`
- `t9-sdk-materializeview-readsfrom-gate` [pending] **READY**: `internal/adapters/sdk/`

### `kg-ideate-skill` — KG-Ideate Skill — Build + Schedule

- status: **active** · current_focus_task: (none set) · tasks: 8 (1 not-done) · path: `.agents/workflow/plans/kg-ideate-skill/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `t1-kg-brief-molecule` | Phase 1 molecule `kg-brief` (degraded-mode capable) | completed | ∅ | — | Y | Author the degraded-mode-capable kg-brief molecule + instruction/template files; Contradictions renders [adapter-absent] when contradicting_claims is missing. |
| `t2-spec-scaffold-molecule` | Phase 2 molecule `spec-scaffold` (competing-decisions fallback) | completed | t1-kg-brief-molecule | — | Y | Author spec-scaffold with the competing-decisions fallback active; the structural-traversal branch is authored as adapter-conditional (verified by gated t7). |
| `t3-plan-scaffold-molecule` | Phase 3 molecule `plan-scaffold` (spec->tasks/write-scopes/dep-order) | completed | t2-spec-scaffold-molecule | — | Y | Author plan-scaffold turning spec into tasks/write-scopes/dep-order with the mandatory recorded concurrency decision; authors via da workflow plan create. |
| `t4-staged-execution-handoff-molecule` | Phase 4 molecule `staged-execution-handoff` (no implementation) | completed | t3-plan-scaffold-molecule | — | Y | Author staged-execution-handoff producing the direct-vs-fanout decision and handing briefing/spec/plan into orchestrator-session-start/isp; emits no code. |
| `t5-orchestrator-compound` | Compound orchestrator `kg-ideate` (T2, dispatches the four molecules) | completed | t1-kg-brief-molecule, t2-spec-scaffold-molecule, t3-plan-scaffold-molecule, t4-staged-execution-handoff-molecule | — | Y | Author the compound kg-ideate SKILL.md (tier: compound) dispatching the four molecules; owns brief -> (spec <-> plan fork) -> handoff. |
| `t6-degraded-mode-e2e` | Degraded-mode end-to-end demonstration (graceful-degradation contract) | completed | t5-orchestrator-compound | — | Y | Demonstrate a full four-phase run without the citation adapter plus a conformance assertion that molecule Load-> refs resolve; recorded home for behavioral verification. |
| `t7-structural-contradiction-traversal` | GATED: Phase 2 step 7 structural contradiction traversal (citation adapter) | blocked | t2-spec-scaffold-molecule, graph-backend-adapter-contract/t4-crg-dual-read | — | Y | GATED single step that flips step 7 from fallback to structural traversal only when the citation adapter ships contradicting_claims end-to-end (§13.4). |
| `t8-proposal-lesson-schema-formalization` | Formalize canonical proposal.schema.json + lesson.schema.json (single-source) | completed | ∅ | — | Y | Create canonical proposal.schema.json + lesson.schema.json (with a scope discriminator) referenced by both the global proposal/lesson system and the KG typed nodes. |

**Not-done task write_scope & readiness:**

- `t7-structural-contradiction-traversal` [blocked] **READY** [status=blocked but deps satisfied — stale status]: `internal/scaffold/home/starter/skills/global/kg-ideate/spec-scaffold/instructions/contradiction-framing.md`; `internal/scaffold/home/starter/skills/global/kg-ideate/spec-scaffold/instructions/kg-queries.md`

### `layered-pr-fanout` — Layered PR fanout: awaiting_review status + slot/dep accounting + blocked-on

- status: **active** · current_focus_task: (none set) · tasks: 7 (0 not-done) · path: `.agents/workflow/plans/layered-pr-fanout/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `lpf-a-status-enum` | awaiting_review task status + sub-statuses + advance validator + max_parallel default | completed | ∅ | go-cli | Y | Adds awaiting_review + awaiting_agent_review/awaiting_owner_review to the status vocabulary; makes `advance --status` accept them with §3.1 transition validation; centralizes status constants; defers the max_parallel_tasks schema field. |
| `lpf-b-slot-dep-accounting` | Slot accounting (§2.8) + eligibility dep-satisfaction for awaiting_owner_review | completed | lpf-a-status-enum | go-cli | Y | Slot accounting per §2.8 (in_progress + awaiting_agent_review hold; awaiting_owner_review + blocked-on free); upstream in {completed, awaiting_owner_review} satisfies a downstream dep, in_progress does not; adds a separate blocked ledger bucket. |
| `lpf-c-blocked-on-state` | blocked-on:<ref> parameterized state + auto-resume predicates | completed | lpf-a-status-enum | go-cli | Y | blocked-on:<ref> frees the slot and is tracked in the blocked bucket, with auto-resume predicate evaluators (task/secret/decision/condition) and a staleness-decay annotation. |
| `lpf-d-base-resolution` | Layered base-resolution algorithm in fanout (§4) | completed | lpf-a-status-enum, lpf-pr-producer | go-cli | Y | fanout computes base_branch from the union of a task's depends_on PR branches (lineage-aware) rather than always master; reworks #214 to consume the internal/events producer instead of the bespoke ghPRLister seam. |
| `lpf-e-transitions-poll` | Verifier/lens/owner transition split + PR poll detector (§6, §3.2) | completed | lpf-b-slot-dep-accounting, lpf-pr-producer | go-cli | Y | Splits verifier/lens-gate/owner-merge transitions per §6 and adds a PR poll detector that consumes event.pr.* (no direct gh) mapping merge/close/request-change/force-rebase to transitions; wires auto-resume predicate evaluators. |
| `lpf-event-foundation` | Unified event contract core: envelope + kind registry + dispatch + producer engine | completed | ∅ | go-cli | Y | New internal/events package: common envelope, kind registry with per-kind disposition (reject/soft), table-driven dispatch, a generic exec/http producer engine (no per-platform files), and a ~100-line built-in JSONPath subset. |
| `lpf-pr-producer` | PR as first event: gh pr_source config + event.pr.* registrations + AuthRoundTripper seam | completed | lpf-event-foundation | go-cli | Y | PR support = config + kind registrations + reactions on the event foundation: canonical PR payload, event.pr.* control-plane kinds, default gh pr_source config block, pr_source AgentsRC field + schema, and the AuthRoundTripper seam with direct-load fallback. |

### `loop-discipline-stop-hooks` — Loop-Discipline Stop Hooks

- status: **active** · current_focus_task: `Final migration of legacy downstream discipline-skill overrides` · tasks: 17 (4 not-done) · path: `.agents/workflow/plans/loop-discipline-stop-hooks/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `p0-sentinel-cli` | Sentinel CLI: write/read/clear under 'da workflow hook-sentinel' | completed | ∅ | — | Y | Introduce sentinel JSON schema v1 + `da workflow hook-sentinel {write,read,clear}`; archive successful sentinels under durable plan history rather than active state. |
| `p1a-mapper-extensions` | Platform mapper extensions: Codex SubagentStop, Copilot agentStop, Cursor subagentStop | completed | p0-sentinel-cli | — | Y | Extend gate-critical platform mappings only - Codex SubagentStop, Copilot agentStop/subagentStop, Cursor subagentStop (Copilot stop is agentStop, not stop). |
| `p1b-canonical-when-values` | New canonical HookSpec.When values from Cursor wider surface and May 2026 refresh | completed | p1a-mapper-extensions | — | Y | Add event-parity When values from R6 and D3/D5 after critical mappings land; document one-to-one mapping, Copilot agentStop, approved non-terminal behavior, and observation-only candidates. |
| `p1c-matcher-verification` | Per-event input/matcher verification and when_events schema extension | completed | p1a-mapper-extensions | — | Y | Add a backward-compatible `when_events` after verifying inputs, matchers, native remediation outputs, and approved PreToolUse/SubagentStart/PreCompact capabilities from official docs; Cursor uses followup_message. |
| `p1d-claude-lifecycle-parity` | Claude documented lifecycle event parity beyond gate-critical events | completed | p1b-canonical-when-values | — | Y | Bring Claude's officially documented wider lifecycle event surface into canonical HookSpec coverage without attaching new gates by implication. |
| `p2-hook-scripts` | Three HOOK.yaml + gate.sh bundles under internal/scaffold/hooks/global/ | completed | p0-sentinel-cli, p1a-mapper-extensions, p1b-canonical-when-values, p1c-matcher-verification | — | Y | Add three canonical hook bundles (iteration-close-gate/isp-gate/loop-worker-gate) + scaffold-copy assertions; ship terminal validation, PreToolUse prevention, SubagentStart bootstrap, PreCompact continuity, with hard outcomes requiring observable evidence + native remediation. |
| `p3-starter-promotion` | Promote iteration-close, isp, loop-worker skills + AGENT.md + loop-worker profile into starter | completed | ∅ | — | Y | Copy iteration-close/isp/loop-worker skills + loop-worker agent AGENT.md + profile into the starter and extend TestCopyMissingStarterAssetsCopiesStarterBundle; no loader code expected. |
| `p3b-companion-discipline-skills` | Scaffold and assess agent-handoff and delegation-lifecycle discipline companions | completed | p2-hook-scripts, p3-starter-promotion | — | Y | Ship complete starter assets for agent-handoff + delegation-lifecycle companion skills and assess whether any evidence-backed hook/continuity advice is justified; route post-tool observation questions to R1.5, no prose-only enforcement. |
| `p4-sentinel-wiring` | Wire 'da workflow hook-sentinel write' call into the three starter skills | completed | p0-sentinel-cli, p3-starter-promotion | — | Y | Add a sentinel-write at each skill's entry (after prerequisites to construct its args, before governed work) in the three starter skills; update gotchas. |
| `p5-e2e-integration` | End-to-end integration test: native remediation and advisory behavior | completed | p2-hook-scripts, p4-sentinel-wiring | — | Y | Shell smoke test in tests/test-loop-discipline-stop-hooks.sh asserting terminal + PreToolUse native remediation, SubagentStart/PreCompact non-blocking behavior, JSON block output, Cursor followup output, advisories (merged via PR #124). |
| `p6-payout-backfill` | Explicit payout migration and active-loop readback | completed | p5-e2e-integration, p3b-companion-discipline-skills | — | Y | Authorized payout config migration + readback (payout at ~/proj-docs/payout): opt payout into hooks/isp/loop-worker and bring shipped config-architecture benefits (unified-config-profiles, distributable manifest/init --from, home-config machine-local split, layered lock-pinning); anticipate L4 multi-harness. |
| `p7-legacy-override-migration` | Final migration of legacy downstream discipline-skill overrides | pending | p6-payout-backfill, p1d-claude-lifecycle-parity | — | Y | Final plan task: inventory managed downstream legacy overrides, migrate compatible copies to starter inheritance, and record retained intentional project variants. |
| `p1e-docs-hooks-consistency` | docs/HOOKS.md consistency pass after P1b/P1c/P1d additions | completed | p1b-canonical-when-values, p1c-matcher-verification, p1d-claude-lifecycle-parity | research | Y | Final consistency pass over docs/HOOKS.md: verify the mapping table, when_events syntax examples, per-platform native-output contracts, and Cursor-wider surface section all reconcile; tables sorted/deduped, each When value in exactly one row. |
| `p3-pre-snapshot` | Source-skill snapshot to prevent drift during P3/P3b/P4 implementation | completed | ∅ | research | Y | Record git SHAs of each source skill tree at task start so P3/P3b/P4 copy from the recorded snapshot rather than the moving head; no code change, just the snapshot record. |
| `p6-rollback-plan` | P6 payout-migration rollback documentation | pending | p6-payout-backfill | research | n | Document a 1-command rollback for P6's payout migration (e.g. git checkout HEAD -- .agentsrc.json && da refresh); operator-facing, cross-linked from the P6 readback artifact. |
| `r1-5-t4-integration` | r1-5 plan integration test (cross-plan coordination acknowledgment) | pending | p5-e2e-integration | research | n | Coordination artifact documenting the handoff between P2's gate.sh outcomes and r1-5's iter-N.hook-outcomes.yaml sidecar write; reserve a clean exit-emit boundary in gate.sh for r1-5's outcome-emit call. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the loop-discipline-hooks minor | pending | p0-sentinel-cli, p1a-mapper-extensions, p1b-canonical-when-values, p1c-matcher-verification, p1d-claude-lifecycle-parity, p2-hook-scripts, p3-starter-promotion, p3b-companion-discipline-skills, p4-sentinel-wiring, p5-e2e-integration, p6-payout-backfill, p7-legacy-override-migration, p1e-docs-hooks-consistency, p3-pre-snapshot, p6-rollback-plan, r1-5-t4-integration | release | Y | Release-gated tail (MINOR path): bump VERSION to the resolved next minor (do NOT hardcode; ~0.4.0 from 0.3.2) and finalize the CHANGELOG section; new CLI + starter hook/skill contracts are additive surface. |

**Not-done task write_scope & readiness:**

- `p7-legacy-override-migration` [pending] **READY**: `.agents/history/loop-discipline-stop-hooks/downstream-override-migration.md`
- `p6-rollback-plan` [pending] **READY**: `.agents/history/loop-discipline-stop-hooks/payout-migration-rollback.md`
- `r1-5-t4-integration` [pending] **READY**: `.agents/history/loop-discipline-stop-hooks/r1-5-coordination.md`
- `release-minor` [pending] **BLOCKED — waits on loop-discipline-stop-hooks/p7-legacy-override-migration[pending], loop-discipline-stop-hooks/p6-rollback-plan[pending], loop-discipline-stop-hooks/r1-5-t4-integration[pending]**: `VERSION`; `CHANGELOG.md`

### `managed-gitignore-autofill` — Wire D14/R8 managed-gitignore auto-fill for per-platform generated outputs

- status: **active** · current_focus_task: `wire-managed-gitignore-autofill` · tasks: 1 (1 not-done) · path: `.agents/workflow/plans/managed-gitignore-autofill/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `wire-managed-gitignore-autofill` | Wire EnsureManagedGitignore into da refresh + retire #381 ad-hoc root rules + fix .agentsrc.lock contract | pending | ∅ | go-cli | Y | Wire EnsureManagedGitignore into `da refresh` so every enabled platform's generated outputs (incl. copilot .github/hooks/*.json) land in the managed .gitignore block; retire #381's ad-hoc root rules; fix the .agentsrc.lock neverIgnored contract (track/commit it) and correct the classify-generated-files-before-cleanup lesson. |

**Not-done task write_scope & readiness:**

- `wire-managed-gitignore-autofill` [pending] **READY**: `internal/links/gitignore.go`; `internal/links/gitignore_test.go`; `internal/platform/platform.go`; `internal/platform/copilot.go`; `commands/refresh.go`; `commands/refresh_test.go`; `.gitignore`; `.agents/lessons/classify-generated-files-before-cleanup/LESSON.md`; `.agents/lessons/index.md`

### `platform-driven-diagnostics` — Platform-driven diagnostics (doctor + status delegate to internal/platform sister interfaces)

- status: **active** · current_focus_task: (none set) · tasks: 8 (0 not-done) · path: `.agents/workflow/plans/platform-driven-diagnostics/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `p0-types-and-helpers` | P0: BrokenLink/PlatformBadge types + sister interfaces + ScanSingleFileLinks/ScanSymlinkDir helpers | completed | ∅ | go-cli | Y | Introduces BrokenLink/PlatformBadge types, the sister interfaces, and Scan helpers. Shipped via PR #118; marker task advanced to completed on creation. |
| `p1-broken-links-cursor-claude` | P1: BrokenLinkReporter for cursor + claude | completed | p0-types-and-helpers | go-cli | Y | cursor + claude implement BrokenLinkReporter; doctor.collectBrokenLinks delegates for those two; moves HasMultipleHardLinks from commands/lifecycle into internal/platform. |
| `p2-broken-links-remaining` | P2: BrokenLinkReporter for codex/copilot/opencode | completed | p1-broken-links-cursor-claude | go-cli | Y | codex/copilot/opencode implement BrokenLinkReporter; collectBrokenLinks collapses to a range over platform.All(); the singleFiles table is deleted from doctor.go. |
| `p3-badge-and-countlinks` | P3: Badge + CountLinks across 5 platforms | completed | p2-broken-links-remaining | go-cli | Y | Badge() + CountLinks() per platform; status's five *TextBadge helpers + collectProjectPlatforms become one loop; doctor's countProjectLinks sums across platforms. |
| `p4-orphan-canonicals-and-userconfig` | P4: OrphanCanonicalReporter + UserConfigReporter | completed | p3-badge-and-countlinks | go-cli | Y | OrphanCanonicalReporter (claude+codex) + UserConfigReporter (claude+codex+opencode); collectOrphanCanonicals + collectUserConfigPlatforms reduce to platform iteration. |
| `p5-audit-printer-and-cleanup` | P5: AuditPrinter + delete per-platform helpers | completed | p4-orphan-canonicals-and-userconfig | go-cli | Y | Per-platform printAudit helpers move to AuditPrinter impls; printAudit becomes a filter-respecting loop; all per-platform-by-name helpers deleted (merged via PR #132). |
| `p6-proof-of-abstraction` | P6: Contract test + stub-platform proof | completed | p5-audit-printer-and-cleanup | go-cli | Y | Contract test asserts every platform.All() entry implements every sister interface it advertises; a noopPlatform stub proves the abstraction (merged via PR #202, commit 8c42c028). |
| `release-record-0.3.3-fold` | Release record: capability folds into 0.3.3 (no separate bump) | completed | p0-types-and-helpers, p1-broken-links-cursor-claude, p2-broken-links-remaining, p3-badge-and-countlinks, p4-orphan-canonicals-and-userconfig, p5-audit-printer-and-cleanup, p6-proof-of-abstraction | release | Y | Release-gated tail; the doctor/status capability already shipped under PATCH 0.3.3 (#145), so this is a no-op release record with no separate version bump (P4-P6 are pure internal cleanup feeding the patch train). |

### `r1-5-hook-enforcement-telemetry` — R1.5: Hook enforcement telemetry

- status: **active** · current_focus_task: `Release: bump VERSION + finalize CHANGELOG for the hook-telemetry minor` · tasks: 12 (1 not-done) · path: `.agents/workflow/plans/r1-5-hook-enforcement-telemetry/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `t0-outcome-contract` | Define durable hook-outcome schema and archived-sentinel linkage | completed | ∅ | — | Y | Define allow/advise/remediate + lifecycle/intervention hook-outcome schema linked to archived sentinels, no transcript content; start after loop-discipline contracts stable. |
| `t1-capture-outcomes` | Persist hook gate outcomes alongside archived sentinel history | completed | t0-outcome-contract | — | Y | Persist gate outcomes alongside archived sentinel history, consuming the completed loop-discipline gate bundles. |
| `t1b-post-tool-observation-evaluation` | Evaluate PostToolUse and PostToolUseFailure as bounded workflow feedback | completed | t1-capture-outcomes | — | Y | Evaluate PostToolUse/Failure payload stability, workflow-command filtering, redaction, and prevention/remediation dedup before any observation scores; non-blocking by default. |
| `t2-scoring-signal` | Add objective hook-outcome signal to scoring | completed | t1b-post-tool-observation-evaluation | — | Y | Add an explainable per-rule hook-outcome objective signal to scoring with a deliberate rubric version bump. |
| `t3-cli-readback` | Render hook outcome contribution in score queries | completed | t2-scoring-signal | — | Y | Render hook-outcome source + rule identifiers in score queries without printing transcript contents. |
| `t-schema-validator-wiring` | Wire hook-outcome schema into the workflow validator pipeline | completed | t0-outcome-contract | go-cli | Y | Register the hook-outcome schema in the bundled validator list and reject records with any disallowed transcript-content field (D2). |
| `t2c-rubric-version-coordination` | Resolve RubricVersion ordering with r5-review-labeling-access | completed | t2-scoring-signal | research | n | Resolve RubricVersion ordering vs the r5 plan at execution time; document the concurrent-plan ordering policy in the rubric doc (D5). |
| `t2d-companion-gate-compatibility` | Verify companion gates emit R1.5-compatible records without schema change | completed | t2-scoring-signal | go-cli | Y | Contribute fixtures for orchestrator-handoff/delegation-closeout gate outcomes and assert the extractor+scorer handle them with no schema change. |
| `t-archival-policy` | Decide hook-outcome sidecar retention and archival | completed | t1-capture-outcomes | research | n | Decide sidecar retention (default never auto-prune) and spec an admin-only prune command; design + docs only (Q3). |
| `t-e2e-integration-test` | End-to-end test exercising sentinel → gate → sidecar → score | completed | t2-scoring-signal, t3-cli-readback | go-cli | Y | Shell integration test: sentinel → gate → sidecar → score, asserting the remediate hook_outcomes contribution row appears. |
| `t-docs` | Update OUTCOME_SCORING_RUBRIC.md and HOOKS.md for R1.5 | completed | t2-scoring-signal, t-archival-policy | research | n | Consolidate all R1.5 doc changes: new signal spec, weight rebalance, ordering + archival policies, approved-rule list, HOOKS.md cross-ref. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the hook-telemetry minor | pending | t0-outcome-contract, t1-capture-outcomes, t1b-post-tool-observation-evaluation, t2-scoring-signal, t3-cli-readback, t-schema-validator-wiring, t2c-rubric-version-coordination, t2d-companion-gate-compatibility, t-archival-policy, t-e2e-integration-test, t-docs | release | Y | Release-gated tail: bump VERSION to resolved next minor and finalize CHANGELOG for the hook-telemetry minor; depends on loop-discipline gates shipping first. |

**Not-done task write_scope & readiness:**

- `release-minor` [pending] **READY**: `VERSION`; `CHANGELOG.md`

### `r2-observability-dashboard` — R2: Real-time observability and evaluation dashboard

- status: **active** · current_focus_task: `Wire /api/v1/observability/events SSE handler to broker` · tasks: 18 (11 not-done) · path: `.agents/workflow/plans/r2-observability-dashboard/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `design-doc` | Resolve R2 open questions (frontend, storage, real-time) in design.md | completed | ∅ | research | n | Resolve R2 open questions: Vite/React/TS/TanStack/Tailwind frontend, read-through disk store + LRU, SSE push, REST/JSON API, 4 routes. |
| `t01-api-contract-design` | Design dashboard API contract (endpoints, payload shapes, JSON Schemas) | completed | design-doc | — | n | Design the dashboard API contract (endpoints, DTO projection, SSE taxonomy, error envelope) + four entity JSON Schemas; shipped via c74d8d8b. |
| `t02-storage-read-layer` | Build dashboard.Store read-through layer over iter-log + score sidecars | completed | t01-api-contract-design | go-cli | Y | Build dashboard.Store read-through over iter-log + score sidecars with a small LRU cache; no writes, no recompute. |
| `t03-handlers-rest` | Implement HTTP handlers for the REST API per the contract | completed | t01-api-contract-design, t02-storage-read-layer | go-cli | Y | Implement pure stdlib REST handlers against Store/Events interfaces with golden + JSON-schema contract tests; browser-edge HTTP surface, wiring lives elsewhere. |
| `t04-sse-broker` | Build SSE event broker (subscribe / publish / fan-out) | completed | t01-api-contract-design | go-cli | Y | Tiny in-process SSE broker (subscribe/publish/fan-out) bound to R3's EventBus interface; disconnect-on-overflow bounded-buffer policy, 15s heartbeat. |
| `t05-sse-handler` | Wire /api/v1/observability/events SSE handler to broker | pending | t03-handlers-rest, t04-sse-broker | go-cli | Y | Wire GET /api/v1/observability/events SSE handler to the broker, emitting event-stream frames with padding + context-cancel handling. |
| `t06-recompute-on-miss-and-fswatch` | Score-recompute-on-miss + iter-log filesystem watcher → broker bridge | completed | t02-storage-read-layer, t04-sse-broker | go-cli | Y | Score-recompute-on-miss in Store + fsnotify iter-log watcher bridging change events to the broker (v1 pre-R3 push path). |
| `t07-standalone-service-bin` | Standalone dashboard service (cmd/da-dashboard) for dev + pre-R3 use | pending | t03-handlers-rest, t05-sse-handler, t06-recompute-on-miss-and-fswatch | go-cli | Y | Standalone cmd/da-dashboard binary wiring Store+Broker+Handlers+Watcher into an http.Server for dev and pre-R3 use, with SPA fallback. |
| `t08-frontend-skeleton` | Vite + React + TS skeleton with routing, API client, query layer | completed | t01-api-contract-design | web | Y | Vite/React/TS skeleton with routing, generated-from-schema types, API client, TanStack Query; in-memory access token per owner auth ruling (#286). |
| `t09-feature-aggregate-view` | Aggregate-dashboard view (runs grid, score trend, cache trend) | pending | t08-frontend-skeleton, t03-handlers-rest | web | Y | Aggregate view: sortable/filterable runs grid + score and cache-hit-rate trend charts with loading/empty/error states. |
| `t10-feature-per-run-drilldown` | Per-run drill-down view (iteration timeline + breakdown + integrity) | pending | t08-frontend-skeleton, t03-handlers-rest | web | Y | Per-run drill-down: iteration timeline + per-signal breakdown table + integrity panel; deep-linkable iteration detail. |
| `t11-frontend-sse-integration` | Connect frontend to /api/v1/observability/events and invalidate queries on events | pending | t08-frontend-skeleton, t05-sse-handler | web | Y | Reconnecting EventSource client + useLiveUpdates hook invalidating the right queries per event topic, with backoff + full refetch on reconnect. |
| `t12-e2e-smoke-live-iteration` | End-to-end smoke — live iteration triggers UI update within 2s | pending | t07-standalone-service-bin, t09-feature-aggregate-view, t10-feature-per-run-drilldown, t11-frontend-sse-integration | go-cli | Y | E2E smoke (Go SSE integration + Playwright UI) proving a live iteration updates the UI within 2s; both behind the e2e build tag. |
| `t13-r3-mount-integration` | Replace fswatch primary with R3 publish primitive; mount handlers in R3 service | pending | t03-handlers-rest, t05-sse-handler, t06-recompute-on-miss-and-fswatch | go-cli | Y | Mount handlers under /api on R3's HTTP edge and bridge R3's EventBus into the broker, demoting fswatch to fallback; gated on R3 event-bus/seam/http-server milestones. |
| `t14-documentation` | Documentation — operator guide, API reference, frontend dev guide | pending | t12-e2e-smoke-live-iteration, t13-r3-mount-integration | docs | n | Operator guide, generated-from-schema API reference, frontend dev guide, and a README mention for the dashboard. |
| `t15-rubric-versioning-ui` | Rubric explainer view (read /api/rubric, render weights + bands) | pending | t08-frontend-skeleton, t03-handlers-rest | web | Y | Rubric explainer view rendering active rubric version/signals/bands with a per-iteration version-mismatch info pill (D3). |
| `t16-coverage-gate-and-ci` | Coverage gate + CI wiring for new dashboard packages | pending | t12-e2e-smoke-live-iteration | go-cli | Y | Coverage floor + CI job for new dashboard Go/web packages plus a build check; Playwright e2e gated to manual/nightly. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the observability-dashboard minor | pending | design-doc, t01-api-contract-design, t02-storage-read-layer, t03-handlers-rest, t04-sse-broker, t05-sse-handler, t06-recompute-on-miss-and-fswatch, t07-standalone-service-bin, t08-frontend-skeleton, t09-feature-aggregate-view, t10-feature-per-run-drilldown, t11-frontend-sse-integration, t12-e2e-smoke-live-iteration, t13-r3-mount-integration, t14-documentation, t15-rubric-versioning-ui, t16-coverage-gate-and-ci | release | Y | Release-gated tail: bump VERSION to resolved next minor and finalize CHANGELOG for the observability-dashboard minor. |

**Not-done task write_scope & readiness:**

- `t05-sse-handler` [pending] **READY**: `internal/dashboard/handlers/handlers.go`; `internal/dashboard/handlers/stream.go`; `internal/dashboard/handlers/stream_test.go`
- `t07-standalone-service-bin` [pending] **BLOCKED — waits on r2-observability-dashboard/t05-sse-handler[pending]**: `cmd/da-dashboard/`; `internal/dashboard/server/`
- `t09-feature-aggregate-view` [pending] **READY**: `web/dashboard/src/views/AggregateView.tsx`; `web/dashboard/src/views/AggregateView.test.tsx`; `web/dashboard/src/components/RunsGrid.tsx`; `web/dashboard/src/components/RunsGrid.test.tsx`; `web/dashboard/src/components/ScoreTrendChart.tsx`; `web/dashboard/src/components/ScoreTrendChart.test.tsx`; `web/dashboard/src/components/CacheTrendChart.tsx`; `web/dashboard/src/components/CacheTrendChart.test.tsx`
- `t10-feature-per-run-drilldown` [pending] **READY**: `web/dashboard/src/views/RunDetailView.tsx`; `web/dashboard/src/views/RunDetailView.test.tsx`; `web/dashboard/src/views/IterationDetailView.tsx`; `web/dashboard/src/views/IterationDetailView.test.tsx`; `web/dashboard/src/components/IterationTimeline.tsx`; `web/dashboard/src/components/IterationTimeline.test.tsx`; `web/dashboard/src/components/SignalBreakdown.tsx`; `web/dashboard/src/components/SignalBreakdown.test.tsx`; `web/dashboard/src/components/IntegrityPanel.tsx`; `web/dashboard/src/components/IntegrityPanel.test.tsx`
- `t11-frontend-sse-integration` [pending] **BLOCKED — waits on r2-observability-dashboard/t05-sse-handler[pending]**: `web/dashboard/src/api/eventStream.ts`; `web/dashboard/src/api/eventStream.test.ts`; `web/dashboard/src/hooks/useLiveUpdates.ts`; `web/dashboard/src/hooks/useLiveUpdates.test.ts`
- `t12-e2e-smoke-live-iteration` [pending] **BLOCKED — waits on r2-observability-dashboard/t07-standalone-service-bin[pending], r2-observability-dashboard/t09-feature-aggregate-view[pending], r2-observability-dashboard/t10-feature-per-run-drilldown[pending], r2-observability-dashboard/t11-frontend-sse-integration[pending]**: `tests/e2e/dashboard_live_iteration_test.go`; `web/dashboard/playwright.config.ts`; `web/dashboard/e2e/live-iteration.spec.ts`
- `t13-r3-mount-integration` [pending] **BLOCKED — waits on r2-observability-dashboard/t05-sse-handler[pending]**: `internal/dashboard/server/r3mount.go`; `internal/dashboard/server/r3mount_test.go`; `internal/dashboard/events/r3bridge.go`; `internal/dashboard/events/r3bridge_test.go`
- `t14-documentation` [pending] **BLOCKED — waits on r2-observability-dashboard/t12-e2e-smoke-live-iteration[pending], r2-observability-dashboard/t13-r3-mount-integration[pending]**: `docs/OBSERVABILITY_DASHBOARD.md`; `docs/DASHBOARD_API.md`; `web/dashboard/README.md`; `README.md`
- `t15-rubric-versioning-ui` [pending] **READY**: `web/dashboard/src/views/RubricView.tsx`; `web/dashboard/src/views/RubricView.test.tsx`; `web/dashboard/src/components/RubricTable.tsx`; `web/dashboard/src/components/RubricTable.test.tsx`
- `t16-coverage-gate-and-ci` [pending] **BLOCKED — waits on r2-observability-dashboard/t12-e2e-smoke-live-iteration[pending]**: `.github/workflows/dashboard.yml`; `internal/dashboard/coverage_test.go`; `web/dashboard/.github/workflows/test.yml`
- `release-minor` [pending] **BLOCKED — waits on r2-observability-dashboard/t05-sse-handler[pending], r2-observability-dashboard/t07-standalone-service-bin[pending], r2-observability-dashboard/t09-feature-aggregate-view[pending], r2-observability-dashboard/t10-feature-per-run-drilldown[pending], r2-observability-dashboard/t11-frontend-sse-integration[pending], r2-observability-dashboard/t12-e2e-smoke-live-iteration[pending], r2-observability-dashboard/t13-r3-mount-integration[pending], r2-observability-dashboard/t14-documentation[pending], r2-observability-dashboard/t15-rubric-versioning-ui[pending], r2-observability-dashboard/t16-coverage-gate-and-ci[pending]**: `VERSION`; `CHANGELOG.md`

### `r3-background-worker-service` — R3: Background-worker service

- status: **active** · current_focus_task: ``da service run|status|stop` cobra subcommand` · tasks: 11 (3 not-done) · path: `.agents/workflow/plans/r3-background-worker-service/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `design-doc` | Resolve R3 open questions in a per-plan design.md | completed | ∅ | research | n | Resolve R3 open questions: `da service` cobra subcommand hosting a minimal in-process scheduler with fsnotify triggers and per-task watermarks. |
| `scheduler-core` | In-process scheduler with interval + fsnotify triggers | completed | design-doc | go-cli | Y | In-process scheduler (Register/Start/Stop) with interval + fsnotify triggers, per-task state tracking, recovered panics; fsnotify dep added. |
| `event-bus` | In-process pub/sub bus for task → consumer events | completed | design-doc | go-cli | Y | In-process pub/sub bus (Publish/Subscribe) with bounded per-subscriber buffers, drop-oldest, topic constants; ephemeral, no durable storage. |
| `http-server` | Service transport layer — UDS/named-pipe control plane + HTTP/SSE edge (mounts for R2/R5, /healthz, /api/tasks) | completed | design-doc, scheduler-core, event-bus, event-bus-interface-seam | go-cli | Y | Transport layer: UDS/named-pipe control plane (peer-cred stop auth) + HTTP/SSE edge with /healthz, /api/tasks, and RegisterMount for R2/R5; Windows pipe stubbed fail-fast. |
| `tasks-iterlog-ingester` | Iter-log ingester task — fsnotify trigger, calls R1 scorer, publishes event | completed | scheduler-core, event-bus, event-bus-interface-seam | go-cli | Y | Fsnotify ingester task calling internal/scoring, writing the sidecar, publishing IterationScored via the EventBus interface, tracking a restart-safe watermark. |
| `tasks-rescore` | Rescore-on-rubric-bump task — interval trigger, rescores when RubricVersion changes | completed | scheduler-core, event-bus, event-bus-interface-seam | go-cli | Y | Interval task rescoring all records only on RubricVersion delta, writing sidecars, publishing RescoreDone, persisting watermark before publish. |
| `service-runtime` | Service runtime that composes scheduler + http + bus + tasks | completed | http-server, tasks-iterlog-ingester, tasks-rescore | go-cli | Y | Run(ctx, Config) composing bus + scheduler + tasks + transport with clean dual-listener shutdown; integration-tested end to end. |
| `cobra-surface` | `da service run\|status\|stop` cobra subcommand | pending | service-runtime | go-cli | Y | `da service run\|status\|stop` cobra commands over the control socket with -d/--detach + pidfile and graceful SIGINT shutdown. |
| `docs-and-verification` | Operator docs + umbrella verification (status, restart-preserves-state) | pending | cobra-surface | go-cli | Y | docs/SERVICE.md + umbrella integration test proving status/health, restart-preserves-state, and events to stubbed R2/R5 consumers. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the da service minor | pending | design-doc, scheduler-core, event-bus, event-bus-interface-seam, http-server, tasks-iterlog-ingester, tasks-rescore, service-runtime, cobra-surface, docs-and-verification | release | Y | Release-gated tail: bump VERSION to resolved next minor and finalize CHANGELOG for the da service minor. |
| `event-bus-interface-seam` | EventBus interface seam over the shipped concrete events.Bus (spec D4.1) | completed | event-bus | go-cli | Y | Define the D4.1 EventBus interface over the concrete Bus as builtin backend, ship a G1-G4 conformance suite; no external adapters (OQ6 interface-only). |

**Not-done task write_scope & readiness:**

- `cobra-surface` [pending] **READY**: `commands/service/`; `commands/root.go`
- `docs-and-verification` [pending] **BLOCKED — waits on r3-background-worker-service/cobra-surface[pending]**: `docs/SERVICE.md`; `internal/service/service_integration_test.go`
- `release-minor` [pending] **BLOCKED — waits on r3-background-worker-service/cobra-surface[pending], r3-background-worker-service/docs-and-verification[pending]**: `VERSION`; `CHANGELOG.md`

### `r4-code-task-generation-eval` — R4: Code-task generation and evaluation harness

- status: **active** · current_focus_task: `Release: bump VERSION + finalize CHANGELOG for the eval-harness minor` · tasks: 19 (1 not-done) · path: `.agents/workflow/plans/r4-code-task-generation-eval/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `design-doc` | Resolve R4 open questions (corpus, sandbox, languages) in design.md | completed | ∅ | research | n | Resolved R4 open questions: hybrid KG corpus, worktree sandbox behind interface, Go/Python/TS, versioned TaskSpec, CLI-first. |
| `taskspec-and-registry` | Define versioned TaskSpec, generator interface, language registry | completed | design-doc | go-cli | Y | Typed versioned TaskSpec v1 + Generator interface + per-language registry; pure data/interfaces, YAML round-trip. Shipped (commit aa12da79). |
| `kg-query-layer` | KG query helpers for the task generator | completed | taskspec-and-registry | go-cli | Y | Read-only adapter over graphstore (SeedSymbols/NeighborhoodFor/ComplexityProxy); NeighborhoodFor traverses ALL edge kinds per recorded ruling. |
| `difficulty-derivation` | KG-derived difficulty signals + bucketing | completed | kg-query-layer | go-cli | Y | Pure fn: neighborhood node/edge counts + cyclomatic proxy to easy/medium/hard + signals map; table-driven tests pin buckets. |
| `generator-go` | Go task generator (per-language adapter | completed | kg-query-layer, difficulty-derivation | go-cli | Y | First adapter; impl-pure-fn/refactor-extract/add-test-coverage templates; KG query picks seed symbol to synthesize prompt + verify cmd to TaskSpec. |
| `generator-python` | Python task generator (per-language adapter | completed | kg-query-layer, difficulty-derivation, generator-go | go-cli | Y | Same template surface as Go with pytest/unittest verification; reuses registry after Go stabilizes the interface. |
| `generator-typescript` | TypeScript task generator (per-language adapter | completed | kg-query-layer, difficulty-derivation, generator-go | go-cli | Y | Third language; adds tsc --noEmit + node --test/vitest; closes the >=3-language gate. |
| `sandbox-worktree` | Worktree sandbox primitive (Sandbox interface + v1 impl) | completed | taskspec-and-registry | go-cli | Y | Sandbox interface + worktreeSandbox v1 (scratch repo, ephemeral branch, temp HOME); concurrent-isolation tested; documented Docker swap point. |
| `verifier-go` | Go verifier (build + test) inside sandbox | completed | sandbox-worktree, generator-go | go-cli | Y | Runs build/test cmds inside sandbox workdir, captures pass/fail + stdout/stderr + duration into typed VerifyResult with context timeout. |
| `verifier-python` | Python verifier | completed | sandbox-worktree, generator-python, verifier-go | go-cli | Y | Mirrors verifier-go for pytest/unittest; sequenced after Go so VerifyResult shape stabilizes first. |
| `verifier-typescript` | TypeScript verifier | completed | sandbox-worktree, generator-typescript, verifier-go | go-cli | Y | tsc --noEmit + node --test/vitest; closes the per-language verifier trio. |
| `agent-runner` | AgentRunner interface + claude / codex bindings | completed | sandbox-worktree | go-cli | Y | AgentRunner abstraction over CLI agents (claude/codex) against sandbox workdir; fakeRunner for tests + real shell-out runners capturing token/cache telemetry. |
| `scoring-bridge` | Emit R1-shaped iteration record + invoke scoring pipeline | completed | taskspec-and-registry | go-cli | Y | Translates EvalRun to IterationRecord/Objectives + calls existing scoring pipeline against eval-namespaced iter-log; introduces no new rubric path/version. |
| `harness-driver` | End-to-end harness wiring (gen -> sandbox -> run -> verify -> score) | completed | generator-go, verifier-go, sandbox-worktree, agent-runner, scoring-bridge | go-cli | Y | Single Harness.Run(ctx, opts) entry point sequencing all five stages; pure orchestration; integration-tested against fakeRunner + verifier-go on a fixture KG. |
| `persistence` | EvalRun + TaskSpec sidecar persistence | completed | harness-driver | go-cli | Y | Atomic-rename sidecar layout under .agents/eval/runs/<run-id>/ (taskspec, eval-run, iter-log, score); tests pin the layout as a stable R2 contract. |
| `cli-eval` | `da eval` CLI command tree | completed | harness-driver, persistence | go-cli | Y | `da eval gen\|run\|ls` command tree mirroring `da score` idioms; `da eval run --language go` producing an R1-scored outcome is the hard test. |
| `r2-visibility-contract` | R2 visibility contract - documented join + fixture | completed | persistence | go-cli | Y | Documents the on-disk join R2 consumes (eval-run -> iter-N.score -> TaskSpec) + ships a frozen fixture run; no code in R2 itself. |
| `docs` | docs/EVAL_HARNESS.md + rubric-cross-link | completed | cli-eval, r2-visibility-contract | go-cli | n | Full EVAL_HARNESS.md (TaskSpec schema, coverage, sandbox model, adding a fourth language) + cross-link from OUTCOME_SCORING_RUBRIC.md. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the eval-harness minor | pending | design-doc, taskspec-and-registry, kg-query-layer, difficulty-derivation, generator-go, generator-python, generator-typescript, sandbox-worktree, verifier-go, verifier-python, verifier-typescript, agent-runner, scoring-bridge, harness-driver, persistence, cli-eval, r2-visibility-contract, docs | release | Y | Release-gated MINOR tail; bump VERSION to next minor at ready-time + finalize CHANGELOG eval-harness section (fires auto-release.yml). |

**Not-done task write_scope & readiness:**

- `release-minor` [pending] **READY**: `VERSION`; `CHANGELOG.md`

### `r5-review-labeling-access` — R5: Human-in-the-loop review, labeling, and access layer

- status: **active** · current_focus_task: `Review surface in R2 (list, inspect, label form, role-aware controls)` · tasks: 11 (4 not-done) · path: `.agents/workflow/plans/r5-review-labeling-access/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `design-doc` | Resolve R5 open question (audit log format + retention) in design.md | completed | ∅ | research | n | Resolved audit-log/retention OQ: YAML sidecar labels, major rubric bump with human_label signal, Bearer-token RBAC via argon2id local users file, JSON-lines SHA-256-chained audit, 5-route HTTP mount into R3. |
| `rbac-identity` | RBAC identity layer (users file, token issuance, Authenticator interface) | completed | design-doc | go-cli | Y | Authenticator interface + LocalUsersAuthenticator (argon2id; reviewer\|admin\|readonly) backed by ~/.config/da/review/users.yaml; token issuance; pure package. Shipped. |
| `label-model` | Label data model + persistence (iter-N.labels.yaml sidecars) | completed | design-doc | go-cli | Y | Label struct + atomic YAML sidecar writing iter-N.labels.yaml adjacent to iter-N.yaml; multi-label per iteration with actor/role/edit-history. Shipped (PR #177). |
| `audit-log` | Append-only chained JSON-lines audit log + verifier | completed | rbac-identity | go-cli | Y | Append-only audit.log.jsonl with SHA-256 prev_hash chaining, Append + Verify, yearly rotation hook; pure file IO, no HTTP. |
| `collection-endpoint` | HTTP route handlers + auth/audit middleware (mounts into R3 server) | completed | rbac-identity, label-model, audit-log | go-cli | Y | HTTPMount (stdlib RegisterMount, no router dep) under /api/reviews with label/audit/users handlers + auth/audit middleware; mounts on R3's HTTP/SSE edge (not UDS). |
| `r1-integration` | Human-label signal + RubricVersion major/minor bump (resolve at task time) | completed | label-model | go-cli | Y | Adds SignalHumanLabel (~0.15) rebalancing the seven signals, ExtractHumanLabelSignals reading labels sidecar, wired through AssembleSignalSet; rubric version resolved at task time (major bump to 3.0.0). |
| `review-ui` | Review surface in R2 (list, inspect, label form, role-aware controls) | pending | collection-endpoint | web | Y | Two React routes /review (filterable, unlabeled-first list) + /review/:iteration (detail + label form); Bearer-token injection; role-aware controls (readonly hides form, admin sees users/audit). |
| `admin-cli` | `da review` CLI for user/audit administration | completed | rbac-identity, audit-log | go-cli | Y | `da review users add\|list\|remove` / `da review audit tail\|verify\|repair\|prune --before-year` nested under existing review group; admin-only, operates on local YAML/JSONL. |
| `e2e-smoke` | End-to-end test - reviewer labels run, label feeds R1 on next recompute | pending | collection-endpoint, r1-integration, review-ui | go-cli | Y | In-process R3 + two seeded users; POST label -> ScoreIteration asserts human_label breakdown row; readonly POST -> 403; audit tamper caught by `da review audit verify`. |
| `docs` | Documentation - REVIEW_LABELING_GUIDE.md, RBAC_AND_AUDIT.md, rubric v3 changelog | pending | collection-endpoint, r1-integration, admin-cli | docs | n | Operator-facing guides: add a reviewer, issue a token, label form semantics, how the label feeds the score, how to verify the audit chain. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the review/labeling minor | pending | design-doc, rbac-identity, label-model, audit-log, collection-endpoint, r1-integration, review-ui, admin-cli, e2e-smoke, docs | release | Y | Release-gated MINOR tail; bump VERSION to next minor + finalize CHANGELOG review/labeling section (fires auto-release.yml). |

**Not-done task write_scope & readiness:**

- `review-ui` [pending] **READY**: `web/dashboard/src/review/`
- `e2e-smoke` [pending] **BLOCKED — waits on r5-review-labeling-access/review-ui[pending]**: `tests/review_e2e/`
- `docs` [pending] **READY**: `docs/REVIEW_LABELING_GUIDE.md`; `docs/REVIEW_RBAC_AND_AUDIT.md`; `docs/OUTCOME_SCORING_RUBRIC.md`
- `release-minor` [pending] **BLOCKED — waits on r5-review-labeling-access/review-ui[pending], r5-review-labeling-access/e2e-smoke[pending], r5-review-labeling-access/docs[pending]**: `VERSION`; `CHANGELOG.md`

### `release-patch-train` — Release patch train (cadence)

- status: **active** · current_focus_task: `release-docs-refresh` · tasks: 2 (2 not-done) · path: `.agents/workflow/plans/release-patch-train/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `release-docs-refresh` | Release: reconcile docs vs code + platform-dirs before the patch bump | pending | ∅ | release | Y | Mandatory pre-cut docs pass (release-docs-refresh skill) reconciling README/docs/**/docs/web + PLATFORM_DIRS_DOCS against code; NEVER touches VERSION/CHANGELOG; recurring predecessor of release-patch. |
| `release-patch` | Release: cadence patch bump + finalize CHANGELOG for bundled fixes | pending | release-docs-refresh | release | Y | Recurring cadence patch bump - bundle patch-eligible merges since last v-tag, increment patch digit, finalize CHANGELOG; fires auto-release; gated only on docs-refresh, never on feature plans. |

**Not-done task write_scope & readiness:**

- `release-docs-refresh` [pending] **READY**: `docs`; `README.md`; `docs/web`
- `release-patch` [pending] **BLOCKED — waits on release-patch-train/release-docs-refresh[pending]**: `VERSION`; `CHANGELOG.md`

### `root-command-decomposition` — Root commands package decomposition

- status: **active** · current_focus_task: (none set) · tasks: 24 (1 not-done) · path: `.agents/workflow/plans/root-command-decomposition/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `t01-design-subpackage-shape` | Design target subpackage shape + shared-symbol contract | completed | ∅ | — | n | Read-mostly design pass producing SHAPE.md: subpackage list (lifecycle + mcp/settings/rules), exported-symbol contract, DI-preservation decision, root-shim deletion policy, per-cluster commit cadence. |
| `t02-introduce-lifecycle-skeleton` | Create commands/lifecycle/ skeleton | completed | t01-design-subpackage-shape | — | Y | Create empty commands/lifecycle/ (doc.go + deps.go mirroring existing deps pattern); no command moves; keeps tests green as a settled target for parallel moves. |
| `t02b-lift-shared-helpers` | Precursor: lift shared lifecycle helpers into commands/lifecycle/ as exported funcs | completed | t02-introduce-lifecycle-skeleton | code-fix | Y | Precursor lifting 15 shared helpers into commands/lifecycle/ as exported funcs (backup/kgmcp/resource_map/project clusters); no behavior change; unblocks t03-t09 command moves. |
| `t03-move-install` | Move install command into commands/lifecycle/ | completed | t02-introduce-lifecycle-skeleton | — | Y | Pure move of install into lifecycle/; root install.go stays wiring NewInstallCmd until t13; verify no new projectsync/platform imports leak into commands/. |
| `t04-move-add-remove` | Move add and remove commands into commands/lifecycle/ | completed | t02-introduce-lifecycle-skeleton | — | Y | Move add + remove (paired via shared add helpers referenced by remove tests) into lifecycle/; root files stay until t13. |
| `t05-move-init` | Move init command into commands/lifecycle/ | completed | t02-introduce-lifecycle-skeleton | — | Y | Move init into lifecycle/; removes commands' only imports of internal/scaffold/home + internal/scaffold/hooks (importguard enforces later). |
| `t06-move-import` | Move import command + import_plugins into commands/lifecycle/ | completed | t02-introduce-lifecycle-skeleton | — | Y | Heaviest move (5 files: import + import_plugins) into lifecycle/; hit undeclared cross-file helper coupling (mirrorBackup/mapResourceRelToDest) mid-flight - fold-back recorded with 3 resolution options. |
| `t07-move-refresh` | Move refresh command into commands/lifecycle/ | completed | t02-introduce-lifecycle-skeleton | — | Y | Move refresh into lifecycle/; preserve t5's testutil.WriteScopeFile call sites through the package change. |
| `t08-move-status` | Move status command (incl. linkcount build-tagged helpers) into commands/lifecycle/ | completed | t02-introduce-lifecycle-skeleton | — | Y | Move status + build-tagged linkcount helpers into lifecycle/, exported for doctor's cross-package use until t09 collapses it to intra-package. |
| `t09-move-doctor` | Move doctor command into commands/lifecycle/ | completed | t08-move-status | — | Y | Move doctor into lifecycle/ (depends on t08's moved linkcount helpers); completes the lifecycle cluster. |
| `t10pre-extract-canonical-cmd-helpers` | Precursor: lift canonicalCmdFlags + canonicalCmdExampleBlock into commands/internal/cmdutil | completed | t01-design-subpackage-shape | — | Y | Precursor lifting canonicalCmdFlags + canonicalCmdExampleBlock into commands/internal/cmdutil (exported); unblocks parallel mcp/settings/rules subpackage splits. |
| `t10a-create-mcp-subpackage` | Move mcp command into commands/mcp/ (per agents/skills file convention) | completed | t10pre-extract-canonical-cmd-helpers | — | Y | Move mcp into commands/mcp/ per agents/skills file convention (cmd/deps/example/list/show/remove/seams/coverage); imports cmdutil helpers; root mcp.go becomes thin shim (deleted in t13). |
| `t10b-create-settings-subpackage` | Move settings command into commands/settings/ (per agents/skills file convention) | completed | t10pre-extract-canonical-cmd-helpers | — | Y | Move settings into commands/settings/ per same file convention; preserve t5 testutil call sites; findSettingsSpec stays package-private. |
| `t10c-create-rules-subpackage` | Move rules command into commands/rules/ (per agents/skills file convention) | completed | t10pre-extract-canonical-cmd-helpers | — | Y | Move rules into commands/rules/ per same file convention; imports shared cmdutil helpers; rulesShowFrontmatterExtra goes to show.go, findRuleSpec stays private. |
| `t11-split-seams-tests` | Split commands/seams_test.go into per-cluster files | completed | t03-move-install, t04-move-add-remove, t05-move-init, t06-move-import, t07-move-refresh, t08-move-status, t09-move-doctor | — | Y | Split seams_test.go (873 LOC) into per-cluster files; partial split kept 22 shim-forwarder-covering tests in root to hold the 95% commands coverage gate - full split deferred to after t13 removes shims (fold-back). |
| `t12-relocate-cross-cutting-tests` | Re-home cross-cutting tests: coverage / parity / wiring / hook-norm / agentsrc-mutations | completed | t10a-create-mcp-subpackage, t10b-create-settings-subpackage, t10c-create-rules-subpackage, t11-split-seams-tests | — | Y | Re-home cross-cutting tests (coverage/parity/wiring/hook-norm/agentsrc-mutations) across subpackages, splitting table tests; wiring_test (KG+Workflow) stays in root. |
| `t13-strip-root-shims` | [SUPERSEDED by t13a + t13b] Delete root re-export shims; switch root.go to subpackage constructors | completed | t12-relocate-cross-cutting-tests | — | Y | Superseded by t13a+t13b. Would delete root re-export shims and switch root.go to subpackage constructors; HEAD validation surfaced a co-commit wall (13 dependent test files) and 5 files holding un-moved production code, forcing the split. |
| `t14-import-graph-assertion` | Add CI assertion that package commands does not import banned internals | completed | t13-strip-root-shims | — | Y | Add importguard CI gate asserting package commands does not import internal/{links,projectsync,scaffold/home,scaffold/hooks} via go list walk + workflow step. |
| `t15-direct-followup-interface-di` | Direct follow-up: land interface-DI on the new lifecycle + mcp/settings/rules subpackages | completed | t14-import-graph-assertion | — | Y | Direct follow-up landing per-file interface-DI on new subpackages; merged via PR #140 (mcp/settings/rules); lifecycle-cluster conversion deferred to t17. |
| `t16-update-docs` | Update docs to reflect new package layout | completed | t14-import-graph-assertion | — | n | Update architecture/boundary docs + CLAUDE.md for the new layout; cross-link + resolve the decomposition proposal; merged via PR #137. |
| `t13a-strip-resource-subpackage-shims` | Delete mcp/settings/rules root shims + rewire 3 root.go AddCommand lines + dead callers | completed | t12-relocate-cross-cutting-tests | code-fix | Y | Delete mcp/settings/rules root shims + rewire 3 root.go AddCommand lines (mcp.NewCmd/settings.NewCmd/rules.NewCmd) + dead callers; re-narrowed to the proven-clean resource half per second fold-back (install/doctor/init/status moved to t13a-pre/t13b). |
| `t13a-pre-lifecycle-constructors` | Precursor for t13b: land lifecycle.NewInitCmd + absorb syncLifecycleGlobals into Deps threading | completed | t13a-strip-resource-subpackage-shims | code-fix | Y | Precursor adding lifecycle.NewInitCmd + absorbing syncLifecycleGlobals (Version/Commit/Describe/ErrorWithHintsFn/Flags) into Deps threading + resolving status jsonFlag closure, so t13b's root rewire doesn't regress runtime behavior. |
| `t13b-strip-lifecycle-cluster-shims` | Strip lifecycle-cluster shim wiring + rewire root.go + per-command test files | completed | t13a-strip-resource-subpackage-shims, t13a-pre-lifecycle-constructors | code-fix | Y | Strip lifecycle-cluster shim wiring + rewire root.go install/doctor/init/status to lifecycle constructors + 9 root test files; leaves un-moved production files (add/import/import_plugins/refresh/remove) intact. |
| `t17-lifecycle-interface-di` | Convert commands/internal/lifecycle/ from func-var seams to interface-DI (deferred from t15) | pending | t15-direct-followup-interface-di | go-cli | Y | Convert commands/internal/lifecycle/ from func-var seams to interface-DI as one unit (deferred from t15 - ~13.9k LOC, ~8 seams entangled with t13a/t13b init/import-cycle wiring); large, own PR. |

**Not-done task write_scope & readiness:**

- `t17-lifecycle-interface-di` [pending] **READY**: `commands/internal/lifecycle/`

### `seam-interface-di-migration` — Migrate func-var test seams to interface-DI

- status: **active** · current_focus_task: (none set) · tasks: 7 (0 not-done) · path: `.agents/workflow/plans/seam-interface-di-migration/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `convention-doc` | Document canonical interface-DI seam shape | completed | ∅ | — | Y | Write canonical interface-DI seam convention (narrow collaborator iface, prod struct impl, ctor/param injection); reference graphstore NewHandle and commands.dirCleaner/osDirCleaner. |
| `pr35-workflow-rework` | Rework PR#35 commands/workflow seams to interface-DI | completed | convention-doc | — | Y | Convert commands/workflow/seams.go func-var seams to interface-DI and rework PR#35 onto the new shape, preserving its schema-compile dedupe value. |
| `commands-pkg` | Convert commands/seams.go (incl. finish remove.go) | completed | convention-doc | — | Y | 12 seams; the architectural-decision task (Cobra free-function injection). Leaves went first to set the pattern; commands-pkg last with a decided injection shape. |
| `kg-pkg` | Convert commands/kg/seams.go to interface-DI | completed | convention-doc | — | Y | 7 func-var seams; behavior-preserving conversion, tests green, coverage gate not regressed. |
| `skills-pkg` | Convert commands/skills/seams.go to interface-DI | completed | convention-doc | — | Y | 4 func-var seams; behavior-preserving conversion. |
| `agents-pkg` | Convert commands/agents/seams.go to interface-DI | completed | convention-doc | — | Y | 1 func-var seam; smallest leaf, fold in with a sibling if cheaper. |
| `platform-pkg` | Convert internal/platform/seams.go to interface-DI | completed | convention-doc | — | Y | 3 func-var seams; behavior-preserving conversion, tests green. |

### `shared-target-projection-wiring` — Shared-target projection wiring

- status: **active** · current_focus_task: `Final doc reconcile + pr10 cross-ref + close` · tasks: 6 (1 not-done) · path: `.agents/workflow/plans/shared-target-projection-wiring/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `stp1-wire-refresh` | Wire shared-target projection into refresh (+ resolve entry-point set) | completed | ∅ | — | Y | Already satisfied on master (8e86a35e): refresh.go:157 runs RunSharedTargetProjection before the per-platform CreateLinks loop; no code change. |
| `stp2-wire-install-add` | Wire the same projection into install + add (entry-order parity) | completed | ∅ | — | Y | Already satisfied on master (same 8e86a35e): add.go:531 and install.go:222 both invoke RunSharedTargetProjection. |
| `stp-import-relink` | Wire projection into import.go relinkImportedProjects (the real gap) | completed | ∅ | — | Y | The genuine gap: relinkImportedProjects looped CreateLinks without projection; add the projection call before the per-project loop matching refresh/install/add ordering. |
| `stp-doctor-repair` | Doctor repair fixes broken projection + links ALL managed da entities | completed | stp-import-relink | — | Y | Doctor repair must run RunSharedTargetProjection AND full CreateLinks across installed platforms (not symlink-only), kept idempotent. |
| `stp3-regression-parity` | Regression: import-relink + doctor + refresh-skill-mirror produce projection; dry-run/idempotent | completed | stp-import-relink, stp-doctor-repair, refresh-skill-relink/fix-shared-skill-relink | — | Y | Behavior tests for the newly wired paths (import relink, doctor, refresh-skill-mirror routing): projection produced, dry-run no-mutation, idempotence, no CreateLinks regression. |
| `stp4-doc-reconcile-close` | Final doc reconcile + pr10 cross-ref + close | pending | stp3-regression-parity | — | Y | Remove the 'known narrow gap' caveat in PLATFORM_DIRS_DOCS.md after import/doctor land; cross-ref pr10-branch-split/retire-old-branches; archive plan. |

**Not-done task write_scope & readiness:**

- `stp4-doc-reconcile-close` [pending] **READY**: `docs/PLATFORM_DIRS_DOCS.md`

### `worker-bundle-lessons` — Lesson preload/injection into worker bundles (relevance-selected required_reads)

- status: **active** · current_focus_task: `load-lessons-1-schema-selection-metadata` · tasks: 4 (4 not-done) · path: `.agents/workflow/plans/worker-bundle-lessons/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `load-lessons-1-schema-selection-metadata` | Add lesson-selection metadata to lesson.schema.json + selector | pending | ∅ | — | Y | Add SELECTION metadata (applies_to_app_types, touched-path/subject globs, scope discriminator repo/user/team/org/public) to frontmatter-only lesson.schema.json; extend the selector. |
| `load-lessons-2-relevance-lessons-facet` | Extend da config relevance lessons facet (tag select, layered) | pending | load-lessons-1-schema-selection-metadata | — | Y | Extend relevance lessonsFacet with --lesson-tag select + schema-metadata (app_type/write_scope/tag) matching + superseded auto-resolve; MUST read the LAYERED resolver (cross-plan dep on config-transitive-layering). |
| `load-lessons-3-bundle-required-reads` | Bundle generation attaches relevance-selected lessons as required_reads | pending | load-lessons-2-relevance-lessons-facet | — | Y | In fanout/bundle generation, run the lessons relevance query per task and attach matching LESSON.md as single-sourced required_reads (interim filesystem match over .agents/lessons/). |
| `load-lessons-4-kg-scoped-view` | Full: query scoped-lessons KG view for selection (end-state) | pending | load-lessons-3-bundle-required-reads | — | Y | End-state: replace the filesystem scan with the scoped-lessons KG view for scope-aware selection; loud JSON/bundle fallback when KG is unavailable. |

**Not-done task write_scope & readiness:**

- `load-lessons-1-schema-selection-metadata` [pending] **READY**: `schemas/lesson.schema.json`; `commands/config/relevance.go`; `commands/config/relevance_lessons.go`
- `load-lessons-2-relevance-lessons-facet` [pending] **BLOCKED — waits on worker-bundle-lessons/load-lessons-1-schema-selection-metadata[pending]**: `commands/config/relevance_lessons.go`; `commands/config/relevance.go`; `commands/config/relevance_lessons_test.go`
- `load-lessons-3-bundle-required-reads` [pending] **BLOCKED — waits on worker-bundle-lessons/load-lessons-2-relevance-lessons-facet[pending]**: `commands/workflow/fanout.go`; `commands/workflow/delegation.go`; `commands/workflow/plan_task.go`
- `load-lessons-4-kg-scoped-view` [pending] **BLOCKED — waits on worker-bundle-lessons/load-lessons-3-bundle-required-reads[pending]**: `commands/config/relevance_lessons.go`; `internal/graphstore/`

### `worktree-platform` — Managed worktree platform for delegation/branch isolation

- status: **active** · current_focus_task: `Rich worktree metadata registry + auto-prune-if-unchanged (layered on wt1's Prune/BaseRef)` · tasks: 9 (6 not-done) · path: `.agents/workflow/plans/worktree-platform/TASKS.yaml`

| task id | title | status | depends_on | app_type | vfy | notes-gist |
|---|---|---|---|---|---|---|
| `wt0-spike-git-mechanism` | Spike: VERIFY go-git v6 x/plumbing/worktree (+ mine swarm-cd) | completed | ∅ | — | Y | Merged via #171: Decision A = pure go-git v6 (v6.0.0-alpha.4), all 4 residuals PASS, zero shell-git; pin reconfirmed current 2026-07-10. |
| `wt1-typed-git-iface` | Typed git/worktree interface (the contract callers bind to) | completed | wt0-spike-git-mechanism | — | Y | Shipped internal/gitwt/{gitwt.go,gogit.go} via #181 with 14 tests; Manager includes Prune()+RecordBaseRef()/BaseRef(); one live consumer internal/eval/sandbox. |
| `wt2-worktree-registry` | Rich worktree metadata registry + auto-prune-if-unchanged (layered on wt1's Prune/BaseRef) | pending | wt1-typed-git-iface | — | Y | Adds per-worktree metadata sidecar under .git/worktrees/<name>/ (readable from main repo) + auto-prune-if-unchanged staleness check (branch tip vs recorded BaseRef); commits-past-base are never auto-pruned. |
| `wt3-subbranch-mergeback` | da worktree create/merge-back CLI: sub-branch -> merge-back/rebase using the recorded base | pending | wt2-worktree-registry | — | Y | New commands/worktree/ package: create-sub-branch + merge-back/rebase reading base EXCLUSIVELY from wt2 registry (never git merge-base); stale-base and branch-drift regression tests. |
| `wt4-index-isolation` | Concurrent-agent index isolation proof + formal done-criterion close (residual after wt1) | pending | wt1-typed-git-iface | — | Y | Real concurrent test: N goroutines each opening its own linked worktree and staging/committing in parallel under go test -race; formalizes the plan's done-criterion. |
| `wt5-skills-integration` | delegation-lifecycle / isp / loop-worker consume the platform | pending | wt3-subbranch-mergeback, wt4-index-isolation | — | Y | Net-new orchestration wiring: the three starter skills create/track/merge-back delegated slices via da worktree (no raw git); reconcile with worker-bundle-authoring commit-2 scoped mode. |
| `wt6-verify-close` | Verify done-criteria + close | pending | wt5-skills-integration | — | Y | Verify all spec done-criteria (stale-base catch, concurrent index isolation, deterministic prune-if-unchanged, single typed iface, config round-trip); re-verify go-git v6 pin; archive plan. |
| `release-minor` | Release: bump VERSION + finalize CHANGELOG for the worktree-platform minor | pending | wt0-spike-git-mechanism, wt1-typed-git-iface, wt2-worktree-registry, wt3-subbranch-mergeback, wt4-index-isolation, wt5-skills-integration, wt6-verify-close | release | Y | Release-gated tail (MINOR): resolve next minor from VERSION at ready-time, bump VERSION + finalize CHANGELOG section, docs-accuracy pass; merge fires auto-release.yml. |
| `refresh-ideate-worktree-platform` | Refresh + re-ideate the worktree-platform spec against today's facts; groom wt2-wt6 | completed | ∅ | ideation | Y | Refreshed the ~2mo-old spec against today (go-git version, wt0/wt1 actual completion, harness/delegation usage, workflow-commit-command relationship, swarm-cd currency) and groomed wt2-wt6 to implementation-ready. |

**Not-done task write_scope & readiness:**

- `wt2-worktree-registry` [pending] **READY**: `internal/gitwt/registry.go`; `internal/gitwt/registry_test.go`
- `wt3-subbranch-mergeback` [pending] **BLOCKED — waits on worktree-platform/wt2-worktree-registry[pending]**: `internal/gitwt/mergeback.go`; `commands/worktree/`; `commands/root.go`
- `wt4-index-isolation` [pending] **READY**: `internal/gitwt/index_isolation_concurrent_test.go`
- `wt5-skills-integration` [pending] **BLOCKED — waits on worktree-platform/wt3-subbranch-mergeback[pending], worktree-platform/wt4-index-isolation[pending]**: `internal/scaffold/home/starter/skills/global/delegation-lifecycle/`; `internal/scaffold/home/starter/skills/global/isp/`; `internal/scaffold/home/starter/skills/global/loop-worker/`
- `wt6-verify-close` [pending] **BLOCKED — waits on worktree-platform/wt5-skills-integration[pending]**: _(none declared)_
- `release-minor` [pending] **BLOCKED — waits on worktree-platform/wt2-worktree-registry[pending], worktree-platform/wt3-subbranch-mergeback[pending], worktree-platform/wt4-index-isolation[pending], worktree-platform/wt5-skills-integration[pending], worktree-platform/wt6-verify-close[pending]**: `VERSION`; `CHANGELOG.md`

## Cross-plan READY NOW (deps all done or empty)

**29 tasks are startable now.** Grouped by `app_type` (drives the execution/verification profile a swarm agent selects). `—` = no `app_type` declared in TASKS.yaml (swarm must infer). Ordering within a group is by plan then declared dependency order.

### app_type: `go-cli`  (7)

- **`config-v2-migration` / `p8-payout-wrk-v2-upgrade`** [ready] vfy=Y (status literal `ready`)
  - Payout-wrk v1->v2 upgrade + fresh install-from-git validation (0.4.1)
  - write_scope: `.agents/history/config-v2-migration/payout-wrk-v2-upgrade-readback.md`
- **`git-ref-work-backend` / `read-from-master-shim`** [pending] vfy=Y
  - read-from-master shim: resolve task-state from the canonical ref, not the worktree copy
  - write_scope: `commands/workflow/plan_task.go`; `commands/workflow/eligible_accounting.go`; `commands/workflow/next.go`; `internal/config/agentsrc.go`; `commands/config/`
- **`graph-backend-adapter-contract` / `t9-sdk-materializeview-readsfrom-gate`** [pending] vfy=Y
  - Wire reads_from enforcement into SDK.MaterializeView
  - write_scope: `internal/adapters/sdk/`
- **`managed-gitignore-autofill` / `wire-managed-gitignore-autofill`** [pending] vfy=Y
  - Wire EnsureManagedGitignore into da refresh + retire #381 ad-hoc root rules + fix .agentsrc.lock contract
  - write_scope: `internal/links/gitignore.go`; `internal/links/gitignore_test.go`; `internal/platform/platform.go`; `internal/platform/copilot.go`; `commands/refresh.go`; `commands/refresh_test.go`; `.gitignore`; `.agents/lessons/classify-generated-files-before-cleanup/LESSON.md`; `.agents/lessons/index.md`
- **`r2-observability-dashboard` / `t05-sse-handler`** [pending] vfy=Y
  - Wire /api/v1/observability/events SSE handler to broker
  - write_scope: `internal/dashboard/handlers/handlers.go`; `internal/dashboard/handlers/stream.go`; `internal/dashboard/handlers/stream_test.go`
- **`r3-background-worker-service` / `cobra-surface`** [pending] vfy=Y
  - `da service run\|status\|stop` cobra subcommand
  - write_scope: `commands/service/`; `commands/root.go`
- **`root-command-decomposition` / `t17-lifecycle-interface-di`** [pending] vfy=Y
  - Convert commands/internal/lifecycle/ from func-var seams to interface-DI (deferred from t15)
  - write_scope: `commands/internal/lifecycle/`

### app_type: `web`  (4)

- **`r2-observability-dashboard` / `t09-feature-aggregate-view`** [pending] vfy=Y
  - Aggregate-dashboard view (runs grid, score trend, cache trend)
  - write_scope: `web/dashboard/src/views/AggregateView.tsx`; `web/dashboard/src/views/AggregateView.test.tsx`; `web/dashboard/src/components/RunsGrid.tsx`; `web/dashboard/src/components/RunsGrid.test.tsx`; `web/dashboard/src/components/ScoreTrendChart.tsx`; `web/dashboard/src/components/ScoreTrendChart.test.tsx`; `web/dashboard/src/components/CacheTrendChart.tsx`; `web/dashboard/src/components/CacheTrendChart.test.tsx`
- **`r2-observability-dashboard` / `t10-feature-per-run-drilldown`** [pending] vfy=Y
  - Per-run drill-down view (iteration timeline + breakdown + integrity)
  - write_scope: `web/dashboard/src/views/RunDetailView.tsx`; `web/dashboard/src/views/RunDetailView.test.tsx`; `web/dashboard/src/views/IterationDetailView.tsx`; `web/dashboard/src/views/IterationDetailView.test.tsx`; `web/dashboard/src/components/IterationTimeline.tsx`; `web/dashboard/src/components/IterationTimeline.test.tsx`; `web/dashboard/src/components/SignalBreakdown.tsx`; `web/dashboard/src/components/SignalBreakdown.test.tsx`; `web/dashboard/src/components/IntegrityPanel.tsx`; `web/dashboard/src/components/IntegrityPanel.test.tsx`
- **`r2-observability-dashboard` / `t15-rubric-versioning-ui`** [pending] vfy=Y
  - Rubric explainer view (read /api/rubric, render weights + bands)
  - write_scope: `web/dashboard/src/views/RubricView.tsx`; `web/dashboard/src/views/RubricView.test.tsx`; `web/dashboard/src/components/RubricTable.tsx`; `web/dashboard/src/components/RubricTable.test.tsx`
- **`r5-review-labeling-access` / `review-ui`** [pending] vfy=Y
  - Review surface in R2 (list, inspect, label form, role-aware controls)
  - write_scope: `web/dashboard/src/review/`

### app_type: `docs`  (2)

- **`agent-ops-hardening` / `p2-brief-template-hardening`** [awaiting_owner_review] vfy=n ⚠️ impl-complete, awaiting owner merge (not agent-actionable)
  - P2: harden the delegation brief/bundle template
  - write_scope: `internal/scaffold/home/starter/skills/global/delegation-lifecycle/`; `internal/scaffold/home/starter/skills/global/orchestrator-session-start/`
- **`r5-review-labeling-access` / `docs`** [pending] vfy=n
  - Documentation - REVIEW_LABELING_GUIDE.md, RBAC_AND_AUDIT.md, rubric v3 changelog
  - write_scope: `docs/REVIEW_LABELING_GUIDE.md`; `docs/REVIEW_RBAC_AND_AUDIT.md`; `docs/OUTCOME_SCORING_RUBRIC.md`

### app_type: `config`  (1)

- **`agent-ops-hardening` / `p1-reconcile-eligible-and-preload-tools`** [awaiting_owner_review] vfy=Y ⚠️ impl-complete, awaiting owner merge (not agent-actionable)
  - P1: fold reconcile bullet into orchestrator-session-start preflight (guarded git fetch) + preload toolset via tools: frontmatter
  - write_scope: `internal/scaffold/home/starter/skills/global/orchestrator-session-start/`; `.agents/active/orchestrator.loop.md`

### app_type: `skill`  (2)

- **`agent-ops-hardening` / `p1-delegation-brief-hardening-skill`** [awaiting_owner_review] vfy=Y ⚠️ impl-complete, awaiting owner merge (not agent-actionable)
  - P1: operationalize pre-fanout bundle hardening (make validate-bundle-against-head + bundle-scope-via-code-graph mandatory in orchestrator-session-start/delegation-lifecycle)
  - write_scope: `internal/scaffold/home/starter/skills/global/orchestrator-session-start/`; `internal/scaffold/home/starter/skills/global/delegation-lifecycle/`
- **`agent-ops-hardening` / `p1-release-cut-skill`** [awaiting_owner_review] vfy=Y ⚠️ impl-complete, awaiting owner merge (not agent-actionable)
  - P1 skill (the one new standalone skill): release-cut-monitor-retry + split-out pin-toolchain CI step
  - write_scope: `internal/scaffold/home/starter/skills/global/release-cut/`; `.github/workflows/`

### app_type: `research`  (2)

- **`loop-discipline-stop-hooks` / `p6-rollback-plan`** [pending] vfy=n
  - P6 payout-migration rollback documentation
  - write_scope: `.agents/history/loop-discipline-stop-hooks/payout-migration-rollback.md`
- **`loop-discipline-stop-hooks` / `r1-5-t4-integration`** [pending] vfy=n
  - r1-5 plan integration test (cross-plan coordination acknowledgment)
  - write_scope: `.agents/history/loop-discipline-stop-hooks/r1-5-coordination.md`

### app_type: `release`  (3)

- **`r1-5-hook-enforcement-telemetry` / `release-minor`** [pending] vfy=Y
  - Release: bump VERSION + finalize CHANGELOG for the hook-telemetry minor
  - write_scope: `VERSION`; `CHANGELOG.md`
- **`r4-code-task-generation-eval` / `release-minor`** [pending] vfy=Y
  - Release: bump VERSION + finalize CHANGELOG for the eval-harness minor
  - write_scope: `VERSION`; `CHANGELOG.md`
- **`release-patch-train` / `release-docs-refresh`** [pending] vfy=Y
  - Release: reconcile docs vs code + platform-dirs before the patch bump
  - write_scope: `docs`; `README.md`; `docs/web`

### app_type: `—`  (8)

- **`config-transitive-layering` / `org-config-spec-transitive-scope`** [pending] vfy=Y
  - Spec org→team→repo transitive extends + source scope/owner routing
  - write_scope: `.agents/workflow/specs/org-config-resolution/design.md`; `.agents/workflow/specs/config-distribution-model/design.md`; `docs/LAYERED_CONFIG_GUIDE.md`
- **`graph-backend-adapter-contract` / `t6-bridge-decommission`** [pending] vfy=Y
  - Bridge decommissioning - close §11.4 gate
  - write_scope: `internal/graphstore/ (deletions)`; `internal/adapters/builtin/crg/ (full feature parity)`; `.github/workflows/ci.yml (parity test promotion)`
- **`kg-ideate-skill` / `t7-structural-contradiction-traversal`** [blocked] vfy=Y ⚠️ status=`blocked` but all deps done (stale — treat as ready)
  - GATED: Phase 2 step 7 structural contradiction traversal (citation adapter)
  - write_scope: `internal/scaffold/home/starter/skills/global/kg-ideate/spec-scaffold/instructions/contradiction-framing.md`; `internal/scaffold/home/starter/skills/global/kg-ideate/spec-scaffold/instructions/kg-queries.md`
- **`loop-discipline-stop-hooks` / `p7-legacy-override-migration`** [pending] vfy=Y
  - Final migration of legacy downstream discipline-skill overrides
  - write_scope: `.agents/history/loop-discipline-stop-hooks/downstream-override-migration.md`
- **`shared-target-projection-wiring` / `stp4-doc-reconcile-close`** [pending] vfy=Y
  - Final doc reconcile + pr10 cross-ref + close
  - write_scope: `docs/PLATFORM_DIRS_DOCS.md`
- **`worker-bundle-lessons` / `load-lessons-1-schema-selection-metadata`** [pending] vfy=Y
  - Add lesson-selection metadata to lesson.schema.json + selector
  - write_scope: `schemas/lesson.schema.json`; `commands/config/relevance.go`; `commands/config/relevance_lessons.go`
- **`worktree-platform` / `wt2-worktree-registry`** [pending] vfy=Y
  - Rich worktree metadata registry + auto-prune-if-unchanged (layered on wt1's Prune/BaseRef)
  - write_scope: `internal/gitwt/registry.go`; `internal/gitwt/registry_test.go`
- **`worktree-platform` / `wt4-index-isolation`** [pending] vfy=Y
  - Concurrent-agent index isolation proof + formal done-criterion close (residual after wt1)
  - write_scope: `internal/gitwt/index_isolation_concurrent_test.go`

## Blocked — waiting on X

**29 tasks blocked** by an unmet dependency. Each resolves the moment its listed upstream reaches a done status (`awaiting_owner_review` also unblocks per the fanout spec).

**`config-transitive-layering`**
- `config-source-scope-schema` [pending] (—) — waits on: `config-transitive-layering/org-config-spec-transitive-scope[pending]`
- `config-transitive-extends-resolver` [pending] (—) — waits on: `config-transitive-layering/config-source-scope-schema[pending]`
- `layered-consumers-relevance-verify` [pending] (—) — waits on: `config-transitive-layering/config-transitive-extends-resolver[pending]`

**`git-ref-work-backend`**
- `git-ref-state-ref-write` [pending] (go-cli) — waits on: `git-ref-work-backend/read-from-master-shim[pending]`
- `per-task-state-files` [pending] (go-cli) — waits on: `git-ref-work-backend/git-ref-state-ref-write[pending]`
- `decouple-coordination-commits` [pending] (go-cli) — waits on: `git-ref-work-backend/git-ref-state-ref-write[pending]`
- `workstore-git-ref-backend` [pending] (go-cli) — waits on: `git-ref-work-backend/per-task-state-files[pending]`, `git-ref-work-backend/decouple-coordination-commits[pending]`
- `document-and-default-git-ref` [pending] (go-cli) — waits on: `git-ref-work-backend/workstore-git-ref-backend[pending]`

**`graph-backend-adapter-contract`**
- `release-minor` [pending] (release) — waits on: `graph-backend-adapter-contract/t6-bridge-decommission[pending]`

**`loop-discipline-stop-hooks`**
- `release-minor` [pending] (release) — waits on: `loop-discipline-stop-hooks/p7-legacy-override-migration[pending]`, `loop-discipline-stop-hooks/p6-rollback-plan[pending]`, `loop-discipline-stop-hooks/r1-5-t4-integration[pending]`

**`r2-observability-dashboard`**
- `t07-standalone-service-bin` [pending] (go-cli) — waits on: `r2-observability-dashboard/t05-sse-handler[pending]`
- `t11-frontend-sse-integration` [pending] (web) — waits on: `r2-observability-dashboard/t05-sse-handler[pending]`
- `t12-e2e-smoke-live-iteration` [pending] (go-cli) — waits on: `r2-observability-dashboard/t07-standalone-service-bin[pending]`, `r2-observability-dashboard/t09-feature-aggregate-view[pending]`, `r2-observability-dashboard/t10-feature-per-run-drilldown[pending]`, `r2-observability-dashboard/t11-frontend-sse-integration[pending]`
- `t13-r3-mount-integration` [pending] (go-cli) — waits on: `r2-observability-dashboard/t05-sse-handler[pending]`
- `t14-documentation` [pending] (docs) — waits on: `r2-observability-dashboard/t12-e2e-smoke-live-iteration[pending]`, `r2-observability-dashboard/t13-r3-mount-integration[pending]`
- `t16-coverage-gate-and-ci` [pending] (go-cli) — waits on: `r2-observability-dashboard/t12-e2e-smoke-live-iteration[pending]`
- `release-minor` [pending] (release) — waits on: `r2-observability-dashboard/t05-sse-handler[pending]`, `r2-observability-dashboard/t07-standalone-service-bin[pending]`, `r2-observability-dashboard/t09-feature-aggregate-view[pending]`, `r2-observability-dashboard/t10-feature-per-run-drilldown[pending]`, `r2-observability-dashboard/t11-frontend-sse-integration[pending]`, `r2-observability-dashboard/t12-e2e-smoke-live-iteration[pending]`, `r2-observability-dashboard/t13-r3-mount-integration[pending]`, `r2-observability-dashboard/t14-documentation[pending]`, `r2-observability-dashboard/t15-rubric-versioning-ui[pending]`, `r2-observability-dashboard/t16-coverage-gate-and-ci[pending]`

**`r3-background-worker-service`**
- `docs-and-verification` [pending] (go-cli) — waits on: `r3-background-worker-service/cobra-surface[pending]`
- `release-minor` [pending] (release) — waits on: `r3-background-worker-service/cobra-surface[pending]`, `r3-background-worker-service/docs-and-verification[pending]`

**`r5-review-labeling-access`**
- `e2e-smoke` [pending] (go-cli) — waits on: `r5-review-labeling-access/review-ui[pending]`
- `release-minor` [pending] (release) — waits on: `r5-review-labeling-access/review-ui[pending]`, `r5-review-labeling-access/e2e-smoke[pending]`, `r5-review-labeling-access/docs[pending]`

**`release-patch-train`**
- `release-patch` [pending] (release) — waits on: `release-patch-train/release-docs-refresh[pending]`

**`worker-bundle-lessons`**
- `load-lessons-2-relevance-lessons-facet` [pending] (—) — waits on: `worker-bundle-lessons/load-lessons-1-schema-selection-metadata[pending]`
- `load-lessons-3-bundle-required-reads` [pending] (—) — waits on: `worker-bundle-lessons/load-lessons-2-relevance-lessons-facet[pending]`
- `load-lessons-4-kg-scoped-view` [pending] (—) — waits on: `worker-bundle-lessons/load-lessons-3-bundle-required-reads[pending]`

**`worktree-platform`**
- `wt3-subbranch-mergeback` [pending] (—) — waits on: `worktree-platform/wt2-worktree-registry[pending]`
- `wt5-skills-integration` [pending] (—) — waits on: `worktree-platform/wt3-subbranch-mergeback[pending]`, `worktree-platform/wt4-index-isolation[pending]`
- `wt6-verify-close` [pending] (—) — waits on: `worktree-platform/wt5-skills-integration[pending]`
- `release-minor` [pending] (release) — waits on: `worktree-platform/wt2-worktree-registry[pending]`, `worktree-platform/wt3-subbranch-mergeback[pending]`, `worktree-platform/wt4-index-isolation[pending]`, `worktree-platform/wt5-skills-integration[pending]`, `worktree-platform/wt6-verify-close[pending]`

## Critical path (longest dependency chain per active plan)

`*` marks a not-done task on the chain. The first `*` in each chain is the plan's current frontier — the next thing to do. Chains use intra-plan deps; the number of trailing `*` = remaining serial depth to finish the plan.

- **`agent-ops-hardening`** (serial depth remaining ≈ 1): p1-delegation-brief-hardening-skill* → p4-fanout-gate-enforcement
- **`config-transitive-layering`** (serial depth remaining ≈ 4): org-config-spec-transitive-scope* → config-source-scope-schema* → config-transitive-extends-resolver* → layered-consumers-relevance-verify*
- **`config-v2-migration`** (serial depth remaining ≈ 1): p0-schema-additive-extension → p1-resolver-core-flat → p4-config-explain-cli → p4b-app-types-snapshot-refactor → p7-v1-deprecation-warnings → p7b-v1-auto-migrator → p8-payout-wrk-v2-upgrade*
- **`git-ref-work-backend`** (serial depth remaining ≈ 5): spec-git-ref-backend → read-from-master-shim* → git-ref-state-ref-write* → per-task-state-files* → workstore-git-ref-backend* → document-and-default-git-ref*
- **`graph-backend-adapter-contract`** (serial depth remaining ≈ 1): t1-none-adapter-end-to-end → t2-compliance-read-only → t5-cross-adapter-reads-from → release-minor*
- **`kg-ideate-skill`** (serial depth remaining ≈ 0): t1-kg-brief-molecule → t2-spec-scaffold-molecule → t3-plan-scaffold-molecule → t4-staged-execution-handoff-molecule → t5-orchestrator-compound → t6-degraded-mode-e2e
- **`loop-discipline-stop-hooks`** (serial depth remaining ≈ 2): p0-sentinel-cli → p1a-mapper-extensions → p1b-canonical-when-values → p2-hook-scripts → p5-e2e-integration → p6-payout-backfill → p7-legacy-override-migration* → release-minor*
- **`managed-gitignore-autofill`** (serial depth remaining ≈ 1): wire-managed-gitignore-autofill*
- **`r1-5-hook-enforcement-telemetry`** (serial depth remaining ≈ 1): t0-outcome-contract → t1-capture-outcomes → t1b-post-tool-observation-evaluation → t2-scoring-signal → t3-cli-readback → t-e2e-integration-test → release-minor*
- **`r2-observability-dashboard`** (serial depth remaining ≈ 5): design-doc → t01-api-contract-design → t02-storage-read-layer → t03-handlers-rest → t05-sse-handler* → t07-standalone-service-bin* → t12-e2e-smoke-live-iteration* → t14-documentation* → release-minor*
- **`r3-background-worker-service`** (serial depth remaining ≈ 3): design-doc → event-bus → event-bus-interface-seam → http-server → service-runtime → cobra-surface* → docs-and-verification* → release-minor*
- **`r4-code-task-generation-eval`** (serial depth remaining ≈ 1): design-doc → taskspec-and-registry → kg-query-layer → difficulty-derivation → generator-go → verifier-go → harness-driver → persistence → cli-eval → docs → release-minor*
- **`r5-review-labeling-access`** (serial depth remaining ≈ 3): design-doc → rbac-identity → audit-log → collection-endpoint → review-ui* → e2e-smoke* → release-minor*
- **`release-patch-train`** (serial depth remaining ≈ 2): release-docs-refresh* → release-patch*
- **`root-command-decomposition`** (serial depth remaining ≈ 1): t01-design-subpackage-shape → t02-introduce-lifecycle-skeleton → t08-move-status → t09-move-doctor → t11-split-seams-tests → t12-relocate-cross-cutting-tests → t13-strip-root-shims → t14-import-graph-assertion → t15-direct-followup-interface-di → t17-lifecycle-interface-di*
- **`shared-target-projection-wiring`** (serial depth remaining ≈ 1): stp-import-relink → stp-doctor-repair → stp3-regression-parity → stp4-doc-reconcile-close*
- **`worker-bundle-lessons`** (serial depth remaining ≈ 4): load-lessons-1-schema-selection-metadata* → load-lessons-2-relevance-lessons-facet* → load-lessons-3-bundle-required-reads* → load-lessons-4-kg-scoped-view*
- **`worktree-platform`** (serial depth remaining ≈ 5): wt0-spike-git-mechanism → wt1-typed-git-iface → wt2-worktree-registry* → wt3-subbranch-mergeback* → wt5-skills-integration* → wt6-verify-close* → release-minor*

### Plans effectively DONE except release/merge tail

- `agent-ops-hardening`: only release/merge tail remains → ['p1-delegation-brief-hardening-skill', 'p1-reconcile-eligible-and-preload-tools', 'p1-release-cut-skill', 'p2-brief-template-hardening']
- `cross-platform-test-skips-audit`: **all tasks done.**
- `layered-pr-fanout`: **all tasks done.**
- `platform-driven-diagnostics`: **all tasks done.**
- `r1-5-hook-enforcement-telemetry`: only release/merge tail remains → ['release-minor']
- `r4-code-task-generation-eval`: only release/merge tail remains → ['release-minor']
- `release-patch-train`: only release/merge tail remains → ['release-docs-refresh', 'release-patch']
- `seam-interface-di-migration`: **all tasks done.**

## write_scope OVERLAP conflicts among READY tasks (parallelization hazards)

Two READY tasks conflict when their `write_scope` paths are equal, or one is nested under the other. Conflicting pairs MUST NOT be dispatched to concurrent swarm agents against the same tree without coordination/serialization. Non-conflicting READY tasks are safe to fan out in parallel.

| task A | task B | overlapping path(s) |
|---|---|---|
| `agent-ops-hardening/p1-delegation-brief-hardening-skill` | `agent-ops-hardening/p1-reconcile-eligible-and-preload-tools` | `internal/scaffold/home/starter/skills/global/orchestrator-session-start`≈`internal/scaffold/home/starter/skills/global/orchestrator-session-start` |
| `agent-ops-hardening/p1-delegation-brief-hardening-skill` | `agent-ops-hardening/p2-brief-template-hardening` | `internal/scaffold/home/starter/skills/global/delegation-lifecycle`≈`internal/scaffold/home/starter/skills/global/delegation-lifecycle`; `internal/scaffold/home/starter/skills/global/orchestrator-session-start`≈`internal/scaffold/home/starter/skills/global/orchestrator-session-start` |
| `agent-ops-hardening/p1-release-cut-skill` | `graph-backend-adapter-contract/t6-bridge-decommission` | `.github/workflows`≈`.github/workflows/ci.yml` |
| `agent-ops-hardening/p1-reconcile-eligible-and-preload-tools` | `agent-ops-hardening/p2-brief-template-hardening` | `internal/scaffold/home/starter/skills/global/orchestrator-session-start`≈`internal/scaffold/home/starter/skills/global/orchestrator-session-start` |
| `config-transitive-layering/org-config-spec-transitive-scope` | `release-patch-train/release-docs-refresh` | `docs/LAYERED_CONFIG_GUIDE.md`≈`docs` |
| `git-ref-work-backend/read-from-master-shim` | `worker-bundle-lessons/load-lessons-1-schema-selection-metadata` | `commands/config`≈`commands/config/relevance.go`; `commands/config`≈`commands/config/relevance_lessons.go` |
| `r1-5-hook-enforcement-telemetry/release-minor` | `r4-code-task-generation-eval/release-minor` | `CHANGELOG.md`≈`CHANGELOG.md`; `VERSION`≈`VERSION` |
| `r5-review-labeling-access/docs` | `release-patch-train/release-docs-refresh` | `docs/OUTCOME_SCORING_RUBRIC.md`≈`docs`; `docs/REVIEW_LABELING_GUIDE.md`≈`docs`; `docs/REVIEW_RBAC_AND_AUDIT.md`≈`docs` |
| `release-patch-train/release-docs-refresh` | `shared-target-projection-wiring/stp4-doc-reconcile-close` | `docs`≈`docs/PLATFORM_DIRS_DOCS.md` |

### Conflict clusters (serialize within a cluster; parallelize across)

- **`docs/` mega-scope cluster** — `release-patch-train/release-docs-refresh` declares `write_scope: docs` (the whole tree), so it collides with EVERY docs task. It is a release-gate barrier: run it alone, after doc-touching work settles. Collides with: `config-transitive-layering/org-config-spec-transitive-scope` (docs/LAYERED_CONFIG_GUIDE.md), `r5-review-labeling-access/docs` (docs/*), `shared-target-projection-wiring/stp4-doc-reconcile-close` (docs/PLATFORM_DIRS_DOCS.md).
- **agent-ops starter-skills cluster** — `p1-delegation-brief-hardening-skill`, `p1-reconcile-eligible-and-preload-tools`, `p2-brief-template-hardening` all rewrite `internal/scaffold/home/starter/skills/global/{orchestrator-session-start,delegation-lifecycle}/`. All three are `awaiting_owner_review` on the SAME PRs (#134) — already merged as a unit upstream; not independently dispatchable.
- **`VERSION`/`CHANGELOG.md` release cluster** — every plan's `release-minor` tail writes `VERSION` + `CHANGELOG.md`. Only ONE release-minor may hold these at a time; serialize all release cuts. Ready now: `r1-5-hook-enforcement-telemetry/release-minor` and `r4-code-task-generation-eval/release-minor` collide directly.
- **`commands/config/` cluster** — `git-ref-work-backend/read-from-master-shim` (`commands/config/`) vs `worker-bundle-lessons/load-lessons-1-schema-selection-metadata` (`commands/config/relevance*.go`). Overlap is the `commands/config/` dir; coordinate or split by file.
- **`.github/workflows/` cluster** — `agent-ops-hardening/p1-release-cut-skill` vs `graph-backend-adapter-contract/t6-bridge-decommission` (ci.yml). Low-risk (different concerns) but same file family.

**Freely parallelizable READY tasks (no write_scope overlap with any other ready task):** 16

- `config-v2-migration/p8-payout-wrk-v2-upgrade`
- `graph-backend-adapter-contract/t9-sdk-materializeview-readsfrom-gate`
- `kg-ideate-skill/t7-structural-contradiction-traversal`
- `managed-gitignore-autofill/wire-managed-gitignore-autofill`
- `loop-discipline-stop-hooks/p7-legacy-override-migration`
- `loop-discipline-stop-hooks/p6-rollback-plan`
- `loop-discipline-stop-hooks/r1-5-t4-integration`
- `r2-observability-dashboard/t05-sse-handler`
- `r2-observability-dashboard/t09-feature-aggregate-view`
- `r2-observability-dashboard/t10-feature-per-run-drilldown`
- `r2-observability-dashboard/t15-rubric-versioning-ui`
- `r3-background-worker-service/cobra-surface`
- `r5-review-labeling-access/review-ui`
- `root-command-decomposition/t17-lifecycle-interface-di`
- `worktree-platform/wt2-worktree-registry`
- `worktree-platform/wt4-index-isolation`

## Non-active plans (excluded from readiness analysis)

| plan id | status |
|---|---|
| `coverage-gate-per-file` | draft |
| `da-recipe-scripts` | completed |
| `dm6-da-sso-autowire` | draft |
| `docs-starlight-migration` | draft |
| `extends-oci-relax` | complete |
| `first-class-resource-docs` | draft |
| `local-gate-ci-parity` | draft |
| `orchestration-companion-stop-hooks` | draft |
| `production-code-helper-extraction` | proposed |
| `sdd-kg-schema-v4-corrections` | draft |

## Swarm-relevant hooks

How an artifact-driven omp swarm consumes/drives this inner loop non-interactively. All paths relative to the dot-agents repo root.

### State files the swarm reads/writes
- **`.agents/workflow/plans/<plan>/TASKS.yaml`** — the authoritative task list. Fields a swarm dispatcher needs per task: `id`, `status`, `depends_on` (bare id = same plan; `plan/task` = cross-plan), `write_scope` (parallel-safety key), `app_type` (execution-profile selector), `verification_required` (gate toggle). **Compute readiness from these directly** — do NOT trust `da workflow eligible` on 0.4.2.
- **`.agents/workflow/plans/<plan>/PLAN.yaml`** — `status` (only `active` plans are live), `current_focus_task` (human-declared frontier hint).
- **`.agents/active/iteration-log/iter-N.yaml`** (+ `iter-N.score.yaml` sidecars) — the canonical per-iteration record `iteration-close` writes; the swarm's evidence trail, not prose.

### Commands a swarm agent invokes (non-interactive)
- `da workflow plan create <id>` — scaffold a plan (PLAN.yaml/TASKS.yaml/.plan.md).
- `da workflow advance` / `da workflow merge-back` — the ONLY sanctioned way to move task status; never hand-edit `status:`. (Per repo rules §Task-Management.)
- `da workflow plan archive --plan <id>` — closeout; bundles PLAN/TASKS/.plan.md + delegate merge-backs.
- `da refresh` — re-link/project per-platform generated outputs after skill/config changes (relevant to `managed-gitignore-autofill`, `shared-target-projection-wiring`).
- `da run <file>` — mechanical shebang recipe of da subcommands (`#!/usr/bin/env -S da run`); in-process, fail-fast, cross-platform — a swarm's mechanical glue layer below skills.
- `da skills promote <name>` then `da refresh` — register + link a new skill.
- Global non-interactive flags to prefer where supported: `--json` (machine output), `--yes`/`-y` (no prompts), `-n`/`--dry-run` (preview). Verify per-subcommand with `da <cmd> --help`; a sibling agent (ConfigProfilesDoc / RunReviewDoc / WorkflowDoc) documents the exact flag surface.

### `app_type` → execution profile (swarm agent role selector)
Observed `app_type` values across active tasks and the profile they imply:
- `go-cli` — 97 task(s)
- `(unset)` — 82 task(s)
- `release` — 12 task(s)
- `research` — 11 task(s)
- `docs` — 8 task(s)
- `web` — 6 task(s)
- `config` — 4 task(s)
- `code-fix` — 4 task(s)
- `skill` — 2 task(s)
- `ops` — 1 task(s)
- `ideation` — 1 task(s)

  Mapping guidance for the swarm: `go-cli` → Go build+`go test ./...`+coverage gate; `web` → `web/dashboard` npm build+vitest; `docs` → docs build + link/claim check (verification_required often false); `config`/`skill` → `da refresh` + skill-architect eval; `research`/`ideation` → artifact-only, usually `verification_required: false`; `release` → VERSION/CHANGELOG bump firing `auto-release.yml` (serialize globally). Where `app_type` is unset, the swarm infers from `write_scope` (e.g. `internal/**`+`commands/**` ⇒ go-cli; `docs/**` ⇒ docs; `.agents/**`/`schemas/**` ⇒ config/spec).

### Gotchas for the swarm dispatcher
- **`da workflow eligible` is unreliable on the installed 0.4.2 binary** — the swarm MUST derive readiness itself from `TASKS.yaml` (as this doc does). Note repo source (module `github.com/AGOrcha/dot-agents`) may already fix this ahead of the shipped binary; verify against `commands/workflow/eligible*.go` before trusting either.
- **`awaiting_owner_review` = impl-complete, human-merge-pending.** It satisfies downstream deps and frees a slot, but is NOT agent-actionable — the swarm should skip it, not re-run it. (agent-ops-hardening's 4 ready tasks are all this: PRs #134/#135.)
- **Stale `status: blocked`.** `kg-ideate-skill/t7-structural-contradiction-traversal` is marked `blocked` but its only dep (`graph-backend/t4-crg-dual-read`) is `completed` — it is actually READY. The swarm should reconcile status vs dep-graph, not trust the literal status.
- **`ready` as a literal status** appears (`config-v2-migration/p8-payout-wrk-v2-upgrade`) — treat as not-done + startable; its live execution runs on a Windows box (host-specific).
- **`write_scope` overlap is the parallel-safety contract.** Coarse scopes (`docs`, whole dirs like `commands/`, `internal/platform/`) are barriers — serialize them. `VERSION`+`CHANGELOG.md` are a global singleton: only one release cut at a time.
- **Cross-plan deps exist** (`plan/task` form, e.g. kg-ideate→graph-backend). A swarm scheduler must resolve deps across ALL active plans, not per-plan.
- **Empty `write_scope`** (e.g. `cross-platform-test-skips-audit/migrate-sites`) means the task edits an unbounded/undeclared set — treat as a wide-scope barrier, not as conflict-free.
- **No AI-attribution trailers** in any commit/PR the swarm authors (repo global rule).
