# TTRPG dogfood adapter

The first real graph-backend adapter, authored to dogfood the shipped adapter
contract (graph-backend-adapter-contract §13.3). Plan task `t3-ttrpg-dogfood`.

This is a **sandbox** adapter: it loads from this local source, not from a
registry. It exists to stress-test the contract and the `da-adapter-sdk`
against an open-ended domain (a TTRPG campaign — DMs invent new relationships
every session) and to surface real author-UX friction.

## Layout

| Path | What it is |
|---|---|
| `schema.yaml` | §4 adapter schema: 7 note types, 13 edge types, typed refs throughout. Validates against the shipped `registry.LoadSchema`. |
| `queries.yaml` | 8 named queries, each exercising a different v1 §5 DSL boundary case (annotated `exercises:`). |
| `corpus/` | Synthetic 10-session log corpus + format README. Substitutes for a live DM dogfood. |
| `oracle.yaml` | Machine-checkable hard-test truth: exact bootstrap counts + named-query results. |
| `bootstrap-skill/` | The Tier 2 bootstrap skill: `SKILL.md` scaffold, `bootstrap.go` core (uses the SDK exclusively), `oci/` package manifest + descriptor. |
| `WISHLIST.md` | §12 budget signals (4 points). Signals only — no grammar extension. |
| `AUTHOR-UX-NOTES.md` | The dogfood's real payload: authoring friction + recommendations. |

## Hard test

The §13.3 hard test runs in `internal/adapters/sdk/dogfood` (under `internal/`
so `go test ./...` reaches it — Go skips dot-prefixed dirs in wildcards):

```
go test ./internal/adapters/sdk/...
```

It (1) loads `schema.yaml` through the shipped registry, (2) bootstraps the
10-session corpus through the SDK and asserts the exact note/edge counts in
`oracle.yaml`, and (3) runs every named query with v1 semantics and asserts the
exact results in `oracle.yaml`. No human in the loop.

## SDK

The `da-adapter-sdk` Go surface this adapter's bootstrap uses lives at
`internal/adapters/sdk` (§8.4). It is the only storage access path — direct DB
connections are forbidden by contract (§8.2), and the namespace-token model is
enforced by the SDK's backing store.

## Deferred

The live "DM friend onboarded / DM-validated results" step is human-in-the-loop
and out of agent scope. The synthetic corpus + encoded oracle substitute for it
so the build is verifiable now. See `AUTHOR-UX-NOTES.md` § Deferred.
