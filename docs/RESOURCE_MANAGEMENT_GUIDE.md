---
title: Managing Rules, MCP & Settings
description: A task-oriented guide to the canonical rules, MCP-server, and settings resource families — what each is, how to declare one, how it is emitted per platform, and the da list/show/remove surface.
sidebar:
  order: 9
---

# Managing rules, MCP servers & settings

`dot-agents` keeps three more managed resource families in one canonical place and wires
them into every platform that can represent them — exactly the model
[Hooks](./HOOKS.md) follows:

- **rules** — editor/agent rule files (`.mdc` / `.md` / `.txt`).
- **mcp** — MCP-server config files (`.json` / `.yaml` / `.yml` / `.toml`).
- **settings** — platform settings files (e.g. `cursor.json`, `claude-code.json`,
  `cursorignore`).

Each family is a sibling of `hooks` under the same per-resource-command model: a canonical
store under `~/.agents/`, a shared link/refresh executor, and a `list` / `show` / `remove`
command surface. The lifecycle contract that defines these families is
[Resource Command Contract](./RESOURCE_COMMAND_CONTRACT.md); this guide is the day-to-day,
task-oriented companion to it.

> **Scope.** This guide covers `da rules`, `da mcp`, and `da settings`. Hooks have their own
> [Hooks guide](./HOOKS.md); agents and skills have their own `agents` / `skills` families.

---

## The canonical model

Each family stores its canonical files under a single source of truth, organized by **scope**:

```text
~/.agents/rules/
  global/        # applied to every managed project
  <project>/     # applied only to that project
~/.agents/mcp/
  global/
  <project>/
~/.agents/settings/
  global/
  <project>/
```

Scope names match the project names shown by `da status`; `global` is the all-projects
scope. The canonical files here are the source of truth — `add`, `import`, `refresh`,
`install`, and `remove` wire them into the per-platform projections. Prefer editing the
canonical path and then re-projecting, rather than hand-editing a platform copy.

| Family | Canonical store | Recognized file types |
|--------|-----------------|------------------------|
| rules | `~/.agents/rules/<scope>/` | `.mdc`, `.md`, `.txt` |
| mcp | `~/.agents/mcp/<scope>/` | `.json`, `.yaml`, `.yml`, `.toml` |
| settings | `~/.agents/settings/<scope>/` | JSON / TOML / YAML configs, plus `cursorignore` |

---

## Declaring a resource

There is no "create" subcommand for these families: a resource is declared by placing a file
under its canonical scope directory, or by letting `da add` / `da import` canonicalize an
existing platform file into that store. The typical flows are:

1. **Author it canonically.** Create the file directly under the canonical scope path —
   e.g. `~/.agents/rules/global/style.mdc`, `~/.agents/mcp/global/mcp.json`, or
   `~/.agents/settings/global/cursor.json` — then run `da refresh` (or `da install`) for the
   project so the platform links are emitted.
2. **Import an existing platform file.** Run `da import` to canonicalize content already in a
   repo (e.g. a `.cursor/rules` file or a `.mcp.json`) into the matching `~/.agents/<family>/`
   store; subsequent `refresh`/`install` keep the link consistent.

After either flow, the canonical file is what every projection is rendered from. To change a
resource, edit the canonical file and re-run `refresh`/`install` — do not edit the emitted
platform copy unless you know it is unmanaged.

---

## Per-platform emit

