# 1. Managed Resource Cleanup And Refresh Import Regression

**Branch:** `feature/workflow-auto-operator`
**Base Commit:** `fe81d54`
**Status:** Completed

---

## Objective

Clean up stale managed artifacts under `~/.agents/resources/` and managed project outputs, then fix the Go config-management path so:

- managed files are not re-imported as user-authored content
- stale generated rule files are pruned
- `refresh` imports unmanaged project files before replacing them with managed outputs
- already-managed files are updated from canonical `dot-agents` state

---

## What Changed

### Cleanup and drift fixes

- Normalized stored project paths so canonical backup paths do not flatten unexpectedly.
- Stopped `add`/`import` from treating managed hardlinks and reserved managed Cursor rule names as unmanaged files.
- Pruned stale managed Cursor and Claude rule outputs when canonical source files disappeared.
- Removed stale generated artifacts from `~/.agents/resources/` and broken managed repo outputs created by earlier refresh behavior.

### Refresh behavior fix

`refresh` now imports project-scope unmanaged files before relinking, even without `--import`.

New behavior:

- plain `refresh` imports project-scope unmanaged files, then relinks
- `refresh --import` imports both project and global scope, then relinks
- refresh-internal import is auto-confirmed so it cannot prompt and then overwrite the same file anyway
- `restoreFromResources()` skips canonical backup snapshots under `resources/<project>/rules|settings|mcp|skills|agents|hooks` so stale canonical backups are not replayed over fresh imports

This supersedes the earlier design note in `.agents/history/import-command/impl-results.1.md` that described import-on-refresh as fully opt-in.

---

## Root Causes

### 1. Refresh relinked before importing project files

The original Go `refresh` path only imported when `--import` was passed. That meant an unmanaged repo `AGENTS.md` could be replaced by the managed global fallback before its content was captured into `~/.agents/rules/<project>/agents.md`.

### 2. Refresh import still behaved like an interactive import

Even when import ran first, it used the normal confirm flow. A user could decline or miss the import prompt, and refresh would then continue and overwrite the repo file with the managed output anyway.

### 3. Canonical backups in `resources/` were replayed as live state

When import replaced an existing canonical destination, the old canonical file was backed up under `resources/<project>/rules/...`. `restoreFromResources()` later treated that backup as something to restore, which could overwrite the newly imported canonical content with stale data.

### 4. Managed outputs were fed back into import/backup

Managed hardlinks, reserved Cursor rule names, and stale generated outputs were being scanned as if they were user-authored content. That caused duplicate backups, broken-resource noise, and inconsistent project output.

### 5. Path normalization bug amplified resource drift

One project path was stored as `/Users/nikashp/Documents/dot-agents/.`, which caused backup-path derivation to behave inconsistently and contributed to malformed `resources/` copies.

---

## Files Changed

| File | Why |
|---|---|
| `internal/config/paths.go` | Normalize expanded paths |
| `internal/config/config.go` | Normalize stored and returned project paths |
| `commands/add.go` | Skip managed outputs during add scans; skip canonical backup snapshots during restore |
| `commands/import.go` | Skip managed import sources; auto-confirm refresh-internal import |
| `commands/refresh.go` | Always import project scope before relinking; `--import` now widens scope to include global |
| `internal/platform/cursor.go` | Prune stale managed Cursor rule files |
| `internal/platform/claude.go` | Prune stale managed Claude rule symlinks |
| `commands/add_test.go` | Regression coverage for managed hardlink skip behavior |
| `commands/import_test.go` | Regression coverage for managed import-source skip behavior |
| `commands/refresh_test.go` | Regression coverage for import-before-relink and canonical replacement |
| `internal/platform/stage1_integration_test.go` | Regression coverage for stale managed output pruning |

---

## Verification

Executed successfully:

```bash
go test ./commands ./internal/config ./internal/platform
go test ./...
```

Targeted regressions added and passing:

- unmanaged repo `AGENTS.md` is imported into `~/.agents/rules/<project>/agents.md` before relink
- existing canonical `rules/<project>/agents.md` is replaced from unmanaged repo `AGENTS.md` during refresh
- stale managed Cursor and Claude outputs are pruned instead of accumulating

---

## Important Outcome

The code path is fixed, but content already overwritten before this change is not automatically recoverable unless a backup still exists.

Observed during live validation for `dot-agents`:

- `AGENTS.md` in the repo was already linked to `~/.agents/rules/global/rules.mdc`
- there was no surviving `~/.agents/rules/dot-agents/agents.md`
- there was no surviving `~/.agents/resources/dot-agents/AGENTS.md`

So the bug is fixed for future refresh runs, but previously lost project-specific `AGENTS.md` content must be reintroduced manually once if it is still desired.
