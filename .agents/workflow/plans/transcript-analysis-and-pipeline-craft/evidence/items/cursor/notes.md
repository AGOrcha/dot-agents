# Cursor corpus — evidence extraction notes

**Assigned:** 36 Cursor sessions (manifest `harness=cursor`, incl. 2 rule-4 negative controls). **Processed: 36/36.**
**Write-dir:** `evidence/items/cursor/`. **Items:** 26 across 51 anchors in `items.yaml`.

## Corpus-level facts
- **Universal telemetry gap:** every one of the 36 sessions has `has_tokens=has_cost=has_wallclock=false`, `started_at/ended_at=null`, and `models=[]` in inventory; no raw record carries a timestamp, token/usage, cost, or model field. Cursor **cannot feed the tokens, cost, latency, or model-attribution axes** — documented as `cost`-class GAP items (`cursor-gap-no-token-cost-wallclock`, `cursor-gap-no-timestamps`, `cursor-gap-no-model-attribution`).
- **No tool outputs:** zero `tool_result` records across all 36 (`cursor-gap-no-tool-results`). Tool inputs may appear but outputs/exit-codes/errors are never persisted → outcome & failure verification from Cursor is intrinsically weak.
- **Two transcript schemas.** Schema **A** (23/36): assistant turns carry `tool_use` blocks but extended reasoning text is the literal `[REDACTED]` (typically 75–90% of turns); only a short action headline + the final deliverable summary survive. Schema **B** (13/36): narration text only — no `tool_use`, no `tool_result`, zero tool-call fidelity. All 13 Schema-B sessions are interactive; all worker/orchestrator sessions are Schema A.
- **Sensitivity:** 33 internal, 3 public-ok, **0 sensitive** — every item carries an internal/public-ok anchor with a redacted excerpt (R5 satisfied; no aggregate-only rows required).

## Item counts
- **By class:** cost=3, craft=7, failure=4, mechanism=11, outcome=1
- **By confidence:** high=15, medium=11
- `cost` items are the telemetry-GAP findings (what Cursor cannot support); `mechanism`/`craft`/`failure`/`outcome` are positive findings.

## Coverage — all 36 sessions

| session | project | status | schema | red% | recs | kind | in items.yaml |
|---|---|---|---|---|---|---|---|
| bbad1924 | dot-agents | complete | A | 88 | 120 | worker | yes |
| a9684182 | payout | complete | A | 78 | 115 | interactive | yes |
| 2880b770 | dot-agents | complete | A | 70 | 102 | interactive | yes |
| 46f9657b | dot-agents | complete | A | 84 | 94 | interactive | yes |
| b07903bd | payout | complete | A | 90 | 89 | interactive | yes |
| d706fa5f | ResumeAgent | complete | B | 0 | 80 | interactive | yes |
| db36ad38 | payout | complete | B | 0 | 64 | interactive | yes |
| acaed551 | payout-payout-code-workspa | complete | B | 0 | 60 | interactive | noted |
| 967c3251 | dot-agents | complete | A | 90 | 59 | worker | yes |
| 5b6f50d7 | dot-agents | complete | A | 84 | 58 | worker | noted |
| 64093e38 | dot-agents | complete | A | 82 | 56 | interactive | noted |
| ac89434f | dot-agents | complete | A | 87 | 54 | interactive | noted |
| 0f185c98 | dot-agents-script-20260415 | complete | A | 83 | 53 | worker | noted |
| 4acb0439 | dot-agents | complete | A | 90 | 52 | worker | yes |
| 9f35e3d3 | payout | complete | A | 77 | 52 | interactive | noted |
| dd6d2e2f | payout | complete | A | 75 | 49 | interactive | yes |
| 1aff5366 | dot-agents-pattern-e-20260 | complete | A | 85 | 48 | worker | yes |
| 878a6961 | dot-agents-dot-agents-code | cutoff | B | 0 | 38 | interactive | yes |
| 3ac340f5 | dot-agents | complete | A | 84 | 38 | worker | noted |
| 65aec9af | payout-worktrees-client-se | complete | B | 0 | 37 | interactive | noted |
| 8a92b83c | dot-agents-dot-agents-code | complete | B | 0 | 35 | interactive | noted |
| 6c1f7726 | payout-payout-code-workspa | cutoff | B | 0 | 28 | interactive | yes |
| 9ba190ca | payout | complete | B | 0 | 22 | interactive | yes |
| 8f5f7f3e | dot-agents | complete | A | 69 | 16 | worker | noted |
| a2563484 | dot-agents | complete | A | 0 | 16 | interactive | yes |
| 6ec974a3 | private-tmp-payout-submodu (NEG) | complete | B | 0 | 15 | interactive | yes |
| d6392ce0 | payout | complete | B | 0 | 10 | interactive | noted |
| 869316af | dot-agents | complete | A | 12 | 8 | worker | noted |
| 47e62b6c | dot-agents | complete | A | 29 | 7 | worker | yes |
| 6b2a7158 | dot-agents | complete | A | 17 | 6 | worker | noted |
| ddb7a4d5 | dot-agents | complete | A | 17 | 6 | worker | noted |
| df428b91 | dot-agents | complete | A | 50 | 6 | interactive | noted |
| bb9afbdf | payout | complete | B | 0 | 6 | interactive | noted |
| 830f6693 | dot-agents-dot-agents-code (NEG) | complete | B | 0 | 4 | interactive | yes |
| 684414e2 | dot-agents | complete | A | 0 | 4 | worker | yes |
| 9948dc94 | payout | complete | B | 0 | 2 | interactive | yes |

