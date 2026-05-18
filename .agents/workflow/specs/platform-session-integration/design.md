# Spec: Platform Session Integration

**ID:** platform-session-integration  
**Status:** stable  
**Created:** 2026-05-10  
**Linked plan:** `.agents/workflow/plans/platform-session-integration/`

---

## Problem

The dot-agents workflow captures what was built (git diff, commit SHA, verifier results) but is blind to how it was built. Session token costs, cache efficiency, cross-platform usage patterns, and recent session context on a branch are all available from platform-native files but are never read or surfaced.

Three concrete gaps:

1. **No token visibility.** Claude Code sessions record per-message token usage (input, output, cache_creation, cache_read) in JSONL files. The iter-log approximates context tokens via ralph-loop-streams heuristics; actual costs are unknown. Cache hit rate — which directly reflects prompt engineering quality — is never computed.

2. **No cross-platform usage picture.** Codex sessions (JSONL under `~/.codex/sessions/`), Cursor AI attribution (SQLite `scored_commits` with human vs AI line counts), and Claude Code stats (`~/.claude/stats-cache.json`) are all disconnected from each other and from the workflow.

3. **Agents start cold on known branches.** Claude Code stores `gitBranch` on every JSONL entry. When an agent is oriented on a branch with prior sessions, it gets no signal that prior sessions exist. This burns context re-establishing state the prior session already had.

---

## Goals

1. Surface real token metrics (input, output, cache_read, cache_creation) per iter-log iteration, aggregated from the session JSONL linked via the session_id captured in P0.
2. Provide a `da session stats` command that aggregates cross-platform usage: token counts by model (Claude Code), session index with thread names (Codex), AI code attribution per commit (Cursor).
3. Enrich `da workflow orient` with recent session context for the current project+branch — session IDs, timestamps, and entry counts from sessions on the same git branch.

---

## Decisions

### D0: All platform read capabilities live in `internal/platform/` as optional interfaces

P0 established `SessionReader` as the first read-side interface on platform structs. P1–P3 extend this pattern with three additional optional interfaces. The command layer calls `platform.All()` and type-asserts to the needed interface; it never hardcodes platform-specific paths or file formats.

**Interface family:**
- `SessionReader` (P0, shipped): harness identity, session env vars, model resolution.
- `StatsReader` (P1): pre-aggregated usage stats — tokens by model, session counts, commit attribution. Each platform reads from its own native store (JSON, JSONL, SQLite).
- `SessionTokenScanner` (P2): scans the platform's session JSONL for per-message token usage within a time window.
- `BranchSessionFinder` (P3): finds recent sessions on the current git branch for orient context.

Adding a new platform means implementing whichever interfaces it supports on its struct. The command layer is not touched.

**Why this matters:** every platform already has read capability shape (Claude Code: JSONL + stats-cache, Codex: JSONL + session_index, Cursor: SQLite, OpenCode/Copilot: unknown). The interface family makes each platform's read contract explicit and testable, and prevents command-layer code from accumulating platform-specific path logic.

### D1: Session JSONL is the token source of truth (not ralph approximations)

Ralph-loop-streams tracks `context_tokens_approx` at dispatch time. Claude Code session JSONL records actual API usage per message including cache breakdown. The JSONL is authoritative; ralph approximations remain useful for real-time per-worker estimates but should not be the reporting surface.

**Rejected alternative:** Parse ralph metrics and treat them as canonical. Rejected because they're approximations and don't capture cache_read vs cache_creation split, which is the signal for prompt engineering efficiency.

### D2: `StatsReader` reads pre-aggregated files where available

`~/.claude/stats-cache.json` is maintained by Claude Code and contains pre-aggregated daily/model token counts. Claude Code's `StatsReader` reads this file rather than scanning hundreds of JSONL files. Codex's `StatsReader` reads `session_index.jsonl` (thread-level metadata, not per-message). Cursor's `StatsReader` reads `scored_commits` from the SQLite db.

**Why:** pre-aggregated sources are fast and don't require scanning all sessions. Raw JSONL scanning is reserved for P2 where we need the specific session's token data over a time window.

### D3: Cursor's `StatsReader` uses SQLite directly in `internal/platform/cursor.go`

