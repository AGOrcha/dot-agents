# Transform Mode

Restructure an existing monolithic skill into the orchestrator pattern.

This is the 5-phase process from Anthropic's restructuring guide.

## Phase 1: Audit (show before building)

Read the existing SKILL.md and identify every distinct concern:
- Voice/style rules
- Step-by-step workflow
- Output format / templates
- Examples (good and bad)
- Evaluation criteria / checklists
- Gotchas and failure points
- Reference material (CLI docs, API signatures, etc.)

**Show the audit to the user before proceeding.** Each concern becomes a separate file.

## Phase 2: Create Folder Structure

Based on the audit, plan the new file layout and show it to the user:

```
skill-name/
  SKILL.md                        (rewrite as orchestrator)
  instructions/
    [concern-1].md
    [concern-2].md
    gotchas.md                    (always)
  templates/                      (if output format was inline)
    output.md
  eval/                           (if checklist/criteria existed)
    checklist.md
    advisory-board.md
  examples/                       (if examples existed)
    good/
    bad/
```

## Phase 3: Move Concerns to Files

For each concern identified in Phase 1:
- Extract the content from SKILL.md
- Write it to the appropriate new file
- Keep the content faithful — don't silently improve or rewrite it during the move

## Phase 4: Rewrite SKILL.md as Orchestrator

The new SKILL.md should contain:
- Improved description (trigger condition)
- `argument-hint` if the skill accepts arguments
- A numbered workflow with `Load →` directives pointing to the files created in Phase 3
- No inline rules

## Phase 5: Validate and Test

Run through `instructions/checklist.md`. Then suggest the user invoke the skill on a real task to verify the split didn't break anything.
