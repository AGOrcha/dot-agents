## Harnesses are everything. Here's how to optimize yours.

**Source**: https://x.com/thealexker/status/2045203785304232162
**Author**: Alex Ker (@thealexker), Baseten eng+GTM
**Date**: 2026-04-17
**Method**: Playwright
**Word count**: ~2200 words

---

### Summary

Alex Ker's guide to optimizing AI coding harnesses covers three surfaces that separate high-quality output from "slop": keeping config files lean and human-written (progressive disclosure), structuring prompts with the R.P.I. framework (Research → Plan → Implement), and using subagents to maintain clean context via fan-out or pipeline patterns. The piece contrasts how Claude Code, Codex, and OpenCode handle MCP tool loading, and argues the harness—not the model—is where engineering judgment compounds.

---

### Body

Engineers used to argue about IDEs, now we argue about harnesses.

I've been using and contributing to open-source harnesses (Roo Code, DeepAgent CLI, HumanLayer), and here's what I wish I knew on day one: there are three things you can do right now to make your harness output orthogonal to slop. Yet all three still require human judgment.

This guide covers these simple surfaces that separate harnesses that compound your output from ones that compound your mistakes: how to keep your config files lean enough to reason over, how to structure prompts using the R.P.I. framework so the model approaches problems the way a staff engineer would, and how to use subagents to keep your main context window clean.

By the end, you'll have a concrete set of changes you can make to your setup today, and a clearer sense of why the harness, not just the model, is where your engineering judgment makes a difference.

If the model is the source of intelligence, then the harness is what makes that intelligence useful. The harness's primary job is to act as the scaffolding that:

- Manages the context in an inherently stateless LLM via sessions and compressions
- Makes functions like tool calls, I/O processing, and guardrails work around the model.

Think of a harness as a `while (have next message) do {tool}` loop. One smooth harness amplifies your speed and quality of all code generated onwards.

### Keep your .md files lean and human-written

The core shortcoming for agents today is the concept of "instruction budget". To paraphrase Kyle from HumanLayer, frontier thinking LLMs can only follow a few hundred instructions before entering the "dumb zone", where it starts to miss attending relevant instructions amongst the bloat. Giving too many instructions is functionally encouraging the model to hallucinate.

For a global system prompt — CLAUDE.md or AGENTS.md — human-written outperforms LLM-generated. ETH research found LLM-generated system prompts degrade performance while costing ~20% more in inference. Describe the minimal requirements: what the project is, who the end users are. Every token should fight for its place, since it will be injected globally on every session.

While instinct is front-load everything the model might need and prescribe if-else rules in as much detail as possible, parsing long context directly consumes valuable space in the context window, forcing the reasoning window to drop.

Instead, apply **Progressive Disclosure**: only let the agent pull context when needed, and let it know what exists by giving individual .md files descriptive names. Here's how that plays out across the three common interfaces.

**CLIs**

Engineers already use progressive disclosure in CLIs without naming it. You run `--help` to see available subcommands, then drill into a specific subcommand's `--help` for its flags. The agent can do the same. This matters most for CLIs the model has never seen — a custom internal tool that wraps your API has zero training data. Without progressive disclosure, you'd need to paste the entire reference into context. With it, the agent runs `mycli --help`, finds the relevant subcommand, then runs `mycli deploy --help` to get specific flags.

Popular tools like kubectl or gh don't demonstrate this well because the model already knows their interfaces from training data. The real test is the CLI nobody outside your company has ever used. A short line like "use uv for Python package management, run `uv --help` to discover subcommands before assuming syntax" gives the agent an entry point without wasting context on a full reference.

**Skills**

This is where the industry has converged. Claude Code, Codex, and OpenCode all implement progressive disclosure for skills the same way: at startup, only the name and description of each skill are loaded into context. The full SKILL.md instructions are read only when the agent decides a skill is relevant to the current task. Skills can point to reference files or scripts, which only load as needed. Write a clear, specific description and the agent can match on it without ever reading the body. Codex's own docs explicitly call this "progressive disclosure" and credit it as core to keeping context clean.

**MCP tools**

This is where harnesses diverge significantly.

