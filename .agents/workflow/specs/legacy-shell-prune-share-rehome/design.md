# Spec: Legacy Shell Prune and `src/share` Rehome

**ID:** legacy-shell-prune-share-rehome  
**Status:** proposed  
**Created:** 2026-05-11  
**Linked plan:** `.agents/workflow/plans/legacy-shell-prune-share-rehome/`

---

## Problem

The repository still carries a legacy shell-era layout under `src/lib/` and `src/share/`, but that layout is no longer the authoritative product surface.

Three concrete issues now exist:

1. **The runtime CLI is Go-first, but the repo still looks shell-supported.** The current command surface lives in `cmd/`, `commands/`, and `internal/`. The remaining `src/lib/**` shell commands are not on the primary in-repo execution path.

2. **The installer path is stale and appears mismatched with the current launcher.** `scripts/install.sh` still packages `src/lib/` and `src/share/` and still assumes an older installable shell launcher shape. The current `src/bin/dot-agents` is a source-checkout wrapper that resolves `REPO_ROOT` and executes `bin/dot-agents` or `go run ./cmd/dot-agents`, which is a different contract.

3. **`src/share/templates/standard/` mixes incompatible ownership classes.** It currently contains bootstrap files for `~/.agents`, authoring templates for skills/agents, and legacy installer payload content. Those assets should not continue to share one undifferentiated tree.

If this stays unresolved, the repo will keep two failure modes alive:

- contributors will treat `src/lib/**` as a supported parallel implementation when it is not
- distribution/bootstrap changes will keep drifting because there is no single authoritative owner for scaffold assets

## Goals

1. Make the supported installer/bootstrap contract explicit.
2. Give every shipped scaffold asset a single authoritative owner.
3. Re-home runtime-owned scaffold assets into Go-owned, embedded scaffold trees.
4. Remove the remaining legacy shell tree once no supported execution path depends on it.
5. Keep docs/examples available where useful, but clearly separate them from runtime-owned assets.

## Non-Goals

- Reaching bash parity with the Go CLI.
- Preserving `src/lib/**` as a second supported implementation.
- Keeping the existing `src/share/templates/standard/` layout for historical convenience.

## Decisions

### D0: This is a deprecation/removal project, not a parity project

The repo should not continue to invest in parallel Go and shell implementations for the same product surface. The correct end state is a single supported Go implementation plus, at most, a narrowly scoped install wrapper if distribution still requires one.

**Rejected alternative:** maintain the remaining shell commands in parity with Go. Rejected because the current repo architecture, tests, and feature work are all centered on the Go path.

### D1: Runtime scaffold assets belong under `internal/scaffold/**`

The repo already has a strong precedent for runtime-owned scaffold assets: `internal/scaffold/hooks/` plus `go:embed`. The same pattern should be extended for any other runtime scaffold content that must ship with the CLI.

Recommended structure:

- `internal/scaffold/home/`
  - assets used to seed `~/.agents/`
- `internal/scaffold/templates/`
  - assets used by authoring commands such as `skills new` and `agents new`

**Rule:** if the CLI needs the asset at runtime, it belongs under `internal/scaffold/**`.

### D2: `src/share` assets must be classified by ownership before they move

Every file under `src/share/templates/standard/` should be labeled as exactly one of:

- `runtime-bootstrap`
- `runtime-authoring-template`
- `docs-example`
- `delete`

This avoids the common cleanup failure mode of moving everything first and deciding ownership later.

### D3: Keep the installer, but replace it with target-aware installation

The repo should keep `scripts/install.sh` as a supported installation path, but the implementation must be replaced.

Required installer behavior:

1. The installer defaults to the Go CLI target.
2. The installer exposes an explicit target selector for the TS port target.
3. The installer no longer depends on `src/lib/` or `src/share/` payload packaging.

Recommended interface:

- CLI flag: `--port go|ts`
- environment fallback: `DOT_AGENTS_PORT=go|ts`

Target expectations:

