---
title: Getting Started
description: Set up da (dot-agents) on a new machine — adopt a shared home config, install an existing repo manifest, or scaffold fresh — then link your editor and verify.
sidebar:
  order: 0
---

# Getting Started

This guide takes you from "just installed `da`" to "editor linked, project bound,
health check green." There are exactly **three setup paths** — pick the one that
matches your situation, then run the shared **Verify** step at the end.

> Looking for the high-level pitch and architecture? See the
> [project overview / README](/). This page is the hands-on quickstart.

## 0. Install the CLI

```bash
brew tap AGOrcha/tap && brew install da
da --version
```

If `da --version` works, continue. (Use your platform's package install if you're
not on Homebrew.)

## Which path am I on?

| Your situation | Path |
|---|---|
| A teammate / another machine published a `da` home config as a **git URL** | [A. Adopt a shared config](#a-adopt-a-shared-home-config) |
| The repo you cloned already has a committed **`.agentsrc.json`** with config sources | [B. Install a repo manifest](#b-install-an-existing-repo-manifest) |
| Neither — empty `~/.agents`, no manifest | [C. Start fresh](#c-start-fresh) |

Not sure? Quick probes:

```bash
ls ~/.agents/config.json        # present → home already initialized here
ls .agentsrc.json && cat .agentsrc.json   # check its "sources" / "extends"
```

A manifest whose `sources` is only `{ "type": "local" }` is self-contained; an
entry with `type: git | http | oci` (or an `extends` array) pulls **real upstream
config** → that's path B.

---

## A. Adopt a shared home config

You have a shared/team `da` config published as a git URL (a teammate's or another
machine's `~/.agents`). This bootstraps `~/.agents` here, then re-links and
rebinds your projects.

**Preconditions:** `~/.agents` must be empty (adoption refuses to clobber a
populated home). Git auth is **ambient only** — ssh-agent or a credential helper;
there is no login command and a URL with an embedded token is refused.

```bash
da init --from <git-url>     # clone + adopt ~/.agents (zero project bindings)
da refresh                   # re-detect platforms + re-link
da add <project-path>        # rebind each project you work in (repeat per repo)
```

Projects arrive **known but unbound** — `da add` rebinds each one to this machine.
Then run [Verify](#verify-every-path).

---

## B. Install an existing repo manifest

The repo already commits a `.agentsrc.json` that declares config **sources** /
**extends** layers. Materialize it on this machine.

**Precondition:** the home must be initialized (`~/.agents/config.json` exists). If
it isn't, run `da init` once first — it scaffolds the home without touching the
repo's manifest.

```bash
ls ~/.agents/config.json || da init   # ensure the home exists
da install                            # resolve sources, materialize, link platforms
da config sync                        # only if the manifest has extends / git layers
```

`da install` reads `.agentsrc.json` in the current directory, so run it from the
repo root. `da config sync` re-fetches the declared layers and rewrites
`.agentsrc.lock` so this machine resolves the exact same effective config as
everyone else — skip it for a local-only manifest with no `extends`. See
[Layered Configuration](./LAYERED_CONFIG_GUIDE.md) for the full layering model.
Then run [Verify](#verify-every-path).

---

## C. Start fresh

No shared config to adopt and no repo manifest to install — scaffold a new home
and bind your project.

```bash
da init                  # scaffold ~/.agents + link the active harness globally
da add <project-path>    # bind a project: generate .agentsrc.json + link platforms
da refresh               # re-detect editors + re-link
da status --audit        # file-level link detail per project
da doctor                # read-only health check
```

Useful `da init` flags: `-y`/`--yes` (non-interactive), `-n`/`--dry-run` (preview
only), `-f`/`--force` (clobber an existing home, backing up the old one first).
If `~/.agents/config.json` already exists, skip `da init` and start at `da add`.
Then run [Verify](#verify-every-path).

---

## Verify (every path)

`da` links config into whichever editors it detects, so install your editor
**before** the final link pass. Supported platforms: **Claude Code**, **Cursor**
(hard links), **Codex**, **GitHub Copilot**, **OpenCode**.

```bash
# 1. Install your editor / harness (if not already installed)
da refresh          # re-detect platforms; auto-enables a newly-installed harness
da status --audit   # confirm each project is linked for the editors you use
da doctor           # read-only health check — fix anything it flags, then re-run
```

You're set up when `da doctor` reports no errors and opening the project in your
editor surfaces the expected rules/instructions.

## Where next

- [Layered Configuration](./LAYERED_CONFIG_GUIDE.md) — the `.agentsrc.json`
  manifest, `extends` layers, the `.agentsrc.lock` lockfile, and the `da config`
  family.
- [Hooks](./HOOKS.md) — wiring agent automation.
- Run `da <command> --help` for the full flag set on any command above.
