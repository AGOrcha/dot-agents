# Writing Effective Handoff Notes

The next agent has ZERO context about what happened in this session. Write notes as if briefing a new team member.

## What Was Accomplished

- List specific changes made, files touched, features completed
- Reference commit hashes for key changes
- Note any architectural decisions and the reasoning behind them

## What Remains

- List incomplete items with enough context to resume without re-investigation
- Include file paths, function names, and specific line references where relevant
- Describe the intended approach if you had one planned but did not execute

## Blockers or Issues

- Document anything that is blocking progress: missing APIs, unclear requirements, external dependencies
- Note any unexpected behavior or bugs discovered during the session
- Include error messages or log snippets if relevant

## What the Next Agent Should Focus On

- Prioritize the remaining work — what is most important or time-sensitive?
- Suggest an order of operations if tasks have dependencies
- Flag anything that needs user input or clarification before proceeding

## Format

Write handoff notes to `.agents/active/` or as a comment in the relevant issue tracker. Keep them scannable — use bullet points, headers, and concrete details rather than vague summaries.
