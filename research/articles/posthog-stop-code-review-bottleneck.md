## Stop being the code review bottleneck

**Source**: https://x.com/posthog/status/2075645235724767739
**Author**: PostHog (@posthog) — words by @jinayoon_, practices from @pauldambra, Daniel Visca, Pawel Cebula, @haacked
**Date**: 2026-07-10 (2:16 PM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~1,700 words
**Engagement**: 14 replies, 31 reposts, 306 likes, 713 bookmarks, 76.9K views

---

### Summary

Agents write code faster than humans can review, so the fix is not reviewing faster — it's reviewing as little as possible by building a delegation pipeline. Four workflow changes from PostHog engineers, each with a steal-this prompt: (1) agent review swarms — qa-swarm spawns four reviewers with different lenses (qa-team, security-audit, a personal-voice reviewer, an XP reviewer), review-triage classifies threads into actionable/nit/ambiguous, outer loop iterates up to 3× (never the authoring agent; mix models/providers); (2) delegate PR babysitting (CI monitoring, flaky reruns, branch freshness) to a loop; (3) StampHog auto-approval agent for low-risk PRs — deterministic gates (PR state, deny-list blast radius, <500 lines/20 files) plus an LLM showstopper check, fail-closed, ~1 in 3 merged PRs and 1.6K PRs/month; (4) verify by observation, not reasoning — decompose big changes into stacked, independently runnable PRs (Graphite), each shipping with an observable check; screenshots/GIFs for frontend.

### Relevance to dot-agents

The closest published analogue to the full-loop orchestration runtime's review stage: multi-lens reviewer fanout + triage into actionable/nit/ambiguous is structurally the reviewer-lens agents + parent verdict gate, and "different models and providers for different reviewers" is the cross-family adversarial gate. StampHog's fail-closed deterministic gating with an LLM that can tighten but never loosen is the same verdict-gating principle as the CC runner fix. "Verify by observation, not reasoning" is the /verify skill's thesis and the ISP verifier stage. Paul's "60% of my token spend is burned automating the toil of handling CI and review" is a useful benchmark for the token-accounting work.

### Body

Agents are writing code faster than any human can review.

The naive solution would be for developers to review code faster. The 500 IQ take is for developers to review as little code as possible.

If you need to be involved in every code review, you will always be the bottleneck. Instead, put yourself outside of the code review loop by building a pipeline that delegates tasks to agents.

We asked engineers at PostHog how they've been reviewing AI-generated code to keep shipping fast without losing quality.

Here are four workflow changes you can steal (with prompts included) to make your life easier.

**1. Make agents review code for you**

The number one thing to add, if you haven't yet, is a way for agents to review code for you.

The goal is to offload the simpler reviews to agents, and flag if something genuinely needs a human.

The key is that the agent that wrote the code can't be the one that reviews it. Agents are bad at checking their own work since they're often unaware of their own blind spots. For the same reason, it's better to have multiple agents with different instructions and goals to cover more gaps, as well as different models and providers for different reviewers.

Here's how one of our engineers, @pauldambra, makes his own custom agent review system work:

First, qa-swarm spawns four reviewer agents, each with their own special instructions:

- qa-team – spawns technical subagents that hunt for security, database, performance, etc.
- security-audit – probes for vulnerabilities like SQL or prompt injections
- paul-reviewer – uses Paul's voice and focuses on observability, rollouts, naming
- xp-reviewer – applies an Extreme Programming lens to review

Then, review-triage sorts those reviews to classify threads into three categories:

- actionable → gets fixed and pushed
- nits → get resolved and replied to with a comment
- ambiguous → escalated and sorted for Paul to work through with the agent later

An outer loop iterates up to three times or until no new actionable threads appear.

From there, you can connect this to another loop that shepherds the PR until it's ready to merge – more on that in the next section.

The takeaway: Save time reviewing code by making agents review each other. This knocks out easier reviews so that only the PRs that really need human attention get flagged.

Steal this — you can check out and copy Paul's qa-swarm and review-triage skills, or use this prompt to design your own review loop based on his:

> Read Paul D'Ambra's qa-swarm skill, plus its sibling review-triage in the same folder, then help me design my own version: https://github.com/pauldambra/dotfiles/blob/main/ai/skills/qa-swarm/SKILL.md
>
> It should take in a single PR, spawn a reviewer panel, triage every finding and existing PR thread into actionable / nit / ambiguous, and keep going until nothing's left but the ambiguous ones flagged for me.
>
> Interview me about my stack, tooling, available models, and how autonomous it should be — what gets auto-fixed vs. only reported, and what it may post to GitHub — before writing the final SKILL.md, then install it.

That said, these systems can get token expensive:

"Something like 60% of my token spend is burned automating the toil of handling CI and review and I don't regret a single dollar." – Paul

So if running multiple agents or loops isn't an option for your team, look for single agent designs like this one by Kun Chen.

**2. Delegate PR babysitting to loops**

The context switching that comes with agentic coding is exhausting. One easy way to reduce that fatigue is by automating code review-adjacent tasks that don't need your attention.

For example, babysitting a single PR can involve tedious tasks like monitoring CI, re-running flaky tests, checking notifications for comments, and keeping the branch up to date.

Why waste your most precious resource – your energy – when you can just delegate all of it to a loop?

The takeaway: Reduce context switching and fatigue by delegating simple tasks like PR babysitting to a loop.

Steal this — you can implement your own PR babysitter skill, based on babysit-prs by @haacked with the prompt below. (It works best if you run it after creating a review loop skill from the previous section.)

> Read https://github.com/haacked/dotfiles/blob/main/ai/skills/babysit-prs/SKILL.md and adapt it for me: same sweep/state design, but it dispatches my own single-PR review skill via a spawned agent per unreviewed PR.
>
> Before writing SKILL.md, interview me on: which skill it dispatches and where my skills live, my stack/tooling/models, and which extra tasks to include — CI monitoring, branch freshness, flaky-test reruns, lint/format autofix, regenerating drifted artifacts, description sync.
>
> Ground the interview in facts you can discover yourself (my open PRs, gh auth, clone layout) rather than asking about them.

**3. Add a PR auto-stamper**

Fast-moving teams generate a lot of small, low-risk PRs, and every one still needs approval on GitHub (a.k.a., a stamp).

At PostHog, we used to handle this in Slack where you drop your PR in #dev-stamp-exchange and wait for someone to give it a quick approval and react with a stamp emoji. We even built a leaderboard for it.

It worked, but each stamp required another engineer to take themselves out of their flow to approve a change they had little to no context on.

Now, most of those are done by our StampHog agent instead. And in just one quarter, it gives the final stamp on roughly 1 in 3 PRs merged into our main repo.

Our engineers add a stamphog label on their PR in GitHub, and it runs a few safety checks based on:

- PR state. No merge conflicts or changes requested
- Blast radius. Deny list keywords (auth, secrets, billing, public APIs, etc.)
- Diff size. Under 500 lines and 20 files
- A simple LLM check. For basic showstoppers

If the agent approves, it'll leave a bare GitHub approval with no line comments.

Otherwise, it refuses or escalates with a 1-2 sentence reason, risk level rating, and next steps. Usually that means routing to a subject matter expert based on CODEOWNERS-soft and git-blame familiarity.

We still use #dev-stamp-exchange when the agent can't auto-accept or route, but it's way less active now. Last month, the StampHog agent took care of 1.6K PRs on its own – that's 1.6K fewer Slack interruptions for our engineers.

The takeaway: Let an agent take care of low-context PR approvals and routing to reduce distractions. Use deterministic checks to route sensitive code to humans.

Steal this — the code for StampHog is available here. Many of its inner workings are specific to PostHog, so instead of copying it, here's a prompt to start customizing one for your repo based on ours:

> Read https://github.com/PostHog/posthog/blob/master/tools/pr-approval-agent/README.md and build the equivalent for the repo at <path>.
>
> Copy the architecture; preserve its safety invariants exactly (fail closed, never request changes or merge, LLM can tighten gates but never loosen).
>
> Their deny-list and thresholds are calibrated to their codebase — re-derive mine: mine my git history for high-blast-radius deny candidates and calibrate size/tier ceilings from my merged PRs, then propose the full gate config for my sign-off before writing any code.
>
> At the same time, ask me whatever you can't derive from the repo — at minimum the CI system and trigger label, escalation routing if there's no CODEOWNERS, and which LLM/SDK to use and how CI gets its credentials.
>
> Leave the result as uncommitted files on my working tree.

**4. Verify by observation, not reasoning**

Agents are good at explaining why their code works. The explanation is often convincing... but also wrong.

If you run the code end to end, you'll frequently find errors the agent never reasoned about, or output that's just slightly not what you asked for.

That's why Daniel Visca's rule of thumb is observability over reasoning. Don't accept an argument that the code works when you can watch it work.

The gold standard is something you can observe directly, like sending a real API request and reading the response. If the behavior is in front of you, you don't have to trust the agent's rationale at all. But this has a scaling problem since a 3,000-line PR would be challenging to trust and observe.

His approach is to make agents decompose the work. For example, for a large change (like building a metrics pipeline end to end), he instructs the agent to produce a stack of small, single-purpose PRs and then uses Graphite for its "stacking" functionality. This makes each diff independently runnable and observable:

At each step of the stack, you can run a real check and confirm the output matches what you expect. Then, as you merge bottom-up, each layer only builds on behavior that's already been verified.

This way, early mistakes can't compound, and when something does break, you're debugging one small diff instead of the whole change.

As a bonus, this lets StampHog from #3 auto-approve the small and focused PRs. You end up with two different checks: the agent reasoning about the code first, and a human observing its actual behavior.

The takeaway: When you can't trust an agent's reasoning, don't read more code; decompose the change until you can watch each piece run. Observation scales better than review.

Steal this — you can set this up by using Graphite to stack smaller PRs produced by your agents with these instructions:

> Split work into a stack of small PRs, each under 400 changed lines and focused on a single change, building only on the PRs below it.
>
> Every PR must ship with its own tests and end with a way to observe it working directly — a command to run and the output I should expect.

This approach is especially valuable for frontend work since deterministic tests don't always capture the visual or behavioral functionality you're looking for.

Pawel Cebula says it's a huge timesaver to have an agent take screenshots and GIFs for each step, with something like this:

> For each PR with frontend work, run the affected screens and capture evidence from the branch's final state: a screenshot of each relevant state (empty, loading, error, populated) and a GIF of the key interaction end to end. Where behavior changes, include before/after.
>
> Attach it all to the PR so the change can be reviewed by observation, not by reading the diff — and re-capture if the code changes after.

Words by @jinayoon_ who was, admittedly, the bottleneck for this post.

### Key Quotes

> "If you need to be involved in every code review, you will always be the bottleneck."

> "The key is that the agent that wrote the code can't be the one that reviews it."

> "Something like 60% of my token spend is burned automating the toil of handling CI and review and I don't regret a single dollar." – Paul D'Ambra

> "Don't accept an argument that the code works when you can watch it work."

> "Observation scales better than review."

### Extraction Notes

Extracted 2026-07-12 via Claude-in-Chrome with a logged-in x.com session. Body verbatim and complete; footnote markers (1/2/3 in the multi-reviewer paragraph) dropped by X's text extraction; "Let an agent can take care" is sic in the original. Linked resources: pauldambra/dotfiles qa-swarm + review-triage skills, haacked/dotfiles babysit-prs skill, PostHog/posthog tools/pr-approval-agent.
