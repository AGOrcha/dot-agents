# Plan: Platform Session Integration

**Status:** pending  
**Spec:** `.agents/workflow/specs/platform-session-integration/design.md`

## What P0 delivered (shipped on current branch)

`da workflow checkpoint --log-to-iter N` writes an `agent` block:

```yaml
agent:
  session_id: ed4c8af1-4683-4e96-9b76-76059b304bde
  harness: claude-code
  harness_version: 2.1.138
  model: sonnet-4-6
  entrypoint: cli
```

**Architecture established by P0:**
- `SessionReader` interface lives in `internal/platform/platform.go`.
- All five platform structs implement it (confirmed for Claude Code; stubs for others).
- Session scanning helpers (`scanJSONLForLastModel`, `claudeSessionJSONLPath`, `findCodexSessionFile`) live in `internal/platform/session.go`.
- `resolveAgentBlock` in `iter_log.go` calls `platform.All()` + type-assert; no platform-specific logic in the command layer.

Adding a new platform or confirming an env var contract = one file in `internal/platform/`. No changes to `iter_log.go` or the checkpoint command.

---

## Interface family (P1–P3 extend the P0 pattern)

```
internal/platform/platform.go
  SessionReader        (P0, shipped)
  StatsReader          (P1) — pre-aggregated usage stats per platform
  SessionTokenScanner  (P2) — JSONL token scanning with time windowing
  BranchSessionFinder  (P3) — recent sessions by git branch for orient
```

Each interface is optional. Platforms implement whichever capabilities their session stores support. Command/workflow code type-asserts through `platform.All()`.

---

## Task sequence

All three P1–P3 tasks are **independent** — no inter-task dependencies. They can be implemented in any order or in parallel. The blocking relationship removed from earlier draft (P1 → P2 → P3) was artificial.

### P1 — `StatsReader` + `da session stats`

New `session` command group. `da session stats` iterates `platform.All()` → `StatsReader`, renders per-platform sections with graceful skip for absent files.

Per-platform implementations:
- `claude.ReadUsageStats` → `~/.claude/stats-cache.json` (pre-aggregated: tokens by model, daily activity, session count)
- `codex.ReadUsageStats` → `~/.codex/session_index.jsonl` (last 10 thread names + timestamps)
- `cursor.ReadUsageStats` → `~/.cursor/ai-tracking/ai-code-tracking.db` `scored_commits` table (last 10 commits: AI vs human attribution)

**Pre-coding decision:** SQLite access strategy for Cursor. Native Go (`modernc.org/sqlite`) vs `os/exec sqlite3` CLI fallback. Check `go.mod` first — if `modernc.org/sqlite` is already present, use it. If absent and adding a pure-Go dep is acceptable, add it. Otherwise use CLI with skip-note when `sqlite3` is absent.

### P2 — `SessionTokenScanner` + `session_tokens` in iter-log

Extends `internal/platform/session.go` with `ScanSessionTokens` for Claude Code. Adds `checkpoint_at` (RFC3339) and `session_tokens` block to `iterLogEntry`. Time-windows the JSONL scan using `checkpoint_at` from the previous iter-log entry.

Schema sync: both `schemas/workflow-iter-log.schema.json` and `commands/workflow/static/workflow-iter-log.schema.json` must be identical — `TestWorkflowIterLogEmbeddedSchemaMatchesCanonical` enforces this.

### P3 — `BranchSessionFinder` + orient section

Adds `claude.FindSessionsOnBranch` which scans `~/.claude/projects/<hash>/` JSONL files (mtime-sorted, max 20, stop at 3 matches). Orient renders "Recent sessions on this branch" section when results are non-empty.

**Pre-coding:** locate the orient render function (`orient.go` or `state.go`) before writing. The section must be omitted entirely (not rendered as empty) when no matches exist.

---

## Resolved pre-coding decisions

**1. Cursor SQLite dep (P1):** `modernc.org/sqlite v1.48.2` is already a direct dep in `go.mod`. Use it directly in `cursor.go`. No new dependency.

**2. Codex token fields (P2) — real implementation, not a stub:** Every Codex session sampled has `event_msg` entries with `payload.type == "token_count"` (81 events in one session). Use `last_token_usage` (per-turn delta), not `total_token_usage` (cumulative). Confirmed fields: `input_tokens`, `output_tokens`, `cached_input_tokens` (→ CacheReadTokens), `reasoning_output_tokens` (→ ReasoningTokens). No cache_creation concept. First event sometimes has null info — guard required.

**3. Orient render function location (P3):** `renderWorkflowOrientMarkdown` in `state.go` owns orient rendering; all section renderers are sibling `renderOrient*Section(state, out)` functions. The new `renderOrientRecentSessionsSection` goes in `state.go` and is called after `renderOrientRecentCommitsSection`. `workflowOrientState` gets a `RecentSessions` field, populated inside `enrichWorkflowState` after preferences resolution.

**4. Cursor token source (P2):** `cursor agent` is a real CLI subcommand (not top-level). `cursor --help` shows only the IDE launcher; `cursor agent --help` exposes the full CLI with `--print`, `--output-format stream-json`, `--resume`, `--worktree`.
Token source: `~/.cursor/projects/<slug>/agent-tools/*.txt` — stream-json per completed `cursor agent` run; final line is `type=result` with camelCase usage (`inputTokens/outputTokens/cacheReadTokens/cacheWriteTokens`). Slug = project path with `/` replaced by `-` (leading `/` stripped).
`.ralph-loop-streams/run-*/` is project-local output from the `ralph-worker` script (`bin/tests/ralph-worker`) running any agent binary — not cursor-specific. Each platform scanner reads its own native files; ralph-loop-streams are not scanned from the cursor scanner.

**5. Cursor CLI agent mode — confirmed present:** `cursor agent` subcommand confirmed. Session runs write result files to `~/.cursor/projects/<slug>/agent-tools/<uuid>.txt`. Native IDE chat files (`agent-transcripts/` JSONL, `worker.log`, `store.db`, `chats/` SQLite) contain NO token data.

**6. OpenCode token source (P2) — real implementation, not a stub:** SQLite at `~/.local/share/opencode/opencode.db` (XDG path, same on macOS). `part` table, rows where `type='step-finish'`, token data in `data` JSON column at `$.tokens.input`, `$.tokens.output`, `$.tokens.cache.read`, `$.tokens.cache.write` (floats). Joined to `message` table via `part.message_id = message.id` to get `message.created_at` (Unix ms) for time-windowing. No session ID env var exists — filter by timestamp only, so all OpenCode usage across projects in the window is included. `modernc.org/sqlite` already in `go.mod` (imported via blank import in `opencode.go`).

**7. GitHub Copilot token source (P2) — partial data only:** Copilot CLI stores `events.jsonl` per session at `~/.copilot/session-state/<session-id>/events.jsonl`. The `session.shutdown` event contains `modelMetrics` with per-model aggregate totals (camelCase: `inputTokens`, `outputTokens`, `cacheReadTokens`, `cacheWriteTokens`, `reasoningTokens`). Per-turn counts are ephemeral (memory only, not written to disk). No session ID env var is published by the CLI. `copilot.ScanSessionTokens` filters by `events.jsonl` mtime > afterTimestamp and sums `session.shutdown` modelMetrics. `MessageCount` = number of model metric entries per completed session. VS Code extension stores no token data locally.

> **Reference convention:** Decisions, requirements, and TASKS notes use *symbol-only* references (function/struct/file names, "after preferences resolution", "before mergeIterLogTopLevelGit returns"). Line numbers drift between sessions and across loop-worker invocations, so they are deliberately omitted.
