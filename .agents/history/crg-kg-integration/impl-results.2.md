# Implementation Results 2

Date: 2026-04-11
Task: CRG+KG Integration — Phase D: Hot/cold note lifecycle bridge

## Summary

Wired the existing filesystem KG notes (hot layer) to the new SQLite `graphstore` warm layer via two new commands: `kg warm` and `kg link`.

## New Functions in `commands/kg.go`

- `graphstoreDBPath(kgHomeDir)` — returns `KG_HOME/ops/graphstore.db`
- `openKGStore(kgHomeDir)` — opens/creates the warm-layer SQLite store
- `noteToKGNote(note, filePath)` — converts a hot `GraphNote` to a `graphstore.KGNote`; sets `archived_at` for `archived`/`superseded` notes
- `runKGWarm(cmd, args)` — scans all `notes/` subdirs + `_archived/`, upserts each into `kg_notes`; sets `last_warm_sync` metadata
- `runKGWarmStats(cmd, args)` — prints warm layer counts (notes, links, code nodes/edges)
- `runKGLinkAdd(cmd, args)` — creates a `note_symbol_links` row
- `runKGLinkList(cmd, args)` — prints all links for a note ID
- `runKGLinkRemove(cmd, args)` — deletes a link by integer ID

## New CLI Surface

```
dot-agents kg warm [--type <note-type>]       # sync hot notes to warm DB
dot-agents kg warm stats                       # show warm layer stats
dot-agents kg link add <note-id> <qn> [--kind] # link note to code symbol
dot-agents kg link list <note-id>              # list all links for note
dot-agents kg link remove <link-id>            # remove link by ID
```

Link kinds: `mentions` (default), `implements`, `documents`, `decides`, `references`

## `runKGSetup` Integration

Phase D adds an `openKGStore` call to `runKGSetup` to initialize the SQLite schema on first `kg setup`. The warm DB is at `KG_HOME/ops/graphstore.db`.

## Tests (9 new in `commands/kg_test.go`)

- `TestRunKGSetup_InitializesWarmDB` — verifies DB file created on setup
- `TestRunKGWarm_IndexesNotes` — 5 notes from `setupKGWithNotes` all indexed
- `TestRunKGWarm_TypeFilter` — `--type entity` indexes only 2 entity notes
- `TestRunKGWarm_Idempotent` — double-run produces same count (upsert)
- `TestRunKGWarm_ArchivedNotesIndexed` — notes in `_archived/` indexed with `archived_at` set
- `TestNoteSymbolLink_AddListRemove` — full CRUD: add, list, remove
- `TestNoteSymbolLink_InvalidKind` — bad kind rejected
- `TestNoteSymbolLink_InvalidRemoveID` — non-integer ID rejected
- `TestRunKGWarmStats` — stats output without error

## Bug Fixed During Implementation

`runKGWarm` initially used `noteType + "s"` to derive subdirs (e.g. "entitys"), failing for irregular plurals. Fixed by using the existing `noteSubdir(t)` function which has the correct "entity" → "entities" mapping.

## Verification

```
go test ./... — all packages green
go run ./cmd/dot-agents kg warm --help → registered
go run ./cmd/dot-agents kg link --help → registered
```
