# dot-agents project overlay (orchestration / release literals)

The starter prompts and skills (`orchestrator` AGENT.md, `orchestrator-session-start`,
`delegation-lifecycle`, `release-cut`) are **generic**: they state the rule and point to "the project
overlay" for the concrete value. This file is that overlay's **prose half** — the literal dot-agents
values the generic prompts resolve. The **structural half** is the per-`app_type`
`execution_profile` in `.agentsrc.json`, read via `da config relevance` (see below).

This is the single home referenced by the generic prompts' "resolve via `da config relevance` / the
project's gate / contributing / test docs" pointers. Keep it in sync with `.agentsrc.json` and
`.github/workflows/`.

## Active-line remote

- **active-line remote name = `AGOrcha`** — the remote dot-agents PRs target (`git remote -v`:
  `origin` → `AGOrcha/dot-agents`). `upstream` is the **stale `NikashPrakash` fork** (the parent,
  often divergent). Derive `eligible` / "already-shipped" from the **active line** only; a task can
  read merged on the parent and still be open on the active line, or vice versa
  (`[[stale-local-master-ref]]`, `[[stale-local-checkout-mass-drift]]`). The orchestrator's `origin`
  / `upstream` framing in the generic prompt is a placeholder — the literal active-line remote is
  `AGOrcha` here.

## Quality gate (locally reproducible)

Satisfy the project's actual gate, not just a local linter (`[[sonarcloud-gate-mechanics]]`,
`[[gates-must-be-locally-reproducible]]`):

- **Build + smoke + scoped CLI:** `go build -o ./bin/da ./cmd/da` then `bash scripts/verify.sh` — the
  `cli-runner` verifier matrix (`reviewers`/`verifiers` overlays carry the full command lists).
- **Cognitive complexity ≤ 15 (SonarCloud S3776).** Note `gocognit` ≠ S3776: a local complexity linter
  computes the metric differently than the SAST gate, so **pin to the gate, not the linter** — a
  function can pass `gocognit` and still trip S3776 (or vice versa).
- **SAST / coverage analysis exclusions:** SonarCloud excludes `dist/` and `.scannerwork/` from
  analysis. Do not let generated/build output skew the gate.
- **PATH / exec hardening (go:S4036):** spawns resolve absolute / `execabs`-checked binaries, never a
  poisonable relative PATH lookup (see `reviewers/adversarial.project.md`).

## Coverage-delta forecast — non-code (manifest) test

The pre-fanout coverage-delta forecast has two shapes by `app_type` (resolve the shape + topology via
`da config relevance --filter topology --app-type <t>`):

- **Code write_scopes** → walk the `*_test.go` callers of every changed/deleted symbol.
- **Non-code write_scopes (docs / config / skill-prose, e.g. scaffold/template content)** → the
  breaking tests are **manifest / file-tree tests that assert on the generated tree, file existence,
  counts, or embedded content**. For dot-agents the concrete manifest test is
  **`internal/scaffold/home/copy_test.go`** (e.g. `TestCopyStarterAssetsIncludesReviewerLensAgents`,
  `TestStarterVerifierSurfaceCrossReference`). Adding / renaming / moving a starter or prompt file
  flips those assertions — walk THIS test, not symbol callers. The `docs` app_type's
  `verifier_sequence` (`schema-check → citation-check → cli-runner`) routes through `cli-runner`, which
  runs `go test ./internal/scaffold/...` and so executes this manifest test.

## Release toolchain / workflow signatures (release-cut)

- **release workflow file = `.github/workflows/auto-release.yml`** (trigger: push to `master` touching
  `VERSION`, or `workflow_dispatch` re-drive).
- **signing / timestamp toolchain:** jsign (`--storetype TRUSTEDSIGNING`, Azure Trusted Signing, OIDC),
  quill (macOS Dev ID), Cosign keyless (Fulcio/Rekor); each pinned `*_VERSION` + `*_SHA256`.
  **Never reintroduce the dotnet `sign` tool** (kernel32 Linux regression, dotnet/sign#711).
- **CI pin-check** = the `lint-workflows` job's signing-toolchain pin guard in
  `.github/workflows/test.yml`.
- **done-signal step** = "Verify Cosign signature on checksums.txt" (`cosign verify-blob` of
  `dist/checksums.txt`).
- **known-failure signatures** = kernel32 `DllNotFoundException`, `DLSequence` ASN.1 cast, jsign
  `tsmode`/TSA timestamp flake, stale-tag git exit 128, version-mismatch / Go-test real regressions.
- Tag-delete + `workflow_dispatch` re-drive commands use the **active-line remote (`AGOrcha`)**, not a
  hardcoded `origin`.

## Resolver surfaces (where these are read)

- `da config relevance --filter topology --app-type go-cli|docs` — the structural topology
  (`verifier_sequence`, fan-out, lenses) the orchestrator resolves the coverage-delta shape from.
- `da config relevance --filter lenses --app-type go-cli` — the review-lens set
  (`architecture-standards, acceptance-invariants, adversarial, cross-harness-adversarial`).
- `da workflow resolve-prompt --kind reviewer|verifier --slug <slug>` — composes the base + lens/kind +
  `.project.md` overlay; the repo-local `.agents/prompts/{reviewers,verifiers}/<slug>.project.md`
  overlays carry the concrete command matrices and dot-agents hotspots.
