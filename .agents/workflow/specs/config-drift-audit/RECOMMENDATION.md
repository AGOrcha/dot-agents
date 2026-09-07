# Recommendation: config-drift-audit → MERGE into `agent-ops-hardening`

**Artifact id:** config-drift-audit
**Lead:** `.agents/active/ideation-inbox/config-agentsrc-doc-drift-audit.md`
**kg-ideate decision:** MERGE-into-`agent-ops-hardening` (no new spec/plan)
**Briefing date:** 2026-07-22

## Why merge, not a new spec+plan

The lead asks for a `da doctor` config/doc drift auditor. Decomposing its three
concrete instances against the current code surface shows the concern is already
**owned** — partly delivered, partly held by open fold-backs on an active plan.
Opening a competing spec/plan is exactly the anti-pattern lesson
`single-source-of-truth-across-specs-and-plans` warns against (it cites the
`config-v2-migration` ↔ `config-v2-coherence` drift by name).

### Coverage map (grounded in the live surface)

| Lead instance | Owner | Code status |
|---|---|---|
| **#3** committed config shape lags spec/schema (schema/field drift) | `config-v2-migration` | **DELIVERED.** `da doctor` lock-drift detector — declares layer/package not in `.agentsrc.lock` → ERROR (`reportLockHealth` / `reportOneProjectLockHealth`, p2); `da config verify` first-class `config-staleness` check comparing `ComputeInputsDigest` vs the §7A units lock (`verifyStaleness`, p4f); `reportManifestDeprecation` warns on legacy v1 shape; `da config lint` validates layer files against the schema with `additionalProperties:false` to catch drift (p4c). |
| **#1** starter-global skills not backfilled (scaffold↔home **content** drift) | `agent-ops-hardening` `fb:scaffold-home-skill-sync-mechanism` (**OPEN**) | **UNCOVERED.** `internal/scaffold/home/embed.go` `CopyMissingStarterAssets` only writes files that do **not** already exist and "existing files are preserved and never overwritten" — no content-digest comparison, so a changed or newly-added starter skill is neither detected nor backfilled. `da doctor` has no "Starter Scaffold" section; `da refresh` reconciles managed *projects*, not the `~/.agents/` home/global scope. |
| **#2** `.agentsrc` over-captures global scope + registry-vs-disk gap | `InstallGenerateScopeFix` (in flight) + `agent-ops-hardening` `fb:agentsrc-registry-completeness` follow-up | **PARTLY RESOLVED.** The registry-completeness fold-back is RESOLVED (arrays backfilled), but its own follow-up is an unbuilt "status --audit registry-vs-disk cross-check"; the over-capture scope narrowing is `InstallGenerateScopeFix`'s job. Residual = the standing audit. |

Net: instance #3 needs **no** new work; instances #1 and #2 are the genuinely
residual drift-audit surface, and both are already first-class fold-back records
on the **active** `agent-ops-hardening` plan. So the correct artifact target is
concrete tasks under that plan, not a fresh spec.

### Architectural fit (keep it consistent with what shipped)

