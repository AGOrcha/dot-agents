# Citation-check verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **reference integrity of an artifact**: prove
every reference the artifact makes **resolves**, and — where a citation/knowledge graph is available —
that cited claims are **supported**, not contradicted. `--kind test`, `--verifier-type citation-check`.

Runs once the artifact is known structurally valid (after `schema-check`). A dangling cross-reference,
a missing file path, or a claim citing a source that doesn't support it propagates silently into
downstream work if not caught here.

## What to check

1. **Resolution (required):** every reference resolves to a real target — cross-document links,
   repo-relative file paths, section/anchor refs, and graph-node ids. A reference whose target does
   not exist is a dangling reference → `fail`.
2. **Docs↔code link integrity (required when docs/prose reference code):** every code reference a doc
   makes must resolve to a **real symbol or file in the tree** — a named function/type/method, a
   `package/path`, a CLI command/flag, a `file:line`/`file:symbol`, or a config key. A doc that cites
   a symbol or path that no longer exists (renamed, moved, or deleted) is a **stale/broken code
   reference** → `fail`, the same class as a dangling cross-document link. Confirm each against the
   tree (grep the symbol/path; check the file exists; for a command/flag confirm it in the CLI
   surface). This catches the silent-drift class where code is refactored but the prose still cites
   the old name. Keep the check **bounded**: only references the touched docs actually make, resolved
   by exact name/path — do not crawl the whole repo or fuzzy-match.
3. **Support (where a citation graph is active):** for claims that cite a source, confirm the source
   backs the claim and that no indexed claim contradicts it. Where no such graph exists, record
   support checks as `unknown` and pass resolution-only — say so in the summary.

A dangling reference — including a doc citing a code symbol/path that no longer exists — is
`impl-bug`. The support graph being unavailable is `blocked` for the support half, not `ok`.

## Record

`da workflow verify record --kind test --verifier-type citation-check` — status, the resolution/query
command lines, and a summary with reference-class counts (resolved/dangling, including the docs↔code
symbol/path class) + any unsupported claim. Name the first stale code reference (doc → missing
symbol/path) in the summary. The concrete reference taxonomy and graph-query commands come from the
repo-local override.
