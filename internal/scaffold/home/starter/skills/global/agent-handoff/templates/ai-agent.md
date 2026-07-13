# Template: AI Agent

Use when the audience is another AI agent or coding-agent session. Optimize for an agent to pick up and start working immediately with zero additional context needed.

```markdown
# Handoff: [Title]

**Created:** YYYY-MM-DD
**Author:** [user or "agent session"]
**For:** AI Agent
**Status:** Ready to execute

---

> **Before you trust anything below: run `da workflow journal recover`** (or `/agent-handoff recover`).
> This document is a point-in-time snapshot of the *why*; the journal re-verifies the live state
> against reality. Where they disagree, the verified recovery view wins — never treat a `changed`,
> `missing`, or `unverified` item here as still-true, and do not resume a `QUARANTINED` bundle.

## Summary

[2-3 sentences: what this project/task is, current state, what needs to happen next. An agent reading only this section should understand the mission.]

## Project Context

[What the project is. Tech stack. Architecture notes. Repo structure highlights. Only what's needed to orient — not a full tour.]

## The Plan

[The shaped plan from `.claude/plans/`, `.cursor/plans/`, `.agents/active/<plan-name>.md` — include the full plan content here. This is the core of the handoff. If no plan file existed, include whatever plan/approach was described by the user.]

## Key Files

| File | Why It Matters |
|------|----------------|
| [path] | [brief reason] |

[Only files directly relevant. Not every file in the repo.]

## Current State

**Done:**
- [Completed items]

**In Progress:**
- [Partially done items with notes on where they stand]

**Not Started:**
- [Items that haven't been touched]

## Decisions Made

[Decisions made during planning/implementation. Include reasoning so the agent doesn't re-litigate them.]

- **[Decision]** — [Why. What was considered and rejected.]

## Important Context

[Gotchas, failed approaches, environment notes, things that aren't obvious from the code.]

- [Item]

## Next Steps

[Priority-ordered. Each step specific enough for an agent to execute without guessing.]

1. **[Step]** — [Acceptance criteria or definition of done]
2. **[Step]** — [Acceptance criteria or definition of done]

## Constraints

[Rules the receiving agent must follow. Files not to touch. Patterns to maintain. Performance requirements. Security considerations.]

- [Constraint]
```
