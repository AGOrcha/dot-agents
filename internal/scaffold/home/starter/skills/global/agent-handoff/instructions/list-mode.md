# List Mode

Display all existing handoffs as a table.

## Steps

1. Read all files in:
   - `.agents/active/handoffs/`
   - `.agents/history/<plan-name>/handoffs/` (any matching directories)

2. For each file, read only the first ~10 lines to extract:
   - Date (from filename or `**Created:**` field)
   - Slug (the part after the date in the filename)
   - Audience / For (from `**For:**` field)
   - Status (from `**Status:**` field)
   - First sentence of the Summary/TL;DR/Where I Left Off section

3. Display as a table:

```
| Date       | Slug                  | For        | Status           | Summary                        |
|------------|-----------------------|------------|------------------|-------------------------------|
| 2026-03-20 | auth-system-migration | AI Agent   | Ready to execute | Migrating session token storage... |
```

4. If no handoffs found, say: "No handoffs found. Run `/agent-handoff` to create one."

## After Delivering

Offer to:
- View a specific handoff: "Run `/agent-handoff view <slug>` for details"
- Create a new one: "Run `/agent-handoff` to create a new handoff"
