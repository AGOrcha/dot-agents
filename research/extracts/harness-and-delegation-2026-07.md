# Distilled takeaways — delegation-economics / loop-craft / harness-config batch (2026-07-13)

Six sources, full bodies in `research/articles/`. Full rubric evaluation:
`research/articles-evaluation-kg-and-adjacent.md` Part L. Batch headline: these supply the
**measured anchors** (L.1 Cognition N=3,000; L.5 LangChain +10–20pt; L.3 ATG paper) for the
delegation-economics + model-routing clusters that Parts J/K corroborated only from linkless
growth-account sources. Frozen-plan (transcript-analysis) items are corroboration-first;
config/pipeline/runtime items route live to their own plans.

## 1. joon-lee (Cognition/Devin): Making Fable Cheaper Than Opus — `measured-with-method`
- **Claim:** across 3,000 instrumented sessions on FrontierCode 1.1, Fable+cheap-sidekick
  costs LESS than Opus+sidekick ($1.86 vs $2.04) AND scores higher (60.7 vs 54.6), despite
  Fable being 2× per-token — because cost is dominated by lead turns (11.5 vs 26.5), context
  dragged (545k vs 1,679k tok), and what the lead decides NOT to do (Fable lead makes zero
  edits in 81% of runs). Mechanism = management style: delegate early, constraint-enumerating
  design-doc briefs, don't pull work back. O(1)-constraint brief → sidekick scored 94; Opus
  hand-impl silently dropped the constraint → 25.
- **Extract:** the measured member of K.3's asserted "96%/46%" + "Fable=manager/Sol=worker"
  class. NEW axis vs H1: cost variance lives in the LEAD's delegation behavior, not the
  executor swap our cells vary. Brief quality is now a measured mediator of correctness AND
  cost → sharpens delegation-bundle authoring. → plan digest `evidence/prior/` as a measured
  prior (additive, gate-neutral); orchestrator-model-swap = parked post-freeze axis.

## 2. blomfield/guilleflor: self-improving company — `asserted`/`practitioner-report`
- **Claim:** every company function = a recursive loop: sensor → policy (autonomous vs human
  sign-off) → tool (deterministic APIs) → quality-gate+learning (failures loop to top). The
  leap = overnight self-repair agent (diagnose→PR→merge→deploy, no human). "Record everything,
  then diarize+synthesize" (can't dump raw into context). YC manual: 2,000 hrs → 150 pages in
  a weekend, self-updates monthly [UNVERIFIED n=1].
- **Extract:** another loop taxonomy (maps to J.5 execution/task/product/system). Function-
  layer framing complements the runtime-layer loops. "Diarize+synthesize" = episodic view
  stores synthesized provenance-bearing traces, not raw. Corroboration only; digest note.

## 3. intuitmachine/arXiv 2607.01942: Atomic Task Graph (ATG) — `measured-with-method` (abstract)
- **Claim:** replace text-trajectory execution substrate with an explicit DAG (node=tool call,
  edge=data dep). Interface-preserving recursion + dependency-aware parallel execution +
  minimal repair (fix only the affected subgraph, freeze validated regions). Abstract:
  "consistently outperforms across ALFWorld/WebShop/ScienceWorld with 7–8B backbones,"
  training-free. Thread's specific %s (20–40% step / 70%+ hallucination / 3× recovery) are
  [UNVERIFIED] vs paper body.
- **Extract:** paper-grade anchor for a bet we already make (TASKS.yaml depends_on IS the DAG).
  Sharpen: the DAG stops at the task boundary — in-stage we still hand agents a text
  trajectory (context-rot surface). Minimal-repair "freeze validated regions on re-run" =
  candidate for full-loop-orchestration-runtime bounded re-entry (don't re-verify passing
  disjoint slices). → live fold-back to full-loop-orchestration-runtime.

## 4. jasonzhou (SuperDesign): loop engineering, 1 month — `practitioner-report`
- **Claim:** loop = contract (Goal/Boundaries/SOP — the boundary/fence is what lets you walk
  away) + state+logs (state absorbs earned lessons; worth more in month 3 than week 1) +
  /verify (evidence-producing; "a PR arrives with a video"; verifier decides if a loop is even
  a good idea) + trigger (for-loop/time/event; gate cheaply so empty runs cost nothing).
  Scales to orchestrator+executor+verifier; evolve role reads last dozen runs and edits the
  loop itself ("a loop to improve the loop"). Concrete contract templates open-sourced.
- **Extract:** fullest external articulation of our loop stack — contract≈active.loop.md+rules,
  state+logs≈workflow state+iteration-log, verify≈verifier profiles+evidence, O+E+V≈ISP roles,
  **evolve≈fold-back→lesson→skill distillation** (= J.5 system loop). Boundary/fence
  corroborates loop-discipline-stop-hooks. Cheap-gate trigger = parked (needs the scheduler
  daemon; lesson agents-lack-autonomous-timers). Sharpen + digest.

