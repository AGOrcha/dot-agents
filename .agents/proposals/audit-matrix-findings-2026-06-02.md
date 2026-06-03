# Audit-matrix findings — 2026-06-02 (release-docs-refresh, parallel sweep)

First proactive run of the release-docs-refresh **audit matrix** with parallel
per-category sub-auditors. global-flags + platform-dirs were already swept this release
(folded into #9); this captures the remaining categories. **2 clean, 4 with drift.**

## Clean (no findings)
- **hooks-events** (`HOOKS.md` vs `internal/platform/hooks.go` tables) — every canonical→
  platform mapping matches; OpenCode correctly documented as unsupported.
- **resource-commands** (`RESOURCE_COMMAND_CONTRACT.md` vs `commands/{rules,hooks,mcp,settings}`)
  — shared-executor contract + flag behavior on `remove` all align.

## Doc-stale (doc lags code → DOC fixes; small ones could fold, larger are follow-ups)
- **skill-command-integration** (`SKILL_COMMAND_INTEGRATION.md`): skill-architect described
  as "future" but it SHIPS + is provider-pluggable; `da config explain/verify` absent;
  hypothetical `da kg <verb>` calls vs the real `da kg query --intent` / `bridge query`
  surface.
- **workflow-commands** (`WORKFLOW_CLIENT_COMMANDS.md`): only ~8 primitives listed; ~27
  real subcommands (merge-back, fold-back, delegation, contract, drift, sweep, bundle,
  hook-*, archive-orphans, plan schedule/derive-scope/check-scope…) undocumented;
  `workflow commit` flags absent; deferred iteration-close section reads as current.
- **DEMO** (`DEMO_README.md`/`DEMO_*`): `da config` missing from "what's in the box";
  platform diagram routes the 5 platforms unevenly; top-level commands list omits
  remove/import/sync/review/session/kg/score; `da explain` topics undersold; an OpenCode
  "…" ellipsis implying >5 platforms.

## Code-violates-contract / design (CODE work → route to plans, NOT release)
- **error-messages** (`ERROR_MESSAGE_CONTRACT.md`): ~5 code sites use raw `fmt.Errorf`
  where the contract requires typed `ErrorWithHints`/`UsageError`
  (`commands/agents/{import,remove}.go`, `commands/workflow/cmd.go` flag validation,
  `commands/kg/sync_code_warm_link.go` arg-shape errors); recovery prose embedded in
  messages instead of hints; `--json` error envelope undocumented (design-issue). → an
  error-contract compliance task.
- **workflow commit `--dry-run`** (`commands/workflow/commit_cmd.go`): local flag ignores
  the global `-n` (sibling of the `da config --json` class; `archive_orphans.go` shows the
  correct `deps.Flags.DryRun()` OR-merge). → `global-flag-compliance` plan.

## Routing recommendation
- **Let #9 (release) merge as-is** — it already carries the global-flags + config-`--json`
  fixes; these findings are not regressions, just stale docs + pre-existing code gaps.
- **Doc-stale** → a "post-0.3.4 docs reconciliation" pass (skill-command-integration +
  workflow-commands + DEMO). The small DEMO/`da config` mentions could fold if the release
  is held; otherwise follow-up.
- **Code gaps** → `error-messages` to an error-contract task; `workflow commit --dry-run`
  to `global-flag-compliance` (route via `da workflow fold-back` once the release lands).
- Validates the **proactively-apply-built-capabilities** lesson: the sweep found this the
  moment the capability existed.
