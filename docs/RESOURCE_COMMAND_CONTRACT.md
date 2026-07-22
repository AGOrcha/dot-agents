---
title: Resource Command Contract
description: The lifecycle contract for managed resources: hooks, rules, MCP servers, and settings.
sidebar:
  order: 4
---

# Resource command contract (hooks, rules, MCP, settings)

This document is the **canonical contract** for the
[`resource-command-parity`](../.agents/history/resource-command-parity/resource-command-parity.plan.md)
plan. Phases 2–5 and future work should cite this file (and the plan) instead of duplicating scope
rules.

## Goals

- Make the **lifecycle story** for each managed resource family explicit: what users can do from
  dedicated commands versus what remains **implicit** through shared flows.
- Keep **one shared planner/executor path** for mutations (`add`, `import`, `refresh`, `install`,
  `remove`, and related readback). New per-resource commands must delegate into that path — no
  parallel ad hoc emitters.
- Document **out-of-scope** resources so the CLI is not pressured to grow a uniform surface for
  everything.

## Strategic shape: per-resource command families

The CLI exposes **per-resource Cobra families** (`hooks`, `rules`, `mcp`, `settings`) with
**shared internals** (planner, platform projection, manifest updates).

Rationale:

- **User mental model:** `da hooks …` matches how people already talk about hook bundles;
  burying hooks under a generic `resources` command would add indirection without reducing code
  duplication.
- **Implementation:** shared mutation/projection code lives in `internal/` packages
  (`internal/links`, `internal/projectsync`, `internal/scaffold`); the per-resource command code
  is a thin composition adapter. After the **root-command-decomposition** plan landed, each family's
  command package is a leaf rather than a flat `commands/*.go` file: `hooks` lives at
  `commands/hooks/`, while `rules`, `mcp`, and `settings` live under `commands/internal/{rules,mcp,settings}/`
  (alongside `commands/internal/lifecycle/` for `install`/`doctor`/`init`/`refresh`/`status`). Root
  (`commands/root.go`) is a composition root that wires each leaf in via its constructor
  (`NewHooksCmd`, `rules.NewRulesCmd`, `mcp.NewCmd`, `settings.NewCmd` — `commands/root.go:210-213`).
  That satisfies the guardrail above without forcing a single CLI noun for all resources.
- **Shared list/show/remove helper:** `rules`, `mcp`, and `settings` do not hand-write their
  `list`/`show`/`remove` flows. Each leaf declares one static `cmdutil.CanonicalResourceDef`
  (`commands/internal/cmdutil/resources.go`: `RulesResource`, `MCPResource`, `SettingsResource`),
  binds its per-verb runner closures through `cmdutil.SpecForResource`, and routes the tree and
  executors through the shared `cmdutil.NewCanonicalResourceCmd` and
  `cmdutil.RunCanonical{List,Show,Remove}` helpers
  (`commands/internal/cmdutil/canonical_cmd.go:37-102`, `canonfile.go`). The three leaves' own
  `RunList`/`RunShow`/`RunRemove` are one-line forwarders into those executors
  (`commands/internal/{rules,mcp,settings}/{list,show,remove}.go`). `hooks` is the exception: it
  keeps a hand-written `list`/`show`/`remove` path (`commands/hooks/{list,show,remove}.go`) because
  its two legacy surfaces (below) fall outside the canonical-file helper's shape.
- **Rollout:** the families shipped incrementally (hooks first); the cross-cutting readback
  commands (`status`, `explain`, `doctor`) are aligned with the same model.

## Managed resource families (in scope for this plan)

| Family   | Dedicated lifecycle commands (CLI) | Also touched indirectly |
|----------|-------------------------------------|-------------------------|
| **Hooks** | `hooks list`, `hooks show`, `hooks remove` | `import`, `refresh`, `install`, `remove`, `status`, `doctor` |
| **Rules** | `rules list`, `rules show`, `rules remove` | `add`, `import`, `refresh`, `install`, `remove`, `status`, `doctor` |
| **MCP** | `mcp list`, `mcp show`, `mcp remove` | same as rules |
| **Settings** | `settings list`, `settings show`, `settings remove` | same as rules |

Canonical hook storage and bundle layout: `~/.agents/hooks/<scope>/<logical-name>/HOOK.yaml`
bundles (optionally with sidecar scripts), where `<scope>` is `global` or a managed
project name (see `da hooks --help`). The hooks family also spans two **legacy**
surfaces so pre-bundle setups stay inspectable:

- **Legacy single-file JSON hooks** (`~/.agents/hooks/<scope>/<name>.json`) are still
  listed, shown, and removed alongside canonical bundles — `hooks list`/`show` label the
  source kind so bundles and legacy files are distinguishable; prefer `HOOK.yaml` for new
  work (import/refresh canonicalize legacy content into bundles).
- **Legacy settings projection (read-only fallback):** when a scope has no canonical or
  legacy hook specs, `hooks list` falls back to projecting the `hooks` block of
  `~/.agents/settings/<scope>/claude-code.json` under a "legacy settings projection"
  header. It is display-only — no dedicated mutation path writes that file through the
  hooks family.

## Explicitly out of scope here

- **Agents** — tracked under **agent-resource-lifecycle** (`agents list`, `agents new`, …).
- **Context, memory, profiles** — no lifecycle command surface by design unless a future contract
  extends this document.

## Readback and cross-cutting commands

These commands summarize or explain **multiple** resource families. They must stay consistent with
this contract (no implying a dedicated lifecycle where none exists yet):

- `status`, `explain`, `doctor`, `install`, `remove`

Phase **5** aligned readback/install/remove copy with this model; **rules** have a dedicated family
(phase **3**); **MCP** and **settings** have dedicated families (phase **4**).

## Retrofit: shipped phases vs this contract

| Phase | Role relative to contract |
|-------|---------------------------|
| **2 — hooks lifecycle** | Shipped `hooks list`, `hooks show`, `hooks remove` on top of canonical `HOOK.yaml` bundles; matches the “per-resource family + shared executor” shape. |
| **5 — readback alignment** | Updated user-visible surfaces so readback and lifecycle wording match the contract (including “implicit until dedicated commands exist”). |
| **3 — rules lifecycle** | Shipped `rules list`, `rules show`, `rules remove` for canonical `~/.agents/rules/` files; matches the per-resource family + shared link/refresh model. |
| **4 — MCP, settings** | Shipped `mcp list`, `mcp show`, `mcp remove` and `settings list`, `settings show`, `settings remove` over canonical `~/.agents/mcp/` and `~/.agents/settings/` trees; matches per-resource family + shared link/refresh model. |

## Canonical task graph note (historical)

This plan is complete: phases 1–5 all shipped and the plan is archived under
`.agents/history/resource-command-parity/`. (Historical note: during
implementation the DAG briefly showed phase 5 completed ahead of phases 3–4;
that drift was reconciled at archive time.)

## Change process

1. Update this document and `.agents/history/resource-command-parity/resource-command-parity.plan.md`.
2. Adjust tests or help text in `commands/` when boundaries or naming change.
3. Run `go test ./commands/...` (or broader) before merge.

> **Boundary note (maintainers):** The `commands/internal/{lifecycle,mcp,settings,rules}` leaves are
> composition targets and **must not import each other**. `tools/importguard` enforces this in CI;
> outsider imports are already blocked by Go's `internal/` package rule. A new resource leaf added
> under `commands/internal/` should be added to `tools/importguard/main.go`'s guarded set.
