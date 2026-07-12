# Case Study — Cross-corpus synthesis: local transcript evidence × prior analyses

Master synthesis for plan `transcript-analysis-and-pipeline-craft`. Convergence is computed **here**
(rubric E2, `evidence-rubric.md:33-35`): the 195 local evidence items (omp-cc-copilot 26, codex 143,
cursor 26) and the ~130 prior findings (`DA`/`EFF`/`BEH`/`PLAN`) are grouped into themes, and each
theme's confidence is its **anchor independence across corpora** (local × prior) **and harness**
(omp / claude-code / codex / cursor / copilot).

Section order is canonical per `methodology/templates/case-study.md`
(sources → anchors → timeline → coverage/confidence → gaps → actionable outcomes). Redaction gate R1–R5
applies: no raw transcript lines beyond the already-redacted item excerpts; every path here is
`~`-normalized; no home-path/user slug is reproduced. Every claim cites ≥1 `item_id` or prior ID.

Companion deliverables (same dir): `actionable-outcomes.md` (numbered outcomes + OH-* reconciliation),
`negative-control-analysis.md` (generalization from the 15+2 controls).

---

## 1. Sources

Consumed by `evidence_id` / finding-ID only (rubric R1; no raw transcripts).

### 1.1 Local corpora (this machine, snapshot 2026-07-12)

- **Inventory:** `evidence/inventory/inventory.jsonl` — 806 sessions / 2733 `.jsonl` (1927 folded
  subagents) / 274,679 primary records across 5 harnesses (`inventory/README.md`).
- **Sampled + extracted (226 sessions → 195 items):**
  - **omp-cc-copilot** (`items/omp-cc-copilot/items.yaml`, 26 items): 5 OMP + 50 claude-code + 1 copilot
    sessions (56 processed). Only in-set harnesses with per-turn dollar cost = OMP + copilot.
  - **codex** (`items/codex/items.yaml`, 143 items): 134 sessions (72 with items, 62 noted-only),
    full programmatic census + hand-anchored excerpts.
  - **cursor** (`items/cursor/items.yaml`, 26 items): 36 sessions (all processed), 20 anchored.
- **Sampling frame:** `items/sampling-manifest.json` — rule1 (all copilot/omp), rule2 (all cc),
  rule3 (codex/cursor stratified), rule4 (15 seeded negative controls). Sensitivity R5: 38 sensitive
  sessions contribute aggregate counts only.

### 1.2 Prior analyses (committed prose, treated `internal`)

- **`DA`** — payout-wrk + dot-agents agent-behavior evidence (`prior/prior-findings-index.md` legend).
- **`EFF`** — AI-assisted delivery-efficiency-gains synthesis (provadm-agc).
- **`BEH`** — Provider-Admin + Roosevelt agent-behavior case-study record.
- **`PLAN`** — ProvAdm analysis-tooling + config-promotion companion note.
- **`OH-A1..E3`** — 14 open hypotheses the priors raised but never tested (`prior/open-hypotheses.md`).
- **Method deltas** — `prior/methodology-deltas.md` (their toolkit vs our rubrics).

### 1.3 Per-harness capability (drives §4 confidence and theme (e))

| harness | sessions | da-loop CLI | tokens | $ cost | wallclock | model attribution | tool_result fidelity |
|---|--:|---|---|---|---|---|---|
| omp | 5 | **drives (heavy)** | y | y (USD) | y (80%) | y (`model_change`) | y |
| claude-code | 50 | **drives (heavy)** | y | **n** | partial (`durationMs`) | y | y |
| codex | 592 | **never** (reads artifacts) | y (89.6%) | **n** | sparse (per-cmd) | y | y (`function_call_output`) |
| cursor | 158 | native loop-worker (no CLI) | **n** | **n** | **n** | **n** | **none (0 `tool_result`)** |
| copilot | 1 | n/a (smoke) | y | credits (not USD) | y | y | — |

Source: `inventory/README.md` coverage notes + per-harness inventories; corroborated by the cost/gap items below.

---

## 2. Anchors

The theme set below is the anchor backing for every §3–§6 statement. Full anchor lines (with sha256
digests, R4) live in the three `items/*/items.yaml` files and `prior/prior-findings-index.md`; this
report cites the stable handles.

