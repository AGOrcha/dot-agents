# CODEX MULTI AGENT PLAYBOOK: SWARMS LVL. 1

**Author:** am.will (@LLMJunky)
**Source:** https://x.com/llmjunky/status/2027032974202421336
**Published:** February 26, 2026
**Engagement:** 126K views · 462 likes · 1,074 bookmarks · 16 replies
**GitHub:** https://github.com/am-will/codex-skills | https://github.com/am-will/swarms

---

Swarms are what happen when you stop thinking about subagents as a novelty and start thinking about them as a **workforce**.

Instead of one agent grinding through random tasks, you’re coordinating multiple agents executing in parallel, each with its own clear scope, working toward the same goal.

The difference between a swarm that works and one that doesn’t comes down to three things: how you plan, how you orchestrate, and the quality of the context you give your agents.

## The Plan.md

For you to get the most out of swarms, your planning has to be exceptional. **Ambiguity is your enemy.** Every unclear requirement multiplies across however many agents you launch.

One agent drifting is annoying. Five agents drifting in parallel is a disaster.

Tip: when the agent stops during the planning process to ask clarifying questions, don’t blindly accept the first option. Open another agent session and ask:

> “Help me choose the best tech stack for my product. Ask me questions and guide me to the most optimal solutions”

Be part of the process. The majority of your time should be spent developing a careful and detailed spec that you know intimately.

**If you’re running swarms, you are the architect. The agents are the builders.**

## The Swarm Planner Skill

The “swarm planner” skill stops and asks questions any time the model detects ambiguity, and builds **dependency maps** into your plan.

To add a dependency map to any plan without the skill:

```
This plan MUST include a dependency graph. Every task declares `depends_on: []` with explicit task IDs T1, T2
```

## Orchestration

The orchestration layer is the most important aspect of utilizing swarms.

**Do not reset context before implementing the plan.** If your context is low (less than 40% left), you can opt to compact, but because you’re using subagents, the parent session will not use a great deal of tokens.

The orchestrator serves these critical functions:
1. Manage the state of plan implementation
2. Call subagents as needed
3. Provide subagents their prompt
4. Validate the subagents’ work
5. Resolve conflicts
6. Ensure the project is continuously moving forward towards success

Think of it like a foreman on the job. It knows who, what, where, when, why, and how — and manages all of that in its context window.

I typically employ one of two strategies: **Swarm Waves** and **Super Swarms**.

## Swarm Waves

Swarm Waves launch one subagent per unblocked task, in waves. Safest path — fewest conflicts, fewest tokens burned.

With this method, if there’s only one unblocked task, it only launches one agent. If there’s eight unblocked tasks, it launches eight agents.

The orchestration layer loops over the plan to look for unblocked tasks, and continuously launches new agents as their dependencies become unblocked, until all tasks are done.

Because we created a dependency map in the planning phase, the orchestration agent knows exactly which tasks can launch in parallel at any given time.

## Super Swarms: Total Parallelism

Launch as many subagents as your machine can handle at once, **regardless of dependencies.** Really fast.

Skip the dependency map. Explicitly guide the orchestrator to launch as many agents as you have configured.

```toml
[agents]
max_threads = 16
```

Note: Codex base max is six parallel agents. Increase max_threads to go beyond. Too many may cause 429 errors — reduce if needed.

Because all/most tasks are done in parallel, this leads to increased conflicts. But the orchestrator is quite adept at identifying these conflicts in real time and handling their resolution on the tail end.

## The Secret Sauce: Context Engineering

To get the best result from your subagents, ensure that each agent gets optimal context for the highest quality outcomes.

**Front-load the subagent’s context with every meaningful detail.** This is especially important when using small/fast models like Spark:
- Reduces the number of tool calls needed to gather context
- No guessing, no ambiguity, just clear up-front instructions

Two benefits:
1. Allows your orchestration agent to fill a very specific role (keep project moving, clear conflicts, provide context, call agents, review outputs). You can extend your context window incredibly far.
2. Front-loading detailed context **saves tokens.** Critical for Spark (128K context window) — give it clearly defined tasks and it shines.

## How to Control Subagent Prompts

Subagent prompt template — give this to your orchestrator as the framework. All [bracketed sections] are variables the orchestrator fills in automatically:

```
You are implementing a specific task from a development plan.

## Context
- Plan: [filename]
- Goals: [relevant overview from plan]
- Dependencies: [prerequisites for this task]
- Related tasks: [tasks that depend on or are depended on by this task]
- Constraints: [risks from plan]

## Your Task
**Task [ID]: [Name]**

Location: [File paths]
Description: [Full description]

Acceptance Criteria:
[List from plan]

Validation:
[Unit Tests or verification from plan]

## Instructions
1. Examine working plan and any relevant or dependent files
2. Implement changes for all acceptance criteria
3. Keep work atomic and committable
4. For each file: read first, edit carefully, preserve formatting
5. Run validation if feasible
6. ALWAYS mark completed tasks IN THE *-plan.md file AS SOON AS YOU COMPLETE IT, and update with:
    - Concise work log
    - Files modified/created
    - Errors or gotchas encountered
7. Commit your work
   - Note: There are other agents working in parallel to you, so only stage and commit the files you worked on. NEVER PUSH. ONLY COMMIT.
8. Double Check that you updated the *-plan.md file and committed your work before yielding
9. Return summary of:
   - Files modified/created
   - Changes made
   - How criteria are satisfied
   - Validation performed or deferred

## Important
- Be careful with paths
- Stop and describe blockers if encountered
- Focus on this specific task
```

## Why This Framework Works

Your agents have amnesia. Without context they have to call many tools and read many files to discover context before starting work — potentially leading to drift.

With this template, every agent understands:
- What the task is and why it exists within the larger spec
- Which files it depends on (full paths and expected contents)
- Where the plan is, and is instructed to read it
- State of the project (within the plan and commits)
- The filenames it needs to work on, and their paths
- Which other tasks it relates to, and their function
- Acceptance criteria and testing methodology
- Step-by-step implementation instructions

## A Note on Model and Reasoning

The only hard rule: **use one of the larger models for orchestration.**

**Pro Subscriptions:**
- Plan with GPT 5.4 High or 5.3-Codex High
- Orchestrate with 5.4-Codex High
- Subagents with Spark xHigh or 5.3-Codex High

**Plus/Business Subscriptions:**
- Plan with GPT 5.4 High or 5.3-Codex High
- Orchestrate with 5.4-Codex Medium
- Subagents with 5.4-Codex Medium

Full config example:

```toml
model = "gpt-5.3-codex"
plan_mode_reasoning_effort = "xhigh"
model_reasoning_effort = "high"

[features]
collaboration_modes = true
multi_agent = true

[agents]
max_threads = 16

[agents.sparky]
config_file = "agents/sparky.toml"
description = "Use for executing implementation tasks from a structured plan."
```

## Key Takeaways

- You don’t need subagents — single agent sessions work fine — but once you learn to wield them properly, they are a great tool to speed up workflows
- Swarms enable horrendously long-horizon tasks if you create the right scaffolding
- Test-First approach works great: have the orchestrator write tests before calling subagents, so the Spark agent has a test it needs to make pass before yielding
- Be part of the process — steer when needed, adapt on the fly

## Resources

- Custom Agent Roles and Skills: https://github.com/am-will/codex-skills
- Swarm Planner + Parallel Task skills README: https://github.com/am-will/swarms
