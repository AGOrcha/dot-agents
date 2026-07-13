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

## Step 4: Anchor the handoff to the journal

A prose handoff is the *why* layer; it is not crash-survivable on its own and goes stale the
moment state changes. Before finishing:

- **Capture a fresh deterministic snapshot** so the receiving session can re-verify live state:
  `da workflow journal snapshot`.
- **Append a reasoned delta** carrying the intent that the document distills — current mental
  model, the in-flight decision and why, the next step, any active blocker — via
  `da workflow journal append` (see `instructions/journal-cadence.md`).
- In the handoff document, tell the receiving agent to **start with `da workflow journal recover`**
  (or `/agent-handoff recover`) and trust that verified view over any claim written here, since
  the journal is re-verified against reality and this prose is a point-in-time snapshot.

This keeps the document and the journal consistent: the prose explains *why*, the journal proves
*what is actually true now*.

## After Delivering

Offer to:
- Create another handoff for a different audience ("Want an AI agent version too?")
- Review and adjust before sharing
