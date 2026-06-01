# Audit Mode

Evaluate an existing skill against best practices without making changes.

## What to Evaluate

Read the skill directory and assess each criterion:

### Structure
- [ ] SKILL.md is an orchestrator (no inline rules, only workflow + Load directives)
- [ ] `instructions/gotchas.md` exists
- [ ] Description is a trigger condition, not a summary
- [ ] `argument-hint` present if the skill accepts arguments
- [ ] Complex output has a template in `templates/`

### Content Quality
- [ ] Gotchas are specific failure points (not generic advice)
- [ ] Instructions are non-obvious (not things Claude already knows)
- [ ] No railroading (flexibility to adapt)
- [ ] Setup/config handled (config.json pattern if user-specific info needed)

### Advanced (for complex skills)
- [ ] Scripts provided for repeated boilerplate
- [ ] Eval layer present for high-stakes output
- [ ] References split into separate files, not embedded in SKILL.md

## Output Format

```
## Skill Audit: [name]

**Category:** [one of the 9]
**Current structure:** [monolithic | partial | fully restructured]

### Passing
- [items that meet best practices]

### Issues
- [item] — [specific problem] → [recommended fix]

### Priority improvements
1. [highest impact change]
2. [second]
3. [third]
```

Do not make changes — only report findings. The user can then run `transform` mode to act on them.
