# SonarQube PR10 Plan

## Goal

Reduce or clear the remaining SonarCloud failures on PR `#10`.

## Findings

- The current quality gate fails on:
  - `new_reliability_rating`
  - `new_duplicated_lines_density`
  - `new_security_hotspots_reviewed`
- The most recent, clearly actionable branch findings are in:
  - `scripts/setup_homebrew_cask_release_secrets.sh`
  - `tests/test-kg-real-crg-build.sh`
  - `.github/workflows/test.yml`
- The duplication failure appears branch-wide across many files in a very large diff against `master`, not just the latest CI-fix commits.

## Plan

- [x] Inspect PR-specific Sonar quality gate conditions and recent findings.
- [x] Fix direct shell/workflow Sonar findings in the current branch tip.
- [x] Re-run local verification and re-check Sonar-facing status signals.
- [x] Mass-fix the simple constant/literal duplication cases by module:
  - `commands/workflow`
  - `commands/kg` and `internal/graphstore`
  - `commands/agents` and `commands/import.go`
  - `internal/platform`
- [x] Document remaining branch-scale blockers:
  - reviewing one fixed hotspot only moved `new_security_hotspots_reviewed` from `0.0` to `2.8`
  - PR `#10` currently spans `324` commits and `893` changed files against `feature/workflow-auto-operator`
  - `new_duplicated_lines_density` is branch-wide debt, not isolated to the latest CI-fix files

## Latest Verification

- `go test ./commands/workflow ./commands/kg ./commands/agents ./internal/graphstore ./internal/platform ./commands`
- `git diff --check`
- `bash -n scripts/setup_homebrew_cask_release_secrets.sh`
- `bash -n tests/test-kg-real-crg-build.sh`
- `ruby -e 'require "yaml"; YAML.load_file(".github/workflows/test.yml"); puts "yaml ok"'`
