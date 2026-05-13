# Proposal: Live `config explain` Surface

## Problem

The config-distribution spec already defines `da config explain [field-path]`, but the current repo
does not yet have a live `config` command tree or a concrete implementation plan that separates:

1. broad effective-config inspection
2. provenance debugging
3. quick extraction of one specific value

Without that split, operators end up with two bad choices:

- read design docs for the intended behavior
- inspect `.agentsrc.json` or future layer files manually and reconstruct the winning value by hand

That is especially painful for fields like:

- `app_type_verifier_map.pa-angular-ui`
- `verifier_profiles.pa-ui-e2e`
- feature flags
- future `app_type` profile refs

---

## Root Cause

The intended command contract exists in spec form, but three operator needs are still conflated:

### 1. Effective config inspection

"What is the winning value right now?"

### 2. Provenance

"Which layer set it, and which layers lost?"

### 3. Narrow extraction

"Give me only the one value I need so I can use it in a script, error message, or follow-on command."

The current `da explain` command is not the answer. It is a static human documentation surface,
not repo-state inspection.

---

## Proposed Solution

Implement the live `config` command subtree already reserved by the design docs, with `explain` as
the first operator-facing entry point.

```text
da config explain [field-path]
```

This command should answer one question well:

> What is the effective value of this config field for this repo, and where did it come from?

It should work for both broad inspection and one-field extraction.

---

## Scope

### What `config explain` should own

- effective value lookup
- layer provenance
- optional full-config dump
- feature-flag resolution inspection
- machine-readable output for scripts and editors

### What `config explain` should not own

- authoring heuristics like "which app_type key should I prefer in TASKS.yaml?"
- domain-specific shortcuts like `workflow app-types`
- human concept documentation already covered by `da explain`

This keeps `config explain` as the generic introspection primitive, while higher-level workflow
commands can stay optimized for authoring.

---

## Command Contract

### 1. Single field

```text
da config explain app_type_verifier_map.pa-angular-ui
```

Default output should include:

- field path
- effective value
- winning layer
- full layer stack when available

Example:

```text
$ da config explain app_type_verifier_map.pa-angular-ui

Field:   app_type_verifier_map["pa-angular-ui"]
Value:   ["pa-ui-unit", "pa-ui-lint", "pa-ui-e2e"]

Layer stack:
  [1] product defaults           -> not set
  [2] user-local                 -> not set
  [3] provider-admin:lang/ui     -> ["pa-ui-unit", "pa-ui-lint"]
  [4] provider-admin:app/ui      -> ["pa-ui-unit", "pa-ui-lint", "pa-ui-e2e"]   <- active
  [5] repo-local .agentsrc.json  -> not set
```

### 2. Full effective config

```text
da config explain --all
```

Outputs the full effective config object, annotated by winning layer or accompanied by provenance
metadata in JSON mode.

### 3. Feature flags

```text
da config explain --flags
```

Shows resolved feature flags across all layers, including enabled state and winning layer.

---

## Easy Single-Piece Extraction

This is the missing operator convenience.

The command should support a narrow output mode so callers do not have to parse human prose when
they only need one specific value.

### Suggested flags

```text
da config explain app_type_verifier_map.pa-angular-ui --value-only
da config explain app_type_verifier_map.pa-angular-ui --origin-only
da config explain app_type_verifier_map.pa-angular-ui --json
```

### `--value-only`

Print only the effective value.

Examples:

```text
$ da config explain app_type_verifier_map.pa-angular-ui --value-only
["pa-ui-unit","pa-ui-lint","pa-ui-e2e"]
```

```text
$ da config explain app_type --value-only
pa-angular-ui
```

This is the easiest way to get one specific piece of info into a script, clipboard action, or
follow-on command.

### `--origin-only`

Print only the winning layer identity.

Example:

```text
$ da config explain app_type_verifier_map.pa-angular-ui --origin-only
provider-admin:app/ui
```

