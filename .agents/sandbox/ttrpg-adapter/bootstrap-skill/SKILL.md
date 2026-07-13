---
name: ttrpg-campaign-bootstrap
description: Bootstrap a TTRPG campaign graph from a corpus of session logs. Parses each session, upserts campaign entities (characters, factions, locations, events, items, quests) idempotently by id, and writes them into the ttrpg adapter namespace via the da-adapter-sdk.
argument-hint: "[--corpus <dir>] [--mode full|incremental] [--since <session>]"
---

# TTRPG Campaign Bootstrap

Tier 2 OCI bootstrap skill for the `ttrpg` graph-backend adapter
(graph-backend-adapter-contract §8.4, §9, §13.3). Populates scoped KG storage
from a corpus of session logs — the first real adapter-author bootstrap that
dogfoods the SDK contract.

## Contract boundaries (non-negotiable)

- **SDK-only storage access (§8.2).** This skill MUST reach storage exclusively
  through `da-adapter-sdk` (`internal/adapters/sdk`). It never opens a DB
  connection, never sees a namespace string it did not declare, and cannot
  widen its own authority — the SDK derives a `{ttrpg, write}` bootstrap token
  for it (§8.2 derivation rule N2).
- **Own namespace only (§8.3).** Bootstrap writes only to `ttrpg`. Cross-adapter
  reads are not reachable here — they require a materialized view's
  `reads_from` declaration. The TTRPG adapter declares none.
- **No raw SQL, no DSL grammar growth (§2.2, Anti-scope).** Authored entirely
  within v1.

## Behavior

The executable core is [`bootstrap.go`](./bootstrap.go) (`bootstrap.Run`). It:

1. Globs `session-*.json` under the corpus dir (default `./corpus`), sorted.
2. Parses each session log (structured shape — see `../corpus/README.md`).
3. Upserts notes **idempotently by id** (last writer wins on fields), so an
   entity introduced in session 1 and updated in session 4 is one note that
   evolves — modeling a persistent, mutating campaign world.
4. Synthesizes one `session` note per log and a `documents` edge from the
   session to each event it introduced (provenance anchor / §7.3 derivation
   source).
5. Bulk-writes via `sdk.WriteNotes` / `sdk.WriteEdges`, and fires a
   `session.recorded` env predicate per session via
   `sdk.DeclarePredicateFired` (§8.4.1).

### Modes (§11.1 build/update parity shape)

- `--mode full` (default): bootstrap the whole corpus.
- `--mode incremental --since <session>`: re-parse only sessions after a
  number — the same upsert path, scoped to new logs. (Incremental scoping is
  scaffolded for the parity contract; the full path is what the hard test
  exercises today.)

## Hard test

`internal/adapters/sdk/dogfood` runs `bootstrap.Run` over the 10-session
synthetic corpus and asserts the exact note/edge counts and named-query results
in [`../oracle.yaml`](../oracle.yaml). This is the no-human-in-the-loop
substitute for live DM-validated results (the live DM dogfood is deferred — see
the PR body and `../AUTHOR-UX-NOTES.md`).

## Packaging

This skill ships as a Tier 2 OCI artifact (§9). The artifact manifest +
descriptor live in [`oci/`](./oci/). `scaffold + manifest is sufficient` for
this task — no live registry is stood up; the digests in
`oci/ttrpg-campaign-bootstrap.manifest.json` are placeholders a real publish
step (`da` external-agent-sources tooling) recomputes at push time.
