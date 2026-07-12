# Codex corpus — evidence extraction notes

Task `evidence-extraction` (harness=codex). **134 assigned sessions, all processed-or-noted.** Authoritative selection = sampling-manifest.json (codex: rule3 strata + 13 rule4 negative controls). Governing rubric: evidence-rubric.md §2 (E1-E5) + §2b (R1-R5).

## Reading strategy (auditable)

134 rollout files cannot be hand-read exhaustively, so extraction ran in two tiers:

1. **Full programmatic parse of every jsonl record** in all 134 sessions (scripts: bulk metric pass + anchor-index pass). For each session this decodes every `session_meta / turn_context / event_msg / response_item` record and derives: models, reasoning_effort, collaboration_mode, approval_policy, sandbox network flag, originator; full `function_call` tool histogram; `apply_patch` count; token totals (`token_count.info.total_token_usage`); rate-limit trajectory (`rate_limits.primary/secondary used_percent`, plan_type); exec exit codes; test/build/lint & git & workflow-artifact command detection; `task_complete` presence + last_agent_message; cutoff/last-record type. This is a *census*, not a sample — every quantitative claim below is reproducible from the raw files.
2. **Targeted hand-anchoring for excerpts.** For each committed item the exact anchor line is re-read raw, sha256-digested (R4), and reduced to a minimal redacted excerpt (R2/R3). Anchor lines chosen per class: first `turn_context` (model/effort/approval/mode/network); first `spawn_agent`+`wait_agent` (orchestration); first `update_plan`; first workflow-artifact read; max-`used_percent` `token_count` (rate-limit wall); last `token_count.info` (cost); `task_complete` + first `apply_patch` (outcome); first test/build command (verification); auto-review verdict message (craft); last record (cutoff).

Tool-heavy spans are covered because the histogram sees *all* tool calls; first/last turns are covered by the session_meta + task_complete anchors. Redaction: home dirs -> `~`, machine temp dirs -> `<tmp>`, ephemeral codex turn/agent/session UUIDs -> `<id>`, secret-pattern scan on every excerpt before write (0 hits). No raw jsonl copied into the repo (R1).

## Coverage

- Sessions total: **134**  |  with >=1 evidence item: **72**  |  noted-only: **62**
- Items emitted: **143**  ({'mechanism': 45, 'failure': 21, 'cost': 42, 'craft': 23, 'outcome': 12})
- Confidence: {'high': 21, 'medium': 122}  (high = >=2 anchors incl. primary transcript; medium = 1 primary anchor)
- Sensitivity: all items `internal`; the 4 `sensitive` sessions contribute **aggregate stats only, no excerpts** (R5).

Noted-only sessions (no standalone item — reason):
- **sensitive (aggregate-only): 4**
- **edited code, not selected for an item (captured in token table): 6**
- **read-only / investigation / planning (no edits; token total in table): 52**

Every token-bearing noted-only session still appears as an anchored row in the Token table below, so its cost/rate-limit axes are captured even without a narrative item.

## Mechanism — how codex structures work vs the OMP/CC `da workflow` loop

- **Zero `da workflow` CLI invocations across all 134 sessions.** A byte-level scan for a real `da <subcmd>` command found 0 (the only textual match is a `ps aux | rg 'da workflow'` process grep). Codex **reads** dot-agents plan artifacts (`.agents/workflow/plans/*`, `TASKS.yaml`, `active.loop`, delegation bundles) as context but never drives `advance`/`merge-back`/`plan` itself.
- Instead codex uses **native primitives**: `update_plan` internal TODO (20 sessions), and the `multi_agent_v1` fan-out `spawn_agent`/`wait_agent`/`close_agent` (14 sessions, 129 total spawns) — including spawning `loop-worker`/`worker` agent_types against dot-agents delegation bundles. Edits go through the `apply_patch` custom tool; shell through `exec_command`.
- Config surface: reasoning_effort mostly `high` (105 high / 14 medium / 8 low); approval `on-request` (110) vs full-auto `never` (23); sandbox network access disabled wherever recorded (45 sessions False, rest unset). Collaboration mode almost always `default`; only 2 sessions used built-in `plan` mode.
- Entry points (originator): `codex_exec` headless 76, `codex-tui` 31, `codex_cli_rs` 12, **`Claude Code` 12** (cross-harness bridge: CC delegating to codex), `Codex Desktop` 3.
- Models: gpt-5.5=86, gpt-5.4=36, gpt-5.4-mini=15, codex-auto-review=8, gpt-5.3-codex=5.

## Failure — rate-limit walls, cutoffs, token gaps

- **Rate-limit walls:** 14 sessions drove a rate-limit window to >=90% used (primary >=90%: 10; secondary/weekly >=90%: 6); a further 17 hit primary 75-89% and 11 hit secondary 75-89%. Plans seen: plus=86, team=31. Items `cx-fail-ratelimit-*` anchor the peak-`used_percent` `token_count` line per wall session. Note: hitting 100% did not always hard-abort — several 100%-primary sessions still reached `task_complete` (the reading is the window state at that turn).
- **Cutoffs:** 7 sessions ended cutoff. All non-trivial ones terminate on `event_msg/turn_aborted` with `reason=interrupted` (user abort), **not** model crash or rate-limit kill; one (rc=2) is an empty `task_started` with no turn. Items `cx-fail-cutoff-*`.
- **Token-gap rows:** 14 sessions record rate-limit % but no token totals (`has_tokens=false`); these are short sessions (rc 2-16). Flagged as `cx-cost-tokengap-*` (class cost, inference_flag: no token axis). This mirrors the corpus-wide pattern where older/short codex rollouts carry rate-limit percentages but not `token_count.info` totals.
- **Abandoned subagent threads: none observed.** All 9 `subagent-thread` sessions in the sample reached `task_complete` (9/9); they are short bounded delegated threads (rc 9-58).

## Cost — token totals for cross-harness comparison

- Token-bearing sessions: **120/134 (89.6%)**. total_tokens: min 15,580, median 692,688, mean 3,524,651, max 62,156,886, sum 422,958,191.
- **Cache dominance (key normalization caveat):** median `cached_input/input` = 89%. The *productive* spend (output+reasoning) has median only **12,070** tokens vs a 692K median total — i.e. >98% of codex token volume is re-sent cached context. Cross-harness cost comparisons should use output+reasoning (and non-cached input), not raw total_tokens, or codex will look ~50x more expensive than it is. Items `cx-cost-permodel-*` record the productive figure per model.
- Noteworthy cost items: `cx-cost-top-*` (12 largest), `cx-cost-permodel-*` (one per model), `cx-cost-negctrl-*` (all token-bearing negative controls). The full per-session table below is the complete anchored comparison dataset.

