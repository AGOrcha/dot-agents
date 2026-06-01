# New Skill Mode

Build a skill from scratch based on a description or idea.

## Step 1: Clarify the Skill

Ask (or infer from context):
1. What does this skill do? What problem does it solve?
2. Which of the 9 categories does it fit? (see `instructions/principles.md`)
3. Does it need multiple modes (like list/create/view)?
4. What's the output — a file, a report, an action, a structured doc?
5. Are there gotchas or failure points the user already knows about?

If the user provided a description in arguments, use it as the skill name/purpose and ask only what's missing.

## Step 2: Design the Structure

Based on the answers, decide which components are needed:

| Component | When to include |
|-----------|----------------|
| `instructions/gotchas.md` | Always |
| `instructions/workflow.md` | Skill has a non-trivial process |
| `instructions/modes.md` | Skill has multiple dispatch modes |
| `templates/` | Skill produces structured output |
| `eval/checklist.md` | Skill produces content that could be subtly wrong |
| `eval/advisory-board.md` | High-stakes review/writing/analysis |
| `scripts/` | Repeated shell/Python boilerplate the skill needs |
| `references/` | Skill relies on external docs or CLI references |

Show the planned structure to the user and confirm before building.

## Step 3: Build the Files

Build in this order:
1. `SKILL.md` — orchestrator with workflow pointing to files
2. `instructions/` files — starting with gotchas, then workflow/modes
3. `templates/` files — output format templates
4. `eval/` files — if needed
5. `scripts/` — if needed

Use `templates/skill-structure.md` for reference layout.

## Step 4: Validate

Run through `instructions/checklist.md` before finishing.
