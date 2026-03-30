# Canonical Hooks Design

This document defines the target canonical storage model for hooks in `~/.agents`, the shared internal hook contract, and the staged migration path from the current flat per-platform files.

## Goals

- Keep exactly one source of truth for each logical hook under `~/.agents`
- Let platforms consume hooks directly when their native format matches closely enough
- Render platform-native config when a platform requires a different file shape
- Make platform support and representability explicit instead of burying it in per-platform wiring

## Non-Goals

- This document does not require the full `HOOK.yaml` bundle format to land in the first implementation pass
- This document does not define bash parity; Go is the first implementation target
- This document does not force all platforms to support all hooks

## Canonical Storage

Target canonical layout:

```text
~/.agents/
  hooks/
    global/
      format-write/
        HOOK.yaml
        run.sh
      block-secrets/
        HOOK.yaml
        check.py
    my-project/
      post-edit-lint/
        HOOK.yaml
        run.sh
```

Each hook directory is one logical hook bundle.

- `HOOK.yaml` is the manifest
- sibling files are hook-local assets such as scripts, templates, or data

## Canonical Manifest

Proposed `HOOK.yaml` schema:

```yaml
name: format-write
description: Format edited files before they are written
when: pre_tool_use
match:
  tools: [write, edit, multi_edit]
  expression: Write|Edit|MultiEdit
run:
  command: ./run.sh
  timeout_ms: 15000
enabled_on: [claude, cursor, codex, copilot]
required_on: [claude]
platform_overrides:
  codex:
    event: pre_tool
```

### Field semantics

- `name`: stable logical hook identifier; defaults to the directory name if omitted
- `description`: human-readable explanation
- `when`: canonical lifecycle event
- `match`: canonical matcher description
  - `match.tools`: structured canonical matcher tokens when the hook can be expressed that way
  - `match.expression`: richer canonical matcher string when token lists are not sufficient or exact formatting matters
- `run`: canonical execution spec
- `enabled_on`: optional allowlist of platforms this hook should emit to
- `required_on`: optional strict subset of `enabled_on`; unsupported emission is an error for these platforms
- `platform_overrides`: platform-specific fields used only when native renderers need extra detail

### Platform support semantics

- If `enabled_on` is omitted, emit everywhere the hook can be represented
- If `enabled_on` is present, emit only for those platforms
- If a platform cannot represent a hook:
  - skip with a warning by default
  - fail when the platform is also listed in `required_on`

## Internal Contract

The Go code should use an explicit shared hook contract instead of platform-local hook logic.

Proposed core types:

- `HookSpec`: canonical hook source plus metadata
- `HookEmissionMode`: how the hook is emitted
- `HookTarget`: destination path plus scope

Recommended emission dimensions:

1. Shape
   - `direct`
   - `render_single`
   - `render_fanout`

2. Transport
   - `symlink`
   - `hardlink`
   - `write`

Examples:

- Cursor hooks: `direct + hardlink` for the current flat-file model, later `render_single + write` from `HOOK.yaml`
- Codex hooks: `direct + symlink` for the current flat-file model, later `render_single + write`
- Claude compatibility hooks: `direct + symlink` today, later `render_single + write` into `.claude/settings*.json`
- GitHub Copilot `.github/hooks/*.json`: `render_fanout + write`

Matcher precedence during rendering:

1. platform-specific `platform_overrides.<platform>.matcher`
2. canonical `match.expression`
3. canonical `match.tools`

## Staged Migration

### Stage A: Centralize current flat files behind `HookSpec`

Current supported sources stay valid:

- `~/.agents/hooks/<scope>/cursor.json`
- `~/.agents/hooks/<scope>/codex.json`
- `~/.agents/hooks/<scope>/codex-hooks.json`
- `~/.agents/hooks/<scope>/claude-code.json`
- `~/.agents/hooks/<scope>/*.json` for Copilot hook fanout
- `~/.agents/settings/<scope>/claude-code.json` for Claude-compatible fallback

This stage introduces a shared hook emitter and explicit emission modes without changing user-visible behavior.

### Stage B: Add canonical `HOOK.yaml` bundle loading

The loader reads `~/.agents/hooks/<scope>/<name>/HOOK.yaml` and materializes canonical `HookSpec` values.

- Flat platform files continue to work during migration
- `status` and `explain` can report which hook sources are canonical vs legacy

### Stage C: Native rendering from canonical hook bundles

Once `HOOK.yaml` loading is stable:

- Cursor and Codex can render aggregate native hook files
- Claude-compatible settings files can be rendered from hook bundles
- Copilot can render `.github/hooks/*.json` from canonical hook bundles

## Representability

Not every platform supports every hook capability. The renderer layer is responsible for deciding whether a `HookSpec` can be represented for a given platform.

Examples:

- A hook targeting `pre_tool_use` may map well to Claude and Codex
- A hook with a matcher requiring tool-name filtering may not map cleanly to every platform
- A fanout-oriented platform may need one output file per hook while another needs one aggregate config file

This is a renderer concern, not a storage concern.

## Initial Go Implementation

The first implementation pass in this repo should:

- add shared `HookSpec` and `HookEmissionMode` types
- centralize current hook lookup and emission helpers under `internal/platform/`
- migrate Cursor, Claude, Codex, and Copilot hook creation onto the shared helpers
- preserve current behavior and tests

The first pass does not need to:

- replace all current flat hook files with `HOOK.yaml`
- update bash emitters
- implement every future renderer shape
