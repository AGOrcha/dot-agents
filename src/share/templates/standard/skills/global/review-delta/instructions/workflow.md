# Review Delta - Workflow

Use **`dot-agents kg`** subcommands from the repository root unless you pass `--repo`.

## Step 1: Ensure the Graph Is Current

Run an incremental update before any impact or review readback:

```bash
dot-agents kg update
```

Stale graphs produce stale impact analysis.

## Step 2: Change Summary and Review Context

Get a concise picture of the current diff:

```bash
dot-agents kg changes --brief
```

For richer change detection (changed functions, risk hints), use full output without `--brief` when needed.

## Step 3: Analyze Blast Radius

For files or symbols in the diff, inspect impact:

```bash
dot-agents kg impact path/to/file.go
```

Adjust `--base` if you are not diffing against the default (see `kg impact --help`).

## Step 4: Structured Graph Queries (Bridge)

For callers, callees, tests, and symbols, use **`kg bridge query`** with the appropriate `--intent`:

```bash
dot-agents kg bridge query --intent callers_of "pkg.Type.Method"
dot-agents kg bridge query --intent tests_for "pkg.Type.Method"
dot-agents kg bridge query --intent callees_of "pkg.Type.Method"
```

Valid intents include: `callers_of`, `callees_of`, `tests_for`, `impact_radius`, `change_analysis`, `symbol_lookup`, `community_context`, `symbol_decisions`, `decision_symbols` (see `kg bridge query --help`).

## Step 5: Generate Structured Output

Load `templates/review-output.md` and fill in each section.

## Token Optimization Notes

- Prefer snippets from `kg changes` and graph query results over whole files.
- Only open a full file when a snippet is insufficient.

## MCP Parity (Optional)

`dot-agents kg serve` exposes the same operations as JSON-RPC tools (`build_or_update_graph_tool`, `get_review_context_tool`, `query_graph_tool`, etc.). Prefer **CLI** in agent sessions; use MCP only when the host environment requires it.
