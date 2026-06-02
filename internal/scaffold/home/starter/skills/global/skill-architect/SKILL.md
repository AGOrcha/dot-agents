---
name: skill-architect
description: "Design, create, or transform a Claude Code skill using Anthropic's best practices. Use when you have a new skill idea to build from scratch, restructure an existing skill into the orchestrator pattern, audit a skill for quality, run evals to test a skill's output, improve a skill iteratively based on test feedback, optimize the skill's description for triggering accuracy, or package a skill for distribution. Supports modes: new, transform, audit, eval, improve, optimize, package."
argument-hint: "[new <name> | transform <path> | audit <path> | eval <path> | improve <path> | optimize <path> | package <path>]"
---

# Skill Architect

Build, test, improve, and ship Claude Code skills using Anthropic's best-practice structure.

## Modes

Parse `$ARGUMENTS` to determine mode:
- `new <name>` → build a new skill from scratch (`instructions/new-skill.md`)
- `transform <path>` → restructure an existing monolithic skill (`instructions/transform.md`)
- `audit <path>` → evaluate an existing skill without changing it (`instructions/audit.md`)
- `eval <path>` → run test cases, grade outputs, and launch the review viewer (`instructions/eval.md`)
- `improve <path>` → iteratively improve a skill based on eval feedback (`instructions/improve.md`)
- `optimize <path>` → optimize the skill description for trigger accuracy (`instructions/optimize.md`)
- `package <path>` → validate and package a skill as a distributable .skill file (`instructions/package.md`)
- no args → ask the user which mode

## Workflow

1. **Load principles** — Read `instructions/principles.md` before any work. These are the non-negotiable rules.

2. **Run the mode** — Dispatch to the appropriate instruction file (see Modes above).

3. **Apply structure** — Use `templates/skill-structure.md` as the reference for folder layout.

4. **Validate output** — Before finishing, check every skill file against `instructions/checklist.md`.

## Skill Lifecycle

The typical flow from idea to shipped skill:

```
new / transform → audit → eval → improve → optimize → package
  (design)       (check)  (test)  (iterate)  (trigger)  (ship)
```

You can enter at any stage. Each mode is independent.

## Provider configuration

The `eval`, `improve`, and `optimize` modes call an LLM under the hood. By
default this is the local `claude` CLI (zero-config — it reuses the host
session's auth, no API key). To run those modes against a different provider
(Anthropic API, an OpenAI-compatible endpoint, or any CLI), set
`SKILL_ARCHITECT_PROVIDER` and the matching credentials. See
`references/providers.md` for the full matrix and caveats (trigger eval needs
an agentic harness; description improvement works with any provider).