### Token table (all token-bearing sessions; anchored to last `token_count.info` line)

Columns: total | input | cached% | out+reas (productive) | pri%max | sec%max | model | slug | ref#L | digest. `*` = sensitive session (aggregate only).

| total | input | cached% | prod | pri% | sec% | model | slug | ref | digest |
|--:|--:|--:|--:|--:|--:|---|---|---|---|
| 62,156,886 | 61,960,105 | 95% | 277,199 | 100.0 | 99.0 | gpt-5.4,gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-07-06T15-34-57-019f38ed-3640-7ac3-948b-73b7b88aafb0#L3526 | sha256:516d89cf6eec… |
| 36,211,825* | 36,078,639 | 92% | 166,457 | 19.0 | 18.0 | gpt-5.4 | ~/Documents/payout | rollout-2026-03-19T12-10-11-019d06dc-9040-74f3-ad3b-9659b753d96d#L2366 | sha256:66c835a6aee7… |
| 33,718,670 | 33,662,199 | 98% | 66,013 | 39.0 | 20.0 | gpt-5.5 | ~/Documents/dot-agents | rollout-2026-05-04T00-50-52-019df153-282f-7722-946f-1f5cebfd97b4#L1869 | sha256:be4c62d33a79… |
| 31,491,218 | 31,293,999 | 94% | 292,878 | 55.0 | 74.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-03-30T20-39-51-019d4155-20b9-7fb2-b3c4-6bc65474dd31#L1854 | sha256:4e54500efe17… |
| 25,388,321 | 25,293,082 | 88% | 128,756 | 38.0 | 16.0 | gpt-5.4 | ~/Documents/payout | rollout-2026-04-06T21-59-07-019d65aa-38a5-7371-8e92-428ee2b8d33f#L1744 | sha256:cc0c238cea93… |
| 23,397,798 | 23,287,197 | 97% | 140,099 | 14.0 | 5.0 | gpt-5.3-codex | ~/Documents/payout | rollout-2026-02-28T02-11-02-019ca316-225d-7ee3-ac1f-3c54c62eb9de#L2099 | sha256:01eb0bcd299a… |
| 15,170,208 | 15,119,222 | 91% | 70,288 | 34.0 | 15.0 | gpt-5.4 | ~/Documents/payout | rollout-2026-04-06T22-37-53-019d65cd-b6cb-7af0-8f93-d28f25cd2457#L802 | sha256:fbbc314987ab… |
| 13,299,581 | 13,201,234 | 92% | 115,926 | 58.0 | 9.0 | gpt-5.5 | ~/Documents/dot-agents | rollout-2026-05-25T14-16-20-019e605a-24a1-7863-adc2-86e948545f5b#L326 | sha256:38380aeb35e6… |
| 10,767,623* | 10,717,862 | 94% | 73,495 | 32.0 | 6.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-05-03T19-07-47-019df019-0e2e-77e1-9d72-1b8df97d979f#L765 | sha256:9a6b671e8e73… |
| 10,766,070 | 10,696,934 | 96% | 110,937 | 82.0 | 89.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-12T12-01-20-019d826d-164f-7cf2-b102-f65830ef293f#L431 | sha256:3ac9ec577d29… |
| 10,467,560 | 10,423,196 | 98% | 58,473 | 56.0 | 32.0 | gpt-5.5 | ~/proj-docs/dot-agents/.claude/worktrees/agent-acde5c2ab70d82f2a | rollout-2026-06-25T17-42-27-019f00bb-fc2c-7d10-af15-0585929e3ed2#L632 | sha256:2f98522f56af… |
| 9,920,244 | 9,867,301 | 96% | 58,820 | 43.0 | 36.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-05-04T21-28-11-019df5bf-f48f-7950-b782-66e91a211966#L603 | sha256:e363392f92f3… |
| 6,854,983 | 6,815,865 | 84% | 47,768 | 14.0 | 4.0 | gpt-5.4 | ~/Documents/payout | rollout-2026-03-19T13-57-48-019d073f-177b-7631-b3fa-9d764674958e#L484 | sha256:c44645052abe… |
| 6,724,395 | 6,700,527 | 92% | 33,953 | 89.0 | 85.0 | gpt-5.3-codex,gpt-5.4 | ~/Documents/dot-agents | rollout-2026-03-30T20-58-24-019d4166-1bea-7ca1-87eb-ad2598228d91#L561 | sha256:237768162da7… |
| 6,618,070 | 6,582,327 | 93% | 47,049 | 42.0 | 38.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-11T09-50-13-019d7cce-b173-7de2-8871-47a8dce5af22#L561 | sha256:de7798c5a398… |
| 6,455,887 | 6,427,511 | 97% | 45,701 | 100.0 | 46.0 | gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-19T00-46-45-019da410-0075-7c12-845f-859f9544e7b0#L362 | sha256:206605a33e7b… |
| 5,970,645 | 5,932,230 | 98% | 60,766 | 36.0 | 11.0 | gpt-5.4 | ~/Documents/payout | rollout-2026-06-14T13-09-56-019ec71c-8745-7ef0-88e7-b9ad0bf36baf#L457 | sha256:457c195124d8… |
| 4,975,835 | 4,945,192 | 93% | 40,127 | 6.0 | 12.0 | gpt-5.3-codex | ~/Documents/payout/swarm-cd | rollout-2026-03-02T15-32-05-019cb040-3ca4-7021-ab77-ab0f215e0f89#L556 | sha256:730c1d8486d0… |
| 4,963,428 | 4,948,590 | 95% | 18,435 | 1.0 | 2.0 | gpt-5.5 | ~/Documents/payout | rollout-2026-04-24T00-40-45-019dbdca-5037-7ad1-b65c-db927f74ede6#L479 | sha256:6fa86c94b4a9… |
| 4,731,089 | 4,713,709 | 96% | 20,248 | 1.0 | 6.0 | gpt-5.4,gpt-5.5 | ~/Documents/dot-agents | rollout-2026-04-27T08-13-03-019dcedb-78bd-7ff1-9571-3dc5726e2938#L319 | sha256:3169e1881c33… |
| 4,320,616 | 4,295,146 | 95% | 30,800 | 15.0 | 94.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-12T18-21-44-019d83c9-5962-7f00-91d2-cc8c18ca4df5#L229 | sha256:3324ca04e08a… |
| 4,245,855 | 4,216,592 | 93% | 39,450 | 82.0 | 89.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-12T12-01-13-019d826c-fca9-78a2-8d62-6bbd92d26274#L234 | sha256:fa6a4e0f499b… |
| 4,222,562 | 4,192,038 | 93% | 41,774 | 82.0 | 89.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-12T12-01-13-019d826c-fccc-7f01-9012-cf560494a5f5#L231 | sha256:d6bfe84c0ed5… |
| 4,187,193 | 4,167,684 | 90% | 22,131 | 39.0 | 38.0 | gpt-5.5 | scratchpad/gate-224 | rollout-2026-06-29T18-24-45-019f157c-24ee-7f72-b0c3-5a63cb0615c3#L368 | sha256:9356744aa2fc… |
| 3,837,084 | 3,806,336 | 92% | 43,107 | 24.0 | 80.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-12T11-29-36-019d8250-070e-72a0-94a5-960f7bac17a7#L277 | sha256:3f0051260e62… |
| 3,595,144 | 3,543,620 | 95% | 85,109 | 4.0 | 77.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-12T11-11-13-019d823f-35b4-7561-b213-c68a0dc8b88e#L295 | sha256:8bfc1872afca… |
| 3,404,860 | 3,375,077 | 80% | 38,465 | 74.0 | 85.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-03-30T19-06-38-019d40ff-cb41-7bd1-a57a-9e5db6ece347#L398 | sha256:f3071016895d… |
| 2,932,276 | 2,894,337 | 93% | 65,071 | 81.0 | 82.0 | gpt-5.3-codex,gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-03-30T21-03-46-019d416b-05fa-7092-a0df-9f7ee15ec1a0#L265 | sha256:a303dc69b8d8… |
| 2,888,652 | 2,857,086 | 94% | 47,267 | 78.0 | 75.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-12T01-06-56-019d8015-f740-7360-a60c-e7d42c19ac82#L464 | sha256:86da3960a340… |
| 2,883,759 | 2,858,084 | 97% | 36,289 | 62.0 | 86.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-19T23-21-05-019da8e7-efcd-7993-9b0a-c0caf0f909db#L231 | sha256:c73a36bf1694… |
| 2,870,066 | 2,858,440 | 95% | 17,017 | 23.0 | 14.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-06-27T16-34-16-019f0aca-4753-7521-b44f-2d3b7b92ae2e#L206 | sha256:673233a1dd65… |
| 1,991,981 | 1,964,198 | 90% | 34,571 | 23.0 | 96.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-12T18-35-21-019d83d5-d2b7-75b2-861c-215717258746#L381 | sha256:294520a8591b… |
| 1,898,200 | 1,889,879 | 94% | 10,545 | 89.0 | 69.0 | gpt-5.5 | scratchpad/rg-267 | rollout-2026-07-02T05-50-24-019f223c-99bd-77b1-b618-6e8e068a3f02#L181 | sha256:e8018854bd23… |
| 1,770,804 | 1,763,060 | 92% | 9,338 | 43.0 | 7.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-06-26T22-35-58-019f06ef-126d-7ab2-970d-8a8d9a3f5e6f#L175 | sha256:56ecbfdaa7cc… |
| 1,766,002 | 1,756,547 | 93% | 13,819 | 39.0 | 6.0 | gpt-5.5 | <tmp> | rollout-2026-06-28T23-17-59-019f1162-3f72-7ba2-88bd-be3df5b99984#L136 | sha256:ac22062d31f0… |
| 1,711,735 | 1,700,525 | 90% | 13,570 | 88.0 | 14.0 | gpt-5.5 | scratchpad/gate-227 | rollout-2026-06-30T00-02-20-019f16b1-3861-7821-963c-610257afa6e6#L197 | sha256:a479099ced57… |
| 1,584,445* | 1,574,570 | 90% | 13,938 | 20.0 | 22.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-06-28T13-00-37-019f0f2d-0792-7173-8a93-1957572474bf#L160 | sha256:c27d34514208… |
| 1,539,628 | 1,531,487 | 86% | 10,141 | 23.0 | 33.0 | gpt-5.5 | scratchpad/rg4- | rollout-2026-07-01T08-22-47-019f1da1-bb0d-7ea3-bf2d-c1438ab8960b#L187 | sha256:f3c6a897aa05… |
| 1,523,143 | 1,505,183 | 91% | 30,006 | 22.0 | 16.0 | gpt-5.4 | ~/Documents/payout | rollout-2026-06-14T22-10-55-019ec90b-d1a3-7e81-972d-a6fb520312f0#L122 | sha256:9db9a1f5ecfe… |
| 1,435,315 | 1,425,308 | 89% | 13,083 | 92.0 | 96.0 | gpt-5.5 | scratchpad/rg-277d | rollout-2026-07-02T20-11-10-019f2550-a595-76a3-81d9-7b038a05b7ba#L166 | sha256:c2a746b77774… |
| 1,282,310 | 1,271,273 | 87% | 14,391 | 29.0 | 23.0 | gpt-5.5 | ~/proj-docs/dot-agents/.claude/worktrees/agent-afd388b810fd00b91 | rollout-2026-06-28T13-14-20-019f0f39-9635-7b32-98a3-2208b05d6d0a#L183 | sha256:4b9625b60408… |
| 1,280,482 | 1,275,740 | 89% | 5,778 | 38.0 | 25.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-06-28T13-20-00-019f0f3e-c7d4-7e12-80e6-8271a871c697#L156 | sha256:b2b53a09ae61… |
| 1,241,739 | 1,233,597 | 89% | 11,626 | 36.0 | 22.0 | gpt-5.5 | <tmp> | rollout-2026-06-29T09-17-18-019f1386-f0ee-7210-b34d-86355991eaf9#L165 | sha256:004fdc0c9d16… |
| 1,202,213 | 1,192,338 | 85% | 14,020 | 12.0 | 28.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-03-29T19-06-55-019d3bd9-aeaf-7780-a348-8281a1763a4f#L214 | sha256:39e9a70dc3c0… |
| 1,193,703 | 1,182,688 | 89% | 16,975 | 70.0 | 11.0 | gpt-5.5 | <tmp> | rollout-2026-06-28T23-46-46-019f117c-9902-72d2-bcce-b95ee05419bb#L166 | sha256:0bda51e04e12… |
| 1,193,475 | 1,184,864 | 90% | 12,765 | 9.0 | 12.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-06-27T16-16-28-019f0ab9-fb01-7852-b758-4bcf0001e1b9#L135 | sha256:1a9d184d5cd2… |
| 1,174,680 | 1,164,381 | 90% | 13,573 | 97.0 | 39.0 | gpt-5.5 | ~/proj-docs/dot-agents/.claude/worktrees/agent-aecfb22a34446e073 | rollout-2026-06-25T21-12-32-019f017c-51ea-71b2-bc4f-fa4cd21a0178#L167 | sha256:2f6c436acaf2… |
| 1,157,361 | 1,146,102 | 87% | 15,262 | 15.0 | 2.0 | gpt-5.5 | scratchpad/flush-279 | rollout-2026-07-03T11-29-47-019f2899-abd7-76d1-9510-22690605e66d#L173 | sha256:8c895a550430… |
| 1,107,861 | 1,079,708 | 90% | 46,942 | 68.0 | 74.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-12T01-10-54-019d8019-97ca-7fc2-95bf-a7ffa930ca3f#L143 | sha256:793a679a88dd… |
| 1,074,209 | 1,064,552 | 85% | 13,479 | 96.0 | 15.0 | gpt-5.5 | scratchpad/regate3-277 | rollout-2026-07-03T15-09-14-019f2962-9694-7392-98f2-81bcbf7240a7#L146 | sha256:4527e5b5ab14… |
| 1,045,321 | 1,035,848 | 87% | 12,608 | 32.0 | 20.0 | gpt-5.5 | scratchpad/rg2-292 | rollout-2026-07-04T00-52-13-019f2b78-50ee-7ac2-af42-86b9a08fce24#L149 | sha256:a755f0533618… |
| 1,044,135 | 1,036,385 | 85% | 11,539 | 53.0 | 24.0 | gpt-5.5 | <tmp> | rollout-2026-06-29T09-51-28-019f13a6-3925-7fc3-a6da-4096788dfdba#L119 | sha256:53df48de435d… |
| 967,850 | 957,487 | 86% | 13,863 | 55.0 | 9.0 | gpt-5.5 | scratchpad/regate2-287 | rollout-2026-07-03T12-53-05-019f28e5-e7e2-7813-877c-c19ad2e32ebf#L154 | sha256:1eec8db1a490… |
| 957,781 | 950,049 | 74% | 11,896 | 36.0 | 80.0 | gpt-5.5 | scratchpad/rgw4- | rollout-2026-07-02T12-59-14-019f23c5-2e3d-7cb1-a27a-fe2a39d5535b#L158 | sha256:e836f5ffe3aa… |
| 866,018 | 855,940 | 84% | 13,680 | 36.0 | 47.0 | gpt-5.5 | ~/proj-docs/dot-agents/.claude/worktrees/agent-a05c3ffe142bf2f1b | rollout-2026-06-26T09-38-34-019f0427-53e7-7d91-9af7-0105007165cd#L137 | sha256:a77e99ea6910… |
| 799,146 | 791,268 | 86% | 10,525 | 13.0 | 70.0 | gpt-5.5 | scratchpad/rg-269 | rollout-2026-07-02T06-13-28-019f2251-b579-7de3-915a-12f7e370243f#L127 | sha256:7c08f2ef510e… |
| 781,903 | 774,379 | 88% | 9,547 | 94.0 | 15.0 | gpt-5.5 | scratchpad/gate-227 | rollout-2026-06-30T00-11-48-019f16b9-e0a4-7e41-bfb9-feb0acd5c2a6#L129 | sha256:8f1c60a9e2c5… |
| 769,037 | 761,461 | 83% | 9,703 | 49.0 | 37.0 | gpt-5.5 | scratchpad/rg-251b | rollout-2026-07-01T09-18-56-019f1dd5-282b-75b2-9fbf-8cce44930c46#L133 | sha256:97c074d22963… |
| 760,789 | 751,200 | 94% | 11,568 | 54.0 | 9.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-05-03T21-30-40-019df09b-dfc8-7840-8035-5c1c93e9d27a#L140 | sha256:6d1fb5753ab5… |
| 707,650 | 698,696 | 76% | 12,455 | 10.0 | 7.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-17T14-28-35-019d9cb3-b0dd-7f02-a202-9afba6d3b54c#L171 | sha256:40c49d01931f… |
| 677,727 | 668,896 | 91% | 11,445 | 20.0 | 64.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-03-30T18-38-48-019d40e6-50a6-74c0-8310-630737ab8f4e#L194 | sha256:4f2c7d6a0686… |
| 609,048 | 599,228 | 69% | 13,419 | 50.0 | 30.0 | gpt-5.5 | scratchpad/bg-gate-py | rollout-2026-07-04T08-42-20-019f2d26-baed-7761-9b20-242185925096#L155 | sha256:0af44bad340b… |
| 607,596 | 592,848 | 90% | 24,044 | 17.0 | 63.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-03-30T18-37-52-019d40e5-74cc-75b0-85a1-bb0d104e8823#L157 | sha256:791ecd3727ad… |
| 597,132 | 588,521 | 91% | 11,667 | 19.0 | 64.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-03-30T18-38-30-019d40e6-09c6-7821-b414-8138db97cd33#L180 | sha256:008637595193… |
| 567,507 | 562,395 | 92% | 6,586 | 26.0 | 96.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-20T08-18-27-019daad3-e7c6-7662-bc15-7a711b059c54#L118 | sha256:e63653d5b942… |
| 558,669 | 552,339 | 87% | 8,312 | 76.0 | 28.0 | gpt-5.5 | scratchpad/gate-p8 | rollout-2026-06-29T10-16-36-019f13bd-3c2e-7d40-adf4-11042c90bf6a#L103 | sha256:e344df548f6f… |
| 556,158* | 545,864 | 88% | 14,380 | 2.0 | 11.0 | gpt-5.3-codex | ~/Documents/payout/po-vault-lib | rollout-2026-03-02T13-11-32-019cafbf-8f95-7673-9512-4141fbf61d08#L292 | sha256:135945e02e9c… |
| 554,714 | 549,308 | 72% | 7,092 | 7.0 | 1.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-06-28T22-11-54-019f1125-c1cd-74f3-9f2b-0a1683c6d80c#L90 | sha256:67f6666f51e7… |
| 533,006 | 527,522 | 83% | 6,406 | 32.0 | 21.0 | gpt-5.5 | scratchpad/gate-233 | rollout-2026-06-30T08-37-37-019f1888-f83b-7be0-924a-e4b741951d30#L111 | sha256:de3becb17ae4… |
| 528,915 | 521,148 | 91% | 11,427 | 26.0 | 50.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-19T09-15-16-019da5e1-8fa1-7ad0-95d0-7ea484853490#L96 | sha256:cd25de10181e… |
| 520,552 | 508,220 | 77% | 18,916 | 62.0 | 43.0 | gpt-5.5 | scratchpad/bg-gate-328 | rollout-2026-07-04T14-25-03-019f2e60-7eb3-7771-a57a-e29212ffdb55#L120 | sha256:9184ce5fd298… |
| 518,539 | 506,946 | 93% | 19,643 | 17.0 | 63.0 | gpt-5.4,gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-03-30T18-37-52-019d40e5-7438-7b81-aa31-2b82c654832d#L138 | sha256:4969e80ec5ed… |
| 513,401 | 505,646 | 81% | 10,049 | 100.0 | 16.0 | gpt-5.5 | scratchpad/gate-232 | rollout-2026-06-30T00-52-08-019f16de-cf92-72e1-8457-41ede9bccd54#L122 | sha256:9e1f7b961173… |
| 508,616 | 500,421 | 81% | 12,257 | 77.0 | 36.0 | gpt-5.5 | ~/proj-docs/dot-agents/.claude/worktrees/agent-aa68802890471e6ef | rollout-2026-06-25T18-19-35-019f00dd-fb6e-75b2-9563-c4f5d61b6183#L103 | sha256:b341d75a9cec… |
| 505,128 | 502,138 | 93% | 3,676 | 75.0 | 73.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-19T17-49-35-019da7b8-702b-72f2-bde7-3c38011c87ae#L99 | sha256:0b32bf501fa5… |
| 487,084 | 477,734 | 78% | 13,127 | 62.0 | 32.0 | gpt-5.5 | scratchpad/bg-gate-306 | rollout-2026-07-04T09-37-27-019f2d59-2e7d-7e53-a5fa-9ce96c106822#L112 | sha256:b4a3875e22da… |
| 479,837 | 471,997 | 73% | 10,246 | 95.0 | 48.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-07-04T15-15-40-019f2e8e-d6e2-7661-9309-21d2d34c21bc#L103 | sha256:49f02bb21430… |
| 468,975 | 460,110 | 78% | 14,940 | 65.0 | 25.0 | gpt-5.4 | ~/Documents/dot-agents | rollout-2026-04-18T16-20-08-019da240-2ef5-74e1-a90e-95a828969ffd#L64 | sha256:6e424a57ec43… |
| 436,436 | 429,567 | 85% | 10,238 | 88.0 | 30.0 | gpt-5.5 | scratchpad/gate-p9 | rollout-2026-06-29T11-08-46-019f13ec-fdc1-7eb2-8dac-463f751a0b0e#L80 | sha256:6d485a8beb1d… |
| 416,692 | 411,824 | 84% | 6,148 | 97.0 | 31.0 | gpt-5.5 | scratchpad/fix-fsops | rollout-2026-06-29T11-45-55-019f140f-0295-7c03-97f4-c07364fb35d8#L85 | sha256:7e4694c461fc… |
| 399,274 | 390,995 | 83% | 11,460 | 4.0 | 34.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-07-04T12-55-25-019f2e0e-6c56-74a1-8b87-346394b5a247#L95 | sha256:f404a6eb8187… |
| 369,573 | 363,238 | 73% | 8,578 | 49.0 | 41.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-07-04T14-14-06-019f2e56-75fb-7f41-b7b3-e7549dbcdb9b#L100 | sha256:453c14ed64a0… |
| 366,257 | 359,483 | 75% | 9,978 | 21.0 | 3.0 | gpt-5.5 | scratchpad/regate-285 | rollout-2026-07-03T11-50-08-019f28ac-4b66-7702-ac6b-7ccb8a637201#L97 | sha256:0a9eb976e44f… |
| 361,595 | 358,785 | 95% | 3,610 | 23.0 | 54.0 | gpt-5.5 | scratchpad/depth-exp-v2 | rollout-2026-06-26T14-50-50-019f0545-3a56-7492-a70d-85b4773dba36#L66 | sha256:551b3b1e8294… |
| 340,672 | 332,149 | 80% | 12,382 | 50.0 | 41.0 | gpt-5.5 | scratchpad/bg-gate-326 | rollout-2026-07-04T14-14-21-019f2e56-b3aa-7451-95aa-dbf214a493ac#L95 | sha256:d143ef785f23… |
| 338,915 | 329,679 | 81% | 13,271 | 33.0 | 38.0 | gpt-5.5 | scratchpad/bg-gate-319 | rollout-2026-07-04T13-46-26-019f2e3d-21ce-7e00-961e-1c06efe4ab9b#L104 | sha256:8590d42bdf2d… |
| 319,221 | 318,778 | 46% | 720 | 22.0 | 22.0 | codex-auto-review | ~/Documents/dot-agents | rollout-2026-05-26T19-06-19-019e6689-fc5a-7593-92e1-11d4aa70dc75#L66 | sha256:8719e91031ac… |
| 285,819 | 284,126 | 87% | 2,419 | 19.0 | 54.0 | gpt-5.5 | scratchpad/depth-exp-v2 | rollout-2026-06-26T14-50-02-019f0544-7e10-7222-a62e-6ffa47ecd1ef#L57 | sha256:4ecceafacd20… |
| 271,115 | 267,442 | 68% | 5,220 | 16.0 | 2.0 | gpt-5.5 | <tmp> | rollout-2026-06-28T22-43-29-019f1142-a96b-77a2-85b9-f3580d8b2f9f#L71 | sha256:fb4caf68ddb9… |
| 262,404 | 257,025 | 82% | 7,033 | 31.0 | 38.0 | gpt-5.5 | ~/proj-docs/dot-agents | rollout-2026-07-04T13-47-04-019f2e3d-b7cb-7c63-9373-5f6c03d0d745#L86 | sha256:0b3f735d9086… |
| 251,456 | 244,861 | 71% | 9,678 | 70.0 | 33.0 | gpt-5.5 | scratchpad/bg-gate-308 | rollout-2026-07-04T10-06-35-019f2d73-dbb6-7fc2-8d0d-a7bb76dc83c7#L77 | sha256:1f1383c059cb… |
| 238,246 | 230,343 | 84% | 12,245 | 52.0 | 59.0 | gpt-5.5 | scratchpad/depth-exp-v3 | rollout-2026-06-26T17-28-11-019f05d5-4974-7333-8ee0-ae2ba514310b#L42 | sha256:a391c21f27cc… |
| 228,985 | 226,659 | 97% | 3,427 | 53.0 | 50.0 | gpt-5.5 | scratchpad/depth-exp | rollout-2026-06-26T12-21-26-019f04bc-7309-7ec2-936c-c7109e8f2980#L47 | sha256:f5ef9045ef81… |
| 202,247 | 200,548 | 76% | 2,294 | 50.0 | 49.0 | gpt-5.5 | scratchpad/depth-exp | rollout-2026-06-26T12-20-38-019f04bb-b4a9-7931-bba2-f584582ed1f5#L41 | sha256:2f9cd2e84824… |
| 183,269 | 178,895 | 66% | 5,823 | 41.0 | 48.0 | gpt-5.5 | ~/proj-docs/dot-agents/.claude/worktrees/agent-a201233811daaa6b4 | rollout-2026-06-26T11-56-27-019f04a5-924e-7102-99b3-76386d406f20#L61 | sha256:82ecfc92e306… |
| 166,121 | 165,085 | 82% | 1,921 | 24.0 | 20.0 | codex-auto-review | scratchpad/gate-233 | rollout-2026-06-30T08-28-38-019f1880-be1a-7512-8258-22d994b35c1c#L57 | sha256:a5b4b1d17bab… |
| 157,632 | 156,518 | 89% | 1,764 | 51.0 | 49.0 | gpt-5.5 | scratchpad/depth-exp | rollout-2026-06-26T12-21-03-019f04bc-1619-7900-a1cd-0e77e7bbf73d#L35 | sha256:e83e2c567cc2… |
| 151,386 | 147,200 | 93% | 4,739 | 72.0 | 62.0 | gpt-5.5 | scratchpad/depth-exp-v4 | rollout-2026-06-26T18-20-54-019f0605-8c99-7b70-84dc-0f12f3f52d56#L32 | sha256:1cf6ecb9d911… |
| 149,108 | 147,225 | 77% | 2,936 | 47.0 | 49.0 | gpt-5.5 | scratchpad/depth-exp | rollout-2026-06-26T12-19-40-019f04ba-d189-7920-8879-98ec0b176e50#L40 | sha256:88153524081b… |
| 146,467 | 144,914 | 87% | 2,285 | 43.0 | 48.0 | gpt-5.5 | scratchpad/depth-exp | rollout-2026-06-26T12-16-16-019f04b7-b63e-7b01-9713-0d17e186c2a0#L38 | sha256:d72fd70127c9… |
| 139,391 | 138,096 | 95% | 1,777 | 55.0 | 50.0 | gpt-5.5 | scratchpad/depth-exp | rollout-2026-06-26T12-22-34-019f04bd-79da-7a93-8b49-821fbaa68ce6#L31 | sha256:502ed02dbccd… |
| 139,375 | 138,004 | 75% | 1,936 | 59.0 | 51.0 | gpt-5.5 | scratchpad/depth-exp | rollout-2026-06-26T12-23-47-019f04be-983a-7302-8061-2fa990249daf#L31 | sha256:939805555bba… |
| 136,198 | 135,250 | 81% | 1,149 | 31.0 | 97.0 | gpt-5.4-mini | ~/Documents/dot-agents | rollout-2026-04-20T08-26-16-019daadb-0ed7-7a42-8514-3ee888c94fc9#L36 | sha256:bb1c6f368074… |
| 130,428 | 128,579 | 71% | 2,713 | 25.0 | 55.0 | gpt-5.5 | scratchpad/depth-exp-v2 | rollout-2026-06-26T14-52-23-019f0546-a39a-71c3-9765-9ec334bb0edd#L51 | sha256:5402f67a1bd1… |
| 123,662 | 120,104 | 94% | 3,919 | 68.0 | 61.0 | gpt-5.5 | scratchpad/depth-exp-v4 | rollout-2026-06-26T18-17-24-019f0602-5705-7220-9f41-abe843b8268b#L28 | sha256:843465588ecd… |
| 123,235 | 120,341 | 65% | 3,126 | 100.0 | 66.0 | gpt-5.5 | scratchpad/depth-exp-v4 | rollout-2026-06-26T18-41-47-019f0618-a92f-73e1-b54b-e17ff196ae47#L27 | sha256:c613892136c4… |
| 118,712 | 116,868 | 87% | 2,971 | 45.0 | 49.0 | gpt-5.5 | scratchpad/depth-exp | rollout-2026-06-26T12-18-20-019f04b9-9958-7b03-9f45-b2c594185e53#L29 | sha256:74c495b82d39… |
| 105,225 | 102,699 | 94% | 4,432 | 26.0 | 55.0 | gpt-5.5 | scratchpad/depth-exp-v2 | rollout-2026-06-26T14-52-50-019f0547-0f6f-7ff1-b92d-8b7a37c0ded7#L32 | sha256:0b2d867e8c5f… |
| 103,668 | 101,594 | 93% | 3,447 | 30.0 | 55.0 | gpt-5.5 | scratchpad/depth-exp-v2 | rollout-2026-06-26T14-54-29-019f0548-8eeb-7720-8788-4655cac920e5#L32 | sha256:69d8197e0317… |
| 100,283 | 96,219 | 92% | 4,310 | 78.0 | 63.0 | gpt-5.5 | scratchpad/depth-exp-v4 | rollout-2026-06-26T18-27-12-019f060b-4e1f-7630-905f-652fb2cccd97#L24 | sha256:f24c9c2771b6… |
| 99,688 | 96,061 | 91% | 3,878 | 79.0 | 63.0 | gpt-5.5 | scratchpad/depth-exp-v4 | rollout-2026-06-26T18-28-39-019f060c-a3d7-7b60-99ca-b740cf7b9275#L24 | sha256:f87ab41e0e41… |
| 98,011 | 94,801 | 75% | 3,593 | 69.0 | 62.0 | gpt-5.5 | scratchpad/depth-exp-v4 | rollout-2026-06-26T18-18-39-019f0603-7c58-7ac2-a9bd-bd16c04141ff#L24 | sha256:6024bd4580f2… |
| 88,109 | 87,886 | 63% | 374 | 85.0 | 13.0 | codex-auto-review | scratchpad/gate-227 | rollout-2026-06-30T00-02-21-019f16b1-3b51-7341-8554-4d3b6f9afb35#L39 | sha256:4147efa1e3ef… |
| 70,997 | 70,421 | 49% | 1,056 | 19.0 | 22.0 | codex-auto-review | ~/Documents/dot-agents | rollout-2026-06-24T11-24-30-019efa3b-992c-7291-9c7a-a825834932d8#L30 | sha256:3b0632ab5157… |
| 68,391 | 67,502 | 74% | 1,320 | 57.0 | 60.0 | gpt-5.5 | scratchpad/depth-exp-v4 | rollout-2026-06-26T18-09-14-019f05fa-dd54-7db1-9238-34bf5c8f36e7#L19 | sha256:6d4914159a4a… |
| 60,108 | 59,728 | 68% | 706 | 88.0 | 37.0 | codex-auto-review | ~/proj-docs/dot-agents/.claude/worktrees/agent-ab819e6c47a85c4f9 | rollout-2026-06-25T20-47-14-019f0165-288c-7503-b2e1-b9a210e91afb#L30 | sha256:5c2b05c58fce… |
| 48,827 | 48,555 | 52% | 451 | 41.0 | 22.0 | codex-auto-review | scratchpad/g235 | rollout-2026-06-30T10-31-13-019f18f0-f7a3-7ff2-bfc8-03e9b80f30d6#L21 | sha256:d9dc95c7c9d8… |
| 45,753 | 44,830 | 17% | 1,490 | 59.0 | 60.0 | gpt-5.5 | scratchpad/depth-exp-v4 | rollout-2026-06-26T18-10-32-019f05fc-0c58-7a13-bd8e-35a4afda52d1#L16 | sha256:66dd9bf0fa6d… |
| 39,737 | 39,545 | 67% | 348 | 88.0 | 30.0 | codex-auto-review | scratchpad/gate-p9 | rollout-2026-06-29T11-08-46-019f13ec-ff32-7d61-a351-16f1b9fca3af#L21 | sha256:94992dc8ab2c… |
| 15,580 | 15,435 | 46% | 220 | 36.0 | 21.0 | codex-auto-review | scratchpad/gate-235 | rollout-2026-06-30T10-23-52-019f18ea-3e71-7021-b4ee-47e24a629949#L11 | sha256:4bb9ce099864… |

