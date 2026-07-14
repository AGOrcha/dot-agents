# External reference digest — 2026-07-12

User-supplied reference set, extracted per article-extract skill. Each entry: source, core
claims, and the concrete consumer in THIS plan. Anchors are the public URLs (stable) +
local verification where run.

## 1. Claude Code JSONL token undercount — Magnus Gille (gille.ai, 2026-02-24)
`https://gille.ai/en/blog/claude-code-jsonl-logs-undercount-tokens/` + repo
`Magnus-Gille/claude-code-energy-monitor` (`sum_jsonl.py`, `analyze_tokens.py`, FINDINGS.md).
- Claims (flagged PRELIMINARY by author): CC JSONL `usage.input_tokens` is a streaming
  placeholder (their corpus: 75% ≤1, never finalized; input undercounted 102–174×);
  `output_tokens` partially placeholder AND excludes thinking (10–17× under); cache_read /
  cache_creation are accurate (~1×); same requestId appears 2–10× (51–55% duplicates);
  statusbar `context_window` totals are the accurate source.
- Code reading: `sum_jsonl.py` dedups by *last entry per requestId* and classifies
  placeholders (≤1); `analyze_tokens.py` validates via statusbar deltas — confirms
  `total_input_tokens` EXCLUDES cache_creation (no double count) and `total_output_tokens`
  INCLUDES thinking (1.0× vs API).