`config-v2-migration` established the split that these tasks must honor: **`da
doctor` is read-only and never repairs** (§7A.6 — it *surfaces* drift and points
at the reconcile command; `finalizeDoctorRun` points at `da refresh`), and the
**reconcile/sync path is a separate verb**. So the auditor SURFACES scaffold /
registry drift in `da doctor`; the non-destructive backfill lives in `da
refresh` (home scope), mirroring how lock drift → `da config sync`. This also
satisfies the lead's own "flags (not silently fixes)" framing and its pairing
with the "`refresh`-manages-home" capability (PR #375).

## Proposed tasks to add to `agent-ops-hardening`

Cross-cutting note for all three (record in task notes): the lock-drift,
units-staleness, and v1-deprecation drift dimensions are **already delivered by
`config-v2-migration` (p2/p4f/p4c)** — these tasks add ONLY the
scaffold-content and registry-vs-disk dimensions and MUST NOT duplicate or
re-report the existing doctor sections. Suggested concurrency: task `t-scaffold-drift-surface`
is the substrate (exports the digest/walk helper); `t-refresh-home-reconcile`
depends on it; `t-registry-disk-audit` is independent and can run concurrently.

---

### Task 1 — `t-scaffold-drift-surface`

**Title:** Surface scaffold↔home content-digest drift in `da doctor` (read-only)

**Write-scope:**
- `internal/scaffold/home/embed.go`
- `internal/scaffold/home/copy_test.go`
- `commands/internal/lifecycle/doctor.go`
- `commands/internal/lifecycle/doctor_test.go`

**Acceptance:**
- `internal/scaffold/home/embed.go` gains an exported comparator (e.g. walk the
  embedded `starter` tree, content-hash each file, diff against the materialized
  `~/.agents/` path) returning missing-in-home and content-diverged entries.
- `da doctor` gains a read-only "Starter Scaffold" section that WARNs on (a) a
  starter entry entirely absent from home (never backfilled — the PR #374/#375
  case), and (b) a home entry whose content-hash diverges from the embedded
  starter; it repairs nothing and points at the refresh reconcile verb.
- Fixture tests: a home missing a newly-added starter skill → warn; a
  byte-identical home → silent (no false positive); user-authored files with no
  starter counterpart are never flagged.
- Verified: `go test ./internal/scaffold/... ./commands/internal/lifecycle/...`
  (focused; no repo-wide suite).
- Realizes `fb:scaffold-home-skill-sync-mechanism` (drift-detect half).

### Task 2 — `t-refresh-home-reconcile`

**Title:** `da refresh` reconciles the `~/.agents/` home/global scope (backfill + digest update)

**Write-scope:**
- `internal/scaffold/home/embed.go`
- `commands/internal/lifecycle/refresh.go`
- `commands/refresh.go`
- `commands/refresh_test.go`

**Depends on:** `t-scaffold-drift-surface` (reuses the digest/walk helper)

**Acceptance:**
- A `CopyOrUpdateStarterAssets`-style reconcile (alongside the existing
  `CopyMissingStarterAssets`) backfills newly-shipped starter-global
  skills/agents into an existing home and updates content-diverged managed
  entries by digest, guarded by the existing EXACT/`--inexact` overwrite
  semantics so user-authored non-starter files are preserved.
- `da refresh` wires this home-scope reconcile (the RunRefresh body in
  `commands/refresh.go`), so a home created *before* a skill joined
  starter-global receives it after `da refresh`; `--dry-run` prints the diff
  without writing.
- Fixture tests: home missing a starter skill → present after refresh; diverged
  managed entry → updated; user file preserved; second run is a no-op
  (idempotent).
- Verified: `go test ./internal/scaffold/... ./commands/... -run Refresh`
  (focused).
- Realizes `fb:scaffold-home-skill-sync-mechanism` (sync-verb half); pairs with
  PR #375 "refresh-manages-home".

### Task 3 — `t-registry-disk-audit`

**Title:** `.agentsrc` registry-vs-disk completeness + scope audit in `da doctor`

**Write-scope:**
- `commands/internal/lifecycle/doctor.go`
- `commands/internal/lifecycle/doctor_test.go`

**Acceptance:**
- `da doctor` WARNs when a skill/agent is live on disk (linked under
  `~/.agents/{skills,agents}/…` or a managed project) but **absent from the
  matching `.agentsrc.json` registry array** — the failure mode where a fresh
  `da install` silently skips a live-but-unregistered resource (the
  `fb:agentsrc-registry-completeness` follow-up).
- `da doctor` WARNs on the over-capture direction: a **project** `.agentsrc`
  declaring a **global-scoped** resource that already auto-resolves at the user
  level (coordinate with `InstallGenerateScopeFix`; do not re-narrow scope in
  doctor — surface only).
- Read-only; each warning points at the reconciling command
  (`da promote --force` / regenerate / `da refresh`).
- Fixture tests: linked-but-unregistered skill → warn; project-declared
  global-scoped entry → warn; a clean registry → silent.
- Verified: `go test ./commands/internal/lifecycle/... -run Doctor` (focused).

---

## Related
- Plan `agent-ops-hardening` (active) — fold-backs
  `fb:scaffold-home-skill-sync-mechanism` (OPEN),
  `fb:agentsrc-registry-completeness` (RESOLVED + follow-up).
- Plan `config-v2-migration` (active) — p2 doctor lock-drift, p4f `config verify`
  units-staleness, p4c `config lint`/`verify` (delivers lead instance #3).
- `InstallGenerateScopeFix` — narrows `install --generate` to project scope
  (lead instance #2, scope half).
- Lessons: `single-source-of-truth-across-specs-and-plans` (why merge, not a new
  spec), `shipped-content-must-be-self-contained` (design constraint on any
  starter-content comparator).
- PR #375 — `da refresh` syncs starter content into user home/global.
- KG: no prior decisions on this topic (`da kg query` returned no results —
  graph is thin; grounded on the corpus/plans/code surface instead).