Full 64-hex digests are in the raw analysis; the truncated form above is a locator. A reviewer with machine access recomputes `sha256` of the referenced line; the digest for every token row equals `sha256(rawline(ref))` by construction.

## Craft — verification & review discipline

- **Verification discipline (strong):** of 38 code-editing sessions (apply_patch>0), 25 ran **both** tests/build/lint **and** git, 12 git-only, 1 test-only, and **0 ran neither**. No codex session edited code without at least a git or test check in-session. Items `cx-craft-verify-*`.
- **External review tooling:** SonarQube MCP (security-hotspot / quality-gate) in 2 sessions (`cx-craft-sonar-*`); GitHub MCP (PR info / changed files / issue comments / CI logs) in 6 sessions (`cx-craft-ghmcp-*`).
- **codex-auto-review approval gate:** 8 sessions run the `codex-auto-review` model, which emits structured `{risk_level,user_authorization,outcome,rationale}` verdicts before allowing escalated/external-write actions (`cx-craft-autoreview-*`).

## Outcome

- 127/134 sessions `complete`, 7 cutoff. `cx-outcome-complete-*` (12, high-confidence, dual-anchored to `task_complete` + first `apply_patch`) capture sessions that self-report edits+validation in the final agent message across dot-agents / payout / sub-repos.

