# CI Smoke Suite Hardening Plan

Status: Active

Date: 2026-04-18

## Outcome

Turn CI from a mostly compile-and-basic-smoke gate into a layered verification pipeline that:

- runs the full Go suite with the repo-standard flags
- validates the built CLI binary rather than launcher shims
- exercises meaningful end-to-end repo flows in isolated homes
- covers command-surface regressions that unit tests do not catch well
- validates release packaging before `auto-release.yml` is the first place a release config breaks

## Audited Current State

Current workflow coverage is split across:

- [test.yml](/Users/nikashp/Documents/dot-agents/.github/workflows/test.yml)
- [auto-release.yml](/Users/nikashp/Documents/dot-agents/.github/workflows/auto-release.yml)
- [scripts/verify.sh](/Users/nikashp/Documents/dot-agents/scripts/verify.sh)
- [tests/test-claude-configs.sh](/Users/nikashp/Documents/dot-agents/tests/test-claude-configs.sh)

What CI currently does reasonably well:

- workflow YAML parses and `actionlint` runs
- Go tests run on macOS and Ubuntu
- basic CLI lifecycle gets touched: `init`, `status`, `doctor`, `add`, `remove`, `sync status`

What it does not currently prove well:

- formatting and static analysis cleanliness
- the built binary behavior as the canonical shipped surface
- isolated-home reproducibility
- correctness of many CLI subcommand surfaces outside `help`
- resource-lifecycle behavior for `rules`, `settings`, `mcp`, `hooks`, `skills`, `agents`
- workflow/KG command smokes in temp repos
- release packaging correctness before release time
- parity between PR CI and release CI

## Gap Analysis

### 1. Toolchain and lint gap

The current test workflow does not check:

- `gofmt` cleanliness
- `go vet`
- shell script correctness beyond whatever `actionlint` sees in embedded workflow shells
- direct CLI packaging/buildability outside `go test`

This means regressions can land that:

- compile and pass tests
- but still ship unformatted code, vet findings, broken shell scripts, or packaging drift

### 2. Binary authenticity gap

Historically the workflow used `./src/bin/dot-agents`, which is a launcher shim. That proves:

- the repo can find a binary or `go run`

It does not prove:

- the actual built `./bin/dot-agents` artifact behaves correctly
- ldflags/version wiring behave as expected in release-like builds
- packaging consumers are testing the same surface that users download

CI should prefer the built binary for smoke steps.

### 3. State isolation gap

The CLI is sensitive to:

- `HOME`
- `AGENTS_HOME`
- runner `PATH`
- pre-existing user config directories such as `.cursor`, `.claude`, `.codex`

Local analysis already exposed two real fragilities:

- tests writing to real `~/.agents` paths can fail or pollute state
- `init --yes` behavior changes with `PATH` contents because platform detection probes installed CLIs and apps

Therefore all smoke jobs should use:

- isolated `HOME`
- isolated `AGENTS_HOME`
- deterministic `PATH`

Without that, CI can become a runner-image lottery rather than a product test.

### 4. Command-surface gap

The repo has a much broader command surface than current CI exercises.

Commands with meaningful user-facing behavior not covered by current smoke steps include:

- `refresh`
- `install`
- `import`
- `explain`
- `review`
- `rules`
- `settings`
- `mcp`
- `hooks`
- `skills`
- `agents`
- large parts of `workflow`
- large parts of `kg`

Go tests cover many code paths, but CLI regressions often happen in:

- Cobra wiring
- flags/defaults
- filesystem assumptions
- help/usage contracts
- project layout interactions
- path normalization and cross-platform handling

Those need end-to-end command smokes in temp repos/homes.

### 5. Resource lifecycle gap

This repo’s product is not just a Go library or a CLI parser. It manages canonical storage and projections for:

- rules
- settings
- mcp
- hooks
- skills
- agents

Current CI does not meaningfully prove the create/list/show/remove or promote/import lifecycles for those buckets.

That is a high-risk omission because these lifecycles are where:

- symlink/hardlink behavior breaks
- repo-local file layout changes drift
- import/refresh compatibility regresses
- platform-specific projections silently stop working

### 6. Workflow and KG gap

`workflow` and `kg` are now major product surfaces, not internal curiosities.