### Theme convergence table (E2 convergence computed here)

Confidence rubric (extends `evidence-rubric.md:64-67` to cross-corpus): **high** = independent primary
anchors from ≥2 local harnesses, **or** ≥1 local-harness primary + ≥1 convergent prior finding;
**medium** = single-harness primary anchor (single corpus) or an inference-flagged aggregate;
**low** = secondary/inference-only. A `low` theme cannot alone become an actionable outcome
(`evidence-rubric.md:67`).

| theme | statement (one line) | local harnesses | prior converge | confidence |
|---|---|---|---|---|
| **T-a1** | `da workflow` CLI is driven only by OMP + Claude-Code; codex never drives it (reads artifacts); cursor runs the loop-worker/orchestrator contract natively without the CLI | cc, codex, cursor | DA-C1, DA-C3, PLAN-G3 | **high** |
| **T-a2** | Orchestration is real: prescribed-skill-driven on CC/OMP, native-primitive-driven on codex (`multi_agent_v1`) and cursor (`Task`) | cc, codex, cursor | DA-C3, EFF-W2, EFF-W3 | **high** |
| **T-a3** | Model/provider is swapped mid-session (OMP `model_change`; codex multi-model runs) — routing is already dynamic | omp, codex | — | **medium** |
| **T-b1** | Rate-limit walls: codex drove 14 windows ≥90%; OMP hit a `usage_limit_reached` zero-output turn | omp, codex | — | **high** |
| **T-b2** | SIGHUP co-termination: two OMP mega-sessions killed ~2s apart with pending tool calls (one terminal/tmux restart) | omp | — | **medium** |
| **T-b3** | Cutoffs / mid-turn termination recur across harnesses (no clean close) | cc, codex, cursor | EFF-LC4 | **high** |
| **T-b4** | Redaction gap: a committed cursor item excerpt leaks a `/home/<user>/` absolute path (R3 violation in a sibling artifact) | cursor | (R3 rubric) | **medium** |
| **T-b5** | Cursor persists **no** `tool_result`: tool outcomes/exit-codes/errors unrecoverable; failures visible only as narration | cursor | — | **high** |
| **T-c1** | Cache-read dominates token volume — 96–98% on OMP/CC/anthropic mega-sessions, but the codex census weakens (median 87.7%, range 16.2–97.8%, 48/120 rows below the 85% cache-hot line) | omp, cc, codex | EFF-LC1 | **high** |
| **T-c2** | Dollar-cost attribution is gappy: only OMP+copilot record cost; CC/codex tokens-only; cursor records nothing; some OMP provider routes bill $0 | omp, cc, codex, cursor, copilot | EFF-LC1, EFF-RI1 | **high** |
| **T-c3** | Per-harness telemetry capability is uneven → dictates which harness can feed which Pareto axis | all 5 | EFF-S2, PLAN-G1 | **high** |
| **T-c4** | Fixed context overhead (tool defs + system prompt) is large: 67% of a trivial copilot task's context was tool definitions | copilot | — | **medium** |
| **T-d1** | Verification discipline is strong where tools persist: 0/38 codex edit-sessions skipped both git and tests; cursor grounds on `workflow tasks && git status` + `ReadLints` | codex, cursor, cc | DA-G2, EFF-W4, EFF-DG2, BEH-M2 | **high** |
| **T-d2** | External review gates are wired in-loop: codex-auto-review structured verdicts, SonarQube MCP, GitHub MCP, code-review-graph MCP, ReadLints | codex, cc, cursor | DA-C10, DA-G3 | **high** |
| **T-d3** | Delegation craft: bounded loop-worker with authoritative write-scope + structured completion reports, cross-harness | cursor, cc, codex | DA-C4, DA-C5, EFF-W2 | **high** |
| **T-d4** | Task state driven from canonical source ("canonical state wins" over stale checkpoints) | cc, cursor | DA-C1, DA-C2 | **high** |
| **T-e1** | Pareto-axis feed matrix: cursor cannot feed tokens/cost/wallclock/model; codex/cc cannot feed $ without reconstruction | all 5 | EFF-RI4 | **high** |
| **T-e2** | Workflow-feature support matrix (loop CLI / orchestration / review gate / async scheduling / compaction) differs per harness → the projection layer must be per-harness | all 5 | EFF-WH2, PLAN-G4 | **high** |

