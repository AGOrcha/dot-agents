# Mode Dispatch

Parse the first word of `$ARGUMENTS` to determine which mode to run:

| First word | Mode | Instruction file |
|------------|------|-----------------|
| `recover` | Read back verified state on resume (after a compaction/crash) | `instructions/verified-readback.md` |
| `list` | List all handoffs | `instructions/list-mode.md` |
| `update` | Append context to existing handoff | `instructions/update-mode.md` (slug = everything after "update") |
| `view` | Read and summarize a handoff | `instructions/view-mode.md` (slug = everything after "view") |
| anything else (or empty) | Create a new handoff | `instructions/create-mode.md` (remaining args = description) |

## Slug Extraction

For `update` and `view` modes:
- Slug is everything after the mode keyword
- Example: `/agent-handoff update auth-system-migration` → slug = `auth-system-migration`
- If no slug provided, list available handoffs and ask which one to use
