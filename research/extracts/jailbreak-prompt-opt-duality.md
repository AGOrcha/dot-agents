# Extract: Jailbreaking == Prompt Optimization (@aminkarbasi / Amin Karbasi)

Source: research/articles/jailbreak-prompt-opt-duality.md
STATUS: PARTIAL — thesis + verified references only; full prose pending maintainer paste.

## Key claims (from article card + verified abstracts)
- Automated jailbreaking and automated prompt optimization are **two facets of the same coin**: the same iterative loop (propose candidate prompt -> evaluate -> score -> reflect/refine), differing only in objective (maximize a safety failure vs. maximize task performance).
- **"Prompt is a control surface."** The prompt is the optimization variable in both regimes; search/refinement machinery transfers between offense and utility.
- The two research communities should cross-pollinate: query efficiency + evaluator robustness (from jailbreaking) and multi-step pipeline abstractions (from prompt engineering).

## Techniques (verified lineages)
- Attack: PAIR (20-query black-box) -> TAP (tree-of-attacks search w/ pruning) -> Adversarial Reasoning (loss-guided test-time compute).
- Optimization: APE -> DSPy (compiled declarative pipelines) -> MIPROv2 (multi-stage instruction/demo opt) -> GEPA (reflective genetic-Pareto, NL reflection, up to 35x fewer rollouts) -> ACE (evolving agentic-context "playbooks," avoids "context collapse").

## What's novel
- A synthesis/position framing unifying two literatures under one feedback-loop abstraction. Reinforces that **reflective NL optimization + a robust evaluator/judge** is the shared engine.

## Mapping to our work
- **planner-evidence-backed-write-scope + verifier/reviewer profiles:** "the same loop, differing only in objective function" is exactly our verify/score/refine iteration loop. The evaluator-robustness emphasis maps to hardening our verifier stage as the *judge* that the whole optimization rides on — a weak judge breaks both safety and quality.
- **work-tracking §3A self-improvement loop:** GEPA/ACE "evolving playbooks that avoid context collapse" is the same shape as our operational view evolving skills/rules from result correlations. ACE's "context collapse" failure mode is a direct warning for any KG semantic-view summarization (cf. compaction-orchestrator's anti-pattern).
- **Security lens (new, not currently a spec):** "prompt is a control surface" + jailbreaking-as-optimization implies our agent/skill prompts and delegation bundles are an attack surface. As skills/rules become KG-canonical, we'd want a red-team/eval edge type. Currently unaddressed by our specs — candidate for a future proposal.
- **Concrete proposal idea:** Adopt GEPA/ACE-style *reflective* (not RL) optimization for evolving skills/rules in the operational view, with the verifier as the scoring judge — and add an adversarial-eval pass (jailbreak-as-optimization) as a gate before a skill/rule is promoted. Unifies dbreunig's "search the prompt" + kmad's "eval as boundary" + this duality.

## Caveats
- Full article prose NOT retrieved (paywalled X-native article). Thesis/tagline from the card; bullets grounded in card + verified arXiv abstracts. A "FAPO" label from one mirror is unverified and excluded. **PENDING maintainer paste of the full article.**
