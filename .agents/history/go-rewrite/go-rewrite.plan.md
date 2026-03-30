---
name: ""
overview: ""
todos: []
isProject: false
---

# Plan: Rewrite dot-agents in Go

## Context

dot-agents is currently ~9,500 lines of bash spread across 27 source files. Bash has real
limitations for this codebase: fragile error handling (`set -e` edge cases), no type safety,
difficult to test, slow on cold starts, and hard to package. Users must have a compatible bash
version and the whole directory tree of scripts must be present.

**Goal**: Rewrite in Go — produces a single static binary, easy to install via Homebrew or
`go install`, easy to test with proper unit tests, and more maintainable long-term. The
`~/.agents/` directory structure and all config file formats stay identical (backward compatible).

---

## Language: Go

**Why Go over alternatives:**

- Compiles to a single self-contained binary — no runtime, no PATH gymnastics, no directory tree
- `brew install` or `go install github.com/.../dot-agents@latest` just works
- `os.Symlink`, `os.Link`, `os.Readlink`, `filepath.Walk` — perfect stdlib for this tool
- `encoding/json` — eliminates the bash/jq dependency
- Easy cross-compilation for macOS (arm64/amd64) and Linux
- Cobra is the standard CLI framework, mirrors current command structure cleanly
- Fast enough to write quickly; easier to contribute to than Rust

---

## Repository Structure

Keep the existing shell scripts in `src/` until the Go port is complete (as a reference and
fallback). Add Go code in the root:

```
dot-agents/
├── cmd/
│   └── dot-agents/
│       └── main.go               # Entry point, Cobra root command
├── internal/
│   ├── config/
│   │   ├── config.go             # Load/save ~/.agents/config.json
│   │   └── paths.go              # AGENTS_HOME resolution, path helpers
│   ├── platform/
│   │   ├── platform.go           # Platform interface
│   │   ├── cursor.go             # Cursor: hard links
│   │   ├── claude.go             # Claude Code: symlinks + user rules
│   │   ├── codex.go              # Codex: AGENTS.md symlink
│   │   ├── opencode.go           # OpenCode: symlinks
│   │   └── copilot.go            # GitHub Copilot: symlinks
│   ├── links/
│   │   └── links.go              # Symlink/hardlink helpers (safe create, inode check)
│   └── ui/
│       ├── output.go             # Colored output, bullets, boxes, steps
│       └── confirm.go            # Interactive confirmation prompts
├── commands/
│   ├── add.go
│   ├── remove.go
│   ├── init.go
│   ├── status.go
│   ├── refresh.go
│   ├── doctor.go
│   ├── skills.go
│   ├── agents.go
│   ├── hooks.go
│   ├── sync.go
│   └── explain.go
├── src/                          # Existing bash scripts (kept for reference until complete)
├── go.mod
├── go.sum
├── scripts/
│   └── install.sh                # Updated: downloads pre-built binary from GitHub releases
├── VERSION
└── README.md
```

---

## Key Design Decisions

### 1. Platform Interface

Mirrors the bash platform registry cleanly:

```go
// internal/platform/platform.go
type Platform interface {
    ID() string
    DisplayName() string
    IsInstalled() bool
    CreateLinks(project, repoPath string) error
    HasDeprecatedFormat(repoPath string) bool
    DeprecatedDetails(repoPath string) string
}
```

All 5 platforms implement this interface. Registry is a simple `[]Platform` slice.

### 2. Config (JSON)

```go
// internal/config/config.go
type Config struct {
    Projects map[string]string `json:"projects"` // name → path
    Version  string            `json:"version"`
}
```

`Load()` / `Save()` using `encoding/json`. Eliminates the jq dependency entirely.

### 3. Links Package

```go
func Symlink(target, link string) error          // ln -sf equivalent
func Hardlink(src, dst string) error             // ln equivalent
func AreHardlinked(a, b string) (bool, error)    // inode comparison
func FindRuleFile(basePath string, exts []string) string  // extension fallback
```

### 4. UI

Use simple ANSI escape codes (no heavy TUI library). Matches existing output exactly:

- `ui.Step(n, msg)`, `ui.Bullet(style, msg)`, `ui.PreviewSection(title, items...)`
- `ui.SuccessBox(msg, nextSteps...)`, `ui.WarnBox(title, lines...)`

### 5. Distribution

- **Homebrew**: Formula in `dot-agents/homebrew-tap` (or update existing)
- **go install**: `go install github.com/dot-agents/dot-agents/cmd/dot-agents@latest`
- **GitHub Releases**: `goreleaser` for automated cross-platform binaries
- **install.sh**: Updated to download from GitHub releases (no longer needs the whole repo)

