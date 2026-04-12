# Canonical Plugin Contract

Status: Directional
Last updated: 2026-04-12
Depends on: `docs/PLATFORM_DIRS_DOCS.md`, `docs/rfcs/resource-intent-centralization-rfc.md`

This document defines the canonical plugin bundle contract for `dot-agents` on the current shared planner/executor architecture. It is a storage and ownership contract, not runtime wiring.

## Platform Plugin Landscape

All five platforms have first-class plugin support at project and user scope (verified 2026-04-12; see `docs/PLATFORM_DIRS_DOCS.md` for source links). The native formats differ significantly:

| Platform | Plugin kind | Project manifest path | User/global path | Marketplace path |
|----------|-------------|----------------------|-----------------|-----------------|
| Cursor | `package` | `.cursor-plugin/plugin.json` | `~/.cursor/plugins/local/<name>/` | `.cursor-plugin/marketplace.json` |
| Claude Code | `package` | `.claude-plugin/plugin.json` | _(via `enabledPlugins` in settings)_ | `.claude-plugin/marketplace.json` |
| Codex | `package` | `.codex-plugin/plugin.json` | `~/.codex/plugins/` | `.agents/plugins/marketplace.json` _(canonical!)_ |
| GitHub Copilot | `package` | `plugin.json` / `.github/plugin/plugin.json` | `~/.copilot/state/installed-plugins/` | `marketplace.json` |
| OpenCode | `native` | `.opencode/plugins/<name>.{js,ts}` | `~/.config/opencode/plugins/` | _(npm-based)_ |

**Two plugin kinds:**
- `package` — Cursor/Claude/Codex/Copilot: installable bundle described by a `plugin.json` manifest; can contain component directories (`agents/`, `skills/`, `commands/`, `hooks.json`, `.mcp.json`).
- `native` — OpenCode: executable JS/TS file(s) with optional npm dependencies via `.opencode/package.json`.

**Codex marketplace path note:** Codex documents `$REPO_ROOT/.agents/plugins/marketplace.json` as a first-party marketplace source — directly inside our canonical storage location and a natural fit for a dot-agents managed registry.

## Canonical Storage

Target canonical layout:

```text
~/.agents/
  plugins/
    global/
      review-toolkit/          ← platform-neutral name
        PLUGIN.yaml
        resources/
          agents/
          skills/
          commands/
          hooks/
          mcp/
        files/                 ← native JS/TS files for OpenCode runtime plugins
        platforms/
          cursor/
            plugin.json        ← Cursor-native manifest passthrough
          codex/
            plugin.json        ← Codex-native manifest passthrough
          opencode/            ← OpenCode-specific overlay files
    my-project/
      custom-export-helper/
        PLUGIN.yaml
        ...
  marketplace.json             ← optional Codex-compatible registry (auto-generated)
```

**Directory roles:**
- `PLUGIN.yaml` — canonical dot-agents manifest; owned by dot-agents
- `resources/{agents,skills,commands,hooks,mcp}/` — canonical shared resources emitted into platform bundles
- `files/` — native plugin source files (JS/TS for OpenCode runtime plugins; scripts, assets for others)
- `platforms/{platformID}/` — platform-specific passthrough content (e.g. native `plugin.json`) that cannot be cleanly represented in the shared model

## Canonical Manifest

`PLUGIN.yaml` schema:

```yaml
schema_version: 1
kind: package                  # package | native
name: review-toolkit
version: "1.0.0"
display_name: Review Toolkit
description: Shared plugin bundle for all platforms
authors:
  - Nikash Prakash
homepage: https://github.com/example/review-toolkit
license: MIT
platforms:
  - claude
  - cursor
  - codex
resources:
  agents:
    - reviewer
  skills:
    - review-pr
  commands:
    - pr-summary
  hooks:
    - lint-on-edit
  mcp:
    - github
marketplace:
  repo: https://github.com/example/review-toolkit
  tags:
    - review
    - git
platform_overrides:
  codex:
    category: Productivity
  opencode:
    marketplace_name: review-toolkit-opencode
```

### Field semantics

- `schema_version`: versioned manifest contract; current contract is `1`
- `kind`: `package` (Cursor/Claude/Codex/Copilot installable bundle) or `native` (OpenCode JS/TS executable)
- `name`: stable logical plugin identifier; should match the bundle directory name under `plugins/{scope}/`
- `platforms`: required list of platform IDs this plugin targets; at least one required; controls which emitters run
- `version`: optional semantic version string; surfaced in marketplace.json listings and platform manifest generation
- `display_name`: optional human-friendly name for marketplace listings
- `description`: human-readable summary of the bundle
- `authors`: optional list of author names; first entry used as primary author in generated manifests
- `homepage`: optional documentation or project URL
- `license`: optional SPDX identifier
- `resources`: optional sub-struct declaring canonical shared components bundled in this plugin
- `marketplace`: optional marketplace registration hints (`repo`, `tags`) for marketplace.json generation
- `dependencies`: optional platform-native dependency declarations (e.g. npm packages for OpenCode runtime plugins)
- `platform_overrides`: platform-specific render hints; common keys: `category`, `marketplace_name`, `path`, `file`; keep narrow so the canonical bundle stays platform-neutral

## Ownership Boundaries

