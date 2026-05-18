## Single-agent AI coding is a nightmare for engineers

**Source**: https://x.com/MilksandMatcha/status/2044863551186309460
**Author**: Sarah Chieng (@MilksandMatcha), Head of DevX at Cerebras; co-author @0xSero
**Date**: 2026-04-16
**Method**: Playwright
**Word count**: ~3000 words

---

### Summary

Sarah Chieng and @0xSero argue that single-agent AI coding has a hard ceiling, and that multi-agent "back of house" workflows (orchestrator + subagents) are the solution. Using a restaurant kitchen metaphor, they present five patterns—Prep Line, Dinner Rush, Courses in Sequence, Prep-to-Plate Assembly, and Gordon Ramsay verification—and benchmark results showing multi-agent workflows cut interventions by 84.3% and run time from 36.5 min to 5.2 min for example tasks. The piece is sponsored by Cerebras (Codex Spark, ~1,200 tokens/sec).

---

### Body

I pay my upfront subscription ($200/month), write what I hope is the right prompt (prompt AND context engineer), and wait. 35 minutes later, the agent is still "synthesizing," "perusing," "effecting," and "germinating" (who came up with these).

By the end, I have files of bad code, a bloated context window, and I'm counting the remaining tokens on my left hand.

Okay, I grab an apple, compact, type some heavy handed verbal abuse, re-explain everything from scratch, and pray the next attempt gets further than the last one… only to be disappointed by the same result.

By now, the spark and joys of AI coding are long dead.

**Stop being a one-shot Sloperator**

This is the single-agent ceiling. Every developer building with AI agents hits it the moment their project graduates from a 3D HTML snake game to anything more practical. This happens for two reasons:

- we expect too much from a single agent
- we do not break problems into simple enough, verifiable tasks

Instead, we're going to walk you through what actually works: running a proper back of house. Multi-agent workflows.

**Welcome to the back of house**

There are a few reasons why multi-agent workflows have become much more practical in recent weeks: underlying models have gotten better, and popular AI coding agents have made multi-agent orchestration easier to set up. In the last quarter, OpenAI rolled out deeper orchestration in Codex workflows, while Anthropic continued expanding Claude Code and the MCP ecosystem.

The biggest unlock, though, is speed. One of OpenAI's latest models, Codex Spark (powered by @cerebras) runs at roughly 1,200 tokens/second, which makes it practical to introduce parallel and verification steps that would otherwise be too time-costly to run.

For an example task using Codex and the Figma MCP to copy a website into Figma, the single agent workflow had a 36.5 min/run average with an average of 12 interventions (and 100% failure rate) while the multi-agent workflow leveraging CodeX Spark had a 5.2 minute run, 2 manual interventions, and success on the first try.

**What is a multi-agent workflow?**

Multi-agent workflows fix the single-agent ceiling at the architecture level. Instead of one cook doing everything, you have a head chef who takes the order, breaks it into scoped, verifiable tickets, and hands each one to a line cook to execute.

**The Head Chef (Orchestrator)**: Takes the order from the human, break it into a working list of tickets, then call line cooks to each go out and complete one smaller, scoped job. The orchestrator is responsible for planning, coordination, and task decomposition. Its only tool is `delegate_task`, and it only sees high-level goals plus summaries of subagent outputs.

**The Line Cooks (Subagents)**: Take the ticket given by the Head Chef and get the job done, no questions asked. Each line cook gets its own fresh station (context window), does its work, returns the plate, and clocks out. Subagents can read, write, use MCPs, and any other tools needed. They only see their assigned prompt and a fresh context window (no prior history).

The trick to keeping things orderly: the line cook doesn't get the full order history. It also doesn't get your 15,000-token master plan document. It gets the minimum viable context to cook one specific dish.

**Three immediate wins from running a back of house**

1. **Tokens: your effective context window goes from ~200K to 25M+**

   The human talks exclusively to the orchestrator. The orchestrator is stripped of all tools other than `delegate_task`. If the orchestrator wants to take an action, it spawns a sub-agent via `delegate_task`. Each sub-agent has its own fresh context window, starting only with a prompt. Sub-agents can read, write, use MCPs, and any other tools. Sub-agents return a summary of their work back to the Head Chef.

   This means the orchestrator never has to read files, write files, or see tool-call results directly, effectively extending its context window to as many sub-agents as it can spawn. You can work all day without losing context, compacting, or starting over.

