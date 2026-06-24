# Jailbreaking and Prompt Optimization: Two Facets of the Same Coin

- **Author:** Amin Karbasi (@aminkarbasi) — VP & Chief AI Scientist, Cisco
- **Source tweet:** https://x.com/aminkarbasi/status/2069666945134375225
- **X long-form article:** https://x.com/i/article/2069652814331285504 (body paywalled / HTTP 402; no public mirror found)
- **Published:** 2026-06-24
- **Retrieved:** 2026-06-24
- **Status:** PARTIAL — full article prose NOT retrievable (X-native long-form article, paywalled, no cross-post/mirror located). The thesis + tagline (from the article card) and ALL referenced papers are verified. **Full prose PENDING maintainer paste.**

## Verified thesis (from article card / preview)

Automated jailbreaking and automated prompt optimization are "two facets of the same coin": both are iterative feedback loops (propose candidate -> evaluate response -> score -> refine), differing only in the objective function — one maximizes a safety failure, the other maximizes task performance. Tagline: **"Prompt is a control surface."** Recommendation: the security/red-teaming and prompt-optimization communities should cross-pollinate — query efficiency + evaluator robustness from jailbreaking; systematic multi-step pipeline abstractions from prompt engineering.

> The thesis above derives from the article preview card, not the full verbatim prose. The bullet synthesis below is grounded in that card plus the verified paper abstracts. A "FAPO" label surfaced by one mirror summarizer could NOT be matched to any verified arXiv ID and is treated as unverified.

## Verified referenced papers (titles/authors fetched directly from arXiv)

**Jailbreaking / adversarial (red-team) lineage:**
- arXiv:2310.08419 — **PAIR**: "Jailbreaking Black Box Large Language Models in Twenty Queries" (Chao, Robey, Dobriban, Hassani, Pappas, Wong)
- arXiv:2312.02119 — **TAP**: "Tree of Attacks: Jailbreaking Black-Box LLMs Automatically" (Mehrotra, Zampetakis, Kassianik, Nelson, Anderson, Singer, **Karbasi**)
- arXiv:2502.01633 — "Adversarial Reasoning at Jailbreaking Time" (Sabbaghi, Kassianik, Pappas, Singer, **Karbasi**, Hassani) — loss-signal-steered test-time compute; SOTA attack success.

**Prompt-optimization lineage:**
- arXiv:2211.01910 — **APE**: "Large Language Models Are Human-Level Prompt Engineers" (Zhou et al.)
- arXiv:2310.03714 — **DSPy**: "Compiling Declarative Language Model Calls into Self-Improving Pipelines" (Khattab, Singhvi, … Zaharia, Potts)
- arXiv:2406.11695 — **MIPROv2**: "Optimizing Instructions and Demonstrations for Multi-Stage Language Model Programs" (Opsahl-Ong, Ryan, … Zaharia, Khattab)
- arXiv:2507.19457 — **GEPA**: "Reflective Prompt Evolution Can Outperform Reinforcement Learning" — genetic-Pareto optimizer using NL reflection; beats GRPO by avg ~6% (up to 20%) with up to 35x fewer rollouts; beats MIPROv2 by >10%.
- arXiv:2510.04618 — **ACE**: "Agentic Context Engineering: Evolving Contexts for Self-Improving Language Models" — evolving-playbook contexts; +10.6% agents, +8.6% finance; avoids "context collapse."

## Concept synthesis (grounded in card + verified abstracts)

- **Unifying claim:** automated jailbreaking and automated prompt optimization are the same algorithmic loop (generate candidate prompt -> run model -> score -> reflect/refine), differing only in objective (elicit harm vs. maximize task quality).
- **"Prompt as control surface":** the prompt is the optimization variable in both regimes; the same search/refinement machinery transfers between offense and utility.
- **Attack lineage (PAIR -> TAP -> Adversarial Reasoning):** query-efficient black-box attacks -> tree-search with pruning -> loss-guided test-time-compute reasoning at SOTA.
- **Optimization lineage (APE -> DSPy -> MIPRO -> GEPA -> ACE):** LLM-authored instructions -> compiled declarative pipelines -> multi-stage instruction/demo optimization -> reflective genetic-Pareto evolution -> evolving agentic context "playbooks."
- **Shared technical levers:** evaluator/judge robustness, query/rollout efficiency, NL reflection, multi-step pipeline abstractions.
- **Positioning:** a synthesis/position piece (Karbasi co-authored TAP and Adversarial Reasoning) arguing the two communities should merge tooling and insights.
