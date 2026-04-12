# Review PR - Common Failure Points

1. **Stale graph.** Run `dot-agents kg update` (with correct `--base` when needed) before trusting impact.

2. **Full-file dumps.** Use graph snippets and `kg changes` output first.

3. **Missed renames.** Check callers of old and new symbols with `kg bridge query --intent callers_of`.

4. **Inheritance / Liskov.** When base types change, trace dependents and overrides via bridge queries and impact.

5. **Only the latest commit.** Always diff `main...branch` (or equivalent), not a single commit, for multi-commit PRs.

6. **Assuming `kg search` exists.** Vector similarity search is on the MCP path today (`kg serve` + `semantic_search_nodes_tool`); use bridge intents for CLI-first review.