## Sensitive sessions (aggregate only — R5)

| session | rc | total_tok | apply_patch | spawn | models | slug |
|---|--:|--:|--:|--:|---|---|
| rollout-2026-03-02T13-11-32-019cafbf-8f95-7673-9512-4141fbf61d08 | 293 | 556,158 | 5 | 0 | gpt-5.3-codex | ~/Documents/payout/po-vault-lib |
| rollout-2026-03-19T12-10-11-019d06dc-9040-74f3-ad3b-9659b753d96d | 2367 | 36,211,825 | 50 | 23 | gpt-5.4 | ~/Documents/payout |
| rollout-2026-05-03T19-07-47-019df019-0e2e-77e1-9d72-1b8df97d979f | 766 | 10,767,623 | 11 | 0 | gpt-5.4 | ~/Documents/dot-agents |
| rollout-2026-06-28T13-00-37-019f0f2d-0792-7173-8a93-1957572474bf | 161 | 1,584,445 | 0 | 0 | gpt-5.5 | ~/proj-docs/dot-agents |

These 4 feed only the aggregate counts above and the starred rows in the token table; no excerpts or qualitative single-session items reference them alone.

## Noted-only session inventory

**sensitive (aggregate-only)** (4):

- `rollout-2026-03-02T13-11-32-019cafbf-8f95-7673-9512-4141fbf61d08` rc=293 tok=556158 apply=5 model=gpt-5.3-codex status=complete
- `rollout-2026-03-19T12-10-11-019d06dc-9040-74f3-ad3b-9659b753d96d` rc=2367 tok=36211825 apply=50 model=gpt-5.4 status=complete
- `rollout-2026-05-03T19-07-47-019df019-0e2e-77e1-9d72-1b8df97d979f` rc=766 tok=10767623 apply=11 model=gpt-5.4 status=complete
- `rollout-2026-06-28T13-00-37-019f0f2d-0792-7173-8a93-1957572474bf` rc=161 tok=1584445 apply=0 model=gpt-5.5 status=complete