---

## Implementation Phases

### Phase 1 — Foundation (go.mod, config, links, ui, platform interface)

- `go.mod` with only `github.com/spf13/cobra` as external dependency
- `internal/config/` (paths + JSON load/save)
- `internal/links/` (symlink, hardlink, inode check, extension finder)
- `internal/ui/` (colored output matching existing style)
- `internal/platform/platform.go` (interface + registry)
- Wire up Cobra root command with global flags (--dry-run, --force, --verbose, --json, --yes)

### Phase 2 — Platform Implementations

- `cursor.go`: hard link rules, symlink agents
- `claude.go`: user rules (CLAUDE.md), project rules symlinks, agents symlinks, user agents
- `codex.go`: AGENTS.md with .mdc fallback, agents links, skills links
- `opencode.go`: user agents, project agent file links
- `copilot.go`: copilot-instructions.md, github/agents, vscode/mcp.json

### Phase 3 — Core Commands

- `init` — create ~/.agents/ structure, copy templates
- `add` — project scan, backup existing, create dirs, call platform.CreateLinks(), register
- `remove` — detect managed links by inode/target, remove them, unregister
- `refresh` — re-run CreateLinks() for all registered projects

### Phase 4 — Info Commands

- `status` — list projects, show link health
- `doctor` — check installations, validate links, detect redundancy
- `skills` — list/new/edit skill directories
- `agents` — list/new/edit agent directories
- `hooks` — manage ~/.agents/settings/*/claude-code.json hooks
- `sync` — git operations on ~/.agents/
- `explain` — static text descriptions

### Phase 5 — Distribution & Cleanup

- Add `goreleaser.yaml` for GitHub release automation
- Update `scripts/install.sh` to fetch binary from releases
- Update Homebrew formula
- Write unit tests for `links`, `config`, platform logic
- Remove `src/` bash scripts (or archive them)

---

## What Stays the Same (No Migration Needed)

- `~/.agents/` directory structure — identical
- All config file formats (JSON, TOML, .mdc, .md)
- Symlink/hardlink layout in project directories
- Template files in `share/templates/`
- Command names and flags

---

## Critical Files to Create


| File                            | Purpose                                                    |
| ------------------------------- | ---------------------------------------------------------- |
| `go.mod`                        | Module definition (`github.com/spf13/cobra` only)          |
| `cmd/dot-agents/main.go`        | Cobra root, global flags, command registration             |
| `internal/config/paths.go`      | `AgentsHome()`, `UserHomeRoots()`, `ExpandPath()`          |
| `internal/config/config.go`     | `Load()`, `Save()`, `AddProject()`, `RemoveProject()`      |
| `internal/links/links.go`       | `Symlink()`, `Hardlink()`, `AreHardlinked()`, `FindFile()` |
| `internal/ui/output.go`         | All terminal output helpers                                |
| `internal/platform/platform.go` | Interface + `NewRegistry()`                                |
| `internal/platform/claude.go`   | Most complex platform (user rules + project rules)         |
| `commands/add.go`               | Largest command                                            |
| `commands/remove.go`            | Second largest command                                     |
| `scripts/install.sh`            | Updated installer                                          |


---

## Key Implementation Notes (from bash source analysis)

### Config JSON structure (actual format in use)

```json
{
  "version": 1,
  "defaults": { "agent": "cursor" },
  "projects": {
    "project-name": {
      "path": "/path/to/project",
      "added": "2024-01-01T00:00:00Z"
    }
  },
  "agents": {
    "cursor": { "enabled": true, "version": "..." },
    "claude": { "enabled": true, "version": "..." }
  },
  "features": {
    "tasks": false,
    "history": false,
    "sync": false
  }
}
```

### Platform linking details

**Cursor** (HARD LINKS — Cursor doesn't follow symlinks):

- `~/.agents/rules/global/*.{mdc,md}` → `.cursor/rules/global--{name}.mdc`
- `~/.agents/rules/{project}/*.{mdc,md}` → `.cursor/rules/{project}--{name}.mdc`
- `~/.agents/settings/{project,global}/cursor.json` → `.cursor/settings.json`
- `~/.agents/mcp/{project,global}/cursor.json` → `.cursor/mcp.json`
- `~/.agents/settings/{project,global}/cursorignore` → `.cursorignore`
- `~/.agents/agents/{project}/*/AGENT.md` → `.claude/agents/{name}/` (symlink dirs, GCD compat)

**Claude Code** (SYMLINKS):

- `~/.agents/rules/global/{claude-code,rules}.{mdc,md,txt}` → `~/.claude/CLAUDE.md` (user-level)
- `~/.agents/rules/{project}/*.{md,mdc,txt}` → `.claude/rules/{project}--{stem}.md`
- `~/.agents/settings/{project}/claude-code.json` → `.claude/settings.local.json`
- `~/.agents/mcp/{project,global}/claude.json` → `.mcp.json`
- `~/.agents/agents/{project}/*/` → `.claude/agents/{name}/` (symlink dirs)
- `~/.agents/skills/{project}/*/` → `.claude/skills/{name}/` + `.agents/skills/{name}/`
- `~/.agents/agents/global/*/` → `~/.claude/agents/{name}/` (user-level)
- `~/.agents/skills/global/*/` → `~/.claude/skills/{name}/` (user-level)

**Codex** (SYMLINKS):

- `~/.agents/rules/global/{agents,rules}.{md,mdc}` → `AGENTS.md` (primary)
- `~/.agents/rules/{project}/{agents}.{md,mdc}` → `AGENTS.md` (project override)
- `~/.agents/settings/{project,global}/codex.toml` → `.codex/config.toml`
- `~/.agents/agents/{project}/*/` → `.claude/agents/{name}/` (GCD compat)
- `~/.agents/skills/{project}/*/` → `.agents/skills/{name}/`
- `~/.agents/agents/global/*/` → `~/.codex/agents/{name}/` (user-level)
- `~/.agents/skills/global/*/` → `~/.agents/skills/{name}/` (user-level)

**OpenCode** (SYMLINKS):

- `~/.agents/settings/{project,global}/opencode.json` → `opencode.json`
- `~/.agents/rules/{project}/opencode-*.md` → `.opencode/agent/{name-without-prefix}.md`
- `~/.agents/rules/global/opencode-*.md` → `~/.opencode/agent/{name-without-prefix}.md` (user-level)
- `~/.agents/skills/{project}/*/` → `.agents/skills/{name}/`

**GitHub Copilot** (SYMLINKS):

- Priority chain for `.github/copilot-instructions.md`:
  1. `~/.agents/rules/{project}/copilot-instructions.md`
  2. `~/.agents/rules/global/copilot-instructions.md`
  3. `~/.agents/rules/{project}/rules.{md,mdc,txt}`
  4. `~/.agents/rules/global/rules.{md,mdc,txt}`
- `~/.agents/skills/{project}/*/` → `.agents/skills/{name}/`
- `~/.agents/agents/{project}/*/AGENT.md` → `.github/agents/{name}.agent.md`
- Priority chain for `.vscode/mcp.json`: `{project,global}/copilot.json`, `{project,global}/mcp.json`
- `~/.agents/settings/{project,global}/claude-code.json` → `.claude/settings.local.json`

### Remove logic

- Cursor: only remove hard links where inode matches source in `~/.agents/`
- Others: only remove symlinks where target starts with `~/.agents/` path
- `--clean`: also `rm -rf ~/.agents/{rules,settings,mcp,skills,agents}/{project}/`

### Refresh logic

- Re-runs CreateLinks() for all enabled platforms
- Writes `.agents-refresh` marker file with version + git commit
- Restores from `~/.agents/resources/{project}/` before re-linking

### Inode comparison (cross-platform)

- macOS: `stat -f %i`
- Linux: `stat -c %i`
- Go equivalent: `os.Lstat()` → `FileInfo.Sys().(*syscall.Stat_t).Ino`

### Windows WSL support

- When repo path matches `/mnt/c/Users/{user}/...`, also mirror user-level configs to that Windows home

---

## Verification

After Phase 3 is complete, run against a real project:

```bash
# Build
go build -o dot-agents-go ./cmd/dot-agents

# Test core flow
./dot-agents-go init
./dot-agents-go add ~/Documents/payout
ls -la ~/Documents/payout/.cursor/rules/    # global--rules.mdc + payout--*.mdc
ls -la ~/Documents/payout/.claude/rules/    # payout--*.md only
ls -la ~/.claude/CLAUDE.md                  # → ~/.agents/rules/global/rules.mdc
ls -la ~/Documents/payout/AGENTS.md        # → ~/.agents/rules/global/rules.mdc
./dot-agents-go status
./dot-agents-go remove payout --dry-run
./dot-agents-go remove payout

# Verify binary is self-contained
file dot-agents-go
ldd dot-agents-go 2>/dev/null || otool -L dot-agents-go  # should show no external deps beyond libc
```

