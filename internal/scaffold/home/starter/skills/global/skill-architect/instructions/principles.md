# Core Principles for Skill Architecture

These rules come from Anthropic's internal best practices. Apply all of them.

## 1. SKILL.md Is the Orchestrator, Not the Rules

SKILL.md contains no rules itself. It contains only:
- A workflow with numbered steps
- `Load →` directives pointing to instruction files
- References to templates for output

If you find rules in SKILL.md, they belong in `instructions/`.

## 2. Description Is a Trigger Condition

The description field answers "when should Claude invoke this skill?" — not "what does this skill do?"

Write it as: "Use when X" or "Use after Y" or "Use whenever Z happens."
Include "supports modes:" if the skill has multiple modes.

## 3. Progressive Disclosure

Each step in the workflow loads only what it needs. Don't frontload all instruction files at the top. Use `Load →` directives inline with each step.

## 4. Every Skill Gets a Gotchas File

`instructions/gotchas.md` is the highest-signal content in any skill. It captures real failure points. Start minimal (3-5 items), grow over time as Claude hits edge cases.

## 5. Avoid Railroading

Don't over-specify. Give Claude the information it needs and the flexibility to adapt. Templates should be starting points, not rigid requirements.

## 6. Scripts Enable Composition

If Claude reconstructs the same boilerplate each invocation, extract it to `scripts/`. Claude should decide what to do, not how to set up.

## 7. The 9 Skill Categories

Every skill fits one cleanly. If it straddles two, split it:
1. Library & API Reference
2. Product Verification
3. Data Fetching & Analysis
4. Business Process & Team Automation
5. Code Scaffolding & Templates
6. Code Quality & Review
7. CI/CD & Deployment
8. Runbooks
9. Infrastructure Operations

## 8. Eval Layer for High-Stakes Skills

Add `eval/checklist.md` (pass/fail gate) and `eval/advisory-board.md` (3 parallel reviewer personas) to any skill that produces content Claude could silently get wrong.
