# omp-cc-copilot corpus — extraction notes

Scope: all manifest selections with harness `omp`, `claude-code`, `copilot` = **56 sessions** (50 claude-code rule2-all-cc, 5 omp rule1-all-omp, 1 copilot rule1-all-copilot). No rule4 negative-controls fall in this harness set (those are codex/cursor, owned by sibling extractors); within-set generalization checks are provided by the copilot 2+2 smoke session and the 20-session D12 hop-chain experiment batch — both structurally *unlike* the dot-agents/payout workflow-loop mega-sessions.

Governing rules applied: rubric §2 (E1-E5), §2b redaction gate (R1-R5). Digests = sha256 of the raw anchored jsonl line(s). Sensitive sessions (rubric R5) contribute **aggregate counts only** — no excerpts; every qualitative item (mechanism/failure/craft/outcome) carries ≥1 anchor from an internal session with a redacted excerpt. Home paths redacted to `~`; org/user identifiers stripped; all excerpts secret-scanned before write.

## Harness schema notes (for reviewers)
- **omp** (`~/.omp/agent/sessions/`): assistant `message.usage` = input/output/cacheRead/cacheWrite/totalTokens + `cost{...}` (USD) + `duration` (ms). Tools = `toolCall` content blocks. `model_change`, `thinking_level_change`, `session_exit` (custom) records. Only harness in-set with per-turn **dollar cost + duration**.
- **claude-code** (`~/.claude/projects/`): assistant `message.usage` = input/cache_creation/cache_read/output tokens (**no dollar cost**). Delegation = `Agent` tool; task-mgmt = `TaskCreate/Update/Stop`; coordination = `SendMessage`; scheduling = `ScheduleWakeup/Cron/Monitor`; `Workflow`/`Skill` tools; MCP sonarqube + code-review-graph. `pr-link` records (repeat per reference — count DISTINCT prNumber). `<synthetic>` model turns = compaction boundaries. Subagent turns live in separate files (`subagent_files` in inventory), not inline (`isSidechain` always 0 here).
- **copilot** (`~/.copilot/session-state/.../events.jsonl`): `session.shutdown` carries modelMetrics (per-model requests/usage) + token accounting (system/toolDefinitions/conversation/current).

## OMP cost/mechanism table (all 5 sessions)
Aggregate stats; sensitive sessions = numbers only. [INFERENCE] where reconstructed from per-turn `duration`/per-model rollups.

| session | proj | status | turns | tok_total | cost $ | cacheR %$ | models | exit |
|---|---|---|---|---|---|---|---|---|
| 019f3cbc | dot-agents | complete | 2 | 0 | 0.00 | 0% | 1 | dispose(normal) |
| 019f3cf2 | dot-agents | complete | 353 | 88,877,412 | 40.75 | 65% | 6 | signal/SIGHUP |
| 019f3f23 | payout | complete | 1457 | 707,823,313 | 491.84 | 69% | 3 | signal/SIGHUP |
| 019f4eda | payout | cutoff | 341 | 104,872,253 | 81.78 | 62% | 1 | none(cutoff) |
| 019f4eea | dot-agents | cutoff | 981 | 405,390,538 | 319.66 | 63% | 3 | none(cutoff) |

**OMP tool-call counts (aggregate, sensitive sessions — counts only, R5).**
- 019f3cf2 (dot-agents): bash=278, read=187, job=47, grep=43, todo=31, glob=14, edit=12, task=10, write=8, lsp=7, irc=5, rewind=4
- 019f3f23 (payout): bash=826, read=322, todo=205, eval=129, write=81, grep=77, job=75, task=52, irc=32, github=30, glob=14, search_tool_bm25=5
- 019f4eda (payout): bash=107, read=103, edit=59, grep=35, todo=31, write=10, eval=10, glob=9, rewind=2, job=2, checkpoint=1, ask=1
- 019f4eea (dot-agents): bash=333, read=190, job=125, edit=92, todo=73, grep=54, write=48, eval=35, glob=21, task=12, github=10, rewind=6