These files are projected only into platforms that can represent them. `add`, `import`,
`refresh`, `install`, and `remove` are the flows that wire each canonical file into its
platform location. For the authoritative per-platform paths and link strategy (hard link vs
symlink vs rendered copy) see [PLATFORM_DIRS_DOCS.md](./PLATFORM_DIRS_DOCS.md); the families
fan out to (consistent with each resource's `--help`):

- **rules** → Cursor, Claude Code, Codex, and GitHub Copilot rule locations.
- **mcp** → Cursor, Claude Code, Copilot, and related MCP-config locations.
- **settings** → the per-platform settings files (e.g. Cursor / Claude Code settings,
  `cursorignore`).

Because the projection is link-based, editing the canonical file and re-running `refresh`
keeps every platform copy in sync from one edit.

---

## The list / show / remove surface

All three families share the same three subcommands, with the same argument shape:

```
da <family> list   [scope]          List canonical files for a scope (default: all scopes)
da <family> show   <scope> <name>   Show metadata for one canonical file
da <family> remove <scope> <name>   Remove a canonical file (canonical storage only)
```

`<family>` is `rules`, `mcp`, or `settings`. `<scope>` is `global` or a managed project name.
`<name>` is the file name (e.g. `mcp.json`) or its stem (e.g. `mcp`), matching what `list`
prints.

### `list`

Lists the canonical files for a scope. With no argument it inspects every scope; pass a
project scope (or `global`) to narrow it. When a scope has no files (or no directory yet) the
command prints an informational hint rather than erroring.

```console
$ da rules list
$ da rules list billing-api
$ da mcp list
$ da settings list my-app
```

### `show`

Renders metadata for a single canonical file. For **rules**, `show` additionally surfaces the
file's frontmatter `description:` when present.

```console
$ da rules show global rules.mdc      # or: da rules show global rules
$ da mcp show global mcp.json
$ da settings show global cursor.json
```

### `remove`

Deletes the file from **managed canonical storage only** — it does not unlink platform
copies. After a removal, run `da refresh` (or `da install`) for the relevant project so the
platform links stay consistent.

```console
$ da rules remove global old-rule.mdc
$ da mcp remove global stale.json
$ da settings remove my-app cursorignore
```

`remove` honors the global `--dry-run`, `--yes`, and `--force` flags (see
[Global Flag Contract](./GLOBAL_FLAG_CONTRACT.md)): `--dry-run` previews the deletion,
`--yes` skips the confirmation prompt, and `--force` proceeds past soft guards.

---

## Worked example

Add a global MCP config, verify it lists and shows, project it, then remove it.

### 1. Author the canonical file

```console
$ cat ~/.agents/mcp/global/mcp.json
{
  "mcpServers": {
    "code-review-graph": { "command": "da", "args": ["kg", "serve"] }
  }
}
```

### 2. List and show it

```console
$ da mcp list global
$ da mcp show global mcp.json      # or: da mcp show global mcp
```

`show` prints the resolved scope, file name, and source path under `~/.agents/mcp/global/`.

### 3. Project it into a repo

From inside a managed project, re-project so the platform MCP links pick up the new file:

```console
$ da refresh
```

### 4. Remove it when it is stale

```console
$ da mcp remove global mcp.json --dry-run   # preview
$ da mcp remove global mcp.json             # delete from canonical storage
$ da refresh                                # re-project so platform links drop it
```

The same three-step authoring/projection/removal flow applies to `da rules` and
`da settings` — only the file types and per-platform destinations differ.

---

## Reference

### Command quick reference

| Command | Default scope | Args | Notes |
|---------|---------------|------|-------|
| `rules list [scope]` | all scopes | optional scope | Lists `.mdc`/`.md`/`.txt` under `~/.agents/rules/` |
| `rules show <scope> <name>` | — | scope + name/stem | Also prints frontmatter `description:` when present |
| `rules remove <scope> <name>` | — | scope + name | Canonical storage only; honors `--dry-run`/`--yes`/`--force` |
| `mcp list [scope]` | all scopes | optional scope | Lists `.json`/`.yaml`/`.yml`/`.toml` under `~/.agents/mcp/` |
| `mcp show <scope> <name>` | — | scope + name/stem | |
| `mcp remove <scope> <name>` | — | scope + name | Canonical storage only; honors `--dry-run`/`--yes`/`--force` |
| `settings list [scope]` | all scopes | optional scope | Lists settings configs + `cursorignore` under `~/.agents/settings/` |
| `settings show <scope> <name>` | — | scope + name/stem | |
| `settings remove <scope> <name>` | — | scope + name | Canonical storage only; honors `--dry-run`/`--yes`/`--force` |

### See also

- [Resource Command Contract](./RESOURCE_COMMAND_CONTRACT.md) — the lifecycle contract that
  defines the hooks/rules/MCP/settings families and what stays implicit through shared flows.
- [Hooks](./HOOKS.md) — the sibling family with the same canonical model.
- [PLATFORM_DIRS_DOCS.md](./PLATFORM_DIRS_DOCS.md) — authoritative per-platform locations and
  link strategy.
- [Global Flag Contract](./GLOBAL_FLAG_CONTRACT.md) — `--dry-run` / `--yes` / `--force` / `--json` semantics.