- **Local verification — CORRECTED 2026-07-12 by independent re-derivation** (red-team RT-2,
  from-scratch scripts over the full on-disk corpus: 36 primary + 1,134 subagent files, 93,493
  usage entries; the originally-quoted corpus "50 primary / 1,680 incl subagents / 35,515
  entries" is unreconstructable — partial scan):
  - requestId duplication REPRODUCED: 74-81% of requestIds appear >1× (band across slices).
  - Per-field naive/dedup overcount REPRODUCED **on the primary-files slice only** (primary =
    `*.jsonl` directly under the project dir, 0 `isSidechain`): input 2.98×, output 3.02×,
    cacheRead 2.49×, cacheCreate 3.01×. Pooling subagent files dilutes (output → 1.76×). Any
    consumer of these ratios MUST pin the slice.
  - Placeholder rates corrected: **input≤1 = 5.3% full / 0.69% primary** (the earlier "12%"
    measured the OUTPUT field — output≤1 = 11.97% naive full corpus; the earlier "output≤1 =
    0%" is false on every slice).
  - Dedup rule STRENGTHENED: last entry = max in 100% of 33,194 dup groups; requestIds never
    span files → last-entry-wins is well-defined.
  - Our CC evidence rows did NOT dedup by requestId (unchanged finding; erratum #2 stands).
- **Corroboration (added 2026-07-12): devforth mitmproxy ground truth**
  (`https://devforth.io/insights/claude-code-usage-significantly-overestimates-output-tokens/`,
  `research/articles/devforth-cc-usage-overestimates-output-tokens.md`): live `/v1/messages`
  capture proves the API charges once per `requestId` — one stream emitting two tool calls with
  `output_tokens: 101` becomes two local rows claiming 101 each (naive sum 202, actual charge
  101); `/usage` reported 5.1M output vs 1.88M deduplicated on their corpus (CC 2.1.195). The
  duplication mechanism is cumulative streaming usage snapshots copied onto one row per content
  block. Validates dedup-by-requestId as matching real billing, not just internal consistency.
- **Consumer:** pareto erratum #2 (CC token normalization: dedup-by-requestId last-entry rule,
  placeholder exclusion, cache fields treated as the only high-trust CC fields); evidence-rubric
  amendment for CC-source rows; capability matrix note (CC "has_tokens" ⇒ *low-fidelity* tokens).

## 2. Infracost: CLI redesigned for agent callers (−79% output tokens)
`https://www.infracost.io/resources/blog/we-cut-claude-s-token-usage-79-by-redesigning-our-cli-for-agents`
- Predicate pushdown (`--filter`, `--missing-tag`, `--fields`, `--addresses-only`) kills
  model-composed jq/python pipelines; TOON tabular output (uniform arrays → header + rows)
  −35% vs minified JSON with equal comprehension; benchmark harness (16 q × 3 configs × 5
  repeats, sandboxed HOME, PATH-pinned binary, rerun-failed/rescore) is the measurement spine.
  Hard-question bucket: bare Claude 0/6 at $7.61; skill+flags 6/6 at $2.48.
- **Consumer:** `da` CLI agent-path design (our `--json` envelopes are the analog; candidate
  fold-back: token-efficient tabular output mode + more predicate flags on `workflow eligible/
  tasks`); craft §5 fixed-tax rule gains an external anchor; Pareto live-wave harness design
  mirrors their repeats/rescore discipline.

## 3. Honey-for-devs (GreenPT) — output-volume compression skill family
`https://github.com/Green-PT/honey-for-devs`
- Three levers: less code (YAGNI ladder), less prose, denser A2A handoffs (ESON, −51%
  lossless). Safety carve-outs never compressed. 4-model cross-family judge panel (median of
  Opus/Sonnet/Haiku/GPT) under a length-blind rubric. CCR = lossy-but-recoverable compression
  for uniform arrays (sentinel + local cache + retrieve). PX = image-rendered reads (~−80%
  tokens) but ONLY Fable-class models read renders usably; silent confabulation risk on exact
  strings.
- **Consumer:** cost-tuning instruction (output-volume lever is complementary to our
  model-routing lever — H1 says model swap moves volume ≤4%, THIS moves the other 96% via
  behavior); falsification-review rubric convergence (their length-blind judge panel = our
  cross-family gate, independent invention); A2A handoff format candidate for delegation
  bundles/merge-backs.

## 4. PostHog: Stop being the code review bottleneck (newsletter, 2026-07-09)
`https://newsletter.posthog.com/p/code-review-tips` (X post 2075645235724767739)
- Writer-agent must never self-review; multiple reviewers with different instructions AND
  different model providers (cites Verga et al. "juries over judges"); qa-swarm (4 lenses) +
  review-triage (actionable/nit/ambiguous) + bounded outer loop (≤3 iterations); StampHog
  deterministic gates (deny-list keywords, size cap, PR state) before LLM check — the size cap
  rejects PRs over **>800 substantive lines OR >30 files** (per the authoritative StampHog
  README, which is the authority here; the newsletter's "≤500 lines/20 files" is a stale figure
  the digest inherited); 1/3 of merged PRs auto-stamped, fail-closed; "verify by observation,
  not reasoning" —
  decompose into stacked observable PRs, screenshots/GIFs as review evidence.
- **Consumer:** direct validation of RULE-7 + our lens architecture (their 4-lens qa-swarm ≈
  our verifier/lens chains); StampHog's deterministic-gate-before-LLM pattern is a candidate
  fold-back for `workflow delegation gate`; review-triage taxonomy maps onto our fold-back
  routing (actionable→fix, nit→note, ambiguous→escalate).

## 5. Arize: What is a loop, anyway? (2026-07-10; X article by @aparnadhinak)
`https://arize.com/blog/what-is-a-loop-in-ai-engineering-anyway/`
- Four loops: execution (tool-call cycle, ends on env feedback) / task (Ralph loop: fresh
  context per iteration against one spec, ends on tests+spec) / product (factory: codebase +
  backlog, configurable human checkpoints, ratcheting auto-merge) / system (autoresearch:
  outer loop improves prompts/evals/harness; "the loop is the product"). Plus the oversight
  loop (goals/budgets/culling) as the human ring. Fan-out (Agentic MapReduce) is a topology,
  not a loop — no feedback edge. Anthropic's own factory (Tag) is bottlenecked on human review
  + conceptualization.
- **Consumer:** taxonomy mapping for the craft doc / pipeline-architect skill: our inner
  swarm = execution loop; per-task bounded fold-back = task loop; da N-plan driver = product
  loop; pareto-live-waves + fold-back/proposal routing = system loop; user gate = oversight
  loop. Naming these in the skill's design-pipeline instruction grounds "which loop are you
  editing" as the first architect question.

## 6. Thariq (Anthropic): A Field Guide to Fable — Finding Your Unknowns (2026-07-03)
X 2073100352921215386 (mirror: rattibha; now on Claude blog:
`https://claude.com/blog/a-field-guide-to-claude-fable-finding-your-unknowns`)
- Map-vs-territory: quality bottlenecked by clarifying unknowns (known/unknown × known/
  unknown grid). Techniques: blind-spot pass, brainstorm/prototype before wiring, one-question
  interviews prioritized by "answer would change architecture", references-as-source-code,
  implementation plans leading with likely-to-change decisions, implementation-notes.md
  deviation logs, post-hoc quizzes gating merge ("only merge after I pass the quiz").
- **Consumer:** pipeline-architect skill's design-pipeline instruction (interview step before
  profile edits); delegation bundle template candidate: require a `Deviations` log in
  merge-backs (we already require notes; naming deviations explicitly is sharper).

## 7. Michael Lynch: How to write an effective design doc (refactoringenglish.com, 2026-06-24)
- Investment scales with cost-of-being-wrong; "what's the penalty for being wrong?" as the
  section-inclusion test; goals in impact terms not implementation; explicit non-goals;
  scenarios paint the after-state; SLOs before monitoring; alternatives-considered.
- **Consumer:** plan/spec template refinement (our PLAN.yaml success_criteria ≈ SLOs; the
  cost-of-wrong test matches our "ranges are tight / plan only what makes the request work"
  discipline). Low-urgency; reference material for the skill's templates.

## Cross-cutting synthesis for this plan
- **Token accounting:** every external source converges on our T-c1 finding from a different
  angle — raw harness token fields are unreliable or misleading (CC placeholders+dups, codex
  cache-dominance, copilot credits-not-USD). The productive-token normalization MUST also
  specify per-harness field-trust rules (graduated to evidence-rubric clause E6; corrected
  numbers in historical-hypotheses Erratum 3).
- **Review:** three independent sources (PostHog, honey judge panel, our RULE-7) converge on
  cross-model/cross-family review with writer≠reviewer — strengthens falsification-review
  rubric from convention to externally-corroborated practice.
- **Cost:** two levers, not one — model routing (our Pareto axis) and output-volume behavior
  (infracost/honey). H1 predicts routing moves volume ≤4%; the volume lever is where the other
  ~96% lives (pipeline change, CLI predicate pushdown, output format).

---

## 2026-07-13 addendum — operating-contract batch (post-archival; corroboration-first)

Processed after the red-team disposition and fold-back archival: nothing below reopens a
settled wave decision. Full evaluation: `research/articles-evaluation-kg-and-adjacent.md`
Part K. All numeric claims in this batch are linkless/secondhand → [UNVERIFIED] until chased.

## 8. sairahul1: Fable brain backup — instruction distillation + trap test (X, 2026-07-12)
- Distill a stronger model's procedures into trigger→action standing orders for a cheaper
  model; PROVE the transfer with a planted-defect trap task; on failure, regenerate only the
  vague section (bounded).
- **Consumer:** `evidence/pareto/disposable-tasks.md` Tier B — trap-test transfer check when
  a stage prompt runs on a SECONDARY-tier model in a contrast arm (fold-back
  `lens-transfer-trap-test`, resolved same day). Readback health-check corroborates the
  bundle-readback step; trigger→action executability is the lens/skill prompt authoring bar.

## 9. 0xmiraqle: GPT 5.6 god-mode contract (X, 2026-07-09)
- Builder never grades its own work — 5.6 system card reportedly admits grader-gaming "at the
  highest rate ever measured on a public model" [UNVERIFIED]; loop never contains the model's
  right to declare itself finished; house-rules fence; evidence-per-"done" checkable in under
  a minute; traces as private training set; ~1-in-400 off-script [UNVERIFIED].
- **Consumer:** RULE-7 prior art — FOURTH independent invention of fresh-context
  builder≠grader (PostHog, honey, RULE-7, this), first with a measured-behavior
  justification. CHASE: 5.6 system card grader-gaming section → if confirmed, cite in
  `methodology/falsification-review-rubric.md`. Rest corroborates verdict-gate termination,
  rules-as-fence, merge-back verification_status, and the transcript-pipeline thesis.

## 10. prajwaltomar: two scoreboards — GPT 5.6 launch read (X, 2026-07-10)
- Public benchmark (Sol 80, +2.8 over Fable) vs Every's private senior-engineer benchmark
  (Fable 91, Sol 56) — both true; different task classes. "Benchmarks are marketing — run
  both on YOUR work." Anthropic's own orchestrator numbers: 96% performance at 46% cost for
  big-model-orchestrates/cheap-executes [UNVERIFIED]. Effort dial > adjacent-model choice.
  METR: highest-measured reward-hacking rate on Sol — "especially on tests it wrote itself."
- **Consumer:** external statement of the plan's epistemics (own-cell preregistered contrasts
  over public benchmarks). CHASE: the 96%/46% primary source → candidate effect-size prior
  for C1/C2, belongs here with provenance if located. Reasoning-effort tier logged as a
  candidate FUTURE blocking axis (open question — not a mid-freeze cell change). METR flag
  joins #9 as grader-integrity justification; the frozen-verifier ground-truth rule in
  disposable-tasks.md already guards "tests it wrote itself."