Anchor rollup (handles cited by theme):
- **T-a1**: `cc-workflow-cli-drives-taskstate`, `cc-workflow-tool-runs-scripts`; `cx-mech-loopbridge-01..08`;
  `cursor-loop-worker-harness`, `cursor-orchestrator-role-boundary`; DA-C1, DA-C3, PLAN-G3.
- **T-a2**: `cc-wave-parallel-delegation-agent`, `cc-orchestrator-skill-overnight`; `cx-mech-spawn-01..13`,
  `cx-mech-updateplan-01..06`, `cx-mech-planmode-01..02`; `cursor-nested-delegation`,
  `cursor-parallel-agent-orchestration`, `cursor-orchestrator-readback`; DA-C3, EFF-W2, EFF-W3.
- **T-a3**: `omp-provider-model-switch-midsession`; `cx-mech-model-01..04`, `cx-cost-top-01/03/12` (multi-model rows).
- **T-b1**: `cx-fail-ratelimit-01..14`; `omp-usagelimit-zero-output-failure`.
- **T-b2**: `omp-session-exit-distribution` (inference-flagged: tmux restart from 2s proximity).
- **T-b3**: `cc-cutoff-ends-midturn`; `cx-fail-cutoff-01..07`; `cursor-cutoff-plan-handoff-a`,
  `cursor-cutoff-plan-handoff-b`, `cursor-abandoned-tiny-session`; EFF-LC4.
- **T-b4**: `cursor-negctrl-conversational-diagnosis` (excerpt reproduces a `/home/<user>/` path — R3 gap).
- **T-b5**: `cursor-gap-no-tool-results`, `cursor-applypatch-context-failure`, `cursor-schema-narration-only`,
  `cursor-schemaA-reasoning-redacted`.
- **T-c1**: `cost-cacheread-dominates-context`; `cx-cost-permodel-01..04`, `cx-cost-top-01..12`;
  `omp-cost-totals-019f3cf2/019f3f23/019f4eda/019f4eea`.
- **T-c2**: `cc-tokens-recorded-no-dollar-cost`; `omp-cost-cursor-routed-zero-dollar`;
  `cursor-gap-no-token-cost-wallclock`; `copilot-tooldef-fixed-overhead`; codex `has_cost=false` (notes);
  EFF-LC1.
- **T-c3**: all `cursor-gap-*`, `cx-cost-tokengap-01..14`, `cc-tokens-recorded-no-dollar-cost`,
  `omp-turn-duration-shape-*`; `inventory/README.md`; EFF-S2, PLAN-G1.
- **T-c4**: `copilot-tooldef-fixed-overhead`.
- **T-d1**: `cx-craft-verify-01..08`; `cursor-git-workflow-grounding`, `cursor-readlints-verification`;
  `cc-mcp-quality-gates-inloop`; DA-G2, EFF-W4, EFF-DG2, BEH-M2.
- **T-d2**: `cx-craft-autoreview-01..08`, `cx-craft-sonar-01..02`, `cx-craft-ghmcp-01..05`;
  `cursor-sonar-mcp-discipline`, `cursor-negctrl-refactor-for-sonar`; `cc-mcp-quality-gates-inloop`;
  DA-C10, DA-G3.
- **T-d3**: `cursor-loop-worker-harness`, `cursor-completion-reports`; `cc-wave-parallel-delegation-agent`;
  `cx-outcome-complete-01..12`, `cx-mech-spawn-*`; DA-C4, DA-C5, EFF-W2.
- **T-d4**: `cc-workflow-cli-drives-taskstate`; `cursor-orchestrator-readback`; DA-C1, DA-C2.
- **T-e1 / T-e2**: capability rows §1.3 + every cost/gap item; EFF-RI4, EFF-WH2, PLAN-G4.

---

## 3. Timeline

Local record timestamps only (mtime banned, `evidence-rubric.md:16`); reconstructed spans flagged `[INFERENCE]`.

