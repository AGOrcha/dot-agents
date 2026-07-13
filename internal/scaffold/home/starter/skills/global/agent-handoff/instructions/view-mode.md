# View Mode

Read and present a conversational summary of an existing handoff.

## Steps

1. **Find the handoff** — Search for the slug in `.agents/active/handoffs/` or `.agents/history/<plan-name>/handoffs/`. If no exact match, list available handoffs and ask which one.

2. **Read the full file.**

3. **Present a conversational summary** covering:
   - What this handoff is about (1-2 sentences)
   - Who it's for
   - Current status
   - Key next steps (bulleted)
   - Any updates that have been appended

Keep it brief — the user can read the full file if they want details. Don't just regurgitate the file.

## After Delivering

Offer to:
- Update the handoff: `/agent-handoff update <slug>`
- Create a new handoff for a different audience
