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
- **Local verification (this machine, 400 CC files, 35,515 usage entries):** input≤1 = 12%
  (NOT 75% — placeholder severity is version/era-dependent), output≤1 = 0%, but
  **76% of requestIds appear >1× — naive summation overcounts ~2.4×.** Our CC evidence rows
  did NOT dedup by requestId.
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
  deterministic gates (deny-list keywords, ≤500 lines/20 files, PR state) before LLM check —
  1/3 of merged PRs auto-stamped, fail-closed; "verify by observation, not reasoning" —
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
X 2073100352921215386 (mirror: rattibha; now on Claude blog)
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
  specify per-harness field-trust rules (erratum #2).
- **Review:** three independent sources (PostHog, honey judge panel, our RULE-7) converge on
  cross-model/cross-family review with writer≠reviewer — strengthens falsification-review
  rubric from convention to externally-corroborated practice.
- **Cost:** two levers, not one — model routing (our Pareto axis) and output-volume behavior
  (infracost/honey). H1 predicts routing moves volume ≤4%; the volume lever is where the other
  ~96% lives (pipeline change, CLI predicate pushdown, output format).
