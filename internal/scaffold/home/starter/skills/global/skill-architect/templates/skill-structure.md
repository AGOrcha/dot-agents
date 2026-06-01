# Reference: Skill Folder Structure

Copy this layout when building new skills. Remove sections that aren't needed — don't create empty directories.

```
skill-name/
│
├── SKILL.md                          # Orchestrator — workflow only, no rules
│
├── instructions/
│   ├── gotchas.md                    # Always: 3+ specific failure points
│   ├── workflow.md                   # Non-trivial multi-step process
│   ├── modes.md                      # If skill dispatches on $ARGUMENTS
│   ├── [concern].md                  # One file per distinct rule set
│   └── auto-gather.md                # If skill silently collects context
│
├── templates/
│   └── [output-name].md              # Structured output format templates
│
├── eval/
│   ├── checklist.md                  # Pass/fail gate (10 items max)
│   └── advisory-board.md             # 3 parallel reviewer personas
│
├── examples/
│   ├── good/                         # Annotated examples of great output
│   └── bad/                          # Anti-patterns to avoid
│
├── scripts/
│   └── [helper].sh / [helper].py     # Executable utilities
│
└── references/
    └── [topic].md                    # CLI docs, API references, etc.
```

## SKILL.md Template

```markdown
---
name: "skill-name"
description: "Use when [trigger condition]. Supports modes: [list them if applicable]."
argument-hint: "[arg1 | arg2 | <description>]"
---

# Skill Name

One sentence describing what this skill does.

## Workflow

1. **[Step name]**
   Load → `instructions/[file].md`
   [One line describing what happens in this step]

2. **[Step name]**
   Load → `instructions/[file].md`
   [One line describing what happens in this step]

3. **Generate output**
   Load → `templates/[output].md`
   [One line describing the output]
```

## Gotchas File Template

```markdown
# Gotchas: [Skill Name]

Common failure points:

## [Failure Category]
- [Specific failure] — [What goes wrong and why]
- [Prevention or fix]

## [Another Category]
- [Specific failure] — [What goes wrong and why]
```
