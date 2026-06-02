# Skill Quality Checklist

Run this before declaring any skill creation or transformation complete.

| # | Check | Pass condition |
|---|-------|----------------|
| 1 | SKILL.md has no inline rules | SKILL.md body contains only workflow steps with Load directives |
| 2 | Description is a trigger condition | Starts with "Use when", "Use after", or "Use whenever" |
| 3 | `instructions/gotchas.md` exists | At least 3 specific failure points documented |
| 4 | Progressive disclosure | Each workflow step loads only what it needs |
| 5 | Output templates exist | Any structured output uses `templates/` not inline markdown blocks |
| 6 | Argument hint present if needed | `argument-hint` set if skill accepts arguments |
| 7 | Category identified | Skill clearly fits one of the 9 categories |
| 8 | No railroading | Instructions give Claude flexibility, not micro-management |
| 9 | Eval layer for high-stakes skills | `eval/` present if skill produces reviewable content |
| 10 | Scripts for repeated boilerplate | `scripts/` present if skill runs the same shell/Python setup each time |
