# Review PR - Workflow

Use **`dot-agents kg`** from the repository root unless you pass `--repo`. Pass `--base` when the PR does not target your default branch.

## Step 1: Identify the Changes

- **PR number:** `git fetch origin pull/<N>/head:pr/<N>` then `git diff <default-branch>...pr/<N>`.
- **Branch name:** `git diff <default-branch>...<branch>`.
- **Neither:** compare current branch to `<default-branch>` (e.g. `main`).

Review **all** commits in the PR, not only `HEAD~1`.

## Step 2: Update the Graph

```bash
dot-agents kg update --base <merge-base-or-target>
```

Use the appropriate `--base` for the PR’s merge target when it differs from the default.

## Step 3: Change and Impact Readback

Summary of the delta:

```bash
dot-agents kg changes --brief
```

Deeper impact for specific paths:

```bash
dot-agents kg impact path/to/file.go --base <base>
```

## Step 4: Structural Queries (Bridge)

Use **`kg bridge query`** for callers, tests, callees, and symbol context:

```bash
dot-agents kg bridge query --intent callers_of "qualified.name"
dot-agents kg bridge query --intent tests_for "qualified.name"
```

See `dot-agents kg bridge query --help` for the full intent list.

## Step 5: Semantic / Docs Helpers (MCP Optional)

Embedding-based **semantic search** and **plugin docs sections** are exposed on **`dot-agents kg serve`** as MCP tools (`semantic_search_nodes_tool`, `get_docs_section_tool`, etc.). If you need those exact surfaces from an agent host that only speaks MCP, run `kg serve` and call the tools by name.

For CLI-only workflows, prefer structural intents (`symbol_lookup`, `callers_of`, …) plus `kg changes` / `kg impact`.

## Step 6: Generate Structured Output

Fill `templates/review-output.md`. For large PRs, prioritize highest-impact files first (most dependents / risk).
