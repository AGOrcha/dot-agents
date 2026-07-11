# Integration verifier — dot-agents repo overlay (web app_type)

Composes after `verifiers/verifier.base.md` (the contract) and `verifiers/integration.md` (the kind)
when present; in this repo those upstream layers resolve empty, so this overlay is **self-sufficient**
and carries the full verifier contract on its own.

## Role

Exercise a Go HTTP service's handlers/API **end-to-end, in-process** — the full router with its
middleware, not bare handler funcs and not a unit mock. You prove the wired service answers real
requests correctly, including error paths. This is the layer above `unit`; it catches routing,
middleware, serialization, and status-mapping regressions unit tests miss.

## How to run

1. **Scoped (required) — map `write_scope_touched` to the handler/router packages.** Prefer the
   in-process harness so no port/network flakiness:
   - Build the app's real router via its constructor (`NewRouter()` / `NewServer()` / `mux.Handler`),
     then either drive it with `httptest.NewRequest` + `httptest.NewRecorder`, or stand up a live
     `httptest.NewServer(router)` and hit it with an `http.Client`.
   - `go test -race -count=1 -timeout=180s <router/handler packages>` (add `-tags=integration` /
     `-run Integration` if the suite is tag- or name-gated). Route through the **full router** so
     auth, CORS, content-negotiation, and routing precedence are actually engaged.
2. **Full suite (final pass):** `go test ./... -race -count=1 -timeout=300s`.

## What to assert — positive AND negative

- **Positive:** happy-path routes return the expected **status** (200/201/204), **body** (JSON-decode
  then assert fields/shape, not a substring match), and **headers** (`Content-Type`, cache, auth,
  `Location` on create). Assert the request→handler→response round-trip.
- **Negative / error paths (required):** malformed/invalid body → 400 with the error envelope shape;
  missing/invalid auth → 401/403; unknown route/id → 404; wrong method → 405; downstream failure →
  500 surfaced (not a panic/hang). Assert the error **status and body**, never merely "not 200".
- **Wiring:** confirm middleware runs (auth rejects before the handler; CORS headers present) by
  testing through the router, not the bare handler.

## Record

```
da workflow verify record --kind test --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type integration \
  --command "go test -race -count=1 -timeout=180s <packages>" \
  --summary "<routes exercised, status/body/headers asserted per route, error paths covered, first failure>"
```

`--kind test` (the real surface accepts only `test|lint|build|format|custom|review`); the `integration`
identity rides `--verifier-type integration`, which writes the typed `integration.result.yaml`. Name
the first failing route/assertion in the summary. If the scoped run fails you may skip the full suite
but record `--status fail`. A service that builds and unit-passes but returns the wrong status/body for
a wired route is `--status fail`, not `pass`.
