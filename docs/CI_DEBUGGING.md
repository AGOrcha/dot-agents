# CI Debugging

Practical notes for investigating failing PR gates and CI checks in this repo.

## Watching a PR gate

Use [`scripts/pr-gate-wait.sh`](../scripts/pr-gate-wait.sh) instead of an ad-hoc
poll loop: it wraps `gh pr checks <pr> --watch` and, when the gate goes red,
auto-fetches the failing job log via `gh run view --log-failed` so the failure
context is in front of you immediately.

## Fetching Sonar findings

Fetch SonarQube/SonarCloud findings through the `mcp__sonarqube` MCP tools
(for example `search_sonar_issues_in_projects`), **not** via `curl`/DNS calls to
`sonarcloud.io`. The MCP path resolves the project key and auth for you and
avoids the curl/DNS retry loops.

Caveat: the MCP server applies a security filter that can **under-report legacy
`VULNERABILITY`-type issues**. When you specifically need the full set of
older vulnerability findings, treat the MCP result as possibly incomplete and
cross-check against the SonarCloud UI or the strict gate in
[`scripts/sonar-new-issues-gate.sh`](../scripts/sonar-new-issues-gate.sh).
