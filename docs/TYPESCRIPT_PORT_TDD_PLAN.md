# TypeScript Port Plan

## Purpose

Build a native Windows-friendly TypeScript port for machines that cannot run the Go toolchain locally, while keeping the current Go CLI and docs as the behavioral source of truth.

The office-machine branch `origin/feature/go-fixes-and-typescript-port-for-availability-on-restricted-machines` is donor material only. It is useful for implementation hints, but the current repo has moved enough that the TypeScript port must be rewritten against current contracts rather than merged as-is.

## Current Contract Model

The plan is anchored on the current repository behavior, not the donor branch shape:

- `.agentsrc.json` must preserve unknown fields on round-trip and source merge.
- Codex agent TOML must emit `developer_instructions`, not the donor-era `is_background`.
- Hook renderer expectations come from current Go tests and renderer shapes.
- The current CLI surface includes `workflow` and KG-adjacent commands, but those are not automatically part of the TypeScript MVP.
- Stage 2 bucket expansion and plugin-resource work are active canonical plans, so the TypeScript port must treat them as deferred unless a later phase explicitly opts in.
- The donor branch used the Node built-in test runner, not Vitest; keep the test toolchain minimal unless a stronger need appears.

## Porting Rules

- Treat current Go tests and current docs as the oracle, not the older donor branch.
- Preserve the canonical `~/.agents/` data model and current project manifest semantics.
- Keep the TypeScript port in `ports/typescript/` as an explicit variant, not as a shadow replacement for the Go CLI.
- Prefer Windows-safe link behavior:
  - file symlink when allowed
  - directory junction for directory links on Windows
  - hard-link fallback for file links when symlink creation is blocked
- Do not imply full CLI parity before the workflow, KG, plugin, and Stage 2 boundaries are decided.

## Merge Strategy

Do not merge donor commit `d1b1e46` wholesale.

Use the donor branch as source material only:

1. Copy the `ports/typescript/` subtree in controlled slices onto the current branch.
2. Rework copied code against current contracts before claiming parity.
3. Salvage only the compatible ideas that still match current behavior:
   - MCP detection support for `.mcp.json` and `mcpServers`
   - extra hook-renderer shape assertions
4. Drop stale donor assumptions instead of carrying them forward:
   - `is_background` Codex output
   - Bash implementation as a fallback truth source
   - implicit Stage 1-only worldview
   - accidental full-parity language

## MVP Boundary

The initial TypeScript MVP should stay narrow and useful for restricted machines:

- `init`
- `add`
- `refresh`
- `status`
- `doctor`
- `skills`
- `agents`
- `hooks`

`import` is optional for the MVP. It can land with the first wave only if the canonicalization logic stays aligned with current Go tests without pulling in unresolved plugin and Stage 2 work.

The following are explicitly deferred until later waves:

- **Workflow / KG boundary (Phase 4):** see **`docs/TYPESCRIPT_PORT_BOUNDARY.md`**. Chosen stance: optional future **read-only `workflow`** surfaces in TS; **`kg/*`**, **workflow writes**, and **full orchestration** remain **Go-only**.
- KG bridge / MCP server features (beyond what Stage 1 already covers for detection)
- plugin import and emission
- Stage 2 buckets: commands, output-styles, modes, themes, prompts, and finalized plugin buckets

## Phase 1 Checkpoint

Phase 1 is complete when this document and the canonical plan artifacts reflect current contracts instead of donor-branch assumptions.

Checkpoint criteria:

- The donor branch has been reviewed as source material, not as a merge target.
- The MVP and deferred boundaries are explicit.
- The canonical plan files point at the next implementation phase rather than continuing to describe phase 1 as active work.
- The current Go contracts are the reference point for `.agentsrc.json`, Codex TOML, hook rendering, workflow boundaries, and Stage 2/plugin deferral.

## Phase Order

### Phase 1: Donor Audit And Plan Rewrite

Acceptance gate:

- The donor branch has been compared to the current branch.
- This plan and the canonical `typescript-port` plan artifacts exist.
- The MVP and deferred scope are explicit.
- The plan is framed around current Go contracts, not donor-branch defaults.

### Phase 2: Foundations On Current Contracts

Acceptance gate:

- Config, path, and link helpers match current Go behavior where the port claims parity.
- `.agentsrc.json` round-trips unknown fields and merged sources correctly.
- MCP detection supports the documented shapes now expected by the Go branch.
- Codex TOML output uses `developer_instructions`.
- Hook rendering tests mirror current Go expectations.

Suggested Go sources to mirror:

- `internal/config/config_test.go`
- `internal/config/agentsrc_test.go`
- `internal/links/links_test.go`
- `internal/platform/hooks_test.go`
- `internal/platform/codex_test.go`

### Phase 3: Stage 1 Command MVP

Acceptance gate:

- The bounded command set works from the TypeScript CLI.
- The TypeScript tests cover the supported command behavior.
- Windows-safe link behavior is exercised in tests.

Suggested Go sources to mirror:

- `commands/add_test.go`
- `commands/init_test.go`
- `commands/refresh_test.go`
- `commands/status_test.go`
- `commands/doctor_test.go`
- selected `commands/import_test.go` cases if `import` is admitted into MVP

### Phase 4: Advanced Surface Decision

Acceptance gate:

- The repo explicitly decides whether the TypeScript port stays resource-focused or grows into selected `workflow` support.
- CLI help and docs reflect that decision.
- No command is advertised without tests and a documented capability boundary.

### Phase 5: Stage 2 And Plugin Alignment

Acceptance gate:

- The TypeScript port follows the stabilized Go contracts for Stage 2 buckets and plugin resources.
- Deferred functionality lands only after the canonical Go-side contract work is settled.

External prerequisites:

- `.agents/workflow/plans/platform-dir-unification/`
- `.agents/workflow/plans/plugin-resource-salvage/`

### Phase 6: Packaging And Release Docs

Acceptance gate:

- Packaging, install instructions, and naming are finalized.
- README and docs clearly describe the TypeScript port as a Windows-friendly variant with declared scope.
- The TypeScript suite documents any intentional divergence from Go behavior.

## Definition Of Done

- Restricted machines can use the TypeScript CLI for the agreed MVP without requiring the Go toolchain.
- The TypeScript port tracks current Go contracts for the surfaces it claims to implement.
- Deferred workflow, plugin, and Stage 2 areas are documented as deferred rather than left ambiguous.
- The port has its own test suite and packaging path under `ports/typescript/`.
