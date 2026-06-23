# Update Mode

Append new context to an existing handoff document.

## Steps

1. **Find the handoff** — Search for the slug in `.agents/active/handoffs/`. Match on the slug portion of the filename (after the date). If no exact match, list available handoffs and ask which one.

2. **Ask three questions:**
   - What's changed since the last handoff?
   - Any new decisions, blockers, or completed items?
   - Should the status change?

3. **Append an update section** at the bottom of the file. Never overwrite existing content:

```markdown

---

## Update — YYYY-MM-DD

[New context, decisions, progress, blockers. Same style as the original template.]
```

4. Confirm what was added.

## After Delivering

Offer to:
- Continue updating
- View the full handoff: `/agent-handoff view <slug>`
