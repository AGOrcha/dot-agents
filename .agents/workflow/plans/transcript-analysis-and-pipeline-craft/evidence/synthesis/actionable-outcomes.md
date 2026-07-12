# Actionable outcomes — cross-corpus synthesis

Derived from `synthesis-report.md` (themes T-a1..T-e2). Each outcome: **statement**, **supporting
IDs** (item_id / prior ID), **confidence** (≥medium required, `evidence-rubric.md:67`), **CONSUMER**
class + **concrete target path**. Consumers: `proposal` | `plan-task` | `skill` | `config` |
`code change`. Paths verified to exist unless marked *(new)*.

Redaction: all paths `~`-normalized; no home-path/user slug reproduced.

---

## O1 — Emit per-harness projection swarm YAMLs from the Layer-1 profile IR

**Statement.** The full-loop swarm projections are hand-written today
(`.agents/workflow/runtime/full-loop/profile-driven.swarm.yaml`, `reconcile.swarm.yaml`) — the
Layer-2 gap the plan names. Local evidence shows the projection MUST be **per-harness**, because the
loop mechanism differs radically: OMP + Claude-Code drive `da workflow` directly, codex **never**
does (it reads `.agents/workflow/*` artifacts as context and orchestrates via its own primitives),
and cursor runs the loop-worker/orchestrator contract natively without the CLI. A single emitted
projection cannot serve all four.
**Supporting.** `cc-workflow-cli-drives-taskstate`, `cc-workflow-tool-runs-scripts`,
`cx-mech-loopbridge-01..08`, `cursor-loop-worker-harness`, `cursor-orchestrator-role-boundary`,
`cx-mech-spawn-01..13`, `cx-mech-updateplan-01..06`; DA-C1, DA-C3, PLAN-G3, PLAN-G4 (T-a1, T-a2, T-e2).
**Confidence.** high (3 local harnesses + 2 prior findings).
**CONSUMER.** code change → `internal/config/profile_projection.go` (emitter) +
`.agents/workflow/runtime/full-loop/*.swarm.yaml` (become emitted, not authored). Feeds the
`platform-projection-layer` plan task.

## O2 — Add a per-harness telemetry-capability matrix to the Pareto cell manifest; hard-exclude Cursor from tokens/cost/wallclock/model axes

**Statement.** The 4-axis frontier (token cost, token volume, accuracy, wall-clock) can only be fed
by harnesses that record the axis. Cursor records **none** of tokens/cost/wallclock/model/timestamps;
CC and codex record tokens but no dollar cost; only OMP+copilot record cost. The Pareto cell
definition (`model_family × task_class × cache_regime × retry_regime`) must carry a harness-capability
mask so a cell is never scored on an axis its harness cannot supply.
**Supporting.** `cursor-gap-no-token-cost-wallclock`, `cursor-gap-no-timestamps`,
`cursor-gap-no-model-attribution`, `cc-tokens-recorded-no-dollar-cost`, `cx-cost-tokengap-01..14`,
`copilot-tooldef-fixed-overhead`; `inventory/README.md` coverage notes; EFF-RI4 (T-c3, T-e1).
**Confidence.** high (all 5 harnesses + prior).
**CONSUMER.** plan-task + config → `pareto-historical` / `pareto-live-waves`; cell manifest under
`evidence/pareto/` *(new)*; rule addendum to `methodology/pareto-measurement-rubric.md` §"Unit".

## O3 — Normalize cross-harness token cost on productive tokens (output+reasoning + non-cached input), not raw totals

