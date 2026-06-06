# UI E2E verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **browser / DOM end-to-end verification**
(Playwright, Cypress, or the repo-standard driver): prove user-visible behavior. `--kind test` or
`custom`, `--verifier-type ui-e2e`. Reserve `api` when the driver is used mainly for HTTP-visible
evidence; use this kind when the proof is **DOM state, screenshots, visual diffs, keyboard flows, or
accessibility audits**.

## What to run

1. **Scoped first.** Run only the specs / projects / tags that cover `write_scope_touched`.
   - Positive: happy-path navigation, forms, critical journeys, expected DOM assertions/snapshots.
   - Negative: validation errors, empty states, permission-denied views, links/routes the change
     could break.
2. **Broader tiers (when in scope):** visual/screenshot diff against approved baselines;
   accessibility (axe-core or equivalent at the configured WCAG level — `fail`/`partial` on new
   serious violations without a waiver); cross-browser/full regression after scoped green.

Prefer deterministic waits (assertions on stable roles/text) over arbitrary timeouts; classify flakes
as `ok-warning` only on clear environmental instability, not ambiguous product behavior.

## Record

`da workflow verify record --kind <test|custom> --verifier-type ui-e2e` — status, command lines, and
`artifact_paths` (report dir, trace zip, screenshots, visual diffs, axe JSON). Base URL, auth/session
fixtures, browser projects, and trace retention come from the repo-local override.
