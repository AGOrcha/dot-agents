# Evidence — dashboard live-smoke harness (baseline, proven)

The verification standard for every task in this plan: **build the shipped artifact, spin it
up, exercise the real path, and assert machine-checkable output** (HTTP status, `jq` on JSON,
SSE first-byte, `lsof` bind, exit codes) — NOT a passing `*_test.go` alone. This file records
the reference harness, run for real against the currently-shipped `cmd/da-dashboard` on
2026-07-18 so the plan's smokes extend a proven baseline rather than a hypothetical.

## Baseline run (shipped `cmd/da-dashboard`, real iter-log data, 142 iter files)

```sh
go build -buildvcs=false -o /tmp/da-dashboard ./cmd/da-dashboard
/tmp/da-dashboard --iter-log-dir <repo>/.agents/active/iteration-log --addr 127.0.0.1:7399 &
B=http://127.0.0.1:7399
```

Machine-checkable assertions (all PASS at baseline):

| Check | Command | Expected | Grounds task |
|---|---|---|---|
| liveness | `curl -s -o/dev/null -w '%{http_code}' $B/api/health` | `200` | t1 |
| health status | `curl -s $B/api/v1/observability/health \| jq -r .data.status` | `ok` | t1 |
| runs is array | `curl -s $B/api/v1/observability/runs \| jq -e '(.data\|type=="array") and (.data\|length>0)'` | exit 0 (3 runs) | t1 |
| **deny-by-default** | `curl -s -X POST -o/dev/null -w '%{http_code}' $B/api/v1/observability/runs` | `405` | t5, t6 |
| **loopback-only bind** | `lsof -nP -iTCP:7399 -sTCP:LISTEN` → 127.0.0.1 only, no `*`/`0.0.0.0` | `1:0` | t7 |
| SSE first-byte | connect `$B/api/v1/observability/events`, time to first byte | `< 5s` (heartbeat `:`) | t6, t8 |

Result: **6/6 PASS.** POST→405 and loopback-only bind are the load-bearing security
observations already true of the shipped artifact — the plan's job is to keep them true (and
add the cap/coalescing/payload-hygiene/authz checks) under the new subsystem + runtime
composition.

## How each task consumes this harness
- Every task's `verification_required` smoke = build the task's shipped artifact, spin it up
  (`da dashboard serve` for the subsystem; the slim release entrypoint for t4/t4a; `da service`
  for the mounted runtime), then run the assertions above PLUS the task's own new assertion,
  capturing exit codes / status / `jq` — never "tests passed" prose.
- t12-verify-close runs the FULL set live against the shipped runtime as the plan's end-to-end
  proof.