**Statement.** Cache-read is 89–99% of token volume in **every** telemetry-bearing harness, so raw
`total_tokens` overstates codex cost ~50×. The token-volume and token-cost axes must be computed on
the productive figure (output+reasoning and non-cached input), reporting raw + cache-adjusted per the
rubric.
**Supporting.** `cost-cacheread-dominates-context` (CC `input=2 / cache_read=28,175`),
`cx-cost-permodel-01..04` (median cached/input 89%; productive median ~12k vs ~692k total),
`cx-cost-top-01..12`, `omp-cost-totals-019f3cf2/019f3f23/019f4eda/019f4eea` (cacheRead 96–98% of
tokens, 61–69% of $); EFF-LC1 (T-c1).
**Confidence.** high (omp + cc + codex).
**CONSUMER.** config/code change → token-cost + token-volume axis definitions in
`methodology/pareto-measurement-rubric.md:17-18` and the eventual `evidence/pareto/frontier-report.md`
+ `rows.jsonl` normalization.

## O4 — Ship a dollar-cost attribution shim: reconstruct CC/codex cost from tokens×published-rate (flagged [INFERENCE]); treat $0 provider routes as suspect

**Statement.** Only OMP + copilot carry cost; CC and codex are token-only, and OMP itself records
$0.00 for some provider routes (cursor-routed model turns) — dollar cost is provider-route-dependent,
not universal. Any cross-harness $ comparison must reconstruct CC/codex cost from tokens×rate and flag
it `[INFERENCE]`, never silently mix with recorded OMP cost; copilot "cost" is premium-request
credits, not USD.
**Supporting.** `cc-tokens-recorded-no-dollar-cost`, `omp-cost-cursor-routed-zero-dollar` (gpt-5.4
turns $0.00 vs anthropic/codex routes billed), `copilot-tooldef-fixed-overhead` (credits), codex
`has_cost=false` (`items/codex/notes.md`); EFF-LC1, EFF-RI1 (T-c2).
**Confidence.** high (5 harnesses).
**CONSUMER.** code change + proposal → cost-synthesis step feeding `evidence/pareto/frontier-report.md`;
new proposal `.agents/proposals/cross-harness-cost-normalization.md` *(new)*.

## O5 — Ship the pipeline-architect skill on the Layer-1 profile IR, encoding the cross-harness verification discipline

**Statement.** Verification discipline is the strongest craft signal and it generalizes: 0/38 codex
edit-sessions skipped **both** git and tests (25 ran both), cursor workers ground on
`workflow tasks && git status` and check `ReadLints`, and CC wires quality gates in-loop. The
pipeline-architect skill should make an in-session verification stage a non-optional part of any
emitted pipeline, per app_type.
**Supporting.** `cx-craft-verify-01..08`, `cursor-git-workflow-grounding`,
`cursor-readlints-verification`, `cc-mcp-quality-gates-inloop`; DA-G2, EFF-W4, EFF-DG2, BEH-M2 (T-d1).
**Confidence.** high (codex + cursor + cc + 4 prior findings).
**CONSUMER.** skill → `.agents/skills/pipeline-architect/` *(new)*; register via `da skills promote`.

## O6 — Add a review-gate stage abstraction to the profile IR that binds to each harness's native gate

**Statement.** Review gates are already in-loop but harness-specific: codex uses `codex-auto-review`
(structured `{risk_level,user_authorization,outcome,rationale}` verdicts before escalation), CC uses
SonarQube + code-review-graph MCP, cursor uses SonarQube MCP + `ReadLints`. A `review-gate` stage
kind in the IR should project to whichever gate the target harness supports — and satisfy the
cross-family blocking-review rule (`falsification-review-rubric.md:23-25`).
**Supporting.** `cx-craft-autoreview-01..08`, `cx-craft-sonar-01..02`, `cx-craft-ghmcp-01..05`,
`cursor-sonar-mcp-discipline`, `cursor-negctrl-refactor-for-sonar`, `cc-mcp-quality-gates-inloop`;
DA-C10, DA-G3 (T-d2).
**Confidence.** high (codex + cursor + cc + prior).
**CONSUMER.** code change → `internal/config/execution_profile.go` (stage kind) +
`internal/config/profile_projection.go`; proposal `.agents/proposals/review-gate-stage-kind.md` *(new)*.

