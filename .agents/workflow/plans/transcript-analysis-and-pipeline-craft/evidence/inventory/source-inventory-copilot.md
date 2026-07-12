# Source Inventory — GitHub Copilot CLI

Rubric: `../../methodology/evidence-rubric.md` §1. One row per session (session directory/file).
Timestamps are first/last **record** timestamps (never file mtime). Subagent transcripts are folded
into their parent session (see Exclusions); `record_count` counts the **primary** `.jsonl` only.

- **Corpus path:** `~/.copilot/session-state/<uuid>/events.jsonl (+ workspace.yaml sidecar)`
- **Sessions:** 1  |  **folded subagent transcripts:** 0  |  **total `.jsonl` files:** 1
- **Date range:** 2026-05-11T12:11:20Z → 2026-05-11T12:11:39Z
- **Coverage:** tokens 100.0% · cost 100.0% · wallclock 100.0%
- **Status:** {'complete': 1}  |  **Sensitivity:** {'internal': 1}
- **Models seen (union, primary+subagents):** claude-haiku-4.5

| evidence_id | project_slug | started_at | ended_at | records | subs | tokens | cost | wallclock | model(s) | status | sensitivity |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `copilot:23e45a55-b230-439a-9efb-4cc6cfa08f0e:23e45a55-b230-439a-9efb-4cc6cfa08f0e` | ~/Documents/dot-agents | 2026-05-11T12:11:20Z | 2026-05-11T12:11:39Z | 8 | 0 | y | y | y | claude-haiku-4.5 | complete | internal |

## Known-absent sources
OpenCode: not installed on this machine (checked 2026-07-12: `~/.opencode`, `~/.local/share/opencode`, `~/.config/opencode` all absent). Recorded as known-absent, not a gap.

## Exclusions applied
- 1 session-state dir has NO `events.jsonl` (scaffold only) → excluded, listed below.
- Non-transcript sidecars excluded: `workspace.yaml`, `vscode.metadata.json`, `research/`, `checkpoints/`, `files/`.
- Copilot `cost` = premium-request credits (`totalPremiumRequests`/`modelMetrics.*.requests.cost`), NOT USD — `has_cost=y` reflects that billing signal; unit noted for downstream normalization.
- **Excluded session dir (no primary transcript):** `e1932096-8ee6-406e-a1bb-74afdd45da63` — cwd `~`, created `2026-04-21T04:15:41.727Z`; contents `['checkpoints', 'files', 'research', 'workspace.yaml']`.