- `go` installs the supported Go binary / release asset.
- `ts` installs the TypeScript port variant using the accepted TS binary name `da-ts`, and validates the documented Node.js prerequisite rather than pretending the TS port is a drop-in binary-identical replacement.

**Not acceptable:** keeping `src/lib` alive only because the current installer script still expects it.

### D4: `dot-agents init` seeds curated starter home content

The supported bootstrap contract should preserve a starter bundle for shared-home initialization.
That starter bundle should move into a Go-owned scaffold home instead of staying in the old
`src/share` payload tree.

- `dot-agents init` creates canonical directories plus starter home-owned files such as
  `.gitignore`, `README.md`, `rules/global/rules.mdc`, and supported settings/config stubs
- `dot-agents init` may continue to scaffold embedded runtime bundles that are already part of the
  Go-owned surface, such as `internal/scaffold/hooks`
- `dot-agents init` should seed curated starter global skills and starter global agents from
  shipped runtime assets where the project intends them to exist by default
- `dot-agents skills new` and `dot-agents agents new` remain the authoring entrypoints for new
  skill and agent manifests

This introduces one explicit migration gap relative to the current implementation direction:

- `commands/init.go` already writes starter files directly and scaffolds embedded hook bundles
- `commands/skills.go` and `commands/agents/new.go` already synthesize authoring templates in Go
  rather than copying them from `src/share`
- `commands/init.go` does not currently seed the legacy starter skill bundle, so P2 must re-home
  that bundle into Go-owned scaffold assets and teach `init` to install it from there

Consequence for classification:

- `src/share/templates/standard/skills/global/**` is runtime-bootstrap content
- repo-local/project config examples under `src/share/templates/standard/agentsrc.json`,
  `config.json`, and `settings/project/**` are not authoritative runtime home assets

### D5: Docs-only examples should live outside the runtime asset tree

Any example templates worth keeping for documentation should move under a docs-owned location such as:

- `docs/examples/`
- `docs/reference/templates/`

This keeps runtime-owned and docs-owned content from drifting together again.

## Requirements

### R1: Asset classification inventory

Before any major tree deletion, the plan must produce a stable inventory for all `src/share/templates/standard/**` files with one ownership label per file. The inventory is part of the migration contract and should be easy for future contributors to read.

### R2: Single runtime owner for scaffold content

After migration, no scaffold asset should have two authoritative runtime sources. If a file is runtime-owned, the CLI should read it from the canonical Go-owned scaffold tree rather than keeping an equivalent inline string and a template file in parallel.

### R3: Installer/bootstrap compatibility is tied to supported paths only

Verification should focus on whatever installation/bootstrap path the project explicitly supports after the decision gate. Unsupported legacy paths should not hold the repo structure hostage.

### R3a: Installer target selection is explicit

The supported installer must make target selection explicit rather than implicit:

- default install target: Go CLI
- explicit alternate install target: TS port (`da-ts`)

The selection surface should be stable enough to document in both the main installer instructions and the TS-port README.

### R3b: Bootstrap ownership is minimal and command-owned

The supported bootstrap path should preserve this split:

- `dot-agents init` owns the shared-home starter bundle, including starter skills/agents plus
  embedded runtime bundles
- `dot-agents skills new` owns skill authoring manifests/templates
- `dot-agents agents new` owns agent authoring manifests/templates

The starter bundle may be copied into `~/.agents` during initialization, but the authoritative
source must live under `internal/scaffold/**`, not `src/share/**`.

### R4: Shell tree deletion happens only after dependency removal

`src/lib/**` and any runtime payloads under `src/share/**` may be deleted only after:

- runtime-owned assets have a new authoritative home, and
- the supported installer/bootstrap path no longer depends on the old packaging layout

### R5: Reachability proof after cleanup

Post-migration, a repository search for `src/lib` and `src/share/templates/standard` should only return intentional history/docs references. No supported code path should reference `src/lib/**`.

## Asset Inventory

