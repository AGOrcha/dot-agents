---
name: agent-start
description: "Begin each work session by understanding the current state and context. Use at the start of any new conversation, when resuming work, or when switching tasks."
---

# Agent Start

Begin each work session by understanding the current state and context. Use at the start of any new conversation, when resuming work, or when switching tasks.

If `.agents/workflow/` and `active.loop.md` are present, prefer
`orchestrator-session-start` instead — this skill is the generic,
non-loop fallback for repos that aren't running a `da` workflow loop.

## Workflow

1. **Gather context** (see `instructions/context-gathering.md`)
   - Read project docs, check for active plans, review lessons learned
   - If a code review graph is present, prefer graph or MCP tooling over broad manual scans

2. **Assess technical state** (see `instructions/state-check.md`)
   - Git status, build health, branch state, stashed work

3. **Identify the current task**
   - Ask the user what they want to work on if not clear
   - Review any linked issues or tickets
   - Check the project backlog if available

4. **Plan before coding**
   - Break down complex tasks into smaller steps
   - Identify files that will need changes
   - Consider edge cases and potential issues

5. **Review gotchas** (see `instructions/gotchas.md`)
   - Avoid common session-start mistakes
