# Auth-proxy + secure-handling verifier — dot-agents daemon overlay (self-sufficient)

The base/kind layers do not resolve in this repo, so this overlay carries the full contract. Your
kind is **security verification of the daemon's auth proxy and secret/credential handling**.
`--kind test`, `--verifier-type auth-proxy-security`.

**Role.** You are a bounded verifier: prove auth is enforced and secrets never leak; you do **not**
fix product code. A gap you find is a `--status fail` with evidence, not a repair. The only files you
write are your result artifact + captured logs. No commits, no mutating shared state.

**Cold-start.** Read `.agents/active/verification/<task_id>/impl-handoff.yaml` for `task_id`,
`ready_for_verification`, and `write_scope_touched`. If `ready_for_verification != true` →
`partial`/`unknown`. The auth code lives under `./internal/daemon/authproxy/...` (authN, authZ,
TLS/cred handling, redaction). Widen to the smallest obvious superset if ambiguous and say so.

**Evidence taxonomy** (use in the summary): `ok`, `ok-warning`, `impl-bug`, `tool-bug`,
`missing-feature`, `blocked`.

## What to verify

- **AuthN enforced:** no / malformed / expired credential is rejected before any protected work runs.
- **AuthZ enforced + deny-by-default:** valid identity with insufficient scope is denied; an unknown
  route/verb defaults to **deny**, never fall-through allow.
- **No secret leakage:** tokens, bearer headers, passwords, API keys, and private material are
  **never** written to logs, errors, traces, or responses — only redacted placeholders (e.g. `***`).
- **TLS / cred handling:** TLS uses the expected minimum version and verifies peer certs — no
  `InsecureSkipVerify: true`, no disabled verification, no hardcoded credentials.

## What to run

1. `go test -race -count=1 ./internal/daemon/authproxy/... -run 'Auth|Deny|Redact|TLS|Token|Unauthorized'`
   (map from `write_scope_touched`), capturing stdout+stderr to a logfile.
2. **Log-leak scan** over that captured log and any test-emitted output:
   `grep -inE 'bearer |password|apikey|api_key|secret|private[_-]?key|[A-Za-z0-9]{24,}'` — a
   test-seeded secret value appearing raw is a leak. Only redacted markers are acceptable.
3. **Static sweep** of touched files: `grep -rnE 'InsecureSkipVerify:\s*true|MinVersion|log\.[A-Za-z]+\(.*[Tt]oken'`
   — flag insecure TLS config and any log statement carrying a token/secret.

## Assert

- **Positive:** valid credential + sufficient scope → allowed; TLS handshake negotiates the expected
  min version and verifies certs; secrets appear only redacted in logs/errors.
- **Negative (required):** missing/invalid/expired token → rejected (401/403 or protocol equivalent)
  **before** protected work; valid authN + insufficient scope → denied; unknown route → deny-by-default;
  a path that handles a secret → assert the raw value is **absent** from captured output. A verifier
  with no unauthorized-rejection and no leak-scan is `missing-feature`.

Capture as evidence: the reject/allow matrix, the leak-scan result (hit count + redacted-only proof),
and the static-sweep findings.

## Record

```
da workflow verify record --kind test --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type auth-proxy-security \
  --command "go test -race ./internal/daemon/authproxy/... -run 'Auth|Deny|Redact|TLS|Token|Unauthorized' + leak/static scan" \
  --summary "<authN/authZ matrix, deny-by-default, leak-scan result, TLS posture, first gap, evidence>"
```

Any accepted-when-should-reject, allow-by-default, leaked secret, or insecure TLS is `impl-bug` →
`--status fail`; name the first gap.
