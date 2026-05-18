# Hooks

Hooks let your AI agents run a command automatically at a point in their
lifecycle — for example, formatting a file after it is edited, blocking a
write that would commit a secret, or loading session context when a session
starts. `da` keeps your hooks in one place and wires them into each platform
that supports them.

> This is the user-facing guide. For a quick command summary see the
> **Hooks** entry in the [README](../README.md#hooks); this document
> covers the storage model and per-platform behavior in more detail.

## The canonical model

Hooks live under a single source of truth at `~/.agents/hooks/`, organized by
scope:

```text
~/.agents/hooks/
  global/        # applied to every managed project
  <project>/     # applied only to that project
```

Scope names match the project names shown by `da status` (`global` is the
all-projects scope).

A hook is a **bundle directory** containing a `HOOK.yaml` manifest plus any
sidecar assets it needs (a script, a template, data):

```text
~/.agents/hooks/global/
  format-write/
    HOOK.yaml      # the hook manifest
    run.sh         # a sidecar script the hook invokes
```

Older single-file JSON hooks (`hooks/<scope>/<name>.json`) are still
recognized and listed for visibility, but bundle directories are the
preferred form for new hooks. `da import` and `da refresh` canonicalize
hook content into this same layout.

## Per-platform behavior

Hooks are distributed only to platforms that can represent them. Coverage
today (consistent with the Hook Wiring Audit in
[PLATFORM_DIRS_DOCS.md](PLATFORM_DIRS_DOCS.md)):

| Platform | Where hooks land | Status |
|----------|------------------|--------|
| Claude Code | `.claude/settings.json` / `.claude/settings.local.json` | Wired |
| Cursor | `.cursor/hooks.json` (project) and `~/.cursor/hooks.json` (user) | Wired |
| Codex | `.codex/hooks.json` | Wired (project `.codex/hooks.json` is rendered from the canonical hook spec and removed again on project teardown) |
| GitHub Copilot | `.github/hooks/*.json` plus Claude-compatible settings | Wired (canonical hooks fan out to `.github/hooks/*.json`; legacy single-file hooks still emit) |
| OpenCode | — | No dedicated hook file is documented upstream, so none is created |

If a platform cannot represent a particular hook, it is skipped for that
platform rather than emitted incorrectly.

## Using hooks

`da hooks` inspects and manages the canonical hook resources under
`~/.agents/hooks/`:

```bash
# List hooks for the global scope (or a project scope)
da hooks list
da hooks list my-app

# Show one hook bundle (or a legacy single-file hook)
da hooks show global session-orient

# Remove a hook bundle directory or a legacy hooks/*.json file
da hooks remove global old-hook-bundle
```

All three subcommands accept the global flags (`--json`, `--dry-run`,
`--verbose`, `--yes`).

### Removing a hook vs. removing a project

These are different operations with different blast radius:

- `da hooks remove <scope> <name>` — **granular**: deletes a single
  hook bundle directory (or legacy `hooks/*.json` file) from
  `~/.agents/hooks/<scope>/`. Nothing else in the scope is touched.
- `da remove <project>` — **project teardown**: unlinks the project
  from every platform and clears the *contents* of its canonical
  directories (including `~/.agents/hooks/<project>/`), but keeps the
  now-empty directories in place.
- `da remove <project> --clean` — teardown that also removes the
  canonical directories themselves (including
  `~/.agents/hooks/<project>/`), leaving no skeleton behind.

The `global` hook scope is shared by every project and is never removed
by a project teardown; prune global hooks explicitly with
`da hooks remove global <name>`.

### Adding a hook

A hook is added by creating its bundle directory under the appropriate
scope — for example `~/.agents/hooks/global/<name>/HOOK.yaml` with any
sidecar scripts beside it. Hooks already present in a project can be pulled
into the canonical store with:

```bash
da import <project>
```

which detects existing hooks (along with rules, skills, and agents) and
copies them into `~/.agents/`. After changing hooks under `~/.agents/`,
re-apply them to your projects with:

```bash
da refresh
```

`da refresh` re-distributes the canonical hooks to each managed project for
every platform that supports them.

## See also

- [README — Hooks](../README.md#hooks) — quick command summary
- [PLATFORM_DIRS_DOCS.md](PLATFORM_DIRS_DOCS.md) — full per-platform resource
  locations and the Hook Wiring Audit
