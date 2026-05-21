# Proposal: split container integration tests into a separate Go module

Status: accepted-direction (user-directed 2026-05-17, from PR #16 review Medium-1)
Scope: project-local (this repo's module graph)

## Problem

pr3b's `internal/graphstore` Postgres integration tests use
`testcontainers-go`, which drags ~40 indirect Docker/moby/containerd
modules + `pgx` into the **main module graph**. These are exercised only
by `*_container_test.go`, yet they enlarge `go.sum`, `go mod`
resolution time, supply-chain surface, and CI cache for *every* build.

## Decision

Extract the container/Postgres integration tests into a **separate Go
module** (e.g. `tests/integration/` with its own `go.mod`) so the
testcontainers/docker closure is pulled **only** when that feature pack
is being tested — not for normal builds/installs of the main module.

**Explicitly rejected: Go build tags.** Per maintainer: build-tag
gating is an ongoing maintenance burden that compounds as the app grows;
avoid adding tag matrices unless unavoidable. A separate module gives
the same isolation without per-file tag bookkeeping.

## Rationale

- Main `go.mod` stays lean; `go build/test ./...` on the core module
  never resolves the Docker closure.
- Integration module opts in to its own heavy deps; CI runs it as a
  distinct job only when relevant.
- No build-tag proliferation in the source tree.

## Scope / sequencing

- Touches `go.mod`/`go.sum` and relocates `*_container_test.go` (and any
  shared harness) under the new module.
- **After pr3b (#16 / 0.3.1) merges** — it owns the current go.mod state;
  doing this before would churn the release PR. Post-0.3.1 work.
- Needs: new `go.mod`, module-path import of the graphstore package
  under test (or a thin exported test seam), a dedicated CI job, and a
  `go.work` (optional) for local multi-module dev.

## Open questions (resolve when planned)

- Module path / location (`tests/integration` vs `integration/`).
- Whether the integration module imports `internal/graphstore` directly
  (Go `internal/` rules: a separate module CANNOT import another
  module's `internal/` — this likely forces either an exported test
  entrypoint, or keeping the integration tests as `package graphstore`
  within the SAME module but a build-excluded path... which reintroduces
  the tag problem). **This `internal/` constraint must be designed
  through before committing to the separate-module approach** — it may
  require a small exported surface in graphstore or relocating the
  integration target out of `internal/`.

→ Graduate to a `workflow/specs/` + plan when scheduled.