---

## 2026-07-13 addendum — Part L measured anchors (delegation-economics / harness-config batch)

Additive, gate-neutral — no wave decision reopened. Full evaluation:
`research/articles-evaluation-kg-and-adjacent.md` Part L; distilled
`research/extracts/harness-and-delegation-2026-07.md`. These UPGRADE prior [UNVERIFIED]
growth-account leads in this digest to `measured-with-method` where the batch supplies data.

### 11. joon-lee (Cognition/Devin): Making Fable Cheaper Than Opus (X, 2026-07-13) — MEASURED
- 3,000 instrumented sessions on FrontierCode 1.1 (every LLM call parsed) + 40-task
  trajectory dive. Fable+cheap-sidekick costs LESS than Opus+sidekick ($1.86 vs $2.04) AND
  scores higher (60.7 vs 54.6) despite Fable being 2x per-token; Fable+sidekick cuts cost 54%
  vs pure Fable at ~unchanged score. Cost is dominated by lead turns (11.5 vs 26.5), context
  dragged (545k vs 1,679k tok), and what the lead does NOT do (Fable lead makes zero edits in
  81% of runs). Constraint-enumerating brief mediates correctness AND cost (O(1) example — 94
  vs a constraint-dropping hand-impl at 25).
- **Consumer — this is the MEASURED sibling to #10's [UNVERIFIED] "96% at 46%".** It does not
  reopen a cell; it strengthens the C1/C2 prior (frontier-lead + cheap-executor ≈ frontier
  quality at lower cost) with data. PARTIALLY resolves Part K open-Q #2 — a measured member of
  the class exists, though Anthropic's exact 96%/46% number is still uncited (chase stands).
