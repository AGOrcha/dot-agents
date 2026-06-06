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
2. **Support (where a citation graph is active):** for claims that cite a source, confirm the source
   backs the claim and that no indexed claim contradicts it. Where no such graph exists, record
   support checks as `unknown` and pass resolution-only — say so in the summary.

A dangling reference is `impl-bug`. The support graph being unavailable is `blocked` for the support
half, not `ok`.

## Record

`da workflow verify record --kind test --verifier-type citation-check` — status, the resolution/query
command lines, and a summary with reference-class counts (resolved/dangling) + any unsupported claim.
The concrete reference taxonomy and graph-query commands come from the repo-local override.
