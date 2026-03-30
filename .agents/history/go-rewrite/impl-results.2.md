# Implementation Results: Go vs Bash Output Audit

**Plan:** `.agents/history/go-rewrite/go_vs_bash_output_audit_f1fd6c32.plan.md`

## Summary

Audited all Go command outputs against their bash equivalents and brought the Go implementation to parity across 5 areas.

## Changes Made

### Phase 1: `add` command parity (`commands/add.go`)

- Added `scanExistingAIConfigs()` — walks the project tree for 30+ AI config file patterns (same set as bash), excluding `.git`, `node_modules`, `vendor`, etc.
- Added `checkExistingConfigFiles()` — identifies root-level managed files that will be replaced.
- Added **"Files to Replace"** section: lists each file with type (file/symlink) before confirmation.
- Added **"Other AI Configs Discovered"** section: shows up to 10 configs found elsewhere in the repo with a migration hint.
- Added **`ui.InfoBox("About Link Types")`** explaining hard links vs symlinks.
- Fixed confirm message to include file count: `"Proceed? (N file(s) will be backed up and replaced)"`.
- Fixed already-registered path to return a non-nil error (was silently returning nil).
- Added restore count reporting: `"Restored N item(s) from ~/.agents/resources/<project>/"`.
- Added `backupExistingConfigsList()` which works from the pre-computed file list; `backupExistingConfigs()` now delegates to it.
- Added `restoreFromResourcesCounted()` shared helper (used by both `add` and `refresh`).

### Phase 2: `sync` command parity (`commands/sync.go`)

- Added `sync commit` subcommand: `git add -A && git commit -m <message>`.
- Added `sync log` subcommand: `git log --oneline --decorate -n 10`.
- Improved `sync init`: when already initialized, shows remote URL if configured or a step-by-step setup guide if no remote.
- Improved `sync push`: shows pending commits before pushing; prompts for confirmation unless `--yes`/`--force`.
- Improved `sync status`: structured summary showing branch, remote URL, ahead/behind counts, staged/unstaged/untracked file counts instead of raw `git status` output.

### Phase 3: `refresh` commit resolution fix (`commands/refresh.go`, `.goreleaser.yaml`)

- Removed broken walk-up heuristic in `resolveRefreshCommit` that tried to find `.git` by walking up from the binary location (would never work in production installs at `/usr/local/bin/`).
- Added `Commit` and `Describe` build-time variables alongside the existing `Version`.
- Updated `.goreleaser.yaml` ldflags for both unix and windows builds to embed `{{.Commit}}` and `{{.Tag}}`.
- `resolveRefreshCommit()` now simply returns the embedded values (empty strings for dev builds).

### Phase 4: `status` audit completeness (`commands/status.go`)

- Added `printOpenCodeAudit()`: checks `opencode.json` symlink and `.opencode/agent/` directory symlinks.
- Included OpenCode in `printAudit()` dispatch (between codex and copilot).
- Extended quick health check to include the `.github/copilot-instructions.md` symlink.

### Phase 5: Minor gaps (`commands/remove.go`, `commands/init.go`)

- `remove`: per-platform removal bullets (`"<Platform> links removed"` or warn on error) instead of one combined `"Removed managed links"` bullet.
- `init`: added "Detected Platforms" section that prints each platform as `✓ <Name> (<version>)` or `○ <Name> (not detected)` after writing config.json.

## Build Verification

```
go build ./...  # exit 0 — no errors
go run ./cmd/dot-agents sync --help  # shows: init, commit, pull, push, status, log
```
