# ADR-0007: TypeScript port binary name — `da-ts`

**Status:** accepted
**Date:** 2026-05-03
**Owners:** dot-agents
**Related:** [ADR-0006](0006-da-rename-strategy.md) (Go binary rename strategy); [`docs/TYPESCRIPT_PORT_BOUNDARY.md`](../TYPESCRIPT_PORT_BOUNDARY.md) (Phase 4 boundary contract); [`binary-rename-da-sweep` plan](../../.agents/workflow/plans/binary-rename-da-sweep/) t6

## Context

ADR-0006 renamed the Go binary `dot-agents` → `da` via hard cutover.
The TypeScript port at `ports/typescript/` ships a separate binary
that implements a Stage 1 subset (per the Phase 4 boundary). Today
that binary is named `dot-agents-ts` (declared in
`ports/typescript/package.json` `bin` field, and surfaced in
`dot-agents-ts --help` text and source comments).

Two naming options:

- **(A) Keep `dot-agents-ts`** — emphasizes that this is the
  TypeScript port of the dot-agents *project*. Inconsistent with the
  Go-binary rename: users now type `da` for the Go variant but
  `dot-agents-ts` for the TS variant. Two different mental models for
  what is functionally one tool family.
- **(B) Rename to `da-ts`** — parity with ADR-0006's UV-style
  abbreviation. Consistent typing convention across both binaries.
  Requires updating `package.json`, source-emitted help text, README,
  and the boundary doc.

The TS port's user contract per the boundary doc is "Stage 1 commands
mirror the Go variant." The binary name is part of that user surface.
Naming asymmetry between `da` and `dot-agents-ts` would force users
to remember two different invocation conventions for what is
functionally one tool family at the Stage 1 level.

## Decision

**Adopt option (B) — rename to `da-ts`.**

- `ports/typescript/package.json` `bin` field changes from
  `dot-agents-ts` → `da-ts`.
- Source-emitted help text in `ports/typescript/src/cli.ts` and
  `ports/typescript/src/boundary.ts` updates the example invocation
  from `dot-agents-ts <command>` → `da-ts <command>`.
- `ports/typescript/README.md` updates installation and invocation
  examples accordingly.
- `docs/TYPESCRIPT_PORT_BOUNDARY.md` gains a small note about the
  binary-naming parity with ADR-0006.
- The `package.json` `name` field (`dot-agents-typescript-port`)
  STAYS — that's the npm package identity, not the binary name. Users
  who `npm link` get `da-ts` on PATH.
- The directory `ports/typescript/` STAYS — that's the source-tree
  identity, not the binary.
- The TS port's existing tests (`ports/typescript/tests/boundary.test.ts`)
  may snapshot help-text strings — update those alongside the source
  edits in the same commit.

The same hard-cutover principle as ADR-0006 applies: only `da-ts`
ships post-rename; no compat shim. Existing `npm link`-installed
`dot-agents-ts` will need re-linking by users, which they would have
to do anyway when the TS port version bumps.

## Consequences

**Easier:**

- Single typing convention across the tool family: `da` for Go,
  `da-ts` for TS variant. Zero mental overhead translating between.
- The boundary doc's "Stage 1 mirrors the Go variant" claim stays
  honest at the binary-name level.
- New users discovering the TS port via the boundary doc see a
  consistent naming story.

**Harder:**

- Existing `npm link`-installed `dot-agents-ts` invocations break.
  Users must re-link or alias.
- One more file family to sweep: `package.json`, source help text,
  README, boundary doc, tests/.

**New risks:**

- Help text and README snapshots may have drift after the rename;
  the existing `boundary.test.ts` provides protection if it
  snapshots the help output.
- Some external integrations (rare; this port hasn't been published
  to npm) might exec `dot-agents-ts` by name. Mitigation: package
  hasn't been published from this repo per the boundary doc, so
  external dependence is minimal.

**Locked-in commitments:**

- Future TS port version bumps assume the `da-ts` name. Reverting
  would itself require an ADR superseding this one.

## Alternatives considered

- **(A) Keep `dot-agents-ts`** — rejected for the asymmetry reason.
  The whole point of ADR-0006's hard cutover was to avoid the
  ongoing cognitive cost of "either name works"; carrying that cost
  into the TS port for no functional benefit defeats the purpose.
- **Rename to `dat`** — considered as a more compact alternative.
  Rejected: `dat` is a real existing tool (the peer-to-peer sharing
  protocol/CLI) and conflicts with established naming. `da-ts`
  preserves the namespace cleanly.
- **Keep `dot-agents-ts` with a `da-ts` alias shim** — same
  rejection logic as ADR-0006's compat-shim option. Hard cutover is
  cleaner.

## References

- ADR-0006 — Go binary rename strategy (parent decision setting the
  `da` convention this ADR extends).
- `docs/TYPESCRIPT_PORT_BOUNDARY.md` — Phase 4 boundary contract;
  this ADR keeps the contract intact and only changes the binary
  name surface.
- `ports/typescript/package.json` — `bin` field is the npm-side
  binary-name declaration.
- `ports/typescript/tests/boundary.test.ts` — existing help-text
  snapshot that the rename must update.