`~/.cursor/ai-tracking/ai-code-tracking.db` is a SQLite 3.x database. Cursor's `StatsReader.ReadUsageStats()` opens it with `database/sql` + `modernc.org/sqlite` (pure-Go, no CGO). This keeps all Cursor path and format knowledge inside `cursor.go`.

**Rejected alternative:** shell out to the `sqlite3` CLI. Rejected because the CLI may not be present on all systems and adds a runtime dependency. `modernc.org/sqlite` is already in the Go ecosystem for this purpose.

**If `modernc.org/sqlite` is not already in `go.mod`**, add it in P1. Check first — other packages in the repo may already pull it.

### D4: `BranchSessionFinder` is branch-scoped, Claude Code only for P3

Claude Code stores `gitBranch` on every JSONL entry — the only confirmed platform that does this. Only `claude` implements `BranchSessionFinder` in P3. Other platforms implement it as stubs returning nil until their session schema is confirmed.

Branch scoping (not project scoping) is intentional: project-level surfaces unrelated work from other branches.

**Rejected alternative:** Filter by active plan wave ID. Plan IDs are not present in JSONL entries.

### D5: Token metrics are a top-level iter-log block, time-windowed by `checkpoint_at`

Token usage belongs at the iteration level (not inside `impl`), since verifier and review stages also consume tokens in the same session. A new `session_tokens` block holds the aggregate. Time-windowing across iterations requires a `checkpoint_at` ISO-8601 timestamp field added to the iter-log entry simultaneously — `SessionTokenScanner` receives `afterTimestamp` and scans entries newer than it.

**Rejected alternative:** store byte offsets (fragile on restart), store processed UUIDs (large), divide full-session totals by iteration count (wrong for uneven sessions).

---

## Requirements

Behavioral contracts only. The plan owns interface signatures, file paths, and per-platform implementation prescriptions.

### P1: Cross-platform usage stats command

A new CLI surface aggregates usage data from every platform that publishes a native store. For each platform with available data: surface aggregate session counts, message counts, per-model token totals (where present), and any platform-specific attribution (Cursor's AI-vs-human commit attribution; Codex's session/thread index). Platforms without a store render a skip note rather than an error. Output style matches existing workflow-status surfaces (plain text, section headers, no ANSI colour). Adding a new platform is a single-file change inside `internal/platform/`; the command layer must not learn platform-specific paths or schemas.

### P2: Real per-iteration token metrics in iter-log

Each iteration's iter-log entry gains a token-usage record sourced from the linked session's authoritative store, time-windowed to that iteration. The window is bounded by a checkpoint timestamp written into the iter-log entry; subsequent iterations consume the previous entry's checkpoint as their lower bound. The recorded metrics distinguish input, output, cache-read, cache-creation, and reasoning tokens, and surface a cache hit rate when the underlying data permits. Scanning failures degrade silently — the iter-log entry still writes without the token block and checkpoint capture is never blocked. The iter-log schema (canonical and embedded copy) remains in sync; the schema-parity test must pass.

### P3: Branch session context in orient

`da workflow orient` surfaces recent prior sessions for the current git branch when the active platform's store records branch metadata. The section identifies platform, a truncated session identifier, a timestamp, and an approximate message count. The section is omitted entirely (not rendered as an empty header) when no matches exist. Branch scoping is intentional: project-level scoping would surface unrelated work from other branches.

---

## Done Criteria

**P1:** Running the new stats command on a developer machine with mixed platform usage renders sections for every platform that has data and skip notes for those that don't, with no command-layer changes required to add a future platform.

**P2:** A fresh iteration entry written by `da workflow checkpoint --log-to-iter` includes a checkpoint timestamp and (when a session is linked) a populated token block whose cache hit rate is plausible for sessions that exercise the prompt cache. Schema parity holds.

**P3:** Orient on a branch with prior platform sessions includes the recent-sessions section; orient on a branch without prior sessions omits the section entirely.

---

## Deferred

- Codex per-session token counts (`response_item` token fields are not consistently present; needs more sampling).
- Cost-in-USD per iteration (requires per-model pricing table; deferred until pricing stabilises).
- Cursor `conversation_summaries` in orient (conversation → branch mapping is non-trivial).
- OpenCode/Copilot `StatsReader`, `SessionTokenScanner`, `BranchSessionFinder` implementations (session schemas not yet confirmed).
- `BranchSessionFinder` for Codex (Codex JSONL does not embed `gitBranch`; would require different correlation strategy).
