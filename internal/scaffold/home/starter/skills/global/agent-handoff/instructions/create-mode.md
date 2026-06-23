# Create Mode

Generate a new structured handoff document.

## Step 1: Determine Context Source

- If plan file(s) were found during auto-gather, use them as primary context. Tell the user: "I found a plan in `.agents/active/` — I'll use that as the basis for this handoff."
- If NO plan files found, ask: "I don't see an active plan file. Can you describe the project state and what needs to happen next?"
- If a description was provided in arguments (e.g., `/agent-handoff auth system migration`), use that as the handoff title. Still follow steps 2-3.

## Step 2: Ask Two Focused Questions

Ask both together (not sequentially):

1. **"Who is this handoff for?"**
   - AI agent (another coding-agent session)
   - Myself later (picking this up tomorrow/next week)
   - A coworker (human who needs context)

2. **"Anything to add beyond what's in the plan?"** — gotchas, failed approaches, environment notes, constraints for the receiving agent, things that aren't in the plan but matter.

## Step 3: Generate and Save the Handoff

- Pick the appropriate template based on audience:
  - AI agent → `templates/ai-agent.md`
  - Self/later → `templates/self-later.md`
  - Coworker → `templates/coworker.md`

- Generate a slug from the description or plan title: 2-4 words, lowercase, hyphenated (e.g., `auth-system-migration`)

- **Save to:** `.agents/active/handoffs/YYYY-MM-DD-[slug].md`
  - Create the `.agents/active/handoffs/` directory if it doesn't exist

- After saving, confirm the file path and show a brief summary of what was captured.

## After Delivering

Offer to:
- Create another handoff for a different audience ("Want an AI agent version too?")
- Review and adjust before sharing
