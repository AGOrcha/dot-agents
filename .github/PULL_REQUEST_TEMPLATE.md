## Summary

<!-- Behavior change, before → after. One or two sentences. -->

## Root cause / spec link

<!-- Fix: what was the root cause? Feature: link the spec/design doc (workflow/specs/<id>/design.md) or plan. -->

## Verification

<!-- Commands run + key results. Focused package tests first, then `go test ./...`.
     Note any e2e/smoke coverage (scripts/verify.sh, tests/test-claude-configs.sh).
     New code must keep SonarCloud new_coverage ≥80% (measured per-package). -->

- [ ] `go test ./...` (or focused packages: `go test ./commands ./internal/...`)
- [ ] `gofmt -l ./cmd ./commands ./internal` clean
- [ ] Relevant shell smoke test (if links/templates/hooks changed)

## Affected platforms

- [ ] Cursor
- [ ] Claude Code
- [ ] Codex
- [ ] Copilot
- [ ] OpenCode
- [ ] None (CLI-core only)

## Lock / schema / manifest impact

<!-- Does this touch .agentsrc.json, .agentsrc.lock, or schemas/agentsrc.schema.json?
     If AgentsRC struct fields changed, schemas/agentsrc.schema.json must be updated in
     the same PR (struct/schema drift is invisible at runtime — see
     .agents/rules/dot-agents/schema-usage.md). Is a migration needed? -->

## Out of scope

<!-- Follow-ups deliberately deferred. -->