Current CI barely touches them. That leaves regressions in:

- workflow readback commands
- checkpoint/verify log writes
- delegation bundle inspection
- graph bridge help/wiring
- KG health/query/help surfaces

Not every subcommand needs a full end-to-end scenario in the main PR gate, but CI needs at least:

- read-only smokes for all major entrypoints
- one temp-repo write-path smoke for `workflow`
- one non-destructive health/help smoke for `kg`

### 7. Release pipeline gap

Release correctness is currently deferred too far into [auto-release.yml](/Users/nikashp/Documents/dot-agents/.github/workflows/auto-release.yml).

That means `.goreleaser.yaml` can drift in ways that:

- do not break `go test`
- do not break simple CLI smokes
- but do break archives, metadata, checksums, or packaging when `VERSION` changes

This should not be optional. CI should always run:

- `goreleaser check`
- `goreleaser release --snapshot --clean`

on at least one Linux job.

Current GoReleaser semantics, verified against official docs:

- `goreleaser check` validates configuration
- `goreleaser release --snapshot` runs the release pipeline without publishing and writes artifacts to `dist/`
- `--clean` removes the existing `dist/` before building

That makes `release --snapshot --clean` the right mandatory packaging smoke.

### 8. Release/PR parity gap

`auto-release.yml` still uses:

- `go test ./...`
- `src/bin` launcher-based smokes

If PR CI and release CI validate different surfaces, one of them will become untrustworthy.

The release workflow should converge on the same:

- Go test flags
- built-binary smoke policy
- packaging validation assumptions

### 9. Heavy integration gap

The Claude behavior script at [tests/test-claude-configs.sh](/Users/nikashp/Documents/dot-agents/tests/test-claude-configs.sh) is valuable, but it is too environment-heavy for the main PR gate in its current form because it assumes:

- Docker/container context
- Claude auth/runtime
- timeout-sensitive interactive behavior

This should still be part of the broader smoke strategy, but likely in:

- nightly CI
- manual dispatch
- a dedicated heavy integration workflow

It is still a real gap and should not be forgotten just because it is not fast-path friendly.

## Target CI Shape

### Job 1: `lint`

Purpose:

- fail fast on low-cost hygiene errors

Candidate steps:

- workflow YAML parse
- `actionlint`
- `gofmt` check
- `go vet ./...`
- `shellcheck` on repo shell scripts

### Job 2: `test-go`

Purpose:

- prove the full Go tree under the repo-standard test policy

Candidate steps:

- `go test ./... -race -count=1 -timeout=300s`

Matrix:

- `ubuntu-latest`
- `macos-latest`

Potential follow-on:

- add `windows-latest` at least for `go build` or full `go test` once stable

### Job 3: `smoke-cli`

Purpose:

- verify the built binary and the user-facing CLI lifecycle in isolated state

Candidate steps:

- `go build -o ./bin/dot-agents ./cmd/dot-agents`
- set isolated `HOME`
- set isolated `AGENTS_HOME`
- set deterministic `PATH`
- smoke `init`, `add`, `status`, `status --audit`, `doctor`, `remove --dry-run`, `sync status`
- run `./scripts/verify.sh`

### Job 4: `smoke-resource-lifecycle`

Purpose:

- cover canonical storage lifecycles not exercised by generic CLI smoke

Candidate slices:

- `rules list/show/remove`
- `settings list/show/remove`
- `mcp list/show/remove`
- `hooks list/show/remove`
- `skills new/promote/list`
- `agents new/promote/list/import/remove`

This should use temp dirs and isolated homes, and avoid relying on the user’s real config.

### Job 5: `smoke-workflow-kg`

Purpose:

- cover the repo’s newer control-plane surfaces

Candidate slices:

- `workflow --help`
- `workflow status|health|prefs`
- one temp-repo `workflow checkpoint`
- one temp-repo `workflow verify record`
- `workflow bundle stages --help`
- `kg --help`
- `kg health`
- `kg bridge health`
- `workflow graph health`

This lane should stay read-only where possible, with only one or two controlled write-path smokes.

### Job 6: `package`

Purpose:

- fail before release time if packaging drifts

Mandatory steps:

- `goreleaser check`
- `goreleaser release --snapshot --clean`

