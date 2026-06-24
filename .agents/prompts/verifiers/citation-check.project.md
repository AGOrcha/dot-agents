# Citation-check verifier — dot-agents repo overlay

Repo-local committed layer. Composes **after** `verifiers/verifier.base.md` (the contract) and
`verifiers/citation-check.md` (the kind: references resolve, claims supported). This file adds **only**
the dot-agents reference taxonomy and query matrix.

## Reference classes + commands

Enumerate the references present, then resolve each:

- **`[[wikilinks]]` / KGNote ids** → `da kg query --intent repo_context "<term-or-id>"`; an id or
  note that returns no node is a dangling reference.
- **Repo-relative file paths / `§`-section / proposal refs** → the path (and section, where
  addressable) exists in the tree.
- **Docs↔code references (code symbols / paths / commands a doc cites)** → resolve each to a real
  target in the tree, bounded to the references the touched docs make:
  - **Go symbols** (functions, types, methods named in prose) → `grep -rn "func <Name>\|type <Name>"
    --include='*.go' .` resolves; a `Package.Symbol` or `pkg/path` resolves to a real package dir. A
    cited symbol that greps to nothing is a **stale code reference** (renamed/deleted) → `--status
    fail`.
  - **CLI commands / flags** (`da <area> <cmd>`, `--flag`) → confirm against the command surface
    (`commands/<area>/…`, or `./bin/da <area> --help`). A doc citing a removed command/flag is stale.
  - **Repo file:line / file:symbol refs** → the file exists and (where addressable) the symbol still
    lives there. Catches the refactor-drift class where code moved but the doc still cites the old
    location. Keep this **bounded** — exact name/path resolution of the refs the docs make, no
    repo-wide crawl or fuzzy matching.
- **Claim support (where the citation adapter `dotagents-builtin:graph/citation@^1.0` is active):** run
  its named queries `claims_citing_source` (the cited source backs the claim) and
  `contradicting_claims` (no indexed claim contradicts it). Where the adapter is absent, record
  support as `unknown` and pass resolution-only — say so in the summary.

`--kind test`, `--verifier-type citation-check`. A dangling reference is a `--status fail`; name the
first one in the summary.