### `--json`

Machine-readable structured output.

Suggested single-field shape:

```json
{
  "field": "app_type_verifier_map.pa-angular-ui",
  "value": ["pa-ui-unit", "pa-ui-lint", "pa-ui-e2e"],
  "active_layer": "provider-admin:app/ui",
  "layers": [
    {"layer": "product-defaults", "value": null, "active": false},
    {"layer": "provider-admin:lang/ui", "value": ["pa-ui-unit", "pa-ui-lint"], "active": false},
    {"layer": "provider-admin:app/ui", "value": ["pa-ui-unit", "pa-ui-lint", "pa-ui-e2e"], "active": true}
  ]
}
```

---

## Field Path Rules

The field-path syntax should make narrow lookup predictable.

### Examples

- `app_type`
- `app_type_verifier_map.pa-angular-ui`
- `verifier_profiles.pa-ui-e2e`
- `flags.graph_bridge`

### Rule

Use dot-separated traversal for object keys. The printed output may normalize to bracket notation
for clarity, but the input path should remain short and easy to type.

If a key itself contains dots in the future, the command can add a quoted path form later. Do not
block the first implementation on generalized path escaping.

---

## Relationship To `workflow app-types`

The two commands should be siblings, not competitors.

### `da config explain`

- generic config introspection
- exact value / origin lookup
- layer provenance
- scriptable field extraction

### `da workflow app-types`

- authoring-focused shortcut
- lists valid app types for the current repo
- marks aliases vs preferred authoring keys
- prints ready-to-paste task / plan / flag snippets

`workflow app-types` should not shell out to `config explain`, but both should use the same shared
effective-config resolver package once config-distribution lands.

---

## Implementation Plan

### Phase 1: land shared effective-config snapshot API

Before wiring the CLI, introduce a reusable resolver API that returns:

- effective config object
- per-field provenance metadata
- flag resolution view

This avoids duplicating resolution logic between:

- `da config explain`
- `da workflow app-types`
- future `da config lint|verify|sync`

### Phase 2: implement `da config explain`

Add a new `config` subtree under the root command and wire:

- `da config explain [field-path]`
- `da config explain --all`
- `da config explain --flags`

### Phase 3: refactor `workflow app-types`

Once Phase 1 exists, update `workflow app-types` to consume the same effective-config snapshot API
instead of reading `.agentsrc.json` directly.

That gives the authoring shortcut full layered visibility without changing its user-facing contract.

---

## Suggested Command Placement

Use the config subtree already reserved in the design docs:

```text
da config explain
```

This is the correct home because the command is not workflow-specific and not documentation-only.

---

## Implementation Scope

| File | Change |
|---|---|
| `commands/root.go` | Add `NewConfigCmd()` |
| `commands/config.go` or `commands/config/*` | New config subtree and `explain` command |
| `internal/config/...` | Shared effective-config snapshot + provenance resolver |
| tests under `commands/config` or `internal/config` | Table-driven tests for field lookup, `--all`, `--flags`, `--value-only`, `--origin-only` |
| workflow command code | Refactor `workflow app-types` to reuse shared resolver once available |

---

## Rejected Alternatives

### 1. Extend `da explain`

Rejected because `da explain` is human documentation, not repo-state introspection.

### 2. Make `workflow app-types` the only discovery surface

Rejected because app-type discovery is only one config use case. Operators also need exact lookup
for flags, verifier profiles, source-resolved fields, and future profile refs.

### 3. Provide only human-readable output

Rejected because scripts and editor integrations need one-value extraction and structured JSON.

---

## Open Questions

1. Should `--value-only` print JSON scalars/arrays exactly, or should there be a second flag for
   shell-safe plain-text extraction?
2. Should `--origin-only` print only the winning layer id, or also include the source digest / ref
   when available?
3. Should `--all --json` inline provenance next to each field, or emit a separate provenance map to
   keep the output easier to diff?