**edited code, not selected for an item (captured in token table)** (6):

- `rollout-2026-03-29T19-06-55-019d3bd9-aeaf-7780-a348-8281a1763a4f` rc=215 tok=1202213 apply=1 model=gpt-5.4 status=complete
- `rollout-2026-04-12T01-10-54-019d8019-97ca-7fc2-95bf-a7ffa930ca3f` rc=144 tok=1107861 apply=1 model=gpt-5.4,gpt-5.4-mini status=complete
- `rollout-2026-04-12T11-11-13-019d823f-35b4-7561-b213-c68a0dc8b88e` rc=296 tok=3595144 apply=5 model=gpt-5.4,gpt-5.4-mini status=complete
- `rollout-2026-04-12T11-29-36-019d8250-070e-72a0-94a5-960f7bac17a7` rc=278 tok=3837084 apply=5 model=gpt-5.4,gpt-5.4-mini status=complete
- `rollout-2026-04-12T12-01-13-019d826c-fccc-7f01-9012-cf560494a5f5` rc=232 tok=4222562 apply=2 model=gpt-5.4,gpt-5.4-mini status=complete
- `rollout-2026-05-03T21-30-40-019df09b-dfc8-7840-8035-5c1c93e9d27a` rc=141 tok=760789 apply=3 model=gpt-5.4 status=complete