## O7 — Add rate-limit + co-termination resilience to the loop runtime

**Statement.** Sessions die three ways that a resilient loop must survive: rate-limit walls (codex
drove 14 windows ≥90%; OMP hit a `usage_limit_reached` zero-output turn), OS-signal co-termination
(two OMP mega-sessions killed ~2s apart with pending tool calls — one terminal/tmux restart), and
plain mid-turn cutoffs (CC/codex/cursor). The runtime should checkpoint before signal-class kills and
treat a rate-limit wall as a first-class stop condition with resumable state.
**Supporting.** `cx-fail-ratelimit-01..14`, `omp-usagelimit-zero-output-failure`,
`omp-session-exit-distribution`, `cc-cutoff-ends-midturn`, `cx-fail-cutoff-01..07`,
`cursor-cutoff-plan-handoff-a/b`, `cursor-abandoned-tiny-session`; `cc-async-overnight-primitives`
(ScheduleWakeup already polls for recovery); EFF-LC4 (T-b1, T-b2, T-b3).
**Confidence.** high (omp + codex + cc + cursor).
**CONSUMER.** proposal + code change → `.agents/proposals/loop-runtime-preemption-resilience.md`
*(new)*; loop runtime + `.claude/workflows/ultracode-wave-engine.mjs` (stop-condition handling).

## O8 — Adopt async-scheduling + compaction primitives as the unattended-run loop contract

**Statement.** Long unattended runs are already viable on Claude-Code via `ScheduleWakeup`
(defer/poll for Docker recovery or a user PR merge before pushing), Cron/Monitor, `<synthetic>`
compaction-boundary turns, and multi-day single-session persistence (~95h to ~372h). These should be
a named part of the loop-runtime/orchestrator contract, not incidental CC features.
**Supporting.** `cc-async-overnight-primitives`, `cc-synthetic-turns-compaction`,
`cc-longlived-multiday-sessions`, `cc-orchestrator-skill-overnight` (T-a2, T-d3).
**Confidence.** medium (single-harness CC, but high-confidence primary anchors).
**CONSUMER.** skill + code change → `.agents/skills/orchestrator-session-start/` (async/overnight
contract) + loop runtime.

## O9 — File the cross-harness loop-projection proposal (the transformer-layer requirement)

**Statement.** The mechanism-divergence finding (T-a1/T-a2/T-e2) is the concrete requirement for the
Layer-2 transformer: the profile IR must project into harness-specific loop drivers. File it as a
canonical proposal structurally parallel to the existing `omp-platform-handling.md` beachhead, so it
routes INTO the proposal queue per the plan's queue-debt constraint.
**Supporting.** T-a1, T-a2, T-e2 anchor sets; `omp-platform-handling.md` (existing parallel);
DA-C3, PLAN-G3/G4.
**Confidence.** high.
**CONSUMER.** proposal → `.agents/proposals/cross-harness-loop-projection.md` *(new)*; then `da review`.

## O10 — Fix a committed redaction leak and add a commit-time home-path scan to the evidence toolkit

**Statement.** A committed cursor item excerpt reproduces a `/home/<user>/` absolute path, violating
R3 (`evidence-rubric.md:54-56`), which requires home paths stripped to `~`-relative. The specific
item must be re-redacted, and the evidence-extraction/commit path must gain a blocking home-path +
secret scan so `~`-normalization is enforced mechanically, not by reviewer diligence (PLAN-G1: evidence
mining is too manual).
**Supporting.** `cursor-negctrl-conversational-diagnosis` (leaked path in excerpt); R3 rubric;
PLAN-G1 (T-b4).
**Confidence.** high (directly verifiable single artifact).
**CONSUMER.** plan-task + code change → re-run `evidence-extraction` redaction pass on
`items/cursor/items.yaml`; add the scan to the `workflow-evidence-analysis` toolkit / commit hook.

