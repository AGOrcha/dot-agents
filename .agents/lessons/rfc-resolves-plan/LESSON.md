---
name: rfc-resolves-plan
description: Plans that say "requires RFC before coding" often already contain the design decisions — write a brief RFC that makes them explicit, then implement immediately in the same session
type: feedback
---

When a plan says "requires RFC before coding" with a list of open questions, check whether the spec's own "Resolved Direction" section or the plan's implementation steps implicitly answer those questions. If they do:

1. Write a brief RFC that makes the decisions explicit (1-2 pages, not a research exercise)
2. Mark it `Status: Accepted` in the same commit
3. Implement in the same session

Exception: if the user explicitly scopes the session to docs/research/planning only, stop after the RFC plus plan/loop alignment. Move implementation into `What's Next` or the active plan instead of writing code immediately.

**Why:** RFC-gated plans are blocked on design clarity, not implementation effort. If the decisions are already resolved in the spec or plan, the RFC is a formalization step, not a research step. Spending a whole iteration on "research" when the answers are available wastes a cycle.

**How to apply:** Before treating a plan's RFC requirement as "do research this iteration, implement next iteration," scan the spec's "Resolved Direction" sections and the plan's own implementation steps. If the open questions can be answered from existing material, write the RFC and implement together unless the user explicitly wants an RFC-only session.

Counterpoint: if the questions are genuinely open (e.g., they involve tradeoffs not yet evaluated), research first.