| Path | Classification | Rationale |
|---|---|---|
| `src/share/templates/standard/.gitignore` | `runtime-bootstrap` | `init` owns this as a shared-home starter file. |
| `src/share/templates/standard/README.md` | `runtime-bootstrap` | `init` owns this as a shared-home starter file. |
| `src/share/templates/standard/agents/_template/AGENT.md` | `runtime-authoring-template` | Canonical starter manifest for `agents new`. |
| `src/share/templates/standard/agentsrc.json` | `docs-example` | Project-local config example, not a shared-home runtime asset. |
| `src/share/templates/standard/config.json` | `delete` | `init` generates this directly; checked-in template is not authoritative. |
| `src/share/templates/standard/rules/global/rules.mdc` | `runtime-bootstrap` | `init` owns this as a shared-home starter file. |
| `src/share/templates/standard/settings/global/claude-code.json` | `runtime-bootstrap` | `init` owns this as a shared-home starter file. |
| `src/share/templates/standard/settings/project/claude-code.json` | `docs-example` | Project-scoped config example, not home bootstrap. |
| `src/share/templates/standard/skills/_template/SKILL.md` | `runtime-authoring-template` | Canonical starter manifest for `skills new`. |
| `src/share/templates/standard/skills/global/agent-handoff/SKILL.md` | `runtime-bootstrap` | Starter skill content that should seed shared-home initialization. |
| `src/share/templates/standard/skills/global/agent-start/SKILL.md` | `runtime-bootstrap` | Starter skill content that should seed shared-home initialization. |
| `src/share/templates/standard/skills/global/build-graph/SKILL.md` | `runtime-bootstrap` | Starter skill content that should seed shared-home initialization. |
| `src/share/templates/standard/skills/global/build-graph/instructions/gotchas.md` | `runtime-bootstrap` | Supporting content for a seeded starter skill. |
| `src/share/templates/standard/skills/global/review-delta/SKILL.md` | `runtime-bootstrap` | Starter skill content that should seed shared-home initialization. |
| `src/share/templates/standard/skills/global/review-delta/instructions/gotchas.md` | `runtime-bootstrap` | Supporting content for a seeded starter skill. |
| `src/share/templates/standard/skills/global/review-delta/instructions/workflow.md` | `runtime-bootstrap` | Supporting content for a seeded starter skill. |
| `src/share/templates/standard/skills/global/review-delta/templates/review-output.md` | `runtime-bootstrap` | Supporting content for a seeded starter skill. |
| `src/share/templates/standard/skills/global/review-pr/SKILL.md` | `runtime-bootstrap` | Starter skill content that should seed shared-home initialization. |
| `src/share/templates/standard/skills/global/review-pr/instructions/gotchas.md` | `runtime-bootstrap` | Supporting content for a seeded starter skill. |
| `src/share/templates/standard/skills/global/review-pr/instructions/workflow.md` | `runtime-bootstrap` | Supporting content for a seeded starter skill. |
| `src/share/templates/standard/skills/global/review-pr/templates/review-output.md` | `runtime-bootstrap` | Supporting content for a seeded starter skill. |
| `src/share/templates/standard/skills/global/self-review/SKILL.md` | `runtime-bootstrap` | Starter skill content that should seed shared-home initialization. |

## Done Criteria

This spec is satisfied when all of the following are true:

1. The supported installer/bootstrap path is explicitly documented and verified.
2. The supported installer can choose between the default Go target and the explicit TS-port target.
3. Runtime-owned scaffold assets live under `internal/scaffold/**` with one authoritative source.
4. `dot-agents init`, `dot-agents skills new`, and `dot-agents agents new` continue to work after scaffold ownership is clarified, with `init` seeding the curated starter bundle and `skills new` / `agents new` remaining command-owned authoring paths.
5. No supported execution path references `src/lib/**`.
6. The remaining legacy shell tree has been deleted, or the only retained shell code is a deliberately supported minimal installer wrapper.

## Deferred

- Reworking documentation/history artifacts that intentionally preserve old shell references for retrospective context.
- Any broader redesign of install UX beyond what is needed to remove the stale `src/lib`/`src/share` payload dependency.