## 5. vtrivedy/LangChain: per-model harness profiles — `measured-opaque`→`measured-with-method`
- **Claim:** harness profile = declarative override layer (system-prompt prefix/suffix, tool
  inclusion+naming, middleware, subagent config, skills) keyed per model/provider; Python or
  YAML; call-site unchanged; ship OpenAI/Anthropic/Google defaults; override/layer/plugin.
  Codex→apply_patch + shell_command alias + parallel-tool-batch prompt; Opus→tool_usage +
  tool_result_reflection blocks. Measured +10–20pt on a curated hard tau2-bench subset. "Same
  model, different harness, very different score" (Terminal-Bench: Claude Code harness ranks
  last among Opus-4.6; harness-only changes took gpt-5.2-codex 52.8→66.5).
- **Extract:** FIRST measured anchor for the per-model harness-config lever (axis J/K skipped).
  Our stage_profiles + execution_profile + config-transitive-layering + craft §6 capability
  mask, independently invented and measured to matter. GAP-ADOPT candidate: do our
  stage_profiles key on model-family, or only stage/app_type? Per-model tool-naming +
  prompt-suffix layer → live fold-back / proposal to pipeline-architect + config-transitive-
  layering.

## 6. thealexker: architect's guide to harness engineering — `practitioner-report`/`asserted`
- **Claim:** buy/customize/build by role; 3 categories (frameworks/SDKs → extensible →
  turnkey); decision = harness-task fit + fluency; 8 diagnostic properties (context+state,
  memory, MCP/tool, standard adherence, model-selection, remote access, observability/self-
  repair, hackability); future = route (classifier → cheap/fine-tuned/frontier; cache-aware
  per model) + serve (BYO-model/dedicated infra). Cites L.5 (not independent — ~1.5 sources).
- **Extract:** 8-property rubric = a self-assessment for dot-agents-as-a-harness (weak axes:
  remote access, self-repair/rollback observability → parked docs pass). Cache-aware routing =
  small operational note for model:-per-agent routing (routing mid-session incurs cache miss).
  "Standard adherence: open vs proprietary" = config-v2 portability framing.

## Cross-cutting (see Part L.7)
1. Three clusters got their first measured member: delegation economics (L.1), per-model
   harness-config (L.5), plan-as-graph (L.3). Never pool the numbers (different instruments).
2. L.1 splits the delegation lever: executor-swap (H1-bounded) vs lead delegation-behavior
   (large, un-instrumented) — orthogonal axes; the latter is a candidate post-freeze wave.
3. Brief quality is now a MEASURED mediator (L.1 O(1) example) → sharpens bundle authoring +
   J.6 Deviations + J.3 never-compress-contract-fields into one rule.
4. Graph/text substrate has a paper (L.3); exposes our remaining gap (DAG stops at task
   boundary); minimal-repair + L.1 "distrust doesn't help" → don't re-verify passing slices.
5. Loop-anatomy convergence is 4-deep (L.4 + L.2 + J.5 + K.2); evolve = sharpest name for
   fold-back→lesson distillation.

---

## Part M addendum — Bilevel Autoresearch (meta-loop paper, arXiv 2603.23420v2, 2026-06-02)

## 7. Qu & Lu: Bilevel Autoresearch — Meta-Autoresearching Itself — `measured-with-method` (PRELIMINARY)
- **Claim:** an OUTER loop meta-optimizes the INNER autoresearch loop by generating + injecting
  new search MECHANISMS as code at runtime (not just tuning parameters); same LLM at both
  levels. Full bilevel (Group C) −0.045 vs inner-only −0.009 val_bpb (~5×); parameter-only
  change (Group B) gives NO reliable gain — mechanism change does. Mechanisms drawn from tabu
  search / bandits / DoE without human spec; win attributed to breaking the inner loop's
  deterministic search patterns (concrete: found smaller batch helped, against a "bigger is
  better" prior). Carrier generalizes beyond code to "skills, prompts, workflows, scripts,
  evaluators, domain principles, world-model assumptions, memory schemas." Caveats — n=3, one
  benchmark, one bilevel step, runtime injection fragile (silent fallback dangerous, arbitrary
  imports), no stability guarantee.
- **Extract:** theoretical anchor for the dot-agents self-improvement/meta-loop thesis —
  MECHANISM change (new lesson/skill/rule/evaluator) ≫ PARAMETER change (tweak a task note) is
  now measured, not asserted. "Breaking deterministic patterns / exploring what priors avoid" =
  RULE-7 diversity + cross-family at the meta level. Failure modes corroborate the
  skill-validation gate (lesson use-skill-architect-for-skill-generation, updated). Bilevel-as-
  our-meta-loop = parked lead (trigger — meta-loop runtime + validation-gate maturity; we are
  behind on autonomy, ahead on the safety/rollback envelope the paper lacks).