- **2026-02-28 → 2026-05:** codex is the earliest and broadest driver of dot-agents/payout work
  (`sampling-manifest.json` rule3 strata; token table in `items/codex/notes.md` starts 2026-02-28).
  Older codex sessions record rate-limit % but not token totals (`cx-cost-tokengap-01..14`) — a
  telemetry-density gradient with recency `[INFERENCE]`.
- **2026-04 → 2026-06:** codex `multi_agent_v1` fan-outs and the depth-experiment scratchpad runs
  (`cx-mech-spawn-*`, D12/depth-exp rows). Cursor sessions (undateable — `cursor-gap-no-timestamps`)
  span the dot-agents/payout loop work; ordering is unavailable for cursor.
- **2026-05-23 → 2026-07-11:** claude-code becomes the heavy `da workflow` loop driver; long-lived
  multi-day sessions (`cc-longlived-multiday-sessions`: 1699c371 ~95h; sensitive 327154c6 ~372h),
  many-PR orchestration (`cc-single-session-multi-pr`), overnight async scheduling
  (`cc-async-overnight-primitives`), compaction (`cc-synthetic-turns-compaction`).
- **2026-07-07 → 2026-07-12:** OMP mega-sessions run the full-loop exercise on dot-agents + payout
  (`omp-cost-totals-*`). Two co-terminate on SIGHUP ~2s apart (2026-07-11T01:06:39.810Z /
  01:06:41.648Z, `omp-session-exit-distribution`); two more start 40–57 min later and are themselves
  live-cutoff at snapshot. This is the exercise whose craft the plan converts into the three assets.
- **Prior-analysis window:** `DA`/`EFF`/`BEH`/`PLAN` were authored 2026-07-10; they lean on
  richer *recent* transcripts and thinner recovered older ones (EFF-S2, EFF-LC2) — the recency bias
  OH-E1 flags, echoed by our own codex density gradient above.

---

## 4. Coverage / Confidence

Per rubric §3, with cross-corpus independence folded in (§2 table).

