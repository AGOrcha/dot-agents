# Token Capital Efficiency

- **Author:** Kevin Madura (@kmad)
- **Source tweet:** https://x.com/kmad/status/2069362498663317737
- **X long-form article:** https://x.com/i/article/2068874313608871936 (body paywalled / HTTP 402)
- **Canonical cross-post:** https://kmad.ai/Token-Capital-Efficiency
- **Published:** 2026-06-23
- **Retrieved:** 2026-06-24
- **Status:** RETRIEVED — tweet was a link card; full article reproduced from author's site.

---

## Source content (verbatim, author's site)

Satya Nadella recently published an excellent article on what a future firm looks like in an AI-driven economy. He also introduces the concept of "token capital" which now exists alongside human capital (and financial capital).

A natural extension is token capital efficiency, which can be defined as the business value an organization captures per dollar invested in tokens; i.e., value generated divided by the volume of tokens consumed times their price, across reasoning, task execution, and learning. Higher efficiency comes from extracting more value per token, consuming fewer tokens per outcome, or sourcing tokens more cheaply. This is directly enabled by a new motion for firms: how well an organization can represent valuable knowledge work as tokens an LLM can process reliably.

Almost no firm today is token capital efficient. Everyone is figuring it out on the fly, often to the detriment of technology budgets. Everyone blindly defaults to the latest model, and now the bill is coming due.

In about eighteen months we have round-tripped from tokenmaxxing to a token spend backlash. CFOs and boards with surprise bills are starting to ask questions. At the center is a core tension between companies rushing to "do AI" and the need for financial responsibility. The usage patterns are different from other enterprise software: simultaneously ubiquitous and often billed on usage. Coupled with the speed of advancements, everyone defaults to the best model for everything, hoping for the best performance regardless of task.

Most organizations push every user, regardless of sophistication, to use AI as much as possible. That's fine — 99% of users shouldn't have to know the difference between an Opus-class and a Haiku-class model — but at enterprise scale there's a meaningful difference. "Use AI as much as possible" with no boundary or governance is exactly how you get ballooning bills with an unclear return profile, plus variable outcomes from two-sentence prompts.

Models are getting so good that there's an emerging bifurcation between frontier vs "commoditized" usage. Frontier capability is useful for true unknowns, complex planning, advanced reasoning. For common, well-defined tasks, frontier models are likely overkill. This article covers an approach for structured, well-understood tasks.

The most obvious way to make an impact is to match task complexity with model capability — but the tasks themselves need to be well understood. By defining meaningful tasks you can dramatically improve token capital efficiency (simultaneously reduce cost and improve outcomes).

Picture every way we get a computer to do something as a single spectrum, from fully deterministic to fully probabilistic. On the far left is the ordinary deterministic program, measurable by construction. As you move right, you trade determinism for flexibility, ceding more of the *how* to the model — first as a spec, then a workflow, then a "nudge" — until on the far right you reach a raw LLM prompt: maximum flexibility, minimum guarantees. The crucial thing: the *what* never disappears. You always have an intent. Only the specification of the *how* fades as you move right.

Most enterprise users and tokenmaxxers live on the right: defer everything to the model. Reasonable for certain work — coding agents fit well because a mature codebase gives the model something to bump up against in the form of tests. A failing test is a boundary. Most knowledge work has no such digitally codified boundary, and this is the source of variable outcomes.

But many knowledge-worker tasks can have well-defined boundaries such that they move left and become much more token capital efficient. Doing this well is a sequence: **define the task, match a model to it, measure the result, then optimize.**

Decomposing complex processes into discrete tasks reduces variance. An effective discrete task is generally a well-defined set of inputs (which may include criteria or process steps) and a desired set of outputs, such that you can measure the acceptability of the output.

By wrapping the probabilistic core in a deterministic shell, you harness the models for the hard work "in the middle" while retaining the ability to understand and monitor inputs and outputs consistently. The less you specify, the more the model has to "improvise," which trends toward the average of its training data. As Thariq from Anthropic put it:

> "Every gap you leave, Claude fills with in-distribution choice."

The discipline of thorough specification becomes a compounding differentiator: every set of tasks you define and build evals around becomes something you own — the expertise and IP that makes your company unique. Evals tell you, for a given set of inputs, whether the process delivers acceptable quality. The IP is composable: agents can string together battle-hardened tasks without reinventing the wheel (and spending tokens to do so).

### You match to the right model by measuring it

With the task defined, which model should run it? The temptation is reputation or benchmark. But a score doesn't give enough information. Measure candidates against the task you just defined, across at least two dimensions: capability and cost. If you haven't defined the task, you can't measure success — and then you can't quantify outcomes/returns at a scale a CFO would accept, and you can't move to a different model while retaining acceptable performance (you never defined the bar).

As Satya said: "A company should be able to switch out a 'generalist' model without losing the 'company veteran' expertise built into their learning system." Public benchmarks are good directional indicators but say nothing about a model's capability to execute a workflow within your accounting department. On your specific tasks, an eval becomes your IP — the boundary that measures a model's performance.

### Everything is an optimization problem

With a task definition and an eval, everything becomes optimization. Walk down the cost curve — smaller models, tighter prompts, less scaffolding — until performance crosses the tolerance you defined (e.g., 97% accuracy on a classification task). That crossing is your stopping point, and done correctly you may save an order of magnitude. Without a spec and eval you can't even see this chart.

A natural first step is prompt optimization — and not by hand. With frameworks like DSPy and GEPA you can (a) structure tasks in a maintainable, measurable way, and (b) automatically identify which cheaper models work with acceptable accuracy. For high-volume, well-understood processes, fine-tuning or RL start to make sense.

### Can you measure your token capital efficiency?

Companies that create an inventory of valuable tasks and evals will save costs short-term, but more importantly will be set up to "build the learning loop … where human capital and token capital compound." Organizations that learn to create a digital inventory of important work won't just spend less — they'll benefit from compounding knowledge, model capability, and cost improvements, while competitors flail re-writing prompts from scratch. Those with high token capital efficiency will win.