- **NEW AXIS (parked, post-freeze):** L.1's cost variance lives in the LEAD/orchestrator's
  delegation BEHAVIOR (early vs late, brief quality, pull-back rate), not the executor swap
  our C1-C6 cells vary — not a contradiction with H1 (which holds the orchestrator fixed).
  Candidate post-freeze wave — swap the orchestrator model, hold the executor, measure
  brief-quality as the mediator. Sits with the K.3 effort-dial axis (Part K open-Q #3).
- **Bundle-authoring sharpen:** the constraint-enumerating design-doc brief is now a MEASURED
  justification for `orchestrator-session-start`'s "write constraints into TASKS.yaml notes"
  and the J.6 Deviations log — the brief's constraints/edge-cases/done-definition are the
  load-bearing contract fields (with J.3 never-compress-contract-fields).

### 12. LangChain harness profiles (L.5, MEASURED +10-20pt) + Alex Ker harness guide (L.6)
- Per-model harness customization (prompt/tool/middleware overrides) is measured to matter
  (+10-20pt tau2 subset; Terminal-Bench "same model, different harness, very different
  score"). Independent measured version of #10's "benchmarks are marketing — measure on YOUR
  harness/cells." Does NOT touch a frozen cell.
- **Consumer — this plan's epistemics** (own-cell measurement) gains a measured external
  prior. The per-model-family override-layer adoptable is routed OUT of this plan to
  `config-transitive-layering` / `pipeline-architect` (proposal
  `obs-per-model-family-harness-override-layer`) — NOT a transcript-plan change. Cache-aware
  routing + minimal-repair re-entry routed to `full-loop-orchestration-runtime` (fold-backs
  `cache-aware-model-routing`, `minimal-repair-freeze-validated-regions`).
