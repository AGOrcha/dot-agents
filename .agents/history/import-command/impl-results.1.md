# 1. Import Platform Configs

**Plan:** `import_platform_configs_a05cba84`
**Branch:** `feature/go-rewrite`
**Status:** Completed + bugs fixed

---

## Objective

Add a `dot-agents import` command that scans platform config files at both project scope (inside managed repos) and global scope (user home), compares them against `~/.agents/`, and imports any new or differing files through the existing backup/restore pipeline. Hook the same logic into `dot-agents refresh` via an `--import` flag.

---

## Files Created / Modified

| File | Change |
|---|---|
| `commands/import.go` | New — import command, all scanning/mapping/conflict logic |
| `commands/import_test.go` | New — tests for `mapGlobalRelToDest` and `filesDifferent` |
| `commands/refresh.go` | Modified — `--import` flag, calls `runImportFromRefresh` before linking |
| `cmd/dot-agents/main.go` | Modified — registered `NewImportCmd()` |
| `src/lib/commands/import.sh` | New — shell parity for `cmd_import` |
| `src/lib/commands/refresh.sh` | Modified — sources `import.sh`, `--import` flag, calls `cmd_import` |
| `src/lib/utils/resource-restore-map.sh` | Modified — added `map_global_rel_to_agents_dest` |

---

## Implementation Summary

### Go (`commands/import.go`)

**Core types and entry point:**

```go
type importCandidate struct {
    project    string
    sourceRoot string
    sourcePath string
    destRel    string
}

func NewImportCmd() *cobra.Command  // --scope project|global|all, --yes, --dry-run
func runImport(projectFilter, scope string) error
func runImportFromRefresh(projectFilter, scope string) error  // skips relink (refresh does it)
func runImportInternal(projectFilter, scope string, skipRelink bool) error
```

**Scanning:**
- `scanProjectImportCandidates` iterates all managed projects (or one if filtered), calls `gatherProjectCandidates` per project
- `gatherProjectCandidates` checks 12 hardcoded single-file paths + walks `.cursor/rules`, `.agents/skills`, `.claude/skills`, `.github/agents`, `.codex/agents`
- `scanGlobalImportCandidates` checks 5 user-home paths via new `mapGlobalRelToDest`

**Global path mappings (new):**

| Source (`~/`) | Destination (`~/.agents/`) |
|---|---|
| `.claude/settings.json` | `settings/global/claude-code.json` |
| `.cursor/settings.json` | `settings/global/cursor.json` |
| `.cursor/mcp.json` | `mcp/global/mcp.json` |
| `.claude/CLAUDE.md` | `rules/global/agents.md` |
| `.codex/config.toml` | `settings/global/codex.toml` |

**Per-candidate logic:**
1. Skip if source is a managed symlink pointing into `~/.agents/` (`isManagedSymlink`)
2. If dest missing → auto-import via `mirrorBackup` + `copyFile`
3. If dest exists and content identical → skip silently
4. If dest exists and content differs → compare mtimes, show newer side, prompt per file; `--yes` auto-accepts
5. On overwrite → backup old `~/.agents` dest first, then backup src, then copy
6. After all candidates processed → `relinkImportedProjects` (unless called from refresh)

### Shell (`src/lib/commands/import.sh`)

Mirrors the Go logic using existing helpers:
- `_import_project_candidates` — same 12-path single list + walk of same dirs using `dot_agents_map_resource_rel_to_agents_dest`
- `scan_global_import_candidates` — 5 home paths via `map_global_rel_to_agents_dest`
- `cmd_import` — same compare/prompt/backup/copy loop using `mirror_project_backup_to_resources` and `cmp -s` for content diff, `stat -f "%m"` for mtime on macOS

### Refresh integration

Both Go and shell: flag-gated with `--import`. When set, import runs before the link phase. Uses `runImportFromRefresh`/`cmd_import ... --scope all` variants to skip the relink that refresh already does.

---

## Bugs Found and Fixed (post-implementation)

### Bug 1 — Backup artifacts imported as real files

**Observed:** `global--rules.mdc.dot-agents-backup` was imported as `rules/global/rules.mdc.dot-agents-backup`

**Root cause:** `gatherProjectCandidates`'s `WalkDir` loop had no `isBackupArtifact` filter. `mapResourceRelToDest` pass-through for `.cursor/rules/*.md` matches the `.dot-agents-backup` suffix.

**Fix:** Added `isBackupArtifact(d.Name())` early-return in the `WalkDir` callback, and the same guard in `addIfMapped` for defense-in-depth.

```go
// WalkDir callback
if isBackupArtifact(d.Name()) {
    return nil
}

// addIfMapped single-file path
if isBackupArtifact(filepath.Base(rel)) {
    return
}
```

### Bug 2 — Double relink when called from `refresh --import`

**Observed:** `p.CreateLinks` ran twice per project — once at end of `runImport`, once in refresh's own project loop.

**Fix:** Introduced `runImportInternal(projectFilter, scope string, skipRelink bool)`. Public `runImport` passes `false`; `runImportFromRefresh` passes `true`. Refresh uses `runImportFromRefresh`.

```go
if !skipRelink && scope != "global" {
    relinkImportedProjects(cfg, projectSet)
}
```

---

## Verification

```
go build ./...          ✓
go test ./commands      ✓  (commands package, 0.23s)
```

Live run (`dot-agents import --scope all`) correctly:
- Imported new `.cursor/mcp.json` files from managed projects
- Prompted for `settings/global/claude-code.json` conflict (src newer) — accepted
- Prompted for `mcp/global/mcp.json` conflict (src newer) — accepted
- Imported `~/.codex/config.toml` (no dest, auto-imported)

Live run (`dot-agents refresh --import`) correctly:
- Ran import phase first
- Then ran normal refresh / link phase for all 3 projects
- No double-link (after bug fix)

---

## Key Design Decisions

- **`--import` is opt-in on refresh** — keeps refresh non-interactive by default; adding `--import` is explicit intent to pull from platform configs
- **Backup before every overwrite** — `mirrorBackup` runs on the existing `~/.agents` destination before replacing it, preserving history in `resources/<project>/backups/<timestamp>/`
- **Global scope uses `"global"` as project key** — consistent with existing `rules/global/`, `settings/global/` directory layout
- **`isManagedSymlink` guard** — prevents importing files that are already symlinks back into `~/.agents/`, avoiding circular backup chains
- **Content comparison before mtime** — `filesDifferent` (byte compare) runs first; mtime is only surfaced if content actually differs, avoiding false prompts for identical files with different timestamps
