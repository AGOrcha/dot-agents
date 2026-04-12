# Gotchas: Build Graph

Common failure points when building or updating the code review graph:

## Graph Not Found

- The `.code-review-graph/` directory may not exist on first run — use `dot-agents kg code-status` to see whether a full `kg build` is needed.

## Stale Graph After Branch Switch

- Switching branches does not always rebuild the graph automatically.
- After `git checkout` to a very different branch, prefer `dot-agents kg build` (full) or `kg update` after reviewing `kg code-status`.

## Large Repos

- Full builds can take a long time; use `kg update` for incremental work when the graph already exists.

## Unsupported Languages Silently Skipped

- If you expect nodes from a file and see none, confirm the language is supported by the parser pipeline.

## Ignore File

- Configure `.code-review-graphignore` so vendor/build artifacts do not bloat the graph.

## Incremental Update and Renames

- If you see stale paths after renames, run a full `dot-agents kg build` once.
