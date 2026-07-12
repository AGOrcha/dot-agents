# Local transcript corpus inventory

Task `corpus-inventory` of plan `transcript-analysis-and-pipeline-craft`. Governed by
`../../methodology/evidence-rubric.md` §1 (inventory fields) and §2b (redaction gate R1–R5).
Snapshot: **2026-07-12**. Harness/transcript dirs are read-only; **zero raw `.jsonl` copied** into the repo (R1).
All committed anchors/paths use `~` for the home dir — no absolute `/Users/<user>` path or dashed user slug appears in artifacts (R3).

## Files
- `inventory.jsonl` — machine-readable, **one row per session** (806 rows). Canonical enumeration.
- `source-inventory-<harness>.md` — rubric §1 table per harness (omp, claude-code, codex, cursor, copilot).
- `README.md` — this summary.

## Per-harness summary

| harness | sessions | subagent files (folded) | total `.jsonl` | date range | tokens% | cost% | wallclock% |
|---|--:|--:|--:|---|--:|--:|--:|
| omp | 5 | 253 | 258 | 2026-07-07 → 2026-07-12 | 100.0 | 100.0 | 80.0 |
| claude-code | 50 | 1630 | 1680 | 2026-05-23 → 2026-07-11 | 80.0 | 0.0 | 20.0 |
| codex | 592 | 0 | 592 | 2026-02-28 → 2026-07-07 | 90.5 | 0.0 | 10.6 |
| cursor | 158 | 44 | 202 | — (no timestamps) | 0.0 | 0.0 | 0.0 |
| copilot | 1 | 0 | 1 | 2026-05-11 → 2026-05-11 | 100.0 | 100.0 | 100.0 |
| **total** | **806** | **1927** | **2733** | 2026-02-28 → 2026-07-12 | | | |

## Count reconciliation (acceptance gate)

Every `.jsonl` under each harness root is accounted for exactly once — as a session row or a folded subagent:

| harness | recursive `.jsonl` census | = primary sessions | + folded subagents |
|---|--:|--:|--:|
| omp | 258 | 5 | 253 |
| claude-code | 1680 | 50 | 1630 |
| codex | 592 | 592 | 0 |
| cursor | 202 | 158 | 44 |
| copilot | 1 | 1 | 0 |
| **total** | **2733** | **806** | **1927** |

`806` sessions + `1927` folded subagent transcripts = `2733` = recursive census `2733`. ✔
Total primary records inventoried: **274,679** (line count of primary `.jsonl` per session).

**Granularity rule.** A *session* = a session directory/file (rubric §1). OMP / Claude Code / Cursor nest
subagent transcripts inside the parent session dir → folded (counted; `models`+`has_*` unioned; `record_count`
= primary only). Codex stores subagent *threads* as independent top-level rollout files → each is its own
session row (`role=subagent-thread`, `parent_thread_id` recorded); 45/592 codex sessions are such threads.

## Status breakdown

| harness | complete | cutoff | truncated |
|---|--:|--:|--:|
| omp | 3 | 2 | 0 |
| claude-code | 38 | 11 | 1 |
| codex | 570 | 22 | 0 |
| cursor | 156 | 2 | 0 |
| copilot | 1 | 0 | 0 |

`cutoff` = ends mid-tool-call or mid-assistant turn (incl. in-progress live sessions). Status for harnesses
without an explicit exit marker (Claude Code, Cursor) is `[INFERENCE]` from the last record / dangling tool-call;
OMP uses the `session_exit` custom record, Codex uses `task_complete`, Copilot uses `session.shutdown`.

## Sensitivity triage (R5)

Each session labelled `public-ok | internal | sensitive` from a **high-confidence** secret scan (private-key blocks,
AWS/GitHub/OpenAI/Slack/GitLab key shapes, JWTs, `user:pass@` URLs, `Authorization: Bearer …`) plus a private-reference
check (home paths, private org/repo names). Hits were **counted only — never quoted or stored** (R2). A `sensitive`
session contributes aggregate stats only downstream (no excerpts).

| harness | public-ok | internal | sensitive |
|---|--:|--:|--:|
| omp | 0 | 1 | 4 |
| claude-code | 0 | 39 | 11 |
| codex | 0 | 571 | 21 |
| cursor | 12 | 144 | 2 |
| copilot | 0 | 1 | 0 |
| **total** | 12 | 756 | 38 |

Most sessions are `internal` (private dev transcripts: home paths, private repos). `sensitive` = a
high-confidence secret-shaped span was detected in the primary or a folded subagent transcript.

## Redaction gate compliance (§2b)
- **R1** — no raw transcripts committed: only inventory rows + `sha256` digests. No `.jsonl` copied under `evidence/`.
- **R2** — secret scan run over every session (primary + subagents); hits counted, never quoted/written.
- **R3** — all paths `~`-relative (home-dir user slug normalized to `~`, incl. dashed `-Users-<user>-`/`Users-<user>-` and slashed `/Users/<user>`); `project_slug` = `~`-relative cwd from records where resolvable.
- **R4** — each row carries `content_sha256` (streamed digest of the primary `.jsonl`) as an auditable anchor.
- **R5** — per-session sensitivity triage (above); `sensitive` → aggregate-only downstream.

## Coverage notes (which axes each harness can feed)
- **OMP** — tokens+cost per `message.usage.cost`; wallclock per `toolResult.details.wallTimeMs`. Richest source.
- **Claude Code** — tokens per `message.usage`; **no `$` cost**; wallclock only in some `toolUseResult.durationMs`.
- **Codex** — tokens per `token_count.info.total_token_usage` (newer sessions only); no cost; wallclock per `exec_command_end.duration`.
- **Cursor** — **no tokens/cost/model/wallclock and no per-record timestamps**; usable for mechanism/craft/outcome text only.
- **Copilot** — tokens per `assistant.message.outputTokens` + `session.shutdown.modelMetrics`; cost = **premium-request credits** (not USD); wallclock per `totalApiDurationMs`.

## Gaps / unknowns
- Cursor (158 sessions): no in-record timestamps → `started_at`/`ended_at` unavailable (mtime forbidden). Time-based axes cannot use Cursor.
- Codex: 56 older sessions record rate-limit % but not token totals (`tokens=n`).
- Claude Code: 1 summary-only stub session has no message timestamps.
- 2 OMP sessions were live at snapshot (`cutoff`); their `record_count`/`ended_at` are a 2026-07-12 point-in-time.

## Known-absent sources
- **OpenCode** — not installed (checked 2026-07-12: `~/.opencode`, `~/.local/share/opencode`, `~/.config/opencode` absent). Known-absent, not a gap.
