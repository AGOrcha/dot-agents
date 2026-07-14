## Jailbreakers and Prompt Optimizers Are Looping the Same Problem

**Source**: https://x.com/aminkarbasi/status/2069666945134375225
**X-native article**: https://x.com/i/article/2069652814331285504
**Author**: Amin Karbasi (@aminkarbasi) — VP & Chief AI Scientist, Cisco
**Date**: 2026-06-24
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~1,100 words
**Engagement**: 3 replies, 6 reposts, 20 likes, 13 bookmarks, 2.8K views

---

### Summary

A synthesis/position piece arguing that automated jailbreaking and automated prompt optimization are "two facets of the same coin": both are the same iterative feedback loop (propose candidate prompt -> evaluate response -> score -> reflect/refine), differing only in the objective function — one maximizes a safety failure, the other maximizes task performance. "The prompt is a control surface." Each community owns lessons the other needs: jailbreakers have hard-won experience with imperfect judges, black-box search, query budgets, transferability, and evaluator hacking; prompt-optimization has better abstractions for programs, traces, metrics, and data splits. Closes by introducing FAPO (Fully Automated Prompt Optimization), which runs the whole loop through coding agents (Codex/Claude Code): evaluate, classify failures, target dominant failure modes, review variants, and accept only changes that beat the previous best.

---

### Body

Automated jailbreaking and automated prompt optimization are usually treated as two different research areas.

I think they are two facets of the same coin.

In automated jailbreaking, the goal is to find a prompt that causes an aligned model to violate its safety behavior. In automated prompt optimization, the goal is to find a prompt or context that makes a model perform better than a generic prompt on a task, benchmark, or workflow.

The objectives are different. One is adversarial. The other is constructive.

But the optimization problem is strikingly similar: keep the model weights fixed, change only the input text, observe the model's behavior, and use feedback to propose a better prompt.

That loop is the common object.

The idea was popularized by @steipete by dates a few years back [sic]

The basic structure behind many automated jailbreak methods is simple:

- An attacker proposes a candidate prompt.
- A target model responds.
- A judge or evaluator scores the response.
- Feedback is sent back to the attacker.
- The attacker uses that feedback to generate a better candidate.

(PAIR; Chao et al., 2023)

PAIR, or Prompt Automatic Iterative Refinement, made this very explicit: use an attacker LLM to generate and refine jailbreak prompts against a separate target LLM, with only black-box access to the target. TAP, or Tree of Attacks with Pruning, pushed this further by searching over many candidate attacks and pruning prompts that were unlikely to succeed before spending target-model queries on them. More recent work on adversarial reasoning at jailbreaking time brought in test-time computation and loss-guided search to make the attack process even more systematic.

Sources: (PAIR; Chao et al., 2023), (TAP; Mehrotra et al., 2024), (Adversarial reasoning; Sabbaghi et al., 2025), (Scaling; Snell et al., 2024).

These papers are usually discussed in the language of AI safety and security. But algorithmically, they are also prompt optimizers.

They are optimizing over natural language strings. They are using an evaluator. They are learning from failed attempts. They are managing query budgets. They are worrying about transferability, evaluator reliability, and overfitting to a judge.

That is exactly the language of automated prompt optimization too.

To be precise, automatic prompt engineering has its own history. APE, for example, treated the instruction as a program to be searched over and selected with a score function. DSPy and MIPRO made this more systematic for multi-stage language-model programs. Recent systems such as GEPA and ACE have made the loop even more explicit: run the system, inspect traces or execution feedback, reflect on failures, update the prompt or context, and keep the changes that improve downstream performance.

So the real distinction is not the mechanism. The distinction is the objective function.

In jailbreaking, the optimizer is trying to maximize the probability of a safety failure. In prompt optimization, the optimizer is trying to maximize task performance, reliability, or user preference. The same machinery can be pointed in either direction.

This is why I think the two communities should be talking more.

