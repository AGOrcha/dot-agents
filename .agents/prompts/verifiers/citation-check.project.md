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
- **Claim support (where the citation adapter `dotagents-builtin:graph/citation@^1.0` is active):** run
  its named queries `claims_citing_source` (the cited source backs the claim) and
  `contradicting_claims` (no indexed claim contradicts it). Where the adapter is absent, record
  support as `unknown` and pass resolution-only — say so in the summary.

`--kind test`, `--verifier-type citation-check`. A dangling reference is a `--status fail`; name the
first one in the summary.
