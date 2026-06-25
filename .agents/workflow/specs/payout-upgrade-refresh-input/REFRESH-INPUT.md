# Payout upgrade/redesign — REFRESH INPUT (not a plan)

Status: inputs-only · Owner: dot-agents · Created: 2026-06-24

> **READ THIS FIRST.** These are **inputs to merge into the EXISTING
> payout-upgrade/redesign spec+tasks**, which the maintainer has in **local,
> unpushed WIP** (TCC-locked, not readable from here). This file is **NOT a
> competing plan** and intentionally creates no PLAN.yaml/TASKS.yaml. When the
> maintainer surfaces the existing spec, fold the sections below into it.

## 1. Prior-artifact finding — NOT on the remote

A comprehensive search found **no payout-upgrade / payout-redesign spec or plan on
the AGOrcha/dot-agents remote** (master or any branch):

- `git ls-tree` over **every** remote branch for `specs/payout*`,
  `plans/payout-upgrade*`, `plans/payout-redesign*` → **zero hits**.
- Remote branches mentioning "payout": only **`origin/workflow/payout-upgrade-forward`**.
- `grep -ril payout` over `.agents/workflow/specs`, `.agents/workflow/plans`,
  `.agents/proposals`, `.agents/history`, and memory/handoff on master → the only
  payout-upgrade-shaped artifact is what PR #153 introduced.

What **does** exist on the remote (related but distinct):

- **PR #153 `workflow/payout-upgrade-forward` (OPEN, not merged)** — adds task
  **`p8-payout-wrk-v2-upgrade`** (status `ready`) to the **`config-v2-migration`**
  plan, a PLAN.yaml pointer comment, and
  `.agents/history/config-v2-migration/payout-wrk-v2-upgrade-brought-forward.md`.
  This is a *brought-forward task*, **not** a payout-upgrade/redesign spec.
- **`p6-payout-backfill` / `p7-legacy-override-migration`** in
  `loop-discipline-stop-hooks` — **unrelated scope**: hook-discipline backfill to the
  LOCAL `/Users/nikashp/Documents/payout`, not the `payout-wrk` v1→v2 config upgrade.

**Conclusion:** the maintainer's payout-upgrade/redesign spec+tasks are **not
published**; they live in this machine's **local unpushed WIP**, currently
unreadable (TCC lock). Do not assemble a fresh competing plan — surface and reuse the
maintainer's. The `p8-payout-wrk-v2-upgrade` **task id must be preserved** when
merging these inputs.

## 2. Starter-vs-project-specific skill/agent classification

Cross-referenced against the live scaffold on master:
`internal/scaffold/home/starter/skills/global/` and
`internal/scaffold/home/starter/agents/global/`.

**Scaffold ships these starter skills (14):** `agent-handoff`, `agent-start`,
`build-graph`, `delegation-lifecycle`, `isp`, `iteration-close`, `loop-worker`,
`orchestrator-session-start`, `plan-wave-picker`, `provider-consumer-pair`,
`review-delta`, `review-pr`, `self-review`, `skill-architect`.

**Scaffold ships these starter agents (4):** `acceptance-invariants-reviewer`,
`adversarial-reviewer`, `architecture-standards-reviewer`, `loop-worker`.

### payout's 16 declared skills

| Skill | Class | In scaffold global skills? |
|---|---|---|
| agent-handoff | **STARTER** | yes |
| agent-start | **STARTER** | yes |
| build-graph | **STARTER** | yes |
| delegation-lifecycle | **STARTER** | yes |
| iteration-close | **STARTER** | yes |
| plan-wave-picker | **STARTER** | yes |
| provider-consumer-pair | **STARTER** | yes |
| review-delta | **STARTER** | yes |
| review-pr | **STARTER** | yes |
| self-review | **STARTER** | yes |
| skill-architect | **STARTER** | yes |
| create-subagent | **PROJECT-SPECIFIC** | no (likely superseded by starter orchestrator-session-start/isp/loop-worker) |
| gh-fix-ci | **PROJECT-SPECIFIC** | no |
| playwright | **PROJECT-SPECIFIC** | no |
| split-reviewable-commits | **PROJECT-SPECIFIC** | no |
| **payout-session-start** | **PROJECT-SPECIFIC** | no — payout's own variant; MUST be sourced |