People working on automated jailbreaking have spent a lot of time studying what happens when prompts are optimized against imperfect judges. They have practical experience with black-box search, query efficiency, transferability across models, guardrail robustness, and evaluator hacking. These are not side issues. They are central to building reliable prompt optimization systems.

At the same time, the prompt optimization community has developed better abstractions for programs, traces, metrics, data splits, and systematic improvement of multi-step LLM pipelines. Those tools can make AI safety evaluations more rigorous, scalable, and reproducible.

The shared lesson is that the prompt is not just a wrapper around the model.

Prompt is a control surface.

If we can automatically optimize prompts to make models more useful, we can also automatically optimize prompts to make models fail. And if we want frontier models to be robust, we should study both sides of that optimization problem together.

The constructive version and the adversarial version are not separate worlds. They are similar feedback loops with a different sign on the reward.

That should be an opportunity: security researchers can help prompt optimization avoid brittle benchmark chasing, and prompt optimization researchers can help security build stronger, more systematic stress tests.

This is also the motivation behind FAPO. We were trying to make the entire feedback loop more precise, agent-ready, and production-ready by automating it through coding agents such as Codex and Claude Code. Instead of only asking a model to rewrite a prompt, FAPO optimizes the whole pipeline: it runs evaluations, classifies failures, identifies the dominant failure modes, generates new variants targeted at those failures, reviews the proposed changes, compares each variant against the previous best version, accepts it only if it improves the system, and rejects it otherwise. Then it iterates. The result is a more disciplined version of the same loop: generate, evaluate, diagnose, improve, and only keep changes that actually move the pipeline forward.

Fully Automated Prompt Optimization

Of course, this is not meant to be a comprehensive overview of either automated jailbreaking or automated prompt optimization. There are many important papers, systems, benchmarks, and implementation details that I am not covering here. The main point is narrower: despite differences in objective, terminology, and engineering choices, these methods are strikingly similar at the level of the optimization loop. It is more useful to study them under the same light, and to let ideas move between the security community and the prompt optimization community.

#### Referenced papers (verified directly on arXiv)

Jailbreaking / adversarial lineage:
- arXiv:2310.08419 — **PAIR**: "Jailbreaking Black Box Large Language Models in Twenty Queries"
- arXiv:2312.02119 — **TAP**: "Tree of Attacks: Jailbreaking Black-Box LLMs Automatically" (co-author Karbasi)
- arXiv:2502.01633 — "Adversarial Reasoning at Jailbreaking Time" (co-author Karbasi)

Prompt-optimization lineage:
- arXiv:2211.01910 — **APE**: "Large Language Models Are Human-Level Prompt Engineers"
- arXiv:2310.03714 — **DSPy**: "Compiling Declarative Language Model Calls into Self-Improving Pipelines"
- arXiv:2406.11695 — **MIPROv2**: "Optimizing Instructions and Demonstrations for Multi-Stage Language Model Programs"
- arXiv:2507.19457 — **GEPA**: "Reflective Prompt Evolution Can Outperform Reinforcement Learning"
- arXiv:2510.04618 — **ACE**: "Agentic Context Engineering: Evolving Contexts for Self-Improving Language Models"

---

### Key Quotes

> "keep the model weights fixed, change only the input text, observe the model's behavior, and use feedback to propose a better prompt. That loop is the common object."

> "Prompt is a control surface."

> "They are similar feedback loops with a different sign on the reward."

> "generate, evaluate, diagnose, improve, and only keep changes that actually move the pipeline forward."

---

### Extraction Notes

Full body recovered 2026-07-12 via Claude-in-Chrome with a logged-in x.com session — this supersedes the earlier NEEDS-PASTE status (the prior unauthenticated Playwright pass could only reach the article card). Body verbatim, including the odd sentence "The idea was popularized by @steipete by dates a few years back" (sic). The tweet page rendered the timestamp as 2:20 AM · Jun 24, 2026 in this session (earlier pass showed 6:20 AM — timezone rendering difference). The arXiv reference list was previously verified directly against arXiv and is retained.
