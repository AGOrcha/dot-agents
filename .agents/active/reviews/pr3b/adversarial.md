# Adversarial Review — pr3b/workflow (PR#16)

_Lens 3 of 3 (adversarial / red-team), general-purpose fallback (codex runtime
Bash-blocked, as in pr3a), read-only._

## Verdict
**acceptable-with-findings**

SQL-injection claim (commit `6cde36e`) holds under exhaustive review: every
query is placeholder/`$N`/`= ANY($1)` parameterized; the only string-built
query (`sqlite.go:322`) concatenates `?` placeholders, never values. CRG
subprocess uses argv-separated `exec.Command` (no shell). Pool, migrations,
container cleanup, covprofile robust. Findings are hardening-tier.

## Critical / security findings
None. No exploitable injection or RCE.

## High

1. **Unbounded BFS depth via MCP — `internal/graphstore/mcp_server.go:335-352`.**
   `handleGetImpactRadius` clamps `depth` only on `<= 0`; an MCP client can pass
   `depth: 1000000`. This path routes to the CRG **Python subprocess**
   (`bridge.GetImpactRadius`), NOT the native bounded BFS, and never sets
   `MaxResults`. Native `bfsImpacted` (impact.go:55-58) caps maxDepth/maxNodes,
   but the subprocess path inherits no server-side ceiling — one malicious/buggy
   MCP call pins CPU/memory in Python. Fix: `min(depth, 6)` clamp + explicit
   `MaxResults`. ~3 lines.

## Medium / hardening

2. **`%q`-into-Python interpolation — `crg.go:687-711`.** `GetImpactRadius`
   builds the Python `changed_files` list with `fmt.Sprintf("%q", f)` spliced
   via `%s` into a `python -c` script. Not exploitable today (paths come from
   CRG-parsed repo files/DB rows) but Go/Python escape grammars differ —
   fragile defense-in-depth. Fix: `json.Marshal([]string)` yields a valid
   Python list literal. ~5 lines.

3. **Cross-process SQLite contention — `sqlite.go:32-40`.** `SetMaxOpenConns(1)`
   + WAL + `busy_timeout=5000` prevents corruption, but concurrent
   `da workflow` + MCP writers hit a hard `SQLITE_BUSY` after 5s. No
   corruption; user-visible flake under concurrent orchestrator use. Document
   or raise timeout (1 line).

4. **Container leak only on hard-kill — `sqlite_test.go:850-854`.**
   `pgTerminateAll()` runs in `TestMain` after `m.Run()`; `kill -9` leaves the
   container (Ryuk sidecar backstop only). Test-only, local-only. Acceptable.

## Hidden assumptions (all validated safe)

- `edgeRowIterator` swallows `Close()` but checks `Err()` on both backends —
  no hidden pool/conn leak; early-error returns precede `rows` use.
- Migrations version-less but fully idempotent (`CREATE ... IF NOT EXISTS`,
  order-independent) — mid-Exec kill cannot wedge. Latent risk only at the
  first non-idempotent migration.
- `OpenPostgres` closes the pool on every error path incl. ping failure.
- Draft-plan surfacing is advisory only — drafts excluded from
  `selectAllEligibleTasks`; hint string only; no path-traversal/YAML injection.
- covprofile cannot panic (bounded 4 MiB scanner, guarded indexes, lenient
  Atoi).
- "Untestable" rationalizations legitimate; latent only if a chan/func field
  is added to a marshaled struct (none today).