## O11 — Replace the substring evidence-scorer with anchor+tool_result-backed confidence (OH-E3 confirmed)

**Statement.** Local evidence **confirms** the prior scorer is unsafe: cursor sessions emit
`## Result` / `verification` completion text with **zero** `tool_result` records, so the substring
probes the coarse scorer keys on (`"result"`, `"verification"`) would rate them `high` while nothing
in-transcript corroborates the work — the exact gameable counter-example the falsification rubric
asks for. Scoring must require an anchor + a real `tool_result`/verifier record, per our confidence
grading.
**Supporting.** `cursor-completion-reports` (self-reported, inference-flagged), `cursor-gap-no-tool-results`;
OH-E3; `methodology-deltas.md` §3 (T-b5, T-d4).
**Confidence.** medium-high (cursor primary + methodology convergence).
**CONSUMER.** config/code change → the `workflow-evidence-analysis` `score-workflow-evidence.py`
thresholds; proposal `.agents/proposals/evidence-scorer-anchor-backed.md` *(new)*.

## O12 — Add cross-harness bridge-origin attribution to telemetry

**Statement.** Claude-Code → codex is a real, recurring bridge (12 codex sessions launched with
`originator='Claude Code'`; the rest via `codex_exec` headless / TUI). Cost and outcome of bridged
work are attributed to codex today with no back-link to the CC orchestrator that spawned it, so
cross-harness cost roll-ups double-count or mis-attribute. Telemetry should record the bridge origin.
**Supporting.** `cx-mech-origin-cc-01..03`, `cx-mech-origin-exec-01..03` (T-a1 bridge, T-a3).
**Confidence.** medium (codex-only, but high-confidence `turn_context` anchors).
**CONSUMER.** code change → codex/CC telemetry emitters + the Pareto `rows.jsonl` schema
(`origin_harness` field).

## O13 — Use the D12 hop-chain + copilot smoke as Pareto calibration controls, not workflow rows

**Statement.** The 18 D12 hop-chain trials (fresh-context telephone-chain, 2–5 turns each) and the
copilot 2+2 smoke (67% of context was tool definitions) measure fixed overhead and context-propagation
limits, not loop delivery. They belong in the Pareto harness as calibration/exclusion rows — cutoff
and trivial-task rows are excluded from the frontier (`pareto-measurement-rubric.md:56`).
**Supporting.** `cc-d12-hopchain-experiment-batch`, `copilot-tooldef-fixed-overhead`,
`cx-cost-negctrl-01..12` (T-c4; negative controls).
**Confidence.** medium.
**CONSUMER.** plan-task → `pareto-historical` exclusion set + cell manifest under `evidence/pareto/`.

## O14 — Register codex full-auto as a candidate cheap-executor route for the Pareto live waves

**Statement.** Codex runs full-auto (`approval_policy=never`) with strong in-session verification and
its own auto-review gate, and it never needs the `da workflow` CLI — a self-contained executor tier.
This makes it a natural candidate cheap-executor route to contrast against the `claude-opus-4-8`
baseline. Historical data can only *propose* this (frontier forbidden from observational rows,
`pareto-measurement-rubric.md:42-45`); it must be settled by paired live contrasts.
**Supporting.** `cx-mech-fullauto-01..06`, `cx-mech-loopbridge-01..08`, `cx-craft-verify-01..08`,
`cx-craft-autoreview-01..08`; OH-A3 (T-a3, T-d1, T-d2).
**Confidence.** medium (historical = hypothesis-only; candidate-route evidence, not a frontier claim).
**CONSUMER.** plan-task → `pareto-live-waves` candidate-route list (one stage-model swap per contrast).

---

## OH-* reconciliation (all 14 hypotheses)

Status by **local evidence only**: `confirmed` (local evidence upholds it), `refuted`, `still-open`
(local corpus cannot settle it — mostly efficiency/causal claims the pareto rubric forbids from
observational data, or correction-attribution we did not code), `partial` (local evidence bears on it
without closing it). None are outright refuted.

