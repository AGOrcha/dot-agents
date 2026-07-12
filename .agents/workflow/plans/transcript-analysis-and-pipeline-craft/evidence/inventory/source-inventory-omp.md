# Source Inventory — OMP (Oh My Pi)

Rubric: `../../methodology/evidence-rubric.md` §1. One row per session (session directory/file).
Timestamps are first/last **record** timestamps (never file mtime). Subagent transcripts are folded
into their parent session (see Exclusions); `record_count` counts the **primary** `.jsonl` only.

- **Corpus path:** `~/.omp/agent/sessions/<slug>/<ts>_<uuid>.jsonl (primary) + <ts>_<uuid>/**.jsonl (subagents)`
- **Sessions:** 5  |  **folded subagent transcripts:** 253  |  **total `.jsonl` files:** 258
- **Date range:** 2026-07-07T13:19:53Z → 2026-07-12T06:24:07Z
- **Coverage:** tokens 100.0% · cost 100.0% · wallclock 80.0%
- **Status:** {'complete': 3, 'cutoff': 2}  |  **Sensitivity:** {'internal': 1, 'sensitive': 4}
- **Models seen (union, primary+subagents):** claude-fable-5, claude-opus-4-6, claude-opus-4-8, claude-sonnet-5, claude-sonnet-5-high, glm-5.2-high, gpt-5.4, gpt-5.4-high, gpt-5.4-mini, gpt-5.5, gpt-5.5-medium, gpt-5.6-sol

| evidence_id | project_slug | started_at | ended_at | records | subs | tokens | cost | wallclock | model(s) | status | sensitivity |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `omp:2026-07-07T13-19-53-265Z_019f3cbc-2db0-7000-a699-bfbf1274aa01:019f3cbc-2db0-7000-a699-bfbf1274aa01` | ~/proj-docs/dot-agents | 2026-07-07T13:19:53Z | 2026-07-07T14:19:24Z | 13 | 0 | y | y | n | claude-opus-4-8, gpt-5.4 | complete | internal |
| `omp:2026-07-07T14-19-35-847Z_019f3cf2-d826-7000-8626-166a741fd7d9:019f3cf2-d826-7000-8626-166a741fd7d9` | ~/proj-docs/dot-agents | 2026-07-07T14:19:35Z | 2026-07-11T01:06:41Z | 1876 | 39 | y | y | y | claude-opus-4-8, claude-sonnet-5-high, glm-5.2-high, gpt-5.4, gpt-5.4-high, gpt-5.4-mini, gpt-5.5, gpt-5.5-medium | complete | sensitive |
| `omp:2026-07-08T00-32-22-093Z_019f3f23-da4d-7000-8465-d94a0d720d5b:019f3f23-da4d-7000-8465-d94a0d720d5b` | ~/proj-docs/payout | 2026-07-08T00:32:22Z | 2026-07-11T01:06:39Z | 5693 | 165 | y | y | y | claude-opus-4-6, claude-opus-4-8, claude-sonnet-5, gpt-5.4, gpt-5.4-high, gpt-5.4-mini, gpt-5.5, gpt-5.5-medium | complete | sensitive |
| `omp:2026-07-11T01-46-34-249Z_019f4eda-dd88-7000-b275-af8a46192d29:019f4eda-dd88-7000-b275-af8a46192d29` | ~/proj-docs/payout | 2026-07-11T01:46:34Z | 2026-07-12T02:26:17Z | 1126 | 1 | y | y | y | claude-fable-5, claude-opus-4-8 | cutoff | sensitive |
| `omp:2026-07-11T02-03-44-211Z_019f4eea-94d3-7000-aee5-b7ce6fff0f02:019f4eea-94d3-7000-aee5-b7ce6fff0f02` | ~/proj-docs/dot-agents | 2026-07-11T02:03:44Z | 2026-07-12T06:24:07Z | 3122 | 48 | y | y | y | claude-fable-5, claude-opus-4-8, gpt-5.4-mini, gpt-5.5, gpt-5.6-sol | cutoff | sensitive |

## Known-absent sources
OpenCode: not installed on this machine (checked 2026-07-12: `~/.opencode`, `~/.local/share/opencode`, `~/.config/opencode` all absent). Recorded as known-absent, not a gap.

## Exclusions applied
- Derived per-run artifacts excluded per rubric: `*.md` summaries (154), `*.log` (934), `.DS_Store` (2).
- 253 subagent/scout transcripts (in `<ts>_<uuid>/` run dirs, incl. nested per-agent subdirs) are folded into their parent session; not separate rows. `models`/`has_*` are unioned across primary+subagents; `record_count` is the primary transcript only.
- Empty slug dir `--private-tmp--` contained no `.jsonl` and no run dirs — no session.
- 2 sessions are still being written at snapshot time (status `cutoff`): the current run `019f4eea` (this analysis) and `019f4eda`. Counts are a 2026-07-12 snapshot.
- No `memory/` or `tool-results/` dirs exist under OMP session dirs on this machine.
