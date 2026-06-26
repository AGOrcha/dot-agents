# Prototype: config-profile selector-merge resolver

Empirical validation of the unified/hybrid config-profile model: a config
"profile" is a **selector-scoped config fragment**, resolved by a **flat
selector-merge cascade** with **no `extends` inheritance** (the explicit
anti-dependency-hell decision), governed by **layering policies** (locks,
precedence, override-permissions) that bind from higher-authority scopes down.

This is a proof-of-concept, not a production integration. It is a **self-contained
module** (`module proto/config-profiles`) so `go test ./...` at the repo root does
**not** pull it into the production coverage gate.

## Layout

- `types.go` — data model (`Profile`, `LayeringPolicy`, `Bundle`, `Context`, ...).
- `resolver.go` — the two-phase resolver.
- `load.go` — deterministic fixture loader.
- `main.go` — `da config explain`-shaped demo.
- `fixtures/` — concrete scenario units (role/app_type/stage profiles, a
  project additive override, org + team layering policies).
- `resolver_test.go` — the three proofs (H1, H7, H8).
- `mutation_check.sh` — proves the proofs are mutation-sensitive.

## Run

```sh
go test ./...
go run . --role orchestrator --app-type go-cli --stage orchestrate --scope project
./mutation_check.sh   # proves breaking the resolver breaks the proofs
```

## Resolution (two-phase)

**Phase 1 — effective policy.** Merge all `layering_policy` units across the scope
chain (`repo → project → user → team → org`), low authority first. A
higher-authority scope's precedence/locks/override-permissions bind lower scopes.

**Phase 2 — effective config.** Selector-merge every `profile` whose selector
matches the context, ordered by the effective precedence (local-wins tail).
Additive sets (`tools.allow`, `skills.*`, `hooks`, `mcp`) union; `deny` subtracts;
`model` is a last-writer scalar. Org `locks` are absolute; `override_permissions`
gate whether a scope may change a field. Output = effective bundle +
contributing refs + a stable 16-hex digest.

## Judgment calls (model under-specifies these)

1. **A profile's *source scope* vs its *selector scope*.** The brief's role/
   app_type/stage profiles have no selector scope, yet `override_permissions` is
   keyed by scope. The prototype adds an explicit `source_scope` field; unset it
   falls back to the selector scope, then to `repo` (the shared committed
   baseline). Without this, scope-less base profiles get gated out by a
   restrictive org permission map and the bundle resolves empty. **The model must
   pin down what scope a context-selected (non-scope-selector) profile is
   contributed from.**
2. **`override_permissions` semantics when the map is non-empty.** Chosen:
   non-empty map = allowlist; a scope absent from the map may change nothing; an
   *empty* map = unrestricted. The model doesn't state the default.
3. **Lock vs override-permission ordering.** A lock is absolute and applied after
   all profiles merge (so it always wins, even over a granted field). Locks and
   permissions are orthogonal layers; the model implies but doesn't state this.

See the PR description / report for the full findings.
