# API verifier (per-type)

Composes on top of `verifier.base.md`. Your kind is **HTTP / contract verification**: prove the
changed request/response behavior holds. `--kind test` (in-process) or `custom` (live service),
`--verifier-type api`.

## What to run

1. **Scoped first.** Exercise only the routes / RPCs / handler tests that cover
   `write_scope_touched`.
   - Positive: expected status codes, happy-path payloads, idempotent retries.
   - Negative: invalid auth, malformed bodies, missing required fields, rate-limit/quota responses.
2. **Broader contract / perf (when the plan calls for it).**
   - Contract: OpenAPI / JSON-Schema diff against checked-in specs, golden responses, or
     consumer-driven fixtures.
   - Performance: bounded load/latency with explicit budgets; fail when a budget regresses without
     an approved change.

When a browser driver (Playwright etc.) is used for HTTP-visible behavior, treat network assertions
(HAR, route interception, status/body) as the API evidence surface and keep scoped-first; reserve the
`ui-e2e` kind for DOM/visual/a11y proof.

## Record

`da workflow verify record --kind <test|custom> --verifier-type api` — status, the scoped + broader
command lines, and `artifact_paths` (OpenAPI/golden diffs, HAR, perf summaries). Base URLs, auth
fixtures, perf budgets, and the touched-path → route/test mapping come from the repo-local override.
