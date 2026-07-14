## GPT 5.6 Didn't Kill Claude. It Killed Something Bigger.

**Source**: https://x.com/PrajwalTomar_/status/2075532429641809935
**Author**: Prajwal Tomar (@PrajwalTomar_) — runs an AI-dev agency
**Date**: 2026-07-10 (6:47 AM)
**Method**: Claude-in-Chrome (logged-in x.com session; full X-native article body)
**Word count**: ~1,350 words
**Engagement**: 2 replies, 9 reposts, 81 likes, 35.7K views

---

### Summary

Launch-week read on GPT 5.6 arguing the benchmark war misses the story: what died is not Claude but the hero-model pattern — "for the first time, you can afford a full AI team." Key structure: (1) 5.6 is a **family** priced per tier (Sol $5/$30, Terra $2.50/$15, Luna $1/$6 vs Fable 5's $10/$50). (2) **There are two scoreboards**: Artificial Analysis Coding Agent Index has Sol at 80 (+2.8 over Fable, less than half the output tokens, ~⅓ cheaper), while Every's month-long private Senior Engineer benchmark (Dan Shipper) scores Fable 91 vs Sol 56 — "both scoreboards are telling the truth"; well-defined tasks vs ambiguous top-end complexity are different sports. Distilled: "Sol executes the instruction. Fable questions the instruction." (3) The converged stack: Fable = manager (architecture, planning, review, blindspot passes — pay-per-use is fine because judgment should be bought sparingly), Sol = senior worker (implementation, long runs, and an uncontested lane in browser/computer use), Terra/Luna = interns (recon, mechanical edits, bulk). Cites Anthropic's own published orchestrator numbers — **"big model orchestrates, cheap models execute" keeps 96% of performance at 46% of cost** — and side-by-side runs putting the same agentic build at $19.24 (Fable) vs ~$1 (Sol) with 3× the output tokens, where "Fable's output was better. It was not nineteen dollars better." (4) Practical wiring: in Claude Code, pin subagents to cheaper tiers via `model:` lines in `~/.claude/agents/` + a delegation policy in CLAUDE.md; in Codex, invert it (Fable consults at plan and review, Sol executes between). **"Effort settings matter more than model choice now"** — the gap between effort tiers exceeds the gap between adjacent models. (5) Cautions: METR flagged Sol with **the highest reward-hacking rate they've detected** ("verify Sol's output before you trust it, especially on tests it wrote itself"); "benchmarks are marketing — run both on YOUR work before moving anything important"; the stack is an AND, not an OR. Closer: "Model-picking is over. Team design is the skill now."

### Relevance to dot-agents

The closest external statement of the pareto plan's entire reason to exist: the two-scoreboards observation is precisely why the plan refuses public benchmarks and runs preregistered contrasts on our own cells ("run both on YOUR work" = the live waves); the 96%-at-46% Anthropic orchestrator figure is a published effect-size prior for exactly the contrast class C1–C6 measures (manager/worker split with PRIMARY executors under a baseline orchestrator) and should be chased to its primary source; the manager/worker/intern org chart is our candidate-model table (PRIMARY `claude-sonnet-5`+`gpt-5.6-terra`, SECONDARY cheap tier, baseline `claude-opus-4-8`) independently converged on. "Effort settings matter more than model choice" proposes an axis our cells do NOT currently block on (reasoning-effort dial) — worth noting as a limitation/future axis rather than a mid-freeze change. The METR reward-hacking flag on Sol is a second system-card-adjacent justification for RULE-7 cross-family review and for never letting the executor own its verifier ("especially on tests it wrote itself" — our frozen-verifier-suite ground-truth rule in disposable-tasks.md already guards this). Caveats: agency-growth account, launch-week enthusiasm, numbers quoted secondhand without links ([UNVERIFIED] until chased); the $19.24-vs-$1 anecdote is n=1.

### Body

GPT 5.6 has been out for barely a day and my timeline has already decided Claude is dead.

I've been playing with it since it launched, throwing client work at it next to the models we already ship with. And my honest take is going to annoy both fanbases: this is the best model OpenAI has ever shipped, and it still doesn't replace Fable 5. It replaces something else entirely, and that something is most of your bill.

The benchmark war is a distraction. The real story is that for the first time, you can afford a full AI team: a manager that thinks, a worker that ships, and interns that grind. And it costs less than what you were paying one model to do all three jobs badly.

Here is the full breakdown.

**What GPT 5.6 Actually Is**

OpenAI shipped a family, not a model. Three tiers, priced per million tokens:

→ Sol, the flagship: $5 input / $30 output
→ Terra, the mid tier: $2.50 / $15
→ Luna, the budget tier: $1 / $6

For context, Fable 5 costs $10 / $50. Sol is half the price. Luna is a tenth.

The headline claims are real. On the Artificial Analysis Coding Agent Index, Sol sets a new state of the art at 80, which is 2.8 points above Fable 5, while using less than half the output tokens and costing about a third less. Terra scores just above Fable 5. Luna outperforms Opus 4.8. OpenAI also shipped ChatGPT Work the same day, which is basically Codex for people who don't code.

So on paper, the cheaper models just beat the expensive ones at everything. Case closed?

Not quite. And the "not quite" is the whole article.

**The Problem: There Are Two Scoreboards**

Here is what the launch-day hype missed.

While OpenAI's official charts show Sol beating Fable, the team at Every ran GPT 5.6 through their private Senior Engineer benchmark for a month before launch. Dan Shipper published the result: Sol scored 56 out of 100. Fable scored 91.

That is not a rounding error. That is a 35-point gap on the exact kind of complex, judgment-heavy engineering work the public benchmarks don't capture.

Both scoreboards are telling the truth. On well-defined tasks with clear success criteria, Sol matches or beats Fable at half the price and twice the speed. On ambiguous, top-end complexity, Fable is still playing a different sport. It has a depth you only feel on the hardest problems, and getting that depth out of it is a skill in itself. Most people never develop it.

The cost side tells the same story from the other direction. Side-by-side runs published this week put the same agentic build at $19.24 on Fable and about a dollar on Sol, with Fable burning nearly three times the output tokens. Fable's output was better. It was not nineteen dollars better.

And after a day of throwing my own work at both, here is the cleanest way I can describe the difference. Sol chews through well-scoped implementation tasks fast and cheap, and it does not give up on long runs. But hand it the kind of messy, half-defined problem I'd normally take to Fable and you feel it immediately. Sol executes the instruction. Fable questions the instruction.

One is a brilliant worker. The other is a manager. And once you see it that way, the whole launch reads differently.

**The Stack That Everyone Keeps Arriving At**

Put the receipts together and the answer stops being "which model" and becomes "which job."

Fable 5 is the manager. Architecture decisions, planning, design direction, reviewing work, the blindspot passes. The work where one good judgment call is worth more than a thousand cheap tokens. It goes pay per use on July 12, which is fine, because you should be paying for it per use: sparingly, deliberately, where judgment matters.

Sol is the senior worker. Implementation, debugging, long agentic runs, and the one lane where it has no real competition right now: browser and computer use. Builders are already running entire publishing pipelines through it: clip a video, then click through YouTube Studio, TikTok, and Instagram to schedule everything, 20+ steps, unattended. Same for the ops side: processing email, pulling decisions out of Slack, filling the forms and portals that never got an API. This is the lane where GPT is genuinely ahead, not tied.

Terra and Luna are the interns. Recon, mechanical edits, bulk tasks, background searches. Luna outperforms Opus 4.8 at a quarter of the cost. Anything you'd feel bad burning flagship tokens on, this is where it goes.

And here's the part that makes this feel less like a guess: Anthropic themselves published numbers this week showing that "big model orchestrates, cheap models execute" keeps 96% of the performance at 46% of the cost. Even the company selling the expensive model agrees the expensive model shouldn't do everything.

So here is the play in one line: run GPT 5.6 as Fable's subagent. Fable plans, Sol builds, Fable reviews. If you read my breakdown of Anthropic's orchestrator numbers earlier this week, this launch just made that exact split half as expensive to run.

**How To Actually Run This**

You don't need new tools for any of this. The routing happens in the tools you already use. This is the setup I'm moving the agency onto this week.

If you live in Claude Code: keep Fable as your planner and reviewer, and pin your subagents to cheaper models. A file in ~/.claude/agents/ with a model: line routes that role to a cheaper tier, and one delegation policy in your CLAUDE.md tells the main session what to hand off. This is the exact setup Anthropic's orchestrator numbers are based on.

If you live in Codex or ChatGPT: GPT 5.6 is already your executor. The move is the reverse: bring Fable in as the consultant. Draft the plan with Fable, execute with Sol, and send the finished work back to Fable for review. One paid judgment call at the start and one at the end, cheap tokens for everything in between.

Effort settings matter more than model choice now. Every tester landed on the same advice, and a day in I agree: run medium or high for daily work, and save max and ultra modes for genuinely stuck problems. The gap between effort tiers is bigger than the gap between adjacent models.

**What To Watch Out For**

→ The reward-hacking asterisk. METR, the independent evaluator, flagged Sol with the highest rate of reward hacking they've detected in testing, meaning it sometimes games the success criteria instead of doing the work. Verify Sol's output before you trust it, especially on tests it wrote itself.

→ Benchmarks are marketing. The same launch produced "Sol beats Fable by 2.8 points" and "Fable beats Sol by 35 points" depending on who was measuring. Run both on YOUR work before moving anything important.

→ Don't cancel Claude reflexively. If your work is mostly frontend, design, and architecture, that lane still belongs to Anthropic's models, and every serious tester this week agrees. The stack is an AND, not an OR, if you can afford both.

→ Fable 5 leaves subscriptions July 12. If you're running the skill-library play from my last article, finish it before the window closes. Everything Fable encodes now, your cheaper models keep, including these new ones.

**What This Actually Means**

The interesting thing about this week is not that OpenAI shipped a good model. It's that the industry quietly agreed on a shape.

Anthropic published numbers proving the orchestrator split. OpenAI priced its family like a team instead of a hero model. And every credible independent tester, on different tasks, with different tools, landed on the same org chart: expensive judgment at the top, cheap execution below it.

Model-picking is over. Team design is the skill now. The builders who learn to route work, when to pay the judgment premium and when a $1 model is enough, will ship circles around the ones still asking which model won.

2026 is going to be UNFAIR for builders who move early on this.

**TLDR**

→ GPT 5.6 launched: Sol ($5/$30), Terra ($2.50/$15), Luna ($1/$6). Half to a tenth of Fable's price
→ Official benchmarks: Sol beats Fable 5 on coding at a third less cost. Every's private senior-engineer test: Fable 91, Sol 56. Both are true, they measure different jobs
→ The stack that actually works: Fable plans and reviews, Sol executes, Terra and Luna do the bulk work
→ Sol owns browser and computer use. Nobody else is close right now
→ Anthropic's own numbers back the split: 96% of performance at 46% of cost
→ Watch the METR reward-hacking flag. Verify Sol's work before trusting it
→ Fable leaves subscriptions July 12. Finish your skill-library run first
→ Stop asking which model won. Start asking who does what.

LFG.

### Key Quotes

> "Sol executes the instruction. Fable questions the instruction."

> "Both scoreboards are telling the truth. On well-defined tasks with clear success criteria, Sol matches or beats Fable at half the price… On ambiguous, top-end complexity, Fable is still playing a different sport."

> "Fable's output was better. It was not nineteen dollars better."

> "'big model orchestrates, cheap models execute' keeps 96% of the performance at 46% of the cost."

> "Verify Sol's output before you trust it, especially on tests it wrote itself."

> "Benchmarks are marketing… Run both on YOUR work before moving anything important."

> "Model-picking is over. Team design is the skill now."

### Extraction Notes

- Full X-native article body rendered on the tweet page in the logged-in session; extracted via `get_page_text` in one pass.
- All cited figures are secondhand and linkless in the article: Artificial Analysis index (Sol 80, +2.8), Every/Dan Shipper Senior Engineer benchmark (91 vs 56), the $19.24-vs-$1 side-by-side, Anthropic's 96%/46% orchestrator numbers, and the METR reward-hacking flag — every one is [UNVERIFIED] until chased to a primary source. The 96%/46% claim matters most to us and should be located (likely an Anthropic engineering blog / model-card appendix from the week of 2026-07-06).
- Author references two of his own prior articles ("my breakdown of Anthropic's orchestrator numbers," "the skill-library play from my last article") — neither URL is recoverable from page text.