Platform:

- `ubuntu-latest`

Reasoning:

- packaging correctness is a core regression class for this repo
- `snapshot --clean` is the closest non-publishing validation of the real release path

### Job 7: `heavy-integration`

Purpose:

- capture behavior tests that are valuable but too slow or environment-sensitive for every PR

Candidate contents:

- Docker-based or sandbox-based integration jobs
- Claude behavior/config integration from `tests/test-claude-configs.sh`

Trigger mode:

- nightly
- manual dispatch
- optional label-triggered PR run

## Task Breakdown

### Task 1: Harden the main test workflow contract

Deliver:

- write down the intended CI layers and their ownership
- stop mixing lint, full tests, packaging, and heavy integrations into one ambiguous job

Acceptance:

- the workflow file structure reflects the layered contract above

### Task 2: Add fast hygiene gates

Deliver:

- YAML parse
- `actionlint`
- `gofmt` check
- `go vet`
- shell lint lane

Acceptance:

- low-cost syntax/hygiene failures fail before heavy jobs start

### Task 3: Normalize full Go test policy

Deliver:

- use `go test ./... -race -count=1 -timeout=300s`
- unify this policy across PR CI and release CI

Acceptance:

- both test and release workflows use the same full-suite policy unless there is a documented exception

### Task 4: Replace launcher-based smoke tests with built-binary smokes

Deliver:

- all smoke steps use `./bin/dot-agents`
- explicit build step before smoke tests

Acceptance:

- no main CI smoke step depends on `src/bin/dot-agents`

### Task 5: Isolate CI runtime state

Deliver:

- temp `HOME`
- temp `AGENTS_HOME`
- deterministic `PATH`

Acceptance:

- smoke jobs do not write to the runner’s real home state
- `init/add/status/...` pass reproducibly in isolated homes

### Task 6: Expand CLI smoke coverage

Deliver:

- integrate `scripts/verify.sh`
- add temp-repo lifecycle checks for `refresh`, `install`, and `import` where practical

Acceptance:

- the smoke lane covers more than just `init/status/add/remove`

### Task 7: Add canonical resource lifecycle smokes

Deliver:

- targeted command tests for `rules`, `settings`, `mcp`, `hooks`, `skills`, and `agents`

Acceptance:

- at least one lifecycle assertion exists for each canonical resource family

### Task 8: Add workflow/KG smokes

Deliver:

- read-only and minimal write-path smokes for `workflow` and `kg`

Acceptance:

- CI can catch Cobra wiring, filesystem assumptions, and basic artifact-write regressions in these command families

### Task 9: Add mandatory package validation

Deliver:

- `goreleaser check`
- `goreleaser release --snapshot --clean`

Acceptance:

- PR CI fails if release config or packaging is broken

### Task 10: Align `auto-release.yml` with PR CI policy

Deliver:

- release workflow uses the strengthened Go test flags
- release workflow uses built-binary smokes

Acceptance:

- release workflow is not validating a weaker or different surface than PR CI

### Task 11: Define heavy integration lanes

Deliver:

- separate workflow or job for Docker/Claude behavior tests
- explicit trigger policy

Acceptance:

- environment-heavy smokes are tracked and runnable, not left as tribal knowledge

## Prioritized Execution Order

1. Task 2: fast hygiene gates
2. Task 3: full Go policy normalization
3. Task 4: built-binary smoke policy
4. Task 5: isolated-home/path discipline
5. Task 9: mandatory package validation
6. Task 6: broader CLI smoke coverage
7. Task 7: canonical resource lifecycle smokes
8. Task 8: workflow/KG smokes
9. Task 10: release workflow parity
10. Task 11: heavy integration lane

## Non-Goals

For this hardening wave, do not:

- turn the main PR workflow into a long-running full-system integration environment
- require Claude-authenticated tests on every PR
- make KG or workflow smokes depend on external services
- conflate packaging validation with release publishing

## Exit Condition

This plan is complete when:

- PR CI validates formatting, vet, full Go tests, built-binary smoke behavior, and package generation
- release CI matches the same verification standard
- resource/workflow/KG smoke gaps are covered by explicit jobs
- heavy integration tests are placed in a tracked workflow instead of living only as ad hoc scripts
