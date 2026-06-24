## Jailbreakers and Prompt Optimizers Are Looping the Same Problem

**Source**: https://x.com/aminkarbasi/status/2069666945134375225
**X-native article**: https://x.com/i/article/2069652814331285504 (body LOGIN-WALLED)
**Author**: Amin Karbasi (@aminkarbasi) — VP & Chief AI Scientist, Cisco
**Date**: 2026-06-24 (tweet timestamp 6:20 AM, verified via Playwright)
**Method**: Playwright (x.com tweet + article-card snapshot via `article-extract` skill, Path B) — article body could not be reached
**Word count**: card excerpt only (~55 words); full body NOT retrieved

**STATUS: NEEDS-PASTE (x.com article body login-walled).** This is the only one of the five whose body is not recoverable — it is an X-native long-form Article with no public author cross-post. See Extraction Notes.

---

### Summary

A synthesis/position piece arguing that automated jailbreaking and automated prompt optimization are "two facets of the same coin": both are the same iterative feedback loop (propose candidate prompt -> evaluate response -> score -> reflect/refine), differing only in the objective function — one maximizes a safety failure, the other maximizes task performance. The framing "prompt is a control surface" implies the two research communities should cross-pollinate. Verified via the embedded reference list (8 arXiv papers spanning the jailbreaking and prompt-optimization lineages).

---

### Body

> Full article body NOT retrieved — login-walled. What follows is the verbatim article CARD text exposed on the tweet page (the article's title + opening lines), captured via Playwright:

**Title (verbatim from card):** "Jailbreakers and Prompt Optimizers Are Looping the Same Problem"

**Opening excerpt (verbatim from card, truncated by X):**

> "Automated jailbreaking and automated prompt optimization are usually treated as two different research areas. I think they are two facets of the same coin. In automated jailbreaking, the goal is to…"

(The card truncates here with an ellipsis; the remaining body is behind the X login wall.)

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

> "Automated jailbreaking and automated prompt optimization are usually treated as two different research areas. I think they are two facets of the same coin." (verbatim, from the article card)

> "Prompt is a control surface." (article tagline)

*(No further verbatim quotes available — article body is login-walled; quotes beyond the card excerpt are not reproduced to avoid fabrication.)*

---

### Extraction Notes

Retrieved via the `article-extract` skill Path B (Playwright). Sequence and findings:
1. Navigated `https://x.com/aminkarbasi/status/2069666945134375225` — Playwright (unauthenticated, no stored X session) loaded the tweet page and the article card, exposing the verbatim article **title**, the opening excerpt above, and the exact timestamp (6:20 AM · Jun 24, 2026). This is MORE than a prior mirror (fxtwitter) returned.
2. Navigated the full article `https://x.com/i/article/2069652814331285504` — Playwright was **redirected to an X login wall** (`/i/jf/onboarding/web?...mode=login`, "Happening now." sign-up page). The article body is therefore not reachable without an authenticated X session, which is not available in this environment.
3. No public author cross-post (blog/Substack/site) for this piece was located, unlike the other four articles.

**Outcome:** Tooling and Playwright are fully functional (verified against all five tweets and the four recoverable bodies); this specific article *body* is gated by X's login wall — the documented `article-extract` gotcha ("Twitter/X requires a logged-in session for most content … tell the user — don't return an empty body silently"). The verified card title/excerpt/timestamp and the 8 reference papers are recorded above; the full prose remains **NEEDS-PASTE** for a maintainer with an authenticated X session.