Cross-session cost shape: cacheRead = **96-98% of tokens** and **61-69% of dollar cost** in all four large OMP sessions; single-turn API durations reach 732s (07-07T14-19) and 239s (07-08). The two `signal/SIGHUP` exits were recorded 2s apart (01:06:39.810Z / 01:06:41.648Z) with pending tool calls → one terminal/tmux restart killed both [INFERENCE]; the two `none(cutoff)` sessions started 40-57min later (01:46, 02:03) and were themselves cut off (live sessions at capture time). See items `omp-cost-totals-*`, `omp-session-exit-distribution`, `omp-turn-duration-shape-*`.

## Coverage — every assigned session accounted for (56/56)

`I`=anchored in ≥1 item; `N`=notes-line only. Sensitive marked `S`.

| # | harness | uid | proj | status | rec | sens | cover | note |
|---|---|---|---|---|---|---|---|---|
| 1 | claude-code | b41d7f0d | ResumeAgent | complete | 17 | int | N | metadata-only stub (0 assistant turns) |
| 2 | claude-code | 598c5d1e | dot-agents | complete | 4 | int | N | metadata-only stub (0 assistant turns) |
| 3 | claude-code | a344b377 | dot-agents | truncated | 1 | int | N | metadata-only stub (0 assistant turns) |
| 4 | claude-code | 403af2d2 | dot-agents | complete | 15 | int | I | items: cc-empty-metadata-stub-sessions |
| 5 | claude-code | a5d6082b | dot-agents | complete | 22 | int | N | metadata-only stub (0 assistant turns) |
| 6 | claude-code | 00b3e815 | dot-agents | complete | 4380 | S | N | 1487 turns, 25 PRs, 43 Agent; pattern covered by anchored siblings |
| 7 | claude-code | 151d7271 | dot-agents | complete | 25106 | S | I | items: cc-single-session-multi-pr,cc-wave-parallel-delegation-agent |
| 8 | claude-code | 1699c371 | dot-agents | cutoff | 13179 | int | I | items: cc-async-overnight-primitives,cc-longlived-multiday-sessions,cc-mcp-quality-gates-inloop,… |
| 9 | claude-code | 327154c6 | dot-agents | complete | 26099 | S | I | items: cc-single-session-multi-pr,cc-synthetic-turns-compaction,cc-wave-parallel-delegation-agent |
| 10 | claude-code | 37db65d6 | dot-agents | complete | 189 | int | N | 50 turns, 0 PRs, 0 Agent; pattern covered by anchored siblings |
| 11 | claude-code | 438f47a4 | dot-agents | complete | 3321 | int | I | items: cc-workflow-tool-runs-scripts |
| 12 | claude-code | 459ba08a | dot-agents | complete | 663 | S | N | 209 turns, 0 PRs, 6 Agent; pattern covered by anchored siblings |
| 13 | claude-code | 50a19fee | dot-agents | complete | 2184 | int | I | items: cc-single-session-multi-pr,cc-workflow-cli-drives-taskstate |
| 14 | claude-code | 598c5d1e | dot-agents | cutoff | 43 | int | N | 20 turns, 0 PRs, 0 Agent; pattern covered by anchored siblings |
| 15 | claude-code | 67f2f621 | dot-agents | complete | 4978 | S | N | 1599 turns, 73 PRs, 115 Agent; pattern covered by anchored siblings |
| 16 | claude-code | 6d747126 | dot-agents | complete | 4402 | S | N | 1562 turns, 16 PRs, 7 Agent; pattern covered by anchored siblings |
| 17 | claude-code | 73309601 | dot-agents | complete | 312 | int | N | 108 turns, 0 PRs, 0 Agent; pattern covered by anchored siblings |
| 18 | claude-code | 999f872a | dot-agents | complete | 28 | int | I | items: cc-empty-metadata-stub-sessions |
| 19 | claude-code | a344b377 | dot-agents | complete | 43 | int | N | 19 turns, 0 PRs, 0 Agent; pattern covered by anchored siblings |
| 20 | claude-code | b1d48aff | dot-agents | complete | 61 | int | N | 28 turns, 0 PRs, 0 Agent; pattern covered by anchored siblings |
| 21 | claude-code | 2c9ee562 | payout | cutoff | 1179 | int | I | items: cc-cutoff-ends-midturn |
| 22 | claude-code | 67c68b8f | payout | complete | 1577 | S | N | payout loop: 694 turns, 11 PRs, 5 Agent calls; converges w/ anchored dot-agents items |
| 23 | claude-code | 7b866571 | payout | complete | 2177 | int | N | payout loop: 850 turns, 9 PRs, 13 Agent calls; converges w/ anchored dot-agents items |
| 24 | claude-code | 8a57a6f1 | payout | complete | 2808 | S | N | payout loop: 965 turns, 3 PRs, 42 Agent calls; converges w/ anchored dot-agents items |
| 25 | claude-code | 92697a8f | payout | complete | 5877 | S | N | payout loop: 2140 turns, 2 PRs, 17 Agent calls; converges w/ anchored dot-agents items |
| 26 | claude-code | acc71d42 | payout | complete | 1715 | int | N | payout loop: 618 turns, 5 PRs, 9 Agent calls; converges w/ anchored dot-agents items |
| 27 | claude-code | c0d52153 | payout | complete | 7319 | S | N | payout loop: 2002 turns, 45 PRs, 95 Agent calls; converges w/ anchored dot-agents items |
| 28 | claude-code | d6e1a088 | payout | complete | 2202 | S | N | payout loop: 726 turns, 0 PRs, 11 Agent calls; converges w/ anchored dot-agents items |
| 29 | claude-code | 1dc81dd7 | ~ | complete | 7 | int | N | metadata-only stub (0 assistant turns) |
| 30 | claude-code | 8bc0a9a6 | ~ | complete | 7 | int | N | metadata-only stub (0 assistant turns) |
| 31 | claude-code | 00a944fd | D12-exp | cutoff | 11 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 32 | claude-code | 00d8b88c | D12-exp | cutoff | 16 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 33 | claude-code | 096bddb0 | D12-exp | complete | 17 | int | I | items: cc-d12-hopchain-experiment-batch |
| 34 | claude-code | 0c0c9deb | D12-exp | complete | 17 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 35 | claude-code | 240ebefe | D12-exp | complete | 8 | int | N | metadata-only stub (0 assistant turns) |
| 36 | claude-code | 30ff98e7 | D12-exp | complete | 34 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 37 | claude-code | 6204d88e | D12-exp | complete | 24 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 38 | claude-code | 734c099b | D12-exp | cutoff | 11 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 39 | claude-code | 83b0b248 | D12-exp | complete | 12 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 40 | claude-code | 9dfb972e | D12-exp | cutoff | 16 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 41 | claude-code | b9ee5365 | D12-exp | complete | 17 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 42 | claude-code | bdc95f81 | D12-exp | cutoff | 16 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 43 | claude-code | c9d92522 | D12-exp | cutoff | 28 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 44 | claude-code | ce455c31 | D12-exp | cutoff | 18 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 45 | claude-code | d1af2072 | D12-exp | cutoff | 16 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 46 | claude-code | d27dd3ab | D12-exp | complete | 8 | int | N | metadata-only stub (0 assistant turns) |
| 47 | claude-code | ed0722b4 | D12-exp | complete | 17 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 48 | claude-code | f69e3fb6 | D12-exp | complete | 17 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 49 | claude-code | fbfbb5b9 | D12-exp | complete | 17 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 50 | claude-code | fc543bfc | D12-exp | complete | 17 | int | N | D12 hop-chain trial (see cc-d12-hopchain item) |
| 51 | copilot | 23e45a55 | dot-agents | complete | 8 | int | I | items: copilot-tooldef-fixed-overhead,copilot-trivial-smoke-outcome |
| 52 | omp | 019f3cbc | dot-agents | complete | 13 | int | I | items: omp-provider-model-switch-midsession,omp-session-exit-distribution,omp-usagelimit-zero-output-failure |
| 53 | omp | 019f3cf2 | dot-agents | complete | 1876 | S | I | items: omp-cost-cursor-routed-zero-dollar,omp-cost-totals-019f3cf2,omp-provider-model-switch-midsession,… |
| 54 | omp | 019f3f23 | payout | complete | 5693 | S | I | items: cost-cacheread-dominates-context,omp-cost-totals-019f3f23,omp-session-exit-distribution,… |
| 55 | omp | 019f4eda | payout | cutoff | 1126 | S | I | items: omp-cost-totals-019f4eda |
| 56 | omp | 019f4eea | dot-agents | cutoff | 3122 | S | I | items: omp-cost-totals-019f4eea,omp-provider-model-switch-midsession |