- `shared_repo`: canonical bundle contents under `~/.agents/plugins/{scope}/{name}/`
- `platform_repo`: emitted platform-local projections (`~/.cursor/plugins/local/`, `.cursor-plugin/`, `.claude-plugin/`, `.codex-plugin/`, `.github/plugin/`, `.opencode/plugins/`)
- `user_home`: global plugin directories (`~/.config/opencode/plugins/`, `~/.copilot/state/installed-plugins/`, `~/.cursor/plugins/local/`) — managed by the platform itself after installation; dot-agents writes only to project-scope targets

The canonical bundle is owned once. Platform-specific outputs are projections owned by the platform adapter.

## Shared Planner / Executor Mapping

- Bucket: `plugins`
- Logical name: bundle name from `PLUGIN.yaml`
- Source kind: `canonical_bundle`
- Canonical source ownership: `shared_repo`
- Projection ownership: `platform_repo`

The shared planner dedupes canonical plugin bundles before any platform-local writer runs. Each platform's `SharedTargetIntents` method calls `BuildSharedPluginBundleIntents` with its own native target path. Platforms without an emitter yet simply omit the call — see `internal/platform/resource_plan.go` doc comment on `BuildSharedPluginBundleIntents`.

## Emitter Implementation Status

| Platform | Emitter implemented | Target path | Notes |
|----------|--------------------|---------|----|
| OpenCode | Yes | `.opencode/plugins/{name}/` | `kind: native` only; emits `files/` + `platforms/opencode/` overlay |
| Cursor | No | `.cursor-plugin/` | `kind: package`; generate or copy `plugin.json` from `platforms/cursor/` |
| Claude Code | No | `.claude-plugin/` | `kind: package`; generate or copy `plugin.json` from `platforms/claude/` |
| Codex | No | `.codex-plugin/` | `kind: package`; generate or copy `plugin.json` from `platforms/codex/` |
| GitHub Copilot | No | `.github/plugin/` or repo root `plugin.json` | `kind: package`; generate or copy from `platforms/copilot/` |

**Emission strategy for package-manifest platforms:** if `platforms/{platformID}/plugin.json` exists in the bundle, copy it to the target. If not, generate a minimal `plugin.json` from `PLUGIN.yaml` fields (`name`, `version`, `description`, `display_name`, first `authors` entry). Never symlink `plugin.json` — platforms that install from local paths read the file directly.

## Import Detection and PLUGIN.yaml Scaffolding

`dot-agents import` detects orphaned platform plugin artifacts and imports them into `~/.agents/plugins/{scope}/{name}/`. Import **always scaffolds `PLUGIN.yaml`** from whatever fields can be extracted from the native artifact — the user should not have to write one from scratch.

| Orphaned path | Import behavior |
|--------------|-----------------|
| `.opencode/plugins/{name}/` | `kind: native`; scaffold `PLUGIN.yaml` with `platforms: [opencode]`; copy files into `files/` |
| `.cursor-plugin/plugin.json` | `kind: package`; read fields from JSON → scaffold `PLUGIN.yaml`; store JSON at `platforms/cursor/plugin.json` |
| `.claude-plugin/plugin.json` | `kind: package`; read fields from JSON → scaffold `PLUGIN.yaml`; store JSON at `platforms/claude/plugin.json` |
| `.codex-plugin/plugin.json` | `kind: package`; read fields from JSON → scaffold `PLUGIN.yaml`; store JSON at `platforms/codex/plugin.json` |
| `.github/plugin/plugin.json` or root `plugin.json` | `kind: package`; read fields from JSON → scaffold `PLUGIN.yaml`; store JSON at `platforms/copilot/plugin.json` |

**Scaffolded `PLUGIN.yaml` mapping from native `plugin.json`:**

| `plugin.json` field | `PLUGIN.yaml` field |
|--------------------|---------------------|
| `name` | `name` |
| `version` | `version` |
| `description` | `description` |
| `display_name` | `display_name` |
| `authors` / `author.name` | `authors` |
| `homepage` | `homepage` |
| `license` | `license` |
| `repository` | `marketplace.repo` |
| `keywords` | `marketplace.tags` |

The native `plugin.json` is preserved verbatim at `platforms/{platformID}/plugin.json` so no data is lost. Fields not representable in the shared model stay in `platform_overrides`.

## Current Architecture Notes

- All five platforms have first-class plugin support; only OpenCode has an emitter today
- The canonical bundle contract intentionally stays platform-neutral so the storage model supports all emitters without format lock-in
- Codex's native marketplace path (`$REPO_ROOT/.agents/plugins/marketplace.json`) aligns naturally with our canonical storage — a future `dot-agents plugins marketplace` command could generate this file from all enabled bundles
- The donor branch `claude/scalable-skill-syncing-sfxOd` is historical provenance only; the current tree already landed `canonicalPackagePluginManifestOutputs`, `canonicalPluginOutputsFromOpenCodeFile`, `LoadPluginSpec`, `ListPluginSpecs`, and `syncPluginOverlayTree`, so Stage 2 planning should build from that rebuilt baseline
- Runtime implementation, multi-platform emitters, and any remaining Stage 2 bucket-expansion slices are tracked in the `plugin-resource-salvage` plan
