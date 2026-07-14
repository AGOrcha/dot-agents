# Tuning Deep Agents to Work Well with Different Models (Harness Profiles)

**Author:** Viv (@Vtrivedy10) — LangChain (co-written w/ @masondrxy, @hwchase17, @chester_curme)
**Source:** https://x.com/vtrivedy10/status/2049535740233523600
**Published:** April 29, 2026
**Engagement:** 132.4K views · 227 likes · 386 bookmarks
**Also:** LangChain Blog version

---

## Relevance to dot-agents

**[OVERLAP-SHARPEN + GAP-ADOPT candidate]** (eval Part L.5). The **first measured anchor** for
the per-model harness-config lever — the axis Parts J/K (economics) and A (KG) never covered. A
harness profile (declarative override of system-prompt affixes, tool inclusion+naming,
middleware, subagent config, skills, keyed per model/provider, YAML, call-site-stable, plugin-
distributable) is almost field-for-field our `stage_profiles` + `execution_profile` +
`config-transitive-layering` + craft §6 capability mask — independently invented and measured to
give +10–20pt on tau2-bench. The GAP-ADOPT: check whether our `stage_profiles` key on
model-family or only stage/app_type; if only the latter, a per-model tool-naming + prompt-suffix
layer (Codex `apply_patch`/`shell_command`; Opus `<tool_usage>`) is a real config gap → live
fold-back / proposal to `pipeline-architect` + `config-transitive-layering` (NOT the frozen
transcript plan). "Same model, different harness, very different score" (Terminal-Bench) is the
measured version of "benchmarks are marketing, measure on your harness."

**TL;DR:** Deep Agents was previously designed generically to work across model families. Today we're adding **model-specific profiles** to adjust prompts, tools, and middleware — conforming to prompting guides specific to each model family. We ship profiles for OpenAI, Anthropic, and Google out of the box, which leads to a **10–20 point jump on a subset of tau2-bench** over the default harness.

## Why per-model harness profiles matter

Until today, deepagents shipped a single set of prompts, tools, and middleware aimed to work across all LLMs. Builders could swap models or extend the harness, but the base prompts/tools/middleware were fixed and not optimized per model.

- **Prompting guides differ per model.** OpenAI's Codex Prompting Guide prescribes specific tool implementations and names (`apply_patch`, `shell_command`) that move the needle on Codex. Anthropic's Claude guidance emphasizes a different set of conventions. Even within a family, the Opus 4.6 → 4.7 migration guide flags prompt-level changes worth making.
- **The same model in a different harness yields much different performance.** Terminal-Bench 2.0 is the cleanest public example — the Claude Code harness ranks last among Opus 4.6 submissions. In prior work, harness engineering took gpt-5.2-codex from 52.8% → 66.5% on Terminal-Bench 2.0 (Top 30 → Top 5) just by applying harness-layer changes like prompts and middleware hooks.
- **A single harness can't be optimal for every model.** So make it easy to vary the harness per model.

## What changed per model

Using the Codex and Claude prompting guides as the source:

**For Codex:**
- *Tool changes:* override the default `file_edit` implementation with the recommended `apply_patch` tool; alias the `execute` tool name as `shell_command`.
- *Prompt changes:* around tool calling and planning, e.g. "Before any tool call, decide ALL files and resources you will need. Batch reads, searches, and other independent operations into parallel tool calls instead of issuing them one at a time."

**For Opus** (all prompting, focused on tool usage and planning), e.g.:
```
<tool_result_reflection>
After receiving tool results, carefully reflect on their quality and determine optimal next steps before proceeding. Use your thinking to plan and iterate based on this new information, and then take the best next action.
</tool_result_reflection>
<tool_usage>
When a task depends on the state of files, tests, or system output, use tools to observe that state directly rather than reasoning from memory about what it probably contains. Read files before describing them. Run tests before claiming they pass. Search the codebase before asserting a symbol does or does not exist. Active investigation with tools is the default mode of working, not a fallback.
</tool_usage>
```

Takeaway: exposing an interface for customizing the harness per model is a helpful primitive for builders to manage profiles per agent, version them, and easily test config differences.

## How profiles work under the hood

A **harness profile** is a declarative override layer for the parts of the harness that vary per model: system prompt prefix/suffix, tool inclusion and naming, middleware selection, subagent configuration, and skills. You register a profile for a model or provider (or load one from YAML), and `create_deep_agent` adapts when you swap the model — **your call site doesn't change.**

Defaults shipped for OpenAI, Anthropic, Google. Override them, layer your own on top, or distribute profiles as plugins.

```python
from deepagents import HarnessProfile, register_harness_profile

register_harness_profile(
    "openai:gpt-5.4",
    HarnessProfile(
        system_prompt_suffix="Respond in under 100 words.",
        excluded_tools={"execute"},
        excluded_middleware={"SummarizationMiddleware"},
    ),
)
```

```yaml
# openai.yaml
base_system_prompt: You are helpful.
system_prompt_suffix: Respond briefly.
excluded_tools:
  - execute
  - grep
excluded_middleware:
  - SummarizationMiddleware
  - my_pkg.middleware:TelemetryMiddleware
general_purpose_subagent:
  enabled: false
```

Register a profile at startup for the models you use, or rely on the built-in profiles. Share a profile via PR or distribute as a plugin via entry points. The goal: whichever model you choose, Deep Agents gives you the tools and defaults to create the best harness for your task.
