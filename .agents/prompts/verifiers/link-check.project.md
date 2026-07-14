# Link-check verifier — dot-agents repo overlay (docs/web)

Repo-local committed layer for the `docs-web` profile. The base contract
(`verifiers/verifier.base.md`) and kind (`verifiers/link-check.md`) do not resolve in this repo, so
this overlay is **self-sufficient**: it carries the full verifier instruction (run → assert → record
→ evidence) on its own. Role: prove every internal + external link in the CHANGED docs resolves — no
dead paths, no missing `#anchors`, no 404s. Bounded to `write_scope_touched` (`docs/**`, `README.md`,
`.agents/**/*.md`, ADRs) — no repo-wide crawl.

## How to run

1. **Internal links (repo-relative paths + `#anchor` fragments).** For each link in the touched docs,
   the target file exists in the tree, and where the link carries a fragment the heading slug exists
   in that file (kebab-cased heading). Resolve with `grep`/path checks, not fuzzy matching.
2. **Starlight content refs.** The site is Astro Starlight (`docs/web/`, content sourced live from
   `docs/**`); `pnpm --dir docs/web check` (astro check) flags broken content-collection / relative
   doc links. Scope failures to touched docs.
3. **External links (`http(s)://`).** Bounded reachability on links the changed docs ADD: prefer
   `lychee`/`markdown-link-check` if on PATH (guarded — absent tool is a skip, say so); else a bounded
   `curl -sSIL --max-time 10 -o /dev/null -w '%{http_code}'` loop. 2xx/3xx = live.

## Assert (positive + negative)

- **Positive:** every internal path resolves, every fragment maps to a real heading, every added
  external link returns 2xx/3xx.
- **Negative (discrimination — the check is not a no-op):** confirm a deliberately-broken link (dead
  path / missing `#anchor` / a known-404 URL) is CAUGHT as a failure. A checker that greens a dead
  link proves nothing.

## Record + evidence

`da workflow verify record --kind test --status pass|fail --verifier-type link-check --summary "..."`.
On fail, name the FIRST dead link (source doc:line → target) in the summary. Capture: internal/external
counts checked, the external tool used (or `[SKIPPED: no link checker]`), and the first failing link.
Any dead link ⇒ `--status fail`.
