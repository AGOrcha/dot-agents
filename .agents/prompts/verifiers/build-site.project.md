# Build-site verifier — dot-agents repo overlay (docs/web)

Repo-local committed layer for the `docs-web` profile. The base contract
(`verifiers/verifier.base.md`) and kind (`verifiers/build-site.md`) do not resolve in this repo, so
this overlay is **self-sufficient**: it carries the full verifier instruction (run → assert → record
→ evidence) on its own. Role: prove the docs site builds cleanly — no broken content refs, no
unresolved imports, no render errors. The site is **Astro Starlight** at `docs/web/` (content sourced
live from `docs/**`).

## How to run

1. **Deps (only if `docs/web/node_modules` absent):** `pnpm --dir docs/web install --frozen-lockfile`.
2. **Type + content-ref check:** `pnpm --dir docs/web check` (astro check) — resolves the content
   collections, TS, and relative doc links.
3. **Full build:** `pnpm --dir docs/web build` — runs the `prebuild` schema copy then `astro build`
   for both the public and `INTERNAL_BUILD=1` internal variants. Must exit 0.

## Assert (positive + negative)

- **Positive:** `build` exits 0; `check` reports 0 errors; no unresolved content ref, missing import,
  missing copied schema (prebuild), or mermaid/render failure.
- **Watch the silent-warning class:** Astro downgrades some broken refs to `[WARN] ... could not be
  resolved`. Scan the build/check output for `could not be resolved` / `[WARN]` on a touched page and
  treat an unresolved ref as **fail even if the exit code is 0** — the exit code alone is not the gate.
- **Negative (discrimination):** confirm the build actually FAILS on a bad ref (a broken content link
  or import surfaces as a non-zero build / an error line), not a silent green.

## Record + evidence

`da workflow verify record --kind test --status pass|fail --verifier-type build-site --summary "..."`.
Capture: the `astro check` summary (errors/warnings/hints counts), the build exit status for both
variants, and — on fail — the failing page + ref. A non-zero build or an unresolved-ref warning on a
touched page ⇒ `--status fail`.
