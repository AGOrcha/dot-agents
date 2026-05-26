# Fold-back: t6 sqlite_test.go + postgres_test.go need storetest extension, not internal/testutil

**Source task**: `go-test-fixture-extraction / t6-extract-graphstore-tests`
**Observed**: 2026-05-26
**Severity**: medium (anti-scope vs. intended outcome mismatch in the bundle)

## Observation

The t6 bundle's prompt frames the task as "use internal/testutil including the
t2_5-added NewTempAgentsHome + WriteScopeFilePath helpers" and lists three
target files. But the **authoritative audit** (T1's design.md, lines 167 and
181-186) categorises these files differently:

| File | Shape (per audit) | Audit-prescribed target |
|---|---|---|
| `internal/graphstore/sqlite_test.go` | Graphstore sub-helper (↔ postgres_test.go) | `internal/graphstore/internal/storetest` |
| `internal/graphstore/postgres_test.go` | Graphstore sub-helper (↔ sqlite_test.go) | `internal/graphstore/internal/storetest` |
| `internal/graphstore/mcp_server_test.go` | Self / table-drive | in-file local helpers |

design.md §"Shape distribution":
> **Graphstore sub-helper (2 files):** sqlite_test.go, postgres_test.go.

design.md §"Notes on what is deliberately not in [the internal/testutil table]":
> `openTestStore` / `openPGTestStore` — handled by the graphstore-internal `storetest`
> sub-helper (decisions section), **not by `internal/testutil`**.

The 3 cross-file blocks design.md flagged (sqlite_test.go:135/505/559 ↔
postgres_test.go:134/369/423) were **already** consumed by the existing
`storetest.RunNodeRoundTrip` / `RunKGNoteRoundTrip` / `RunKGNoteSearch` runners
landed in t2.

## What this worker did and did not do

- **Done in scope** — `mcp_server_test.go` refactored with two file-private
  helpers (`runMCPCallExpectErrorCode`, `decodeResultMap`); 10 call sites
  collapsed; tests + vet + gofmt clean.
- **Not done** — sqlite_test.go and postgres_test.go received no commits.

The remaining cross-file duplication in sqlite/postgres covers patterns the
existing storetest runners don't yet cover:

- `Metadata_RoundTrip` / `Metadata_MissingKey` / `Metadata_Overwrite` (3 mirrored
  tests each, ~30 lines per backend)
- `UpsertEdge_Create` / `UpsertEdge_Update` (2 mirrored tests each, ~24 lines)
- `NoteSymbolLink_RoundTrip` / `NoteSymbolLink_Idempotent` (2 mirrored tests
  each, ~30 lines)

Migrating these requires **new exported runners in
`internal/graphstore/internal/storetest/storetest.go`** (e.g.
`RunMetadataRoundTrip(t, open, keyPrefix)`, `RunEdgeUpsertCreate(t, open,
namePrefix)`, `RunNoteSymbolLinkRoundTrip(t, open, idPrefix)`), each
parameterized with a per-backend key prefix so the shared Postgres
testcontainer doesn't see cross-test row collisions.

That file is **outside the t6 bundle's `write_scope`**, which is restricted to:

```yaml
write_scope:
  - internal/graphstore/sqlite_test.go
  - internal/graphstore/postgres_test.go
  - internal/graphstore/mcp_server_test.go
```

Per `~/.agents/profiles/loop-worker.md` discipline ("if a needed file is
outside scope, stop and write a fold-back observation — do not expand scope")
and the bundle's own anti-scope note ("If you find a duplicated pattern that
warrants a new testutil helper, STOP and report"), this worker did not extend
storetest.

## Decision needed from orchestrator

Pick one of:

1. **Re-fanout t6 with widened `write_scope`** — add
   `internal/graphstore/internal/storetest/storetest.go` to the scope and let
   the next t6 worker land the three new runners + sqlite_test + postgres_test
   migrations as 3 commits (one extension + one per consumer file).
2. **New downstream task `t6b-extend-storetest`** — split storetest extension
   into a t6b task (Tier T2, depends-on t2_5), then t6c covering sqlite +
   postgres migrations. Keeps the per-task commit boundary clean.
3. **Accept partial t6 closure** — close t6 on mcp_server_test alone, file
   the sqlite + postgres residual as deferred Cluster D work for t8 closeout
   to surface in the SonarCloud gate poll.

Recommendation: **(2)**. The storetest extension is a discrete, audit-anticipated
change (design.md §"Graphstore sub-helper" already specs the three runner
names); putting it behind its own task lets a reviewer reason about the
runner shape (keyPrefix parameterization) separately from the consumer
migrations.

## Per-file before/after counts (from this worker's session)

| File | Before LOC | After LOC | Delta | Cluster D dup-line reduction |
|---|---|---|---|---|
| `mcp_server_test.go` | 1223 | 1225 | +2 | ~30 dup lines collapsed (10 sites × 3-line patterns) into 2 helpers (~30 lines new); net dup density reduction is what matters, not raw LOC |
| `sqlite_test.go` | 854 | 854 | 0 | unchanged — needs storetest extension |
| `postgres_test.go` | 509 | 509 | 0 | unchanged — needs storetest extension |

## Action

- Orchestrator picks decision (1) / (2) / (3) above.
- This fold-back becomes starting context for whichever follow-up.
- t6 merge-back records the partial closure honestly.