**read-only / investigation / planning (no edits; token total in table)** (52):

- `rollout-2026-04-18T16-20-08-019da240-2ef5-74e1-a90e-95a828969ffd` rc=65 tok=468975 apply=0 model=gpt-5.4 status=complete
- `rollout-2026-06-26T09-38-34-019f0427-53e7-7d91-9af7-0105007165cd` rc=138 tok=866018 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T11-56-27-019f04a5-924e-7102-99b3-76386d406f20` rc=62 tok=183269 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T12-16-16-019f04b7-b63e-7b01-9713-0d17e186c2a0` rc=39 tok=146467 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T12-18-20-019f04b9-9958-7b03-9f45-b2c594185e53` rc=30 tok=118712 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T12-20-38-019f04bb-b4a9-7931-bba2-f584582ed1f5` rc=42 tok=202247 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T12-21-03-019f04bc-1619-7900-a1cd-0e77e7bbf73d` rc=36 tok=157632 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T12-21-26-019f04bc-7309-7ec2-936c-c7109e8f2980` rc=48 tok=228985 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T12-22-34-019f04bd-79da-7a93-8b49-821fbaa68ce6` rc=32 tok=139391 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T12-23-47-019f04be-983a-7302-8061-2fa990249daf` rc=32 tok=139375 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T14-50-02-019f0544-7e10-7222-a62e-6ffa47ecd1ef` rc=58 tok=285819 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T14-50-50-019f0545-3a56-7492-a70d-85b4773dba36` rc=67 tok=361595 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T14-52-23-019f0546-a39a-71c3-9765-9ec334bb0edd` rc=52 tok=130428 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T14-54-29-019f0548-8eeb-7720-8788-4655cac920e5` rc=33 tok=103668 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T17-28-11-019f05d5-4974-7333-8ee0-ae2ba514310b` rc=43 tok=238246 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T18-09-14-019f05fa-dd54-7db1-9238-34bf5c8f36e7` rc=20 tok=68391 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T18-10-32-019f05fc-0c58-7a13-bd8e-35a4afda52d1` rc=17 tok=45753 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T18-17-24-019f0602-5705-7220-9f41-abe843b8268b` rc=29 tok=123662 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T18-18-39-019f0603-7c58-7ac2-a9bd-bd16c04141ff` rc=25 tok=98011 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T18-20-54-019f0605-8c99-7b70-84dc-0f12f3f52d56` rc=33 tok=151386 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T18-27-12-019f060b-4e1f-7630-905f-652fb2cccd97` rc=25 tok=100283 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T18-28-39-019f060c-a3d7-7b60-99ca-b740cf7b9275` rc=25 tok=99688 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-26T22-35-58-019f06ef-126d-7ab2-970d-8a8d9a3f5e6f` rc=176 tok=1770804 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-27T16-34-16-019f0aca-4753-7521-b44f-2d3b7b92ae2e` rc=207 tok=2870066 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-28T13-14-20-019f0f39-9635-7b32-98a3-2208b05d6d0a` rc=184 tok=1282310 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-28T22-11-54-019f1125-c1cd-74f3-9f2b-0a1683c6d80c` rc=91 tok=554714 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-28T22-43-29-019f1142-a96b-77a2-85b9-f3580d8b2f9f` rc=72 tok=271115 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-28T23-17-59-019f1162-3f72-7ba2-88bd-be3df5b99984` rc=137 tok=1766002 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-28T23-46-46-019f117c-9902-72d2-bcce-b95ee05419bb` rc=167 tok=1193703 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-29T09-17-18-019f1386-f0ee-7210-b34d-86355991eaf9` rc=166 tok=1241739 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-29T09-51-28-019f13a6-3925-7fc3-a6da-4096788dfdba` rc=120 tok=1044135 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-29T10-16-36-019f13bd-3c2e-7d40-adf4-11042c90bf6a` rc=104 tok=558669 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-29T11-08-46-019f13ec-fdc1-7eb2-8dac-463f751a0b0e` rc=81 tok=436436 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-29T18-24-45-019f157c-24ee-7f72-b0c3-5a63cb0615c3` rc=369 tok=4187193 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-30T00-02-20-019f16b1-3861-7821-963c-610257afa6e6` rc=198 tok=1711735 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-06-30T08-37-37-019f1888-f83b-7be0-924a-e4b741951d30` rc=112 tok=533006 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-01T08-22-47-019f1da1-bb0d-7ea3-bf2d-c1438ab8960b` rc=188 tok=1539628 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-01T09-18-56-019f1dd5-282b-75b2-9fbf-8cce44930c46` rc=134 tok=769037 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-02T05-50-24-019f223c-99bd-77b1-b618-6e8e068a3f02` rc=182 tok=1898200 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-02T06-13-28-019f2251-b579-7de3-915a-12f7e370243f` rc=128 tok=799146 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-02T12-59-14-019f23c5-2e3d-7cb1-a27a-fe2a39d5535b` rc=159 tok=957781 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-03T11-29-47-019f2899-abd7-76d1-9510-22690605e66d` rc=174 tok=1157361 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-03T11-50-08-019f28ac-4b66-7702-ac6b-7ccb8a637201` rc=98 tok=366257 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T00-52-13-019f2b78-50ee-7ac2-af42-86b9a08fce24` rc=150 tok=1045321 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T08-42-20-019f2d26-baed-7761-9b20-242185925096` rc=156 tok=609048 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T09-37-27-019f2d59-2e7d-7e53-a5fa-9ce96c106822` rc=113 tok=487084 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T10-06-35-019f2d73-dbb6-7fc2-8d0d-a7bb76dc83c7` rc=78 tok=251456 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T12-55-25-019f2e0e-6c56-74a1-8b87-346394b5a247` rc=96 tok=399274 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T13-46-26-019f2e3d-21ce-7e00-961e-1c06efe4ab9b` rc=105 tok=338915 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T13-47-04-019f2e3d-b7cb-7c63-9373-5f6c03d0d745` rc=87 tok=262404 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T14-14-06-019f2e56-75fb-7f41-b7b3-e7549dbcdb9b` rc=101 tok=369573 apply=0 model=gpt-5.5 status=complete
- `rollout-2026-07-04T14-14-21-019f2e56-b3aa-7451-95aa-dbf214a493ac` rc=96 tok=340672 apply=0 model=gpt-5.5 status=complete

## Gaps / unknowns

- **No `has_cost` axis:** codex rollouts record token counts + rate-limit %, never a dollar figure (`has_cost=false` for all 134). $ cost must be reconstructed downstream from tokens×model price.
- **Wall-clock is sparse:** only the 14 token-gap (short) sessions carry `has_wallclock`; the token-bearing sessions do not expose per-turn wall time in a normalized field (exec outputs carry a `Wall time:` string but it is per-command, not session-level). Session duration is inferable from first/last timestamps only ([INFERENCE], not extracted as items).
- **Rate-limit % is a point-in-time window reading, not a cumulative cost** — a 100% reading marks the window was exhausted at that turn, and does not by itself prove the session was throttled/blocked.
- **Plan-mode under-represented:** only 2 sessions used codex `plan` collaboration mode, so plan-vs-default behavioral contrast is thin in this sample.
- **`da workflow` absence is a true negative** but anchored indirectly (per-session artifact-read anchors + a corpus-wide scan), since a non-event cannot carry a single verbatim line (E1); treated as medium confidence.

## Summary

- **Items: 143** — mechanism 45, failure 21, cost 42, craft 23, outcome 12. Confidence: 21 high, 122 medium.
- **Sessions: 134 processed** (72 with items, 62 noted-only incl. 4 sensitive aggregate-only). 120 anchored token rows; 14 token-gap rows flagged.
- **Notable outliers:** largest session 62.2M total_tokens (99% cached; also a primary+secondary rate-limit wall); 14 rate-limit walls >=90%; a single session issuing 23 `spawn_agent` fan-outs; 12 sessions bridged in from Claude Code; SonarQube + GitHub MCP review paths; codex-auto-review structured risk gating.
- **Headline mechanism contrast:** codex never drives the dot-agents `da workflow` loop — it consumes the loop's artifacts as context and orchestrates via its own `update_plan` + `spawn_agent` fan-out, with strong in-session verification (0/38 edit sessions skipped both tests and git).
