# Distilled takeaways — agent-efficiency / review / loop-craft batch (2026-07-12)

Eight sources, full bodies in `research/articles/`. Full rubric evaluation:
`research/articles-evaluation-kg-and-adjacent.md` Part J. Plan-local consumer map (which plan
artifact consumes each finding, incl. local verification runs):
`.agents/workflow/plans/transcript-analysis-and-pipeline-craft/evidence/prior/external-references-2026-07-12.md`.
This file is the research-side concept extract; the plan digest is authoritative for consumers.

## 1. CC JSONL token undercount (Magnus Gille, gille.ai + claude-code-energy-monitor)
- **Claim (author-flagged PRELIMINARY):** CC JSONL `usage.input_tokens` is a never-finalized
  streaming placeholder; duplicates share a `requestId`; only cache_read/cache_creation are
  high-trust; statusbar context-window totals are ground truth.
- **Extract:** dedup by requestId (last-entry-wins), exclude ≤1 placeholders, trust cache
  fields, validate field semantics via statusbar deltas. Local verification revised the
  severity (12% placeholders, not 75%; 76% duplicate requestIds; per-field overcounts ~2.4–3×).
  → pareto erratum #2; candidate: graduate to an evidence-rubric clause (per-harness
  token-field-trust labels on every cost row).

## 2. Infracost: CLI redesigned for agent callers (−79% output tokens, hard bucket)
- **Claim:** predicate pushdown flags + TOON tabular output moved accuracy 45%→100% and cost
  $7.61→$2.48 on the hard bucket — no model change.
- **Extract:** the strongest external anchor for H1 (to move volume, change the pipeline/tool
  surface, not the model). Their 16q×3cfg×5rep rerun/rescore harness ≈ our
  pareto-measurement-rubric. → candidate fold-back: predicate flags + tabular output mode on
  `da workflow eligible/tasks`.

## 3. honey-for-devs (GreenPT): output-volume compression skill family
- **Claim:** three levers (less code / less prose / denser A2A handoffs, ESON −51% lossless);
  4-model cross-family judge panel; published NEGATIVE result — input-prompt precompression
  saves ~0% on 266 real prompts.
- **Extract:** the bill lives in outputs, not the input prompt (corroborates #2 + H1). Their
  judge panel = independent invention of RULE-7. Import the carve-out rule with any dense
  handoff format: compress prose, never contract fields. Don't import score-averaging for
  blocking gates (vetoes ≠ averages).

## 4. PostHog: stop being the code review bottleneck
- **Claim:** review as a pipeline — 4-lens qa-swarm, actionable/nit/ambiguous triage, ≤3-pass
  outer loop, writer never self-reviews, different providers per reviewer; StampHog fail-closed
  deterministic gates BEFORE an LLM check (README authority: >800 substantive lines / >30
  files; the newsletter's 500/20 is stale), 1/3 of merged PRs auto-stamped; "verify by
  observation, not reasoning."
- **Extract:** closest production analogue of craft §4's spine; their ≤3 bound ≈ our
  target_count: 3. → candidate fold-back: deterministic pre-LLM gate tier for delegation/review
  (fail closed; LLM tightens, never loosens); triage taxonomy → fold-back routing labels.
  Paul's "60% of token spend on CI/review toil" externally checks H6 (review is the cheap-route
  target).

## 5. Arize (@aparnadhinak): four loops taxonomy
- **Claim:** "the loop" = 4 architectures (execution / task / product / system) + the human
  oversight loop; fan-out is topology, not a loop; "a loop without its signal doesn't
  converge."
- **Extract:** naming layer for our stack — inner swarm = execution; bounded fold-back = task;
  N-plan driver = product; pareto-live-waves + fold-back routing = system; user gate =
  oversight. First architect question in pipeline-architect: which loop are you editing? Our
  typed quiescence/exit conditions are the wired version of their argument.

## 6. Thariq (Anthropic): field guide to Fable — finding your unknowns
- **Claim:** quality is bottlenecked by clarifying unknowns (4 quadrants); techniques per phase
  (blind-spot pass, prototypes, one-question interviews, references-as-source-code, deviations
  log, quiz-gated merge).
- **Extract:** KG readback before fanout = the blind-spot pass; "questions whose answer would
  change the architecture" = fork-resolution ordering. → candidate: `Deviations` section in the
  merge-back template (makes plan-vs-actual drift queryable).

## 7. Michael Lynch: how to write an effective design doc
- **Claim:** investment scales with cost-of-being-wrong; goals in impact terms; explicit
  non-goals; SLOs before monitoring.
- **Extract:** validates the workflow-artifact-model spec tier. → candidates: "penalty for
  being wrong" as a section-inclusion test in the spec convention; explicit non-goals block
  (non-goal = never; deferred = later — conflating them re-surfaces settled scope).

## Cross-cutting (see Part J.8 for the full synthesis)
1. Token-field trust is per-harness ingest metadata, not scorer code.
2. Two cost levers: model routing (H1-bounded, ≤~4% volume) vs output-volume behavior (the
   other ~96% — CLI design + compression).
3. Cross-family writer≠reviewer review: three independent inventions (PostHog, honey, RULE-7).
4. Gate ordering: deterministic before LLM, bounded before terminal.
5. Exit signals must be wired, not named — the transcript pipeline is the wiring.
