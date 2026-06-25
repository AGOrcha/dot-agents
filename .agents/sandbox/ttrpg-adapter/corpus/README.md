# Synthetic session-log corpus

Ten session logs (`session-01.json` … `session-10.json`) for the TTRPG dogfood
hard test. This corpus **substitutes for the human-in-the-loop DM dogfood** so
the build is verifiable now (the live "DM friend onboarded / DM-validated
results" step is explicitly deferred — see PR body and AUTHOR-UX-NOTES.md §
Deferred).

## Format

Each file is one session log in a small structured schema the bootstrap skill
parses (`bootstrap-skill/parse.go`). Real DM session logs are freeform prose;
this corpus uses a structured shape so the parse is deterministic and the
expected note/edge counts are an exact oracle (`../oracle.yaml`) rather than an
NLP guess. A production bootstrap skill would run prose through an extraction
model and land on the same note/edge shapes — the structured corpus pins the
*output* contract the hard test asserts, which is what matters for dogfooding
the adapter contract.

```jsonc
{
  "session": { "number": 1, "title": "...", "played_at": "2026-01-04" },
  "notes": [
    { "id": "char:mara", "type": "character",
      "fields": { "name": "Mara", "kind": "pc", "status": "alive",
                  "stated_location": "loc:ironhold", "allegiance": "fac:wardens" } }
  ],
  "edges": [
    { "type": "present_at", "from": "char:mara", "to": "evt:s1-ambush" }
  ]
}
```

Notes are **idempotent by id across sessions**: a character introduced in
session 1 and updated in session 4 is the same note id; the bootstrap upserts
(last writer wins on fields). This models a campaign where entities persist and
evolve. The oracle counts **distinct** ids, not raw note rows.

The `session` block becomes a `session` note; every note/edge a session
introduces is linked back to it via a `documents` edge at bootstrap (provenance
anchor, §7.3 derivation source).
