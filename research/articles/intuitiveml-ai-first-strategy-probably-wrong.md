## Why Your "AI-First" Strategy Is Probably Wrong

**Source**: https://x.com/intuitiveml/status/2043545596699750791
**Author**: Peter Pang (@intuitiveml), Co-founder CreaoAI; previously GenAI at Meta (LLaMA), Apple
**Date**: 2026-04-13
**Method**: Playwright
**Word count**: ~3800 words

---

### Summary

Peter Pang describes how CREAO (25 employees, 10 engineers) dismantled their entire engineering process and rebuilt it around AI agents in two months, averaging 3–8 production deployments/day (vs. one release per sprint cycle before). The key distinction: AI-assisted (bolting AI onto existing workflow, 10–20% efficiency gain) vs. AI-first (redesigning architecture, org structure, and process around AI as primary builder). The piece covers their monorepo unification, six-phase CI/CD pipeline, self-healing feedback loop via Claude + CloudWatch + Linear, and the emergence of two engineer archetypes: the Architect (1–2 people designing SOPs for AI) and the Operator (everyone else).

---

### Body

99% of our production code is written by AI. Last Tuesday, we shipped a new feature at 10 AM, A/B tested it by noon, and killed it by 3 PM because the data said no. We shipped a better version at 5 PM. Three months ago, a cycle like that would have taken six weeks.

We didn't get here by adding Copilot to our IDE. We dismantled our engineering process and rebuilt it around AI. We changed how we plan, build, test, deploy, and organize the team. We changed the role of everyone in the company.

CREAO is an agent platform. Twenty-five employees, 10 engineers. We started building agents in November 2025, and two months ago I restructured the entire product architecture and engineering workflow from the ground up.

OpenAI published a concept in February 2026 that captured what we'd been doing. They called it harness engineering: the primary job of an engineering team is no longer writing code. It is enabling agents to do useful work. When something fails, the fix is never "try harder." The fix is: what capability is missing, and how do we make it legible and enforceable for the agent?

**AI-First Is Not the Same as Using AI**

Most companies bolt AI onto their existing process. An engineer opens Cursor. A PM drafts specs with ChatGPT. QA experiments with AI test generation. The workflow stays the same. Efficiency goes up 10 to 20 percent. Nothing structurally changes.

That is AI-assisted.

AI-first means you redesign your process, your architecture, and your organization around the assumption that AI is the primary builder. You stop asking "how can AI help our engineers?" and start asking "how do we restructure everything so AI does the building, and engineers provide direction and judgment?"

The difference is multiplicative.

I see teams claim AI-first while running the same sprint cycles, the same Jira boards, the same weekly standups, the same QA sign-offs. They added AI to the loop. They didn't redesign the loop.

A common version of this is what people call vibe coding. Open Cursor, prompt until something works, commit, repeat. That produces prototypes. A production system needs to be stable, reliable, and secure. You need a system that can guarantee those properties when AI writes the code. You build the system. The prompts are disposable.

**Why We Had to Change**

Last year, I watched how our team worked and saw three bottlenecks that would kill us.

**The Product Management Bottleneck**: PMs spent weeks researching, designing, specifying features. But agents can implement a feature in two hours. When build time collapses from months to hours, a weeks-long planning cycle becomes the constraint. PMs needed to evolve into product-minded architects who work at the speed of iteration.

**The QA Bottleneck**: After an agent shipped a feature, our QA team spent days testing corner cases. Build time: two hours. Test time: three days. We replaced manual QA with AI-built testing platforms that test AI-written code.

**The Headcount Bottleneck**: Our competitors had 100x or more people doing comparable work. We have 25. We couldn't hire our way to parity. We had to redesign our way there.

**The Bold Decision: Unifying the Architecture**

Our old architecture was scattered across multiple independent systems. A single change might require touching three or four repositories. From a human engineer's perspective, it is manageable. From an AI agent's perspective, opaque. The agent can't see the full picture. It can't reason about cross-service implications. It can't run integration tests locally.

I had to unify all the code into a single monorepo. One reason: so AI could see everything.

This is a harness engineering principle in practice. The more of your system you pull into a form the agent can inspect, validate, and modify, the more leverage you get. A fragmented codebase is invisible to agents. A unified one is legible.

I spent one week designing the new system: planning stage, implementation stage, testing stage, integration testing stage. Then another week re-architecting the entire codebase using agents. CREAO is an agent platform. We used our own agents to rebuild the platform that runs agents.

**The Stack**

**Infrastructure: AWS** — auto-scaling container services, circuit-breaker rollback. CloudWatch is the central nervous system: structured logging across all services, 25+ alarms, custom metrics queried daily by automated workflows. If AI can't read the logs, it can't diagnose the problem.

**CI/CD: GitHub Actions** — every code change passes through a six-phase pipeline: Verify CI → Build and Deploy Dev → Test Dev → Deploy Prod → Test Prod → Release. The CI gate enforces typechecking, linting, unit and integration tests, Docker builds, end-to-end tests via Playwright, and environment parity checks. No manual overrides. The pipeline is deterministic, so agents can predict outcomes and reason about failures.

**AI Code Review: Claude** — every pull request triggers three parallel AI review passes using Claude Opus 4.6:
- Pass 1: Code quality — logic errors, performance issues, maintainability
- Pass 2: Security — vulnerability scanning, authentication boundary checks, injection risks
- Pass 3: Dependency scan — supply chain risks, version conflicts, license issues

