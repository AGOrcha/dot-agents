---
name: Go vs Bash Output Audit
overview: Audit gaps between Go command outputs and their bash equivalents, then bring the Go implementation to parity.
todos:
  - id: add-parity
    content: "add: show Files to Replace, AI Config scan, About Link Types box, resource restore count, fix confirm message, fix already-registered exit"
    status: completed
  - id: sync-parity
    content: "sync: add commit + log subcommands, improve init/push/status output to match bash"
    status: completed
  - id: refresh-commit
    content: "refresh: fix resolveRefreshCommit — embed Commit/Describe via ldflags in goreleaser instead of walking from binary"
    status: completed
  - id: status-opencode
    content: "status: add OpenCode audit, extend quick health check to copilot"
    status: completed
  - id: minor-gaps
    content: "remove: per-platform removal bullets; init: print detected platforms"
    status: completed
isProject: false
---

# Go vs Bash Command Output Audit

## Summary of Gaps

### `add` command

- **Missing: "Files to Replace" section** — bash shows a labeled block listing root-level files that will be backed up/replaced, with type (file vs symlink). Go silently backs them up with only a count bullet.
- **Missing: "Other AI Configs Discovered" section** — bash runs `scan_existing_ai_configs` (30+ file patterns across the whole repo) and shows informational discovery of AI configs found elsewhere. Go has no equivalent.
- **Missing: "About Link Types" info box** — bash shows `info_box "About Link Types"` explaining hard links vs symlinks. Go omits this.
- **Missing: settings template copy** — bash copies `claude-code.json` from `share/templates/standard/` into `~/.agents/settings/<project>/`. Go skips this.
- **Missing: "Restored N item(s) from resources" bullet** — bash calls `restore_project_from_active_resources` and reports the count. Go calls `restoreFromResources` but doesn't count or report it during `add`.
- **Missing: per-platform link output detail** — bash shows per-file link creation within each platform block. Go only shows one bullet per platform.
- **Confirm message difference** — bash shows `"Proceed? (N file(s) will be backed up and replaced)"` when files exist. Go always shows `"Proceed?"`.
- **Already-registered error** — bash returns exit code 1 on already-registered (without `--force`). Go returns nil (soft exit). Should be an error.

### `remove` command

- **Missing: link count in scan step** — bash counts actual managed links per platform and shows a summary. Go just checks if the directory exists.
- **Missing: per-platform removal bullets** — bash reports which platform links were removed. Go emits one combined bullet `"Removed managed links"`.

### `refresh` command

- `**resolveRefreshCommit` is broken** — walks up from the binary location to find `.git`. In production the binary lives in `/usr/local/bin/` or similar — this will never find the dot-agents repo. Bash uses `git -C "$DOT_AGENTS_REPO_DIR"` where the repo dir is known at install time. Fix: embed commit/version at build time via ldflags (already have `Version` from ldflags), and embed `Commit`/`Describe` similarly via `goreleaser`. Fall back to `""` if not set.
- **Missing: "Refreshing enabled platforms and versions" detail** — bash's `refresh_enabled_platforms_and_versions` logs which platforms are enabled vs not and their versions. Go's version is roughly equivalent but the disabled platforms are not shown at all.

### `sync` command

- **Missing: `sync commit` subcommand** — bash has `sync commit [message]` that does `git add . && git commit`. Go's `sync push` does `add + commit + push` but there is no standalone `commit` subcommand. Users expecting bash parity can't commit without pushing.
- **Missing: `sync log` subcommand** — bash has `sync log` showing `git log --oneline --decorate -n 10`. Go has no equivalent.
- `**sync init` missing: remote guidance** — bash detects if a remote is already configured and shows the "next steps" setup guide when already initialized. Go just prints "already a git repository".
- `**sync push` missing: confirmation and pre-push summary** — bash shows commits to be pushed and asks for confirmation before pushing. Go pushes silently.
- `**sync status` is raw** — bash's `sync_status` shows a structured summary (branch, remote, ahead/behind, staged/unstaged/untracked counts). Go just passes raw `git status` output through.

### `init` command

- **Missing: platform detection preview** — bash shows which platforms were detected during init. Go detects and saves to config but doesn't print what was found.
- `**--force` path has no backup** — bash backs up existing `~/.agents/` before reinitializing. Go doesn't.

### `status` command

- `**--audit` missing: OpenCode** — Go's `printAudit` covers cursor/claude/codex/copilot but not opencode. Bash has per-platform audit for all 5.
- **Health check only checks cursor+claude+AGENTS.md** — Copilot, OpenCode, Codex (beyond AGENTS.md) not checked in the quick health summary.

---

## Implementation Plan

### Phase 1: `add` output parity

- Add `scan_existing_ai_configs` equivalent in Go (or reuse a simpler version covering the top patterns)
- Add "Files to Replace" preview section with per-file type display
- Add "Other AI Configs Discovered" informational section
- Add "About Link Types" info box
- Show resource restore count during add
- Fix confirm message to include file count when files will be backed up
- Fix already-registered to return an error (non-nil)

### Phase 2: `sync` subcommand parity

- Add `sync commit` subcommand
- Add `sync log` subcommand
- Improve `sync init` to show remote setup guidance when already initialized
- Improve `sync push` to preview pending commits and confirm before pushing
- Improve `sync status` to show structured summary (branch, remote, ahead/behind, counts)

### Phase 3: `refresh` commit resolution fix

- Remove the walk-up heuristic in `resolveRefreshCommit`
- Add `Commit` and `Describe` build-time variables to `[.goreleaser.yaml](.goreleaser.yaml)` ldflags (alongside `Version`)
- Update `refreshMarkerContent` to use embedded values; fall back to `"dev"` gracefully

### Phase 4: `status` audit completeness

- Add `printOpenCodeAudit` function
- Include OpenCode in `printAudit` dispatch
- Extend quick health check to include copilot symlink

### Phase 5: `remove` + `init` minor gaps

- Add per-platform removal reporting in `remove`
- Add platform detection printout in `init`

---

## Key Files

- `[commands/add.go](commands/add.go)` — phases 1
- `[commands/sync.go](commands/sync.go)` — phase 2
- `[commands/refresh.go](commands/refresh.go)` — phase 3
- `[commands/status.go](commands/status.go)` — phase 4
- `[commands/remove.go](commands/remove.go)` — phase 5
- `[commands/init.go](commands/init.go)` — phase 5
- `[.goreleaser.yaml](.goreleaser.yaml)` — phase 3 (embed Commit/Describe)

