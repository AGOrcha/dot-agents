# Sonar MCP `impactSoftwareQualities=SECURITY` under-reports legacy VULNERABILITY findings

## Pattern

A SonarCloud PR quality gate fails on `new_security_rating > A` (e.g. rating = 3 / D),
but a Sonar **MCP** query for security issues comes back EMPTY:

```
get_project_quality_gate_status(pullRequest=N)
  -> new_security_rating: ERROR, actualValue "3"
search_sonar_issues_in_projects(pullRequestId=N, impactSoftwareQualities=["SECURITY"])
  -> {"issues": [], total: 0}
search_security_hotspots(pullRequest=N)
  -> {"hotspots": [], total: 0}
```

This LOOKS like a stale/inconsistent gate (rating says D, MCP says zero findings) and
tempts you to dismiss it as a flake and just re-run. **It is usually NOT a flake** —
there is a real VULNERABILITY the MCP filter is not showing.

## Root cause

The Sonar MCP's `impactSoftwareQualities=["SECURITY"]` filter keys off the NEW
"software quality impact" taxonomy. Findings still tagged with the LEGACY issue
`type=VULNERABILITY` (common for `githubactions:*` and many language rules) do not
match that impact filter, so they are silently omitted from the MCP result — even
though they are exactly what drives `new_security_rating`.

Real instance (PR #80, docs-starlight-migration / dm7): `new_security_rating=3` with
"0 security issues" from MCP. A direct API query found 3 MAJOR vulns in the newly
added CI job (`.github/workflows/deploy-docs.yml`):
- `githubactions:S6505` — `npx` installs packages on-demand (supply chain)
- `githubactions:S8543` — pin an exact package version
- `githubactions:S8233` — move workflow-level write permission to job level

## Rule

When `new_security_rating` (or `new_reliability_rating`) disagrees with an MCP
"0 issues" result, **query the SonarCloud API directly with the legacy `types`
parameter BEFORE concluding flake**:

```bash
curl -s "https://sonarcloud.io/api/issues/search?componentKeys=<projectKey>&pullRequest=<N>&types=VULNERABILITY&ps=50" \
  | python3 -c "import sys,json; d=json.load(sys.stdin); print('total',d['total']); [print('-',i['severity'],i['rule'],'@',i['component'].split(':')[-1],'line',i.get('line'),'::',i['message']) for i in d['issues']]"
```

(For reliability use `types=BUG`; for maintainability `types=CODE_SMELL`.) Also check
hotspots. Only after BOTH the impact-filtered MCP query AND the legacy-`types` API
query come back empty is "stale gate / flake" a defensible conclusion.

Then FIX the findings (no suppressions, no gaming):
- `npx <pkg> install` in CI -> `npm exec --no -- <pkg> ...` (uses the version pinned
  in package-lock.json and refuses on-demand fetch). [S6505/S8543]
- Workflow-level `permissions:` -> per-job `permissions:` blocks (least privilege).
  [S8233]

## Related

- `sonar-rating-gate-misses-new-issues` — the inverse failure (gate GREEN while new
  CRITICAL issues exist); rating aggregation hides issue count.
- `sonarcloud-gate-mechanics` — free-tier branch-analysis + local-scanner-as-main
  mechanics.