These are review gates, not suggestions. When you deploy eight times a day, no human reviewer can sustain attention across every PR.

**The Self-Healing Feedback Loop**

Every morning at 9:00 AM UTC, an automated health workflow runs. Claude Sonnet 4.6 queries CloudWatch, analyzes error patterns across all services, and generates an executive health summary delivered via Microsoft Teams.

One hour later, the triage engine runs. It clusters production errors from CloudWatch and Sentry, scores each cluster across nine severity dimensions, and auto-generates investigation tickets in Linear. Each ticket includes sample logs, affected users, affected endpoints, and suggested investigation paths. The system deduplicates. If an open issue covers the same error pattern, it updates it. If a previously closed issue recurs, it detects the regression and reopens.

When an engineer pushes a fix, the same pipeline handles it. Three Claude review passes. CI validation. Six-phase deploy pipeline. After deployment, the triage engine re-checks CloudWatch. If the original errors are resolved, the Linear ticket auto-closes.

**Feature Flags and Supporting Stack**

Statsig handles feature flags. Every feature ships behind a gate: enable for the team, then gradual percentage rollout, then full release or kill. The kill switch toggles a feature off instantly, no deploy needed. Bad features die the same day they ship.

Graphite manages PR branching: merge queues rebase onto main, re-run CI, merge only if green. Stacked PRs allow incremental review at high throughput.

Sentry reports structured exceptions merged with CloudWatch by the triage engine. Linear is the human-facing layer: auto-created tickets with severity scores, sample logs, deduplication, and follow-up verification that auto-closes resolved issues.

**The Results**

Over 14 days, we averaged three to eight production deployments per day. Under our old model, that entire two-week period would have produced not even a single release to production.

Bad features get pulled the same day they ship. New features go live the same day they're conceived. A/B tests validate impact in real time.

People assume we're trading quality for speed. User engagement went up. Payment conversion went up. We produce better results than before, because the feedback loops are tighter. You learn more when you ship daily than when you ship monthly.

**The New Engineering Org**

Two types of engineers will exist.

**The Architect**: One or two people. They design the standard operating procedures that teach AI how to work. They build the testing infrastructure, the integration systems, the triage systems. They decide architecture and system boundaries. They define what "good" looks like for the agents. This role requires deep critical thinking. You criticize AI. You don't follow it. When the agent proposes a plan, the architect finds the holes.

This is also the hardest role to fill.

**The Operator**: Everyone else. AI assigns tasks to humans. The triage system finds a bug, creates a ticket, surfaces the diagnosis, and assigns it to the right person. The person investigates, validates, and approves the fix. AI makes the PR. The human reviews whether there's risk.

**Who Adapts Fastest**

I noticed a pattern I didn't expect. Junior engineers adapted faster than senior engineers.

Junior engineers with less traditional practice felt empowered. They had access to tools that amplified their impact. They didn't carry a decade of habits to unlearn.

Senior engineers with strong traditional practice had the hardest time. Two months of their work could be completed in one hour by AI. That is a hard thing to accept after years of building a rare skill set.

In this transition, adaptability matters more than accumulated skill.

**The Human Side**

Two months ago, I spent 60% of my time managing people. Today: below 10%. I went from managing to building. I code from 9 AM to 3 AM most days.

My relationships with co-founders and engineers are better than before. Before the transition, most of my interaction with the team was alignment meetings. Those conversations are necessary in a traditional model. They're also draining. Now we talk about other things.

We don't fire an engineer because they introduced a production bug. We improve the review process. We strengthen testing. We add guardrails. The same applies to AI.

**Beyond Engineering**

If engineering ships features in hours but marketing takes a week to announce them, marketing is the bottleneck. At CREAO, we pushed AI-native operations into every function: product release notes (AI-generated from changelogs), feature intro videos (AI-generated motion graphics), daily posts on socials (AI-orchestrated and auto-published), health reports and analytics summaries (AI-generated from CloudWatch and production databases).

**What This Means**

For engineers: your value is moving from code output to decision quality. Product sense or taste matters. Can you look at a generated UI and know it's wrong before the user tells you?

For CTOs: if your PM process takes longer than your build time, start there. Build the testing harness before you scale agents. Fast AI without fast validation is fast-moving technical debt. Start with one architect.

For the industry: I believe one-person companies will become common. If one architect with agents can do the work of 100 people, many companies won't need a second employee.

The competitive advantage is the decision to redesign everything around these tools, and the willingness to absorb the cost. The cost is real: uncertainty among employees, the CTO working 18-hour days, senior engineers questioning their value. We absorbed that cost. Two months later, the numbers speak.

We build an agent platform. We built it with agents.

---

### Key Quotes

> "AI-first means you redesign your process, your architecture, and your organization around the assumption that AI is the primary builder. You stop asking 'how can AI help our engineers?' and start asking 'how do we restructure everything so AI does the building, and engineers provide direction and judgment?'"

> "The more of your system you pull into a form the agent can inspect, validate, and modify, the more leverage you get. A fragmented codebase is invisible to agents. A unified one is legible."

> "I told a reporter from Business Insider: 'AI will make the PR and the human just needs to review whether there's any risk.'"

> "Junior engineers adapted faster than senior engineers... In this transition, adaptability matters more than accumulated skill."

> "Build the testing harness before you scale agents. Fast AI without fast validation is fast-moving technical debt."