## Sessions anchored in items.yaml (20)
1aff5366, 2880b770, 46f9657b, 47e62b6c, 4acb0439, 684414e2, 6c1f7726, 6ec974a3, 830f6693, 878a6961, 967c3251, 9948dc94, 9ba190ca, a2563484, a9684182, b07903bd, bbad1924, d706fa5f, db36ad38, dd6d2e2f

## Sessions noted-only, no distinct item (16) — with reason
Per the quality-over-volume directive, sessions that only re-instantiate an already-anchored pattern get a note, not a filler item; convergence is counted at synthesis (rubric E2).
- **0f185c98** (dot-agents-script-2026, A/worker, 53 recs): Schema-A loop-worker/orchestrator; re-instantiates loop-worker harness + redaction + no-tool_result + telemetry-gap patterns already anchored (bbad1924 et al.).
- **3ac340f5** (dot-agents, A/worker, 38 recs): Schema-A loop-worker/orchestrator; re-instantiates loop-worker harness + redaction + no-tool_result + telemetry-gap patterns already anchored (bbad1924 et al.).
- **5b6f50d7** (dot-agents, A/worker, 58 recs): Schema-A loop-worker/orchestrator; re-instantiates loop-worker harness + redaction + no-tool_result + telemetry-gap patterns already anchored (bbad1924 et al.).
- **64093e38** (dot-agents, A/interactive, 56 recs): Schema-A interactive; edit/tool mechanics + redaction already anchored.
- **65aec9af** (payout-worktrees-clien, B/interactive, 37 recs): Schema-B narration-only; fidelity-gap already anchored (db36ad38, 9948dc94).
- **6b2a7158** (dot-agents, A/worker, 6 recs): Schema-A loop-worker/orchestrator; re-instantiates loop-worker harness + redaction + no-tool_result + telemetry-gap patterns already anchored (bbad1924 et al.).
- **869316af** (dot-agents, A/worker, 8 recs): Schema-A loop-worker/orchestrator; re-instantiates loop-worker harness + redaction + no-tool_result + telemetry-gap patterns already anchored (bbad1924 et al.).
- **8a92b83c** (dot-agents-dot-agents-, B/interactive, 35 recs): Schema-B narration-only; fidelity-gap already anchored (db36ad38, 9948dc94).
- **8f5f7f3e** (dot-agents, A/worker, 16 recs): Schema-A loop-worker/orchestrator; re-instantiates loop-worker harness + redaction + no-tool_result + telemetry-gap patterns already anchored (bbad1924 et al.).
- **9f35e3d3** (payout, A/interactive, 52 recs): Schema-A interactive; edit/tool mechanics + redaction already anchored.
- **ac89434f** (dot-agents, A/interactive, 54 recs): Schema-A interactive; edit/tool mechanics + redaction already anchored.
- **acaed551** (payout-payout-code-wor, B/interactive, 60 recs): Schema-B narration-only; fidelity-gap already anchored (db36ad38, 9948dc94).
- **bb9afbdf** (payout, B/interactive, 6 recs): Schema-B narration-only; fidelity-gap already anchored (db36ad38, 9948dc94).
- **d6392ce0** (payout, B/interactive, 10 recs): Schema-B narration-only; fidelity-gap already anchored (db36ad38, 9948dc94).
- **ddb7a4d5** (dot-agents, A/worker, 6 recs): Schema-A loop-worker/orchestrator; re-instantiates loop-worker harness + redaction + no-tool_result + telemetry-gap patterns already anchored (bbad1924 et al.).
- **df428b91** (dot-agents, A/interactive, 6 recs): Schema-A interactive; edit/tool mechanics + redaction already anchored.

## Gaps / unknowns (for platform-capability matrix)
- **tokens / cost / wall-clock / timestamps / model:** unsupported by Cursor transcripts (all 36). Source elsewhere or mark N/A for Cursor.
- **tool outputs & exit codes:** unrecoverable (no `tool_result`); failure/outcome claims rely on agent self-narration only.
- **intermediate reasoning (Schema A):** `[REDACTED]`; only headlines + final summary survive, so mid-iteration craft is not auditable in worker sessions.
- **tool calls (Schema B):** absent; 13 interactive sessions expose intent narration but not actions taken.

## Notable outliers
- **bbad1924** (120 recs): richest Schema-A worker — 160 tool_use / 0 tool_result / 106 redacted turns; canonical worker-harness + redaction example.
- **a9684182** (payout migration): end-to-end SonarQube-MCP discipline (disable auto-analysis at start, `analyze_file_list` after `go test`).
- **878a6961 / 6c1f7726** (both cutoff): planning sessions truncated at plan-handoff — produced/updated a `.cursor/plans/*.plan.md`, no execution captured.
- **6ec974a3** (NEG ctrl): only session exposing a tool failure (ApplyPatch context-marker rejection) — visible solely as narration, reinforcing the no-tool_result gap.
- **830f6693** (NEG ctrl): pure SSH/git chat, zero tools — confirms mechanism/craft findings do NOT generalize to non-workflow Cursor use.
- **9948dc94** (2 recs): minimal session — states intent to run tests, then ends; outcome unrecoverable.
