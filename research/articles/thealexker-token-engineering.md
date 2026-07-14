## The end of tokenmaxxing, the start of token engineering

**Source**: https://x.com/thealexker/status/2072776217984405846
**Author**: Alex Ker (@thealexker)
**Date**: 2026-07-02 (4:15 PM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~800 words
**Engagement**: 22 replies, 35 reposts, 176 likes, 232 bookmarks, 35K views

---

### Summary

A senior PM's single unmonitored expensive-model (fable) run cost over $200 on one prompt — all the fancy knobs on, piles of context gathered, subagents spun out, task paused incomplete. Argues token spend is something to engineer deliberately, not maximize: internal token-burn leaderboards are Cookie Clicker (tokens consumed is necessary-but-insufficient, weakly correlated with useful output). Three proposals: (1) Routing — an intelligent routing layer with a lightweight LLM-judge classifier sending frontier models only judgment-heavy tasks and cheap models the bulk; (2) Evals — measure cost per task completed, not dollars per token, with downstream metrics like acceptance rate and PR longevity; LLM-generated code is technical debt until proven otherwise; (3) Knowledge sharing — searchable org knowledge (MCPs, skills) so models stop re-retrieving context, plus cost attribution to find and shrink high-cost tasks.

### Relevance to dot-agents

External validation of the worker-model-selection policy: scope model to task complexity; expensive tiers are not a default. The $200 unmonitored run is exactly the failure mode that rule guards against. The "cost per task completed, not dollar per token" framing is the right unit for the transcript-analysis pipeline's token accounting; the routing proposal matches stage_profiles cheap-tier routing; the knowledge-sharing point (skills/MCP so agents stop re-deriving context) is the KG-as-SOT argument.

### Body

I just had dinner with a friend who is a senior PM working with AI: smart guy, but told me one unmonitored fable run cost him over $200. One prompt.

I was taken aback, but then as he explained, it all made sense: all the fancy knobs were turned on, so Claude gathered piles upon piles of context, before spinning out subagents to process it all. He had to pause the task, incomplete, because it got alarmingly expensive with no end in sight.

Leaderboards inside companies encourage this: how many billions of tokens did you burn through today? The assumption is the higher the tokens, the higher the productivity. Humans are great optimizers: give us a metric (followers, dollars, tokens), and we shall all relentlessly pursue it… and sometimes lose ourselves despite indicators of diminishing returns. However, the problem is that going for a singular objective omits the end goal: useful output or task completed. Most tokens are intermediate steps of contexts and thinking, and there's no guarantees for any sloppiness.

So at best, tokens consumed is a necessary but insufficient number that may be correlated to units of useful work done. But by and large, most are seeing how tokenmaxxing turns out to be oddly similar to the game of Cookie Clicker, going through the motions of clicking for the sake of a number. Yet the results are not free. The frictionless, bottomless production bloats and compounds, until you cannot understand nor review, and you instead proceed to outsource your verification of output to another machine.

The great paradox is that we are consuming turnkey "intelligence" without much thought.

The good news is that more are waking up to this. Solutions are already underway to relieve companies drowning in tokenspend with questionable ROI. When we started, we often conflated good token usage and bad token usage, simply because the terms are ill-defined, and frankly, we did not know what good was. Now we know we need intentionality over efficiency such that we know we are indeed creating what we want to create, rather than pressing continue forever.

Here are my 3 proposals to becoming more intentional and a good token engineer:

**Routing**

One angle to attack the problem is building your personal or organization layer of intelligent routing. Define the set of tasks that are most important, such as brainstorming new ideas, planning, or being creative. These require frontier, closed source intelligence (and increasingly open source intelligence at similar quality), and another set of auxiliary tasks that you execute way more regularly and that require a great number of tokens that "meet the bar". A simple light-weight, low-latency LLM as a judge is the classification layer that determines where the tokens flow, in the process saving you multiples in cost. Subagents with clean context management are a natural instantiation of the many-model future, where each can be spawned to do a smaller task with the model that makes sense.

**Evals**

Knowing if the tokens are useful is different for everyone, but we can reference how the fastest growing AI applications critically evaluate the usefulness of their output. Take code generation: upstream metrics like code change acceptance rate and number of PR merged (and their longevity in the codebase) are indicators of truly generating useful code. For your company internally, think about how to test if tokens are bringing lasting, downstream productivity. Even designing your own vibe-coded sanity checks for models could go a long way to seeing whether you need the absolute highest quality, or a faster, smaller model that is much more budget-friendly. You might be surprised. Generally, you'll want to measure the usefulness of the tokens considering the cost per task completed, not the dollar amount per token. Lastly, LLM-generated code in your codebase should be marked as technical debt unless proven otherwise over time.

**Knowledge sharing**

While routing to multiple models and designing evals as robustly as possible to track token productivity are good approaches, ultimately sharing how to use tokens with the highest efficiency should be an org-wide practice. One form of this could be putting your documents in a searchable knowledge repository, from MCPs to Skills.MD, so models have more context and spend less on retrieving from the original source of truth each time. Longer term, we can build better, more accurate attributions to regularly identify high-cost tasks, then ask ourselves how to reduce them via choosing a different model, a more efficient harness, or how to use both better.

**Takeaway**

Hopefully, you will now start to think about token engineering and break the recursive loops of mindless token generation. This is not to say I'm not stoked to try out the newest models that are token-intensive, but I want to challenge us to think about our token reliance on the utmost frontier of models, and what might already be good enough for the majority of the tasks where we can capture a lot of value. The floor of intelligence has undeniably risen steadily with open models, so a mixture of models, closed and open, may just be how to circumvent the negative economic surplus that happens when we tokenmax a bit too much.

### Key Quotes

> "tokens consumed is a necessary but insufficient number that may be correlated to units of useful work done"

> "tokenmaxxing turns out to be oddly similar to the game of Cookie Clicker"

> "you'll want to measure the usefulness of the tokens considering the cost per task completed, not the dollar amount per token"

> "LLM-generated code in your codebase should be marked as technical debt unless proven otherwise over time"

### Extraction Notes

Full body recovered 2026-07-12 via Claude-in-Chrome with a logged-in x.com session (previous Playwright pass was unauthenticated and only captured the teaser). Body verbatim and complete.