2. **Control: you can enforce sequential workflows at each turn of the agentic loop**

   Instead of one agent doing the exploration, cooking, tasting, and plating, each step becomes a precise, sequential ticket. This is also a great place to use different models for different tasks. With significantly faster models like Codex Spark (~1,200 toks/sec), we can add validation and QA steps that would normally be too time-costly.

   The orchestrator follows a script: Sub-agent A breaks the order into a "contract" with subtasks and criteria. Sub-agent B explores the next subtask. Sub-agent C tests the code generated in the prior subtask. If tests pass, move on. Otherwise respawn the coding line cook to fix identified issues. Sub-agent D documents the subtask and updates the scope checklist.

   In internal trials, this sequential loop reduced manual interventions by 84.3% compared to single-agent runs on the same brief.

3. **Speed: you can run well-defined tasks in parallel**

   Running five parallel mascot generations took roughly one minute versus five minutes sequentially, about a 5x speedup on taste-driven exploration tasks.

**5 Patterns That Actually Work**

**Pattern 1: The Prep Line**

Before service, a professional kitchen doesn't have one cook slowly dicing every single vegetable. It has a row of prep cooks each working independently on the same station. This is the right shape for tasks like design exploration, code variations, or test generation. Have your line cooks each generate many options, then manually pick the best ones. Every line cook works on the same brief independently.

This is the easiest way to get your feet wet with multi-agent workflows because every task is fully independent, with no file conflicts, dependency graphs, or merge logic.

**Pattern 2: The Dinner Rush**

During a Friday night dinner rush, every station in the kitchen is firing simultaneously. Each line cook owns a different job, but they're all plating at once, all contributing to the same ticket.

This is the concept behind "swarms," pioneered by MoonshotAI when they trained Kimi-K2.5. With swarms, each line cook is responsible for a single, scoped, distinct task running simultaneously.

Good fits: building multiple independent components of an app, writing tests for different modules, or porting pages from one framework to another.

The key requirement is that tasks don't share files. The moment two line cooks need to edit the same file, you need a different pattern.

**Pattern 3: Courses in Sequence**

A tasting menu doesn't come out all at once. This is the idea behind phased parallel execution. You break your project into courses (or "waves") where each course strictly depends on the one before it. Within each course, any number of tasks and line cooks can run in parallel. This is perfect for bigger projects like full app rebuilds or large refactors.

**Pattern 4: The Prep-to-Plate Assembly**

Your line cooks don't each build a dish from scratch. One station trims and seasons the protein, the next sears it, the next finishes it in the oven, and the expediter plates and garnishes. In this pattern, line cooks operate sequentially down the pass. Each cook does one smaller task, validates it, then hands the workpiece to the next station.

This pattern is perfect for long-horizon tasks with clear, observable, and verifiable outcomes, research-heavy tasks, or multi-step pipelines. The core principle: do not keep dragging unrelated history through one giant thread. State lives in files and task queues, not in conversation.

**Pattern 5: Here comes Gordon Ramsay**

In a professional kitchen, the chef makes the dish, but it does not go straight to the customer. Instead, it passes through inspection first. This final pattern separates the line cooks that write code from the line cooks that check code. One builder cooks, while two verifiers (a code reviewer and a visual/functional tester) run in parallel to validate the output. If either verifier flags an issue, the builder gets another pass.

This is the single most important rule for avoiding merge conflicts and context drift, and it applies inside every other pattern on this list. Whatever pattern you're running, layer this on top. Use browser automation, screenshots, and deterministic tests for the verify step.

**Where this is heading**

The era of the solo-agent one-shot is over. We're still early, and these patterns will keep evolving as models get faster, context windows get longer, and tooling matures.

Take off the apron and put on the chef's coat. You're running the kitchen now, and your brigade is waiting.

---

### Key Quotes

> "This is the single-agent ceiling. Every developer building with AI agents hits it the moment their project graduates from a 3D HTML snake game to anything more practical."

> "The line cook doesn't get the full order history. It also doesn't get your 15,000-token master plan document. It gets the minimum viable context to cook one specific dish."

> "In internal trials, this sequential loop reduced manual interventions by 84.3% compared to single-agent runs on the same brief."

> "The era of the solo-agent one-shot is over."

---

### Extraction Notes

Article truncated at approximately 3000 words; closing acknowledgments section lightly summarized.