## D12 hop-chain experiment batch (aggregate)
20 sessions live under one scratchpad path (`.../depth-exp-v2/trials_v2/D12/t1`): 18 are hop-chain trials, 2 are metadata-only stubs. Each trial = one 'HOP N of 12 fresh agents' doing a single Read(own level file)+Write to build one shared artifact (context-propagation / telephone-chain test). Not workflow loops. Per-trial: 2-5 assistant turns, 1-2 tool calls, 0.2-3.8k output tokens, sub-minute spans. Statuses (all 20): cutoff=8, complete=12. Representative anchor: item `cc-d12-hopchain-experiment-batch` (096bddb0).

## Gaps / unknowns
- **OMP per-tool dollar cost is not recoverable**: OMP records cost per *assistant turn*, not per tool call; per-tool cost shape can only be approximated via per-turn `duration` (see duration-shape items). Reported as [INFERENCE].
- **cursor-routed $0 cost** (item `omp-cost-cursor-routed-zero-dollar`): whether $0 means truly-free routing vs unreported billing is unresolved from the transcript; flagged [INFERENCE].
- **Subagent transcripts not traversed**: claude-code delegation writes child turns to separate `subagent_files` (inventory: up to 235 for 151d7271); only primary files were in-scope, so sub-agent internal behavior is uncounted.
- **Metadata-only stubs** (999f872a, 403af2d2, a5d6082b, 240ebefe, d27dd3ab, 1dc81dd7, 8bc0a9a6, 598c5d1e, b41d7f0d): 0 assistant turns in-file; their model turns may live in forked/resumed siblings not in the manifest.
- **SIGHUP tmux-restart** linkage is timestamp-proximity inference; not a labelled event.

## Summary
- **Items: 26** — by class: cost=10, craft=4, failure=3, mechanism=7, outcome=2.
- By sensitivity: internal=19, sensitive=7 (sensitive = aggregate-count rows only).
- Sessions processed: 56/56 (all parsed). Anchored in ≥1 item: 15; remaining covered by coverage-table notes lines + D12/OMP aggregate tables.
- **Notable outliers**: OMP 019f3f23 (payout) = 707.8M tokens / $491.84 / 1457 turns / ~72h span (largest); CC 327154c6 = 162 distinct PRs, 26,099 records, ~372h span (15+ days); CC 151d7271 = 235 Agent delegations; copilot 2+2 = 67% of context consumed by tool definitions on a trivial query; two OMP mega-sessions co-terminated by a single SIGHUP.
- **Cross-harness convergence**: cache-read dominates token volume in BOTH omp (96-98%) and claude-code (94-99%); claude-code records no dollar cost while omp/copilot do.
