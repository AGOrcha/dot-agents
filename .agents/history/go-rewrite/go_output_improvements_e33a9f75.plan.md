---
name: Go Output Improvements
overview: Improve the Go CLI output quality, clarity, and user experience across all commands — going beyond bash parity toward a polished, production-grade CLI.
todos:
  - id: output-layer
    content: Add StepN(), standardize blank-line discipline, wire styled error output through main.go
    status: pending
  - id: status-badges
    content: "status: per-platform badge row, last-refreshed timestamp, cleaner project display"
    status: pending
  - id: add-preview
    content: "add: make preview platform-aware (group by platform, show installed/skipped)"
    status: pending
  - id: doctor-health
    content: "doctor: add link health checks per project, git repo validation for ~/.agents/"
    status: pending
  - id: refresh-progress
    content: "refresh: StepN progress per project, always show skipped platforms, add empty-run message"
    status: pending
  - id: sync-colors
    content: "sync status: color staged/ahead counts, suppress ahead/behind when no remote, add summary line"
    status: pending
  - id: hooks-format
    content: "hooks list: format hooks as readable named blocks instead of raw JSON"
    status: pending
  - id: skills-frontmatter
    content: "skills/agents list: read and display frontmatter description, add count summary"
    status: pending
isProject: false
---

# Go Command Output Improvements

## Analysis of Current Output Quality

### Systemic Issues

**1. Inconsistent spacing and structure**

- `Header` always adds a separator line (`────────────────────────────────────────`) but commands like `refresh` follow it with a bold `Section` that collides visually
- `Step()` uses bold only — no visual step numbering or differentiation from `Section()`
- Mixed indentation: some bullets are   `✓` (2 spaces), others in audit functions use       `✓` (6 spaces) with no clear hierarchy model
- Blank line discipline is inconsistent: some sections end with `fmt.Fprintln`, some don't

**2. Weak error UX**

- Errors are just Go-style `fmt.Errorf` strings passed to cobra, printed as bare `Error: ...` — no color, no context hints, no "did you mean" suggestions
- `cobra.SilenceErrors: true` + `SilenceUsage: true` means errors disappear unless the main function re-prints them — currently `main.go` doesn't handle this

**3. `status` is too terse**

- Default view shows a single bullet `"N managed links"` — gives no sense of which platforms are active
- No "last refreshed" info (`.agents-refresh` marker exists but is never read)
- `--audit` info is deeply indented and platform sections lack visual separation

**4. `add` preview is static/misleading**

- Preview section lists 16 items as if they'll all be created, but most depend on which platforms are enabled/installed
- Better: show only items relevant to enabled platforms, mark others as `(not enabled)`

**5. `doctor` is too shallow**

- Only checks `~/.agents/` existence, `config.json` existence, platform installs, and project dir existence
- Doesn't check link health per-project (that's in `status --audit` but `doctor` should surface broken links)
- Doesn't validate `~/.agents/` is a git repo, or that the `.gitignore` covers backups

**6. `refresh` progress is unclear for multi-project runs**

- Prints project name as bold text inline — no visual grouping or progress indicator
- Disabled platforms are silently skipped (only shown with `--verbose`)

**7. `sync status` always shows 0/0 for ahead/behind when no remote**

- Should suppress ahead/behind when `origin/HEAD` doesn't exist
- Staged/Unstaged/Untracked shown as bare numbers with no visual cues (green/yellow/red)

**8. `hooks list` dumps raw JSON**

- Useful data presented as raw JSON indent — should be formatted as a readable table/list

**9. `skills list` / `agents list` are minimal**

- Just prints name + ok/warn bullet — no description from frontmatter, no count summary

**10. `explain` uses plain `fmt.Fprintln` with no ANSI formatting**

- Long text blocks with no headers, indentation, or color — hard to read in terminal

---

## Improvement Plan

### 1. Output layer: step numbering, consistent spacing, error handling

**Files:** `[internal/ui/output.go](internal/ui/output.go)`, `[cmd/dot-agents/main.go](cmd/dot-agents/main.go)`

- Add `StepN(n int, total int, msg string)` variant that prints `[1/4]` prefixes so multi-step commands are scannable
- Standardize blank-line discipline: every `Section`/`Step` call emits exactly one leading blank line; no trailing blank lines from bullets
- Re-export a `PrintError(err error)` that routes through `ui.Error` with red styling — wire into main.go's cobra error handler so all errors get styled output
- Add `ui.Confirm` improvement: show `[y/N]` in dim, highlight the user's response

### 2. `status`: add per-platform summary row and "last refreshed"

**File:** `[commands/status.go](commands/status.go)`

- In the default (non-audit) view, add a platform badge row per project: `Cursor  Claude  Codex  Copilot` with colored ticks/crosses/dashes based on detected links — single line, scannable
- Read `.agents-refresh` marker file and display `last refreshed: <date>` in dim
- Show project path only if it's not just `~/<project>`; suppress it when it matches the name

### 3. `add`: make preview platform-aware

**File:** `[commands/add.go](commands/add.go)`

- Replace the static 16-item preview list with a dynamic one: group by platform, show `(installed)` / `(not installed — skipped)` per platform block
- This makes the preview accurate and educational, not a dump of every possible link

### 4. `doctor`: add link health checks and git repo validation

**File:** `[commands/doctor.go](commands/doctor.go)`

- Add "Link Health" section: for each project, count and report broken managed links (reuse logic already in `status.go`'s health check)
- Add check: is `~/.agents/` a git repo? If yes, show branch + whether there's a remote. If no, suggest `dot-agents sync init`
- Add check: does `~/.agents/.gitignore` exist and cover `*.dot-agents-backup`?

### 5. `refresh`: add per-project progress header and show skipped platforms

**File:** `[commands/refresh.go](commands/refresh.go)`

- Use `StepN` or a clear `[project N/M]` header for each project when refreshing multiple
- Always show skipped-because-not-installed platforms with `ui.Skip()` (currently only shown in `--verbose`)
- Show "nothing to refresh" when count == 0

### 6. `sync status`: improve color and suppress missing-remote noise

**File:** `[commands/sync.go](commands/sync.go)`

- Only show Ahead/Behind row when a remote is configured
- Color staged count yellow if > 0, untracked count dim, ahead count green
- Add a `No changes` / `N changes pending commit` summary line at the end

### 7. `hooks list`: format as readable list instead of raw JSON

**File:** `[commands/hooks.go](commands/hooks.go)`

- Parse the hooks structure and print each hook event (`PreToolUse`, `PostToolUse`, etc.) as a named block with command(s) listed under it — using `Section` + `Bullet` rather than raw JSON

### 8. `skills list` / `agents list`: read frontmatter description

**Files:** `[commands/skills.go](commands/skills.go)`, `[commands/agents.go](commands/agents.go)`

- Parse the YAML frontmatter `description:` field from `SKILL.md` / `AGENT.md` and print it dim next to the name
- Add a count summary line at the bottom: `N skill(s) in global scope`

### 9. `explain`: apply ANSI formatting to text blocks

**File:** `[commands/explain.go](commands/explain.go)`

- Wrap headings in `ui.Bold`, section titles in `ui.Section`, and command examples in `ui.Dim`
- This is a pure output improvement — no logic changes

---

## Priority Order

- High impact / low effort: 1 (output layer), 2 (status badges), 6 (sync status colors), 9 (explain formatting)
- High impact / moderate effort: 3 (add preview), 4 (doctor), 5 (refresh progress)
- Polish: 7 (hooks), 8 (skills/agents frontmatter)

