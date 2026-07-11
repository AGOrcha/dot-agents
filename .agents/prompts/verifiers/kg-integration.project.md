# KG-integration verifier — dot-agents repo overlay

Repo-local committed layer. In this repo the base (`verifiers/verifier.base.md`) and kind
(`verifiers/kg-integration.md`) layers resolve unresolved, so this overlay is **self-sufficient**: it
carries the full verifier contract for the daemon's KG read/write integration.

`--kind test` (conformance + race), `--kind custom` (CLI warm/query lane), `--verifier-type
kg-integration`.

## Role

Prove the daemon's KG integration reads and writes the graph correctly, keeps it consistent, and
never corrupts it under concurrent access. The daemon is **Path B** (persistent local daemon owning
the pool + warm CRG worker) — a *transparent provider swap behind the unchanged `graphstore.Store`
contract* (`internal/graphstore/CONTRACT.md`). "Correct" therefore means: the daemon-backed provider
passes the same conformance the SQLite/Postgres backends do, plus the daemon-specific concurrency
guarantees.

## 1. Conformance against a KG fixture (required)

Run the shared backend-agnostic runners in `internal/graphstore/internal/storetest` against the
daemon-backed `Store` (an `OpenStore` closure that dials the daemon over a fixture `KG_HOME`):
`go test -race -count=1 -timeout=180s ./internal/graphstore/... <daemon-kg-package>`

- **Positive (round-trip):** `RunNodeRoundTrip` (read-back Name/Language/LineStart match),
  `RunKGNoteRoundTrip` (Title/NoteType/Version match), `RunKGNoteSearch` (a shared term hits both
  notes), `RunMetadataRoundTrip` (set→get, overwrite-wins), `RunEdgeUpsertCreate/Update` (stable ID
  on re-upsert), `RunNoteSymbolLink*` (idempotent link). A read that returns different bytes than
  were written is corruption → `--status fail`.
- **Negative:** a missing metadata key returns `""` (not an error/panic); a query for an absent node
  id returns empty, not a crash; a batch that hits an encode error rolls back leaving NO partial row
  (`StoreFileNodesEdges` atomicity).

## 2. No corruption under concurrent access (required)

Drive N concurrent writers + readers through the daemon under `-race`:

- **Assert:** the graph stays consistent — reads never observe a torn/partial write; write-
  serialization is the provider's job (SQLite single-writer + WAL + `busy_timeout=5000`, a pool, or
  the daemon).
- **Negative (SQLite backend):** heavy concurrent writes surface a clean `SQLITE_BUSY` after ~5s of
  contention — a *user-visible flake, never data loss or DB corruption*. Confirm the DB still opens
  and passes `da kg lint` afterward.
- A race reported by `-race`, a deadlock (timeout), or a corrupt graph is a terminal fail.

## 3. Warm + query paths (required)

Against the fixture `KG_HOME` via `./bin/da`:

- `da kg warm` → asserts `notes indexed` (hot FS notes land in the warm SQLite layer).
- `da kg query --intent source_lookup "<seeded-id>"` → returns the ingested id; `da kg query
  --intent graph_health` → `status=healthy`.
- `da kg lint` → 0 errors on the clean fixture; on a deliberately-broken fixture it reports the
  `broken_links` / `orphan_pages` / `contradictions` it should catch (negative path).

## 4. Record

```
da workflow verify record --kind test --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type kg-integration \
  --command "go test -race ./internal/graphstore/... <daemon-kg-pkg> && da kg warm && da kg lint" \
  --summary "<conformance result, concurrency/-race outcome, warm/query/lint asserts, first failure, evidence>"
```

Evidence: the `-race` output (clean, or the first race stack), the `da kg lint` report, warm/query
output, and `graph_health`. A daemon that answers but returns stale/wrong graph data is
`missing-feature`, not `ok`. If the conformance/race run fails you may skip the warm/query lane but
record `--status fail`.
