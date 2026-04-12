# TypeScript Port Plan

## Purpose

Build a native Windows-friendly TypeScript port for machines that cannot run the Go toolchain locally, while keeping the Go CLI as the behavioral source of truth.

The office-machine branch `origin/feature/go-fixes-and-typescript-port-for-availability-on-restricted-machines` is donor material, not a merge target. The current repo has moved substantially since that branch split from `master`, so the port must be rebased onto today's contracts rather than merged wholesale.

## Current Branch Reality

The donor branch started useful work in `ports/typescript/`, but it predates several current-repo behaviors and conventions:

- `.agentsrc.json` now preserves unknown fields on round-trip; the donor TypeScript `AgentsRC` does not.
- Codex agent TOML now emits `developer_instructions`; the donor TypeScript code still renders `is_background`.
- The current CLI has a large `workflow` surface, while the donor TypeScript CLI only covers the core resource-management commands.
- Stage 2 bucket expansion and plugin-resource work are active canonical plans, so the TypeScript port must not assume the current Stage 1 bucket set is the final shape.
- The donor branch doc said "Vitest", but the donor package actually used the Node built-in test runner. The plan should follow the actual runtime and keep tooling minimal unless there is a strong reason to change it.

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

Instead:

1. Copy the `ports/typescript/` subtree in controlled slices onto the current branch.
2. Rework the copied code against current contracts before claiming parity.
3. Salvage only the compatible Go-side donor ideas:
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

- `workflow` parity
- KG bridge / MCP server features
- plugin import and emission
- Stage 2 buckets: commands, output-styles, modes, themes, prompts, and finalized plugin buckets

## Phase Order

### Phase 1: Donor Audit And Plan Rewrite

Acceptance gate:

- The donor branch has been compared to the current branch.
- This plan and the canonical `typescript-port` plan artifacts exist.
- The MVP and deferred scope are explicit.

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
