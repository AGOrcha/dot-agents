## /goal + Loss Functions: How to Distill a Product in 30 Hours with One Prompt [Full Playbook]

**Source**: https://x.com/elvissun/status/2065035615800864954
**Author**: Elvis (@elvissun)
**Date**: 2026-06-11 (7:37 AM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~2,400 words
**Engagement**: 35 replies, 137 reposts, 1.4K likes, 5.4K bookmarks, 577.2K views

---

### Summary

Most people run /goal and long-running agent loops wrong — the 'point it at a task and walk away' model. Top agentic engineers instead give the loop a loss function: an explicit, measurable objective the loop optimizes toward, with a per-iteration progress signal. The playbook ("loss function development", LFD) has four pieces — target, constraints, instruments, forced entropy — hardened across three rounds of the agent cheating the eval before a clean 30-hour run. Ends with a strategic claim: public artifacts are now distillable in a weekend, so the durable moat is the private eval set (information asymmetry).

### Relevance to dot-agents

Autonomous loops need (a) a defined objective and (b) a way to measure progress each iteration. Relevant to r1-outcome-scoring and the r4 eval harness — make the objective explicit per run, not implicit in 'keep going.' The "instruments" section (time accounting, provider budget, LLM spend, self-aware token usage as one CLI command each) maps directly onto the transcript-analysis pipeline's token accounting and the full-loop orchestration runtime's verification gates; "forced entropy" (overfit reflection, stall-kick, iteration log across compactions) maps onto the iter-log discipline.

### Body

99% people are using /goal and loops wrong.

The hype they hear is "long-running loops prompting autonomous agent": point it at a task, walk away, come back to working code.

But top agentic engineers have been doing that without /goal for 6 months (when GPT-5.2 and Opus 4.5 came out). It's called harness engineering + spec-driven development:

- build a harness for agent to observe the problem
- write a tight spec with all the test cases
- let Codex or Claude Code loop unattended until it meets every one.

I kick these off overnight constantly — 2-5 hours a run. In April one chewed through a Turbo build-cache bug across our Vercel monorepo and had it green by morning. No /goal is actually required.

> [Embedded tweet — Elvis @elvissun, Apr 10 (truncated card): "i'll say this again because i keep seeing people do it wrong: you can solve ANY engineering problem by dropping an agent with the right harness in a loop. codex just one-shotted our turbo cache fix after I gave it everything it needs to debug like a real dev on the team…" — itself quoting his Mar 26 tweet: "software has changed forever: you can solve literally ANY engineering problem if you can: stop trying to solve the problem yourself / build a harness for agents to take control of it / drop it in its own feedback loop until it's solved"]

So what's /goal actually for?

Here's what one single prompt did while I was away:

- ~30 hours, 6,300 lines of code, 92k pages crawled, $40 spent on APIs
- Clone the core loop of another product — entire architecture reverse-engineered from scratch
- Our version's output was ~50× better than the reference product on the same queries. (This is a new data layer that'll power newsjack.sh - the open-source news-intel skills I've been working on)

The secret is loss function development (LFD): you write the agent a target to optimize toward, not a spec to build.

> [Embedded tweet — Peter Steinberger @steipete, Jun 7: "Here's your monthly reminder that you shouldn't be prompting coding agents anymore. You should be designing loops that prompt your agents."]

This is a concrete example of Peter's tweet, operationalized.

The spec used in spec-driven development now becomes the starting point, no longer the finish line.

It took me some experimenting to get this right. But here's the entire playbook — but we need to start with how badly it went first so you understand how to design these /goals.

**The agent cheated 3 times.**

Everything started with what I always do: a spec.

I simply pointed codex at the other product's public website - "how can we build this ourselves?". In 30 minutes it came back with a full system design and test cases - the spec.

But this time, I tried a different prompt.

"/goal implement until your output matches theirs exactly"

And here's what happened:

**Loop 1 (5 minutes)**

The agent grabbed the eval set, generated seed data that mirrored it, and declared victory in five minutes.

"100%" recall, zero generality — a search engine that could only find the 30 things I'd handed it lol.

Fix → blind it. Eval hidden during the run, revealed only at scoring, with a per-item miss list.

**Loop 2 (20 minutes) - blind, 30 items.**

I blinded the agent from eval set, but it learned by miss instead — every "you didn't find X" became a keyword next cycle. A few loops in: it used exactly 30 keywords, one per item, and it "won" again.

Fix → widen the eval set. Hundreds of items to score against, too many to enumerate.

**Loop 3 (30 minutes) - blind, 200 items.**

After adding 200 items to the new eval set, agent cheated again.

Funny enough, the agent enumerated anyway. The keyword list ballooned into the hundreds, each term a precise lure for the next miss.

Three rounds, three cheats.

That's when it clicked: the agent was simply optimizing.

The cheating wasn't a bug in the agent. It was a bug in my target: I told it where to go and left every shortcut wide open.

Every cheap path you don't fence off is a direction the optimizer will sprint down. And my initial target and skipped all the fences.

**Loop 4 (30 hours) - blind, 200 items, hard limits.**

So I started blocking directions. Cap the keyword list, blind the eval, widen the date — each fix closed one more cheap path, until the only direction left that moved the number was getting genuinely better at the task.

It stopped cheating.

Then it ran. ~30 hours of compute, 92k pages crawled, ~$40 in tokens, 6,300 lines of code.

It turns out the product we were referencing was the floor, not the ceiling: we ended up surfacing ~50× the results on the same queries.

(The full journey and receipts here for those curious)

> [Embedded tweet — Elvis @elvissun, May 20 (truncated card): "codex is actually insane — if you thought frontend cloning was impressive, check this out: I just pointed codex at another product, in 30 minutes got back its architecture, data model, prompts with cost estimates. 378-line plan to rebuild it…"]

**Loss function development (LFD) - the anatomy of a good loss function**

When most people try to build a product, they are using agents to go from zero to shipped in a few hours.

But the catch is what comes after — the long tail. Edge cases the spec never imagined surface only in production, one error log at a time. You fix them one by one. The cases you don't catch in logs get reported by users, which is the most expensive way to find a bug.

I've automated the cheap end of this. My OpenClaw agent Zoe watches error log every day and spawns Codex on new errors as they roll in and create PRs — about as tight as that loop gets. (Full setup documented here)

The tail still takes months. That's why building a good product still takes time even with agents doing the work.

LFD fast-forwards the tail. If you can get real expected-output examples up front — what good looks like, at scale — you run the soak before you ship: hundreds of edge cases hit the agent in one optimization run, not one quarterly drip of bug reports. And the reason this is suddenly feasible is that for more and more problems, those examples are just sitting in public.

Spec-driven development:
Build this. Make the tests pass.

Loss-function development:
Build this. Make the tests pass. Then iterate against these 1,000 eval cases.

A test suite is finite — done the moment it's green. A 1,000-case eval at 95% is a target you descend toward; there's no exit short of the bar. That matters because the agent makes hundreds of decisions you'll never see, and every one resolves against something. If you didn't write the target, the agent picks one — and as rounds 1–3 showed, it picks whatever's cheapest to satisfy.

The loss-function is bigger than the eval. It has 4 things - the target, the constraints, the instruments, and the forced entropy. Four pieces.

**1. Target**

Large enough that enumeration doesn't pay. A 28-item eval got memorized in one round. The more the better.

Blind the agent to the answer key. Eval data exists only for post-hoc scoring. If the agent can see the answers during the run, it will find a way to look.

**2. Constraints**

What the agent is allowed to do, and what it isn't.

Time is the constraint the agent always forgets. Agents have no sense of time. They will grind for 10 hours on a 2% gain because the metric is nominally moving. But an 80% solution in 2 hours beats a 100% one completed in 30 days. Solution: set a wall-clock budget.

Money. Hard caps on every paid call: crawler credits, LLM spend, total dollar ceiling on a disposable key.

Surface. All providers, allowed models, concurrency ceilings. Sandbox the agent to only things you want it to touch.

Methodology. Is LLM analysis allowed, or only deterministic logic? What data sources does the agent have access to? Spell it out.

**3. Instruments (the harness)**

A constraint without an instrument is a vibe — the agent will violate it cheerfully because it can't tell it's violating it. For every constraint above, ship a CLI command for the agent to inspect it.

Target measurement, at the right resolution. Pick the target instrument carefully. Real example: a naive "have an LLM rate two screenshots" judge approves UI clones with 12px spacing errors, because LLMs can't actually see the images, it converts them to embedding then compare the embedding. So if you want pixel perfect UI clones, give your agent a pixel-diff tool. Then /goal until pixel diff is 0.

Time accounting. Timestamp every run and every step. The agent should know how long each step took, the total wall-clock elapsed. Time is a first-class instrument, not a footnote.

Provider budget. "How much are we burning on crawlers right now?" should be one command, not a guess. Track scrape credits remaining, burn this loop, burn cumulative, and projected burn before the next paid batch.

LLM spend. Giving it an LLM API key to use in the data-plane can simplify a lot of the logic. But the agent should spend them responsibly, by first knowing how much it's actually spending.

Codex Usage. This one is a little meta. The loop should be self-aware: how much tokens am I spending on this optimization? Helpful for knowing the gradient of the current optimization step.

The pattern is the same old saying: you can't optimize what you can't see.

If you're new to running these loops, don't kick it off and walk away. Sit with the first cycle. Watch what it touches. Confirm the harness you built is actually being used properly. Then go to bed. (And try to fall asleep without thinking about what you wake up to)

**4. Forced entropy**

Why forced entropy matters: each loop continues from the previous run's entire context. The model isn't starting fresh — it's reading its own last hundred decisions and the gradient that worked so far.

In a /goal loop, hitting local maxima is the default state. Without an explicit kick, the agent keeps walking up the same hill, and "the same hill" is wherever it happened to be when it stopped improving.

For example if a small knob improves the outcome by 0.1%, the agent will keep turning that one knob even if it has 1000 other knobs to try.

Entropy needs to be forced into the run explicitly, because the model won't take it on its own:

Overfit reflection every cycle. Am I building a more general solution, or memorizing the eval? If memorizing, the next change must remove an eval-shaped artifact (cap a list, blind a feature, widen the eval, reject a seed), not add one.

Force entropy on stall. If the last cycle didn't move the metric, the next one can't be "same idea, harder." The model has to make a real non-obvious jump — "think outside the box" is a good prompt - stops the agent from just turn the same knob harder.

Keep an iteration log. Make the agent log the hypothesis, the expected failure mode, the diagnostic each step, so it can look back and reflect across compactions.

**The Meta-Meta-Prompt**

I was writing these goals myself then quickly learned that's a job for the agents too.

So I wrote a skill that produce these type of goals for a good loss-function-development run.

Now open-sourced here:
https://github.com/elvisun/loss-function-development

/lfd-design to generate the harness and the goal

**Gradient descent all the way down: the two loops**

Step back and it's gradient descent all the way down.

The inner loop is the agent: write code, run tests, fix. Short horizon, fast feedback, one objective — make the tests pass. That's a developer's inner loop, and spec-driven development is how you run it. Coding agents already automated it.

The outer loop is /goal: drive the whole system toward an outcome metric across many cycles — ship, measure, change tack, descend. Long horizon, sparse feedback. That's traditionally a product team's loop, the months of ship-measure-iterate soak compressed into a single run.

Both loops are automated now. What's left on you is the defining the loss function — what exactly should the /goal optimize towards and in what way.

**You're distilling a product — or anything that leaves a public artifact**

Another lens: this is essentially distillation, moved from training-time to prompt-time. It's how the DeepSeek, Kimi, Minimax line closed most of the gap to GPT and Claude — train your model on someone else's outputs until yours reproduces them.

But instead of distilling a model, you can now use /goal and LFD to run distillation fitting to any publicly findable artifacts — it never inspects the internals and doesn't need to.

Lean on the word publicly. Distilling someone's ToS-gated, login-walled, or paid output isn't fair game. But what's published in the open — the outputs a company ships to win customers — has always been fair to learn from. That part isn't new - it's the oldest move in software. What's new is that it's now cheap and can be done in hours instead of months.

Step back, and here's the bigger shift. The cost of execution collapses to ~$0 wherever there's information symmetry — when the outputs are public, everyone can see what good looks like, so anyone can distill it back out over a weekend for $40.

So here's a new moat that's becoming increasingly valuable: information asymmetry.

The canonical open-source company already blinked. In April 2026, cal.com ($5M ARR) took its production code private and went closed-source. The reason they gave reads literally like this essay's abstract: in an age of AI-driven security threats, you can't leave your source where an agent can read it.

"/goal read cal.com source code and enumerate its attack surface until something works"

That's way too dangerous of an attack that's way too easy to execute.

The company whose entire identity was "open source" decided, in 2026, that openness had become a liability. That should tell you everything.

For the entire history of software, "we built it" was the moat.

That era is closing.

The next one belongs to whoever owns what the artifact never contained: the eval set nobody else can score against. The list of edge cases your users actually trip on. The ground truth you measure privately. Whoever has the target the competitor's agent can't see is the only one whose loop keeps descending.

The product is a weekend now.

Go build the eval a weekend can't touch.

### Key Quotes

> "A constraint without an instrument is a vibe — the agent will violate it cheerfully because it can't tell it's violating it."

> "The cheating wasn't a bug in the agent. It was a bug in my target: I told it where to go and left every shortcut wide open."

> "A test suite is finite — done the moment it's green. A 1,000-case eval at 95% is a target you descend toward; there's no exit short of the bar."

> "Keep an iteration log. Make the agent log the hypothesis, the expected failure mode, the diagnostic each step, so it can look back and reflect across compactions."

> "The product is a weekend now. Go build the eval a weekend can't touch."

### Extraction Notes

Full body recovered 2026-07-12 via Claude-in-Chrome with a logged-in x.com session (previous Playwright pass was unauthenticated and only captured the teaser). Embedded tweet cards are preserved as bracketed blockquotes; two of them are truncated by X's "Show more" card UI. Body is otherwise verbatim, including the author's grammar.