Claude Code ships with built-in MCP tool search: at session start, it loads a lightweight index of tool names, then searches and pulls full schemas on demand — Anthropic reports this reduces context usage by over 85%. Codex and OpenCode load all configured MCP tool definitions into context at session start. OpenCode's docs explicitly warn users to limit which servers they enable because context fills fast.

If your harness doesn't handle this for you, manage it yourself: be selective about which MCP servers you connect per project, and write tool descriptions that are specific and keyword-rich so any search-based discovery actually works. Also make sure to disconnect irrelevant or unused MCPs to save on context and inference tokens.

### Use R.P.I to work in a higher abstraction

With lean config files and a minimal tool set in place, the next decision is how to structure your prompts. HumanLayer's R.P.I framework is a useful guideline: every prompt should do exactly one of three things when interacting with the harness.

**Research**: If the code-base is unique and complex, give the agent the problem statement, and let it explore the structure including prior art, function definitions, and how files are related to each other. It is important that no action is taken at this step.

**Plan**: The agent writes a step-by-step execution plan. The human should proactively review and verify the validation of the generated plan, given your contextual and domain knowledge around the codebase. Outsourcing thinking or being lazy at this step will cost you dearly later on.

**Implement**: Execute the approved plan in a new context window that we can call the main window. This is the bottom of your stack. If the plan is long and intimidating, we suggest using a pattern of subagents each in its session so irrelevant intermediate states and iterative thinking for a subtask is not polluting the main context window.

Operating a harness is leading it to behave in a way the best staff engineers approach problem-solving: break problems into subproblems, plan before implementing, get a second set of eyes on the plan. The abstraction has shifted from line-by-line code to prompts, but the underlying discipline has not changed.

### Use subagents to maintain clean context

The core heuristic for subagents is simple: use one when a summary of the work is sufficient for your main agent. If you need the intermediary context — if you'll want to ask "how does this connect to what I looked at earlier" — keep it in your primary window. The main agent should only delegate work whose intermediate steps it doesn't need to reason about.

Two patterns work well for subagents:

**Parallel fan-out**: This works best for investigation and research. When an alert fires, your agent can research the issue, generate three candidate theories for root cause, then spin up a subagent for each theory to investigate simultaneously. Each subagent digs into logs, traces, and metrics independently. The main agent gets back three summaries and synthesizes a conclusion without ever having hundreds of log lines in its own context. The value is speed and context isolation: three parallel searches finish faster than three sequential ones, and the noise stays contained.

**Pipelines**: Pipelines enforce depth where fan-out explores breadth. Push a feature through sequential roles: a UX designer who evaluates user experience, an architect who assesses technical feasibility, a devil's advocate who stress-tests assumptions. Each stage receives the previous stage's output and adds analysis. The main agent gets a layered, multi-perspective evaluation without holding all three lenses in context at once.

### Takeaway: Commit

There is a temptation when a harness fails on a task is to switch harnesses. We are not saying don't try all the setups out there, from Cursor, Claude Code, OpenCode, Codex, to Deep Agent CLI; we are simply saying that you need to invest time into molding one truly for your workflow. Different harnesses have differing constraints, context window strategies, and tool routing logic — constantly switching means you lose the institutional knowledge encoded in your config files and start the failure-case log from zero.

So our recommendation is to pick the harness that covers the majority of your team's use case and treat every failure as a data point: what broke, at which step, under what conditions. Add that to your .md files and change up prompting strategies accordingly. The best harness is the harness that you have customized and iterated on with human engineering; it's the one that can handle edge cases which are smoothed out through your team's usage.

---

### Key Quotes

> "Frontier thinking LLMs can only follow a few hundred instructions before entering the 'dumb zone', where it starts to miss attending relevant instructions amongst the bloat. Giving too many instructions is functionally encouraging the model to hallucinate."

> "ETH research found LLM-generated system prompts degrade performance while costing ~20% more in inference."

> "Claude Code ships with built-in MCP tool search: at session start, it loads a lightweight index of tool names, then searches and pulls full schemas on demand — Anthropic reports this reduces context usage by over 85%."

> "The best harness is the harness that you have customized and iterated on with human engineering."