| OH | claim (abbrev) | status by local evidence | basis (item_id / prior / note) |
|---|---|---|---|
| **OH-A1** | inner/meta-loop made delivery materially more efficient | **still-open** | no paired timing; historical=hypothesis-only (`pareto-measurement-rubric.md:42-45`). Routes to `pareto-live-waves`. |
| **OH-A2** | 3-canonical-file re-entry cuts real re-entry cost | **still-open** | CC canonical-state loop confirmed (`cc-workflow-cli-drives-taskstate`, `cc-longlived-multiday-sessions`) but tokens-to-context never measured. |
| **OH-A3** | cheap-tier stage swaps sit on/near the frontier | **still-open (candidate-supported)** | cheap models already in-loop (`cx-cost-permodel-03` gpt-5.4-mini, `cx-craft-autoreview-*`, `omp-provider-model-switch-midsession`) → O14 candidate route, but no frontier from historical rows. |
| **OH-B1** | correctness converges when *right constraints* front-loaded | **still-open** | corrections not coded in this census; bundle-as-scope confirmed (`cursor-loop-worker-harness`, `cx-mech-loopbridge-*`) but causal front-loading untested. |
| **OH-B2** | thin worker payload *caused* correction loops | **still-open** | delegation bundle IS the authoritative scope carrier and IS read cross-harness (`cursor-loop-worker-harness`, `cc-wave-parallel-delegation-agent`, `cx-mech-loopbridge-*`); causation to corrections not tested. |
| **OH-B3** | companion surfaces are a systematic blind spot | **still-open** | not observable in our mechanism/cost/craft census; carried to `falsification-review`. |
| **OH-C1** | human vs agent found-issue split holds | **still-open** | who-found-what not anchored per-issue here. |
| **OH-C2** | worst direction changes = unknown-unknowns | **still-open** | direction-change taxonomy not coded locally. |
| **OH-D1** | credentialing evidence degraded by *Windows* `da` friction | **partial (consistent)** | this corpus is darwin, where `da workflow` is driven heavily and works (`cc-workflow-cli-drives-taskstate`) → consistent with friction being environment-specific, but the Windows counterfactual is unrun. |
| **OH-D2** | payout influenced ProvAdm design | **still-open** | cross-repo influence outside the local corpus's reach. |
| **OH-D3** | environment mode materially affects behavior | **still-open** | multi-env sessions observed (`~/proj-docs`, `~/Documents`, `.claude/worktrees`, `<tmp>` in codex token table) but no task-fixed controlled comparison. |
| **OH-E1** | recency/retention bias does not distort era comparison | **partial (bias real)** | telemetry-density gradient with recency confirmed locally: older codex sessions record rate-limit % but no token totals (`cx-cost-tokengap-01..14`); consistent with EFF-S2/LC2 — a density bias exists, though we run no era comparison to distort. |
| **OH-E2** | mtime fallback anchors are accurate enough to order cases | **partial (concern upheld)** | cursor records **zero** timestamps (`cursor-gap-no-timestamps`), so for the one harness where mtime would be the only anchor, ordering is unvalidatable → supports our mtime ban (`evidence-rubric.md:16`); direct divergence test not run. |
| **OH-E3** | coarse substring scorer is a safe prioritizer | **confirmed unsafe** | cursor self-reports `## Result`/`verification` text with **zero** `tool_result` (`cursor-completion-reports`, `cursor-gap-no-tool-results`) → substring-present but unverifiable = the gameable counter-example; routes to O11. |

Tally: **confirmed 1** (OH-E3), **partial 3** (OH-D1, OH-E1, OH-E2), **still-open 10**, **refuted 0**.
The still-open efficiency/routing set (OH-A1/A2/A3) and correction-attribution set (OH-B*/C*) are the
mandate for `pareto-live-waves` and `falsification-review` respectively.
