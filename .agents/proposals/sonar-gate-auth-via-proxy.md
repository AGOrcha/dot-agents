# Follow-up: route sonar-gate token via daemon auth-proxy injector

**Surfaced:** 2026-05-29, maintainer comment on PR #185 (`scripts/sonar-new-issues-gate.sh:76`).

## The stopgap
`sonar-new-issues-gate.sh` resolves the SonarCloud token directly — `$SONAR_TOKEN`,
then `$SONARQUBE_TOKEN`, then parsing the gitignored `.mcp.json`
(`mcpServers.sonarqube.env.SONARQUBE_TOKEN`). Functional now, but **handling raw
credentials in a gate script is not good long-term**: it duplicates token-resolution
logic, couples the script to `.mcp.json`'s shape, and spreads credential handling
across surfaces.

## The fix (once available)
Per `[[agorcha-public-vs-internal-and-obs-deploy]]` §5.5, a **`da service` (run -d) auth-proxy
injector** is the planned single point that holds credentials and injects them for
registry / observability / MCP / CLI / external-agent-sources access — so workers,
CLI, and scripts become credential-unaware (talk to a localhost proxy). When that
lands, `sonar-new-issues-gate.sh` should obtain its SonarCloud access **through the
auth proxy**, not by reading `.mcp.json`/env itself. Same pattern applies to any other
script doing direct token resolution (the `precommit-mandate.sh cmd_sonar` token block
has the same `.mcp.json` parse — migrate both together).

## Scope when picked up
- Replace the `resolve_token` block in `sonar-new-issues-gate.sh` (+ the twin in
  `precommit-mandate.sh`) with an auth-proxy call.
- Gate on the auth-proxy injector existing (don't build it here).
- Until then the direct-resolution stopgap stays (CI has SONAR_TOKEN as a secret; local
  reads .mcp.json).

## Cross-references
- PR #185 (where this stopgap lives)
- `[[agorcha-public-vs-internal-and-obs-deploy]]` §5.5 (the auth-proxy direction)
- `[[unified-pluggable-event-contract]]` (sibling daemon-surface work)
