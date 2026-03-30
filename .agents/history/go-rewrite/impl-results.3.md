# Go Output Improvements — impl-results.3

**Plan:** `.agents/history/go-rewrite/go_output_improvements_e33a9f75.plan.md`

## Summary

All 9 items from the plan implemented. Build clean, smoke tests pass.

## Changes

### 1. `internal/ui/output.go` — StepN
- Added `StepN(n, total int, msg string)` that prints `[n/total] msg` with dim prefix

### 2. `cmd/dot-agents/main.go` — Styled error output
- Import `ui` package; route `Execute()` errors through `ui.Error()` for red ANSI styling

### 3. `commands/status.go` — Per-platform badge row + last-refreshed + audit per-file detail + git info
- Replaced single "N managed links" bullet with a `✓ Cursor  ✓ Claude  - Codex` badge row
- Added `readRefreshTimestamp()` to parse `.agents-refresh` and show `last refreshed: YYYY-MM-DD HH:MM UTC`
- Suppress project path display when it equals `~/<name>`
- `--audit` Claude section: now lists each symlink by filename+target (was just ok/broken counts)
- `--audit` OpenCode `.opencode/agent/` section: same per-entry filename+target detail
- `.dot-agents-backup` files excluded from Cursor audit and badge checks
- Inline git repo status under `~/.agents` path: shows `git: <branch> (<remote>)` dim; if no remote shows yellow `! no remote — run: dot-agents sync init`; if not a git repo shows yellow `! not a git repo — run: dot-agents sync init` (moved from doctor)

### 4. `commands/add.go` — Platform-aware preview
- Replaced static 16-item flat list with per-platform grouped preview
- Each platform shows `(hard links)` or `(symlinks)` note
- Not-installed platforms show `(not installed — skipped)` and are skipped

### 5. `commands/doctor.go` — Link health + auto-repair
- New "Link Health" section: lists each broken link individually with red `✗ path → target`
- Added `collectBrokenLinks()` returning per-link platform, relative path, and target for precise reporting
- Added `countProjectLinks()` helper reusing `collectBrokenLinks` for ok/broken counts
- Auto-repair: when not `--dry-run` and broken links exist, calls `CreateLinks` per affected platform; shows `repaired <Platform> links` per fix
- `--dry-run`: shows per-platform `DryRun` messages for what would be repaired, then prints "Run without --dry-run to apply repairs"
- Nothing broken → no repair output, no misleading messages — clean exit
- `.dot-agents-backup` files filtered from all Cursor link checks (badge row, audit, doctor) to prevent false-positive warnings
- Doctor defaults to fix mode (not dry-run); `status --audit` is the read-only diagnostic path

### 6. `commands/refresh.go` — Per-project progress + always-show-skipped
- Uses `ui.StepN(i+1, total, name)` when refreshing multiple projects
- Always prints `ui.Skip()` for uninstalled platforms (was verbose-only)
- Shows "Nothing to refresh." when count == 0

### 7. `commands/sync.go` — Sync status UX
- Suppresses Ahead/Behind row when no remote is configured
- Colors: staged count yellow if > 0, ahead count green if > 0, untracked count dim
- Added summary line: `No changes — working tree clean` or `N change(s) pending commit`

### 8. `commands/hooks.go` — Readable hook blocks
- Parses `hooks` map and prints each event as a `Section` with matcher + commands listed
- Falls back to raw JSON for unknown structures

### 9. `commands/skills.go` + `commands/agents.go` — Frontmatter + count
- Added `readFrontmatterDescription()` to parse YAML `description:` field from SKILL.md/AGENT.md
- Prints description dim next to skill/agent name
- Adds `N skill(s)/agent(s) in <scope> scope` summary line

### 10. `commands/explain.go` — ANSI formatting
- Replaced raw string constants with structured print functions
- Commands table uses cyan for names, dim for descriptions
- Structure diagram uses bold+cyan for node names with inline dim descriptions