Tally: **11 starter / 5 project-specific** (`payout-session-start` is the
genuinely-bespoke one; `create-subagent`/`gh-fix-ci`/`playwright`/
`split-reviewable-commits` are general-purpose skills that simply are not in the
scaffold's *starter* set, so payout must source them or drop ones now superseded).

### payout's 3 declared agents

| Agent | Class | In scaffold global agents? |
|---|---|---|
| test-runner | **PROJECT-SPECIFIC** | no |
| verifier | **PROJECT-SPECIFIC** | no (the scaffold ships reviewer agents + stage_profiles verifier dispatch, not a `verifier` agent dir) |
| security-reviewer | **PROJECT-SPECIFIC** | no (closest starters: `adversarial-reviewer`, `architecture-standards-reviewer`) |

All 3 agents are **not** in the scaffold starter set → payout must source them, or
map them onto starter reviewer agents / the `stage_profiles` verifier dispatch.

> Caveat: starter = scaffold default. Whether a starter skill is *auto-linked* on a
> given box depends on the installed dot-agents version + `da refresh`. The refresh
> direction is: **drop explicit refs to the 11 starter skills** (they arrive by
> default) and **keep+source only the project-specific ones**. Final disposition of
> `create-subagent`/`test-runner`/`verifier`/`security-reviewer` (source vs map vs
> drop-as-superseded) is confirmed against the live Windows box during execution.

## 3. New-info delta (since the existing plan was created)

Fold these into the existing spec — each changes its assumptions:

- **`da config migrate` is MERGED to master** (`commands/config/migrate.go`,
  `commands/config/migrate_test.go`) and **verified working on Windows** (writes v2 +
  `.agentsrc.json.v1.bak`, folds deprecated keys, idempotent). If the existing plan
  encodes "da config migrate not on master yet" as a blocker, that blocker is
  **CLEARED**.
- **config-v2 §15 unified-units model** (`specs/config-distribution-model/design.md`
  §15): a single units section keyed by `source:path@version`, top-level
  `inputs_digest`, and `sources`/`extends`/`packages` with **source-kind
  orthogonality including OCI**. v2 manifests validate against §15 — the migration
  target is §15, not the old two-tier (§1–§2) framing.
- **0.4.0 shipped** (VERSION=0.4.0); the payout upgrade ships with **0.4.1** per
  maintainer.
- **The starter skill set now EXISTS** in the scaffold (§2). This is *why* the
  manifest must drop explicit starter refs.
- **verifier/reviewer `stage_profiles` + cross-harness adversarial reviewers** now
  exist; migrate folds deprecated `verifier_profiles` / `reviewer_profiles` /
  `app_type_verifier_map` keys into the unified `stage_profiles` /
  `execution_profile` model.
- **Orchestrator/executor prompt machinery** (starter `orchestrator-session-start` /
  `isp` / `loop-worker` + reviewer agents) likely **supersedes** payout's
  `create-subagent` ref — flag for confirm-and-drop.
- **KG-as-SOT direction**: SDD artifacts trend toward the knowledge graph as
  system-of-record; keep the payout readback structured/KG-ingestible.

## 4. Source-content-gap resolution options

`da install` skips all 16 skills + 3 agents as "not found in any source" — a
**source-content gap, NOT a code bug** (verified on Windows; sources = local
`~/.agents` + git `NikashPrakash/agents-config`). Two complementary moves resolve it:

1. **Trim** the manifest so it no longer references the **11 starter skills** (they
   arrive by default). This alone removes 11 of the 16 skips.
2. **Populate the declared source** with the remaining project-specific resources —
   primarily **`payout-session-start`**, plus any kept non-starter skill/agent — in
   the git source `NikashPrakash/agents-config` and/or local `~/.agents`. They are
   absent today, which is the proximate cause of the skips.

After (1)+(2) the redesigned v2 manifest references only resources that exist in a
declared source → a **fresh install-from-git-source resolves clean with zero skips**.

## 5. Also-carry-forward (from PR #153's brought-forward note)

- **Windows "failed to get the local" install error** — not a literal in the
  codebase; runtime symptom. Most plausible: local-source resolve
  (`internal/config/local_source.go`, `local source: …`) reached via the
  install-from-git path, which on Windows shells out to a real git binary
  (`commands/internal/lifecycle/install.go` `CloneGitSource` →
  `exec.LookPath("git")` + `git clone --depth 1`). Candidates: git not on PATH;
  submodule handling in the shallow clone (payout-wrk has submodules); `~/.agents`
  not initialized. Durable fix if PATH/clone is the cause: fold-back
  `fb:consolidate-clone-on-gogit` (in-process go-git). Diagnose **before** the live
  migrate/install.
- **Preserve task id `p8-payout-wrk-v2-upgrade`** when merging these inputs into the
  existing spec/plan.