- **High-confidence themes (14):** T-a1, T-a2, T-b1, T-b3, T-b5, T-c1, T-c2, T-c3, T-d1, T-d2, T-d3,
  T-d4, T-e1, T-e2 — each carries independent primary anchors from ≥2 local harnesses and/or a
  convergent prior finding. The cache-read dominance (T-c1) is the theme with the broadest
  cross-harness anchoring, strongest on the OMP/CC/anthropic mega-sessions: OMP 96–98%
  (`omp-cost-totals-*`), CC turn `input=2 / cache_read=28,175` (`cost-cacheread-dominates-context`).
  Codex is the variable leg — cached/input median 89% but cached/total median 87.7% with a wide
  spread (range 16.2–97.8%, 48/120 rows below the 85% cache-hot line; erratum #1), productive-token
  median 124,297 tok (12.28% of total) (`cx-cost-permodel-*`, `items/codex/notes.md`).
- **Medium-confidence themes (4):** T-a3 (omp+codex, no prior), T-b2 (omp-only + tmux inference),
  T-b4 (single artifact, but directly verifiable), T-c4 (copilot-only, n=1 smoke).
- **Mixed-version note (E4):** no score-sidecar comparisons are made here; all local items carry
  `rubric_version: null` (`items/*/items.yaml`), so no 3.0.0/2.1.0 aggregation risk. Prior findings
  are prose (no rubric version). Any cost figure reconstructed from per-turn `duration` or
  tokens×rate is `[INFERENCE]` (`omp-turn-duration-shape-*`, and see Outcome 3/4).
- **Corpus balance:** convergence is genuine, not codex-weighted volume: codex contributes 143/195
  items but the high-confidence themes each also carry OMP/CC/cursor primary anchors, so no theme
  rests on a single harness except the explicitly medium ones (T-b2, T-c4).
- **Prior-finding integration:** ~130 prior findings map onto the themes without contradiction; the
  priors' qualitative claims (DA-C1/C3, EFF-W2/W4, BEH-M2) are *mechanistically confirmed* by local
  transcripts, while their **quantitative** claims (EFF efficiency %) remain untested here (see §5, OH-A*).

---

## 5. Gaps / Unknowns

Unanchored or unresolved claims land here (rubric §3, template §5); these are review-debt, not findings.

- **No efficiency delta with a CI.** Local historical evidence is observational and confounded
  (`pareto-measurement-rubric.md:42-45`); it cannot establish the priors' efficiency gain (OH-A1) or a
  model-routing frontier (OH-A3). These require `pareto-live-waves` paired snapshot-identical runs.
- **Correction-attribution untested.** We did not code who-found-what or front-loading vs
  correction-count (OH-B1/B2/B3, OH-C1/C2); our census records mechanism/cost/craft, not per-issue
  correction taxonomy. Carried as review-debt to `falsification-review`.
- **Cursor is time-blind and outcome-blind.** No timestamps (`cursor-gap-no-timestamps`), no
  `tool_result` (`cursor-gap-no-tool-results`) → cursor outcomes are self-reported only
  (`cursor-completion-reports`, inference-flagged); cursor cannot order events or verify tool success.
- **`da workflow` absence in codex is a true-negative anchored indirectly** (`cx-mech-loopbridge-*`,
  medium): a non-event cannot carry a single verbatim line (E1), so it rests on per-session artifact-read
  anchors + a corpus-wide byte scan.
- **OMP per-tool dollar cost is unrecoverable** (cost is per assistant turn, not per tool call);
  only per-turn `duration` approximates it (`omp-turn-duration-shape-*`, `[INFERENCE]`).
- **Subagent internals uncounted.** Claude-code writes child turns to separate `subagent_files`
  (up to 235 for 151d7271) not traversed in-scope (`items/omp-cc-copilot/notes.md`).
- **Cross-repo influence (OH-D2) and environment-mode effect (OH-D3)** are out of the local corpus's
  reach (different repos / no controlled env pairing).
- **Windows `da` friction counterfactual (OH-D1)** cannot be run: this corpus is darwin, where `da
  workflow` is driven heavily and successfully (`cc-workflow-cli-drives-taskstate`).

---

## 6. Actionable outcomes (theme → outcome map)

Detailed numbered outcomes, confidence grades, consumers, and the full 14-row OH-* reconciliation are
in **`actionable-outcomes.md`**. Summary of the theme→outcome routing (only ≥medium themes qualify,
`evidence-rubric.md:67`):

| theme(s) | → outcome | consumer class |
|---|---|---|
| T-a1, T-a2, T-e2 | O1 emit per-harness projection swarm YAMLs from the profile IR; O9 file the cross-harness projection proposal | code change / proposal |
| T-c1, T-c3, T-e1 | O2 per-harness telemetry-capability matrix in the Pareto cell manifest; O3 normalize cost on productive tokens | plan-task / config |
| T-c2 | O4 dollar-cost attribution shim (reconstruct CC/codex, flag `[INFERENCE]`) | code change / proposal |
| T-d1, T-d2 | O5 pipeline-architect skill (verification+review discipline); O6 review-gate stage abstraction | skill / code change |
| T-a3, T-d2 | O14 codex full-auto executor as a Pareto cheap-route candidate | plan-task |
| T-b1, T-b2, T-b3 | O7 rate-limit + co-termination resilience in the loop runtime | proposal / code change |
| T-a2, T-d3 (CC async) | O8 async-scheduling + compaction loop-runtime contract | skill / code change |
| T-b4 | O10 fix the `/home/<user>/` redaction leak + add a commit-time home-path scan | plan-task / code change |
| T-b5, T-d4 (OH-E3) | O11 replace the substring evidence-scorer with anchor+`tool_result` confidence | config / proposal |
| T-a1 bridge, T-a3 | O12 add cross-harness bridge-origin attribution to telemetry | code change |
| T-c4, negative controls | O13 D12 + copilot-smoke as Pareto calibration controls (not workflow rows) | plan-task |

Downstream consumers named by the assignment — **craft-extraction**, the **user**, and the
**proposal queue** (`.agents/proposals/`) — read `actionable-outcomes.md` directly; O9 and the
proposal-class outcomes are the routing INTO that queue rather than around it (plan constraint,
`transcript-analysis-and-pipeline-craft.plan.md:49-52`).
