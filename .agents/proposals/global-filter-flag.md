# Global `--filter` flag — Docker-CLI-style resource filtering on `da`

**Status:** project-local convention proposal (per `[[proposal-routing]]` — governs the
dot-agents CLI surface, not a shared `~/.agents/` resource).
**Scope:** a cross-cutting CLI affordance every `da` command that emits or acts on a **resource
collection** honors. Pairs with the existing global flags (`--json`, `-n`, `-f`, `-v`, `-y`) and
with the HTTP list endpoints of `[[api-conventions]]`.

This is a convention/decision doc — the rule and the shared mechanism it implies, not a task plan.

---

## Problem

Many `da` subcommands return or operate on **sets** of resources: workflow tasks and plans, KG
notes, skills, agents, rules, projects, review labels, and scores. Today each command filters its
own way — bespoke per-command flags (`--status`, `--type`, `--plan`), positional arguments, or no
filtering at all. There is **no uniform filter surface**, so:

- The same logical filter (`status=pending`) is spelled differently per command, or unavailable.
- Adding a filter to one command teaches a user nothing about the next.
- As the resource families grow (the `[[api-conventions]]` domains — `workflow`, `review`, `kg`,
  `observability`, `config`, `registry`), the per-command flag sprawl compounds.

Docker solved the same problem with one repeatable, uniform `--filter` flag
(`docker ps --filter status=running --filter name=web`). `da` should adopt the same shape.

---

## Proposal

A **global, repeatable `--filter <key><op><value>` flag**, honored by any command that emits or
acts on a resource collection. Repeats are **AND-combined**.

```
da workflow eligible --filter status=pending
da plan list         --filter plan=config-v2
da kg query          --filter type=decision --filter status=active
da skills list       --filter platform=claude
```

### Operator set

| Operator | Meaning                     | Example                          |
|----------|-----------------------------|----------------------------------|
| `=`      | equals (eq)                 | `--filter status=pending`        |
| `!=`     | not equals                  | `--filter status!=completed`     |
| `=~`     | regex match                 | `--filter name=~^p[0-9]`         |
| `>` `<` `>=` `<=` | numeric compare    | `--filter score>0.8`             |

- `=` (eq) is the baseline every filterable command supports. `!=`, `=~`, and numeric comparators
  are supported where the field type allows (regex on string fields; `>`/`<` on numeric fields such
  as review `score`).
- **Unknown filter keys error** with the valid-key list for that command, in the repo's hint-style
  error voice — e.g. `unknown filter key "stauts"; valid keys for "workflow eligible": status,
  plan, type, depends_on`. No silent no-op.
- **Multiple `--filter` flags AND together.** OR-within-a-key is an open question (below).

---

## Mechanism

Wire the flag **once**, not per command — mirroring how `--json`/`-n` are persistent global flags
on the root command (`commands/root.go`, `Flags` struct; see `[[api-conventions]]`'s consumers and
the global-flag coverage harness in `internal/globalflagcov`).

- **Shared `cmdutil` filter parser.** A small package parses `--filter` tokens into a typed
  `[]FilterClause{ Key, Op, Value }`, validates operator syntax, and is registered as a persistent
  `StringArrayVar` on the root command (alongside the existing `--dry-run`/`--json` plumbing). One
  registration point; commands opt in by consuming the parsed clauses.
- **A `Filterable` interface resources implement.** Each resource type (or its list provider)
  exposes its filterable fields and their types:

  ```go
  type Filterable interface {
      FilterKeys() []FilterKey      // {name, kind: string|numeric|enum}
      FilterValue(key string) (any, bool)
  }
  ```

  The shared parser then matches parsed clauses against `FilterValue`, applies the operator by field
  kind, and produces the valid-key list for the unknown-key error from `FilterKeys()`. A command
  that returns a `[]Filterable` gets filtering for free; the eq/`!=`/regex/numeric matching lives in
  the shared layer, never re-implemented per command.
- This is the same "share the cross-cutting flag, let each command opt its data in" pattern the
  global flags already use — the flag is defined once and the per-command cost is implementing
  `Filterable` on the resource.

---

## Scope / rollout

Versioned and incremental — first the highest-traffic collection commands, then widen:

1. **Wave 1:** `workflow eligible` / `workflow` list views, `plan list`, `kg query`.
2. **Wave 2:** `skills list`, `agents list`, `rules list`, `projects` listing.
3. **Wave 3:** `review` labels/scores (exercises `>`/`<` numeric on `score`).

Each command joins by implementing `Filterable` on the resources it emits; the flag and parser are
already wired, so a wave is "teach these resources their filter keys," not "add a flag."

**Pairs with `--json`** (filter then format): `--filter` narrows the set, `--json` renders it —
`da kg query --filter type=decision --json`. **And with the `[[api-conventions]]` list endpoints:**
the HTTP `?filter=` query param is the API analogue with the **same key/op/value semantics**, so a
filter expression means the same thing on the CLI and over `GET /api/v1/<domain>/<resource>`. One
filter grammar, two front doors.

---

## Open questions

- **Filter-key discovery.** How does a user learn the valid keys without triggering an error? A
  `da <cmd> --filter-keys` (prints the `FilterKeys()` set and types) vs. surfacing them only in the
  unknown-key error vs. listing them in `--help`. Leaning toward `--filter-keys` plus inclusion in
  the error hint.
- **Case sensitivity.** Are keys and `=` values case-sensitive? Proposal: keys case-insensitive,
  values case-sensitive unless a field is declared case-insensitive (enums).
- **Multi-value OR within one key.** Docker allows `--filter status=pending,in_progress` as an OR
  over one key while distinct keys AND. Adopt comma-OR-within-key, or require regex (`status=~^(pending|in_progress)$`)?
  Comma-OR is more discoverable but adds grammar; defer the decision to Wave 1 implementation.

---

## Relationship to other specs

- `[[api-conventions]]` — the HTTP `?filter=` query param is the API analogue of this flag; same
  key/op/value grammar across CLI and the `/api/v1/<domain>/<resource>` list endpoints.
- `[[proposal-routing]]` — routes this as a project-local CLI convention.
