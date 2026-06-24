# Reference (PROJECT EXAMPLE): a release-workflow step map

> **This is an illustrative project example, not load-bearing.** It maps one concrete
> release workflow (`auto-release.yml`) to show the SHAPE a step map takes — the
> idempotency guard, the version-match gate, the pinned signing-tool installs, and the
> signature-verification done-signal. **Read your project's own release-workflow step
> map as authoritative**; use this only as a template for what to capture. The
> placeholders in `instructions/workflow.md` (release-workflow file, signing toolchain,
> pin-check job, done-signal step) resolve to the project's real values, not these.

Single `release` job (`runs-on: ubuntu-latest`, `environment: release`). Triggers:
push to `master` touching `VERSION`, **or** `workflow_dispatch` (the re-drive path).
`concurrency: group: release, cancel-in-progress: false` — runs serialize, never
cancel mid-publish. Most steps are gated `if: steps.check.outputs.exists == 'false'`,
so a re-drive of an already-shipped version no-ops cleanly.

| # | Step | What it does / why it matters at cut time |
|---|------|--------------------------------------------|
| 1 | Checkout (`fetch-depth: 0`) | Full history needed for tagging. |
| 2 | Read version | Reads `VERSION` → `version` / `tag=v<version>` outputs. |
| 3 | **Check if release exists** | The idempotency guard. If `v<version>` already exists, everything below no-ops. This is what makes `workflow_dispatch` re-drive safe. |
| 4 | Set up Go (`1.26.x`) | — |
| 5 | Install dependencies (`jq`) | — |
| 6 | Build CLI | ldflags inject Version/Commit/Describe. |
| 7 | **Verify CLI version matches** | Hard gate: CLI `--version` must equal `VERSION`. Mismatch = real regression, not infra. |
| 8 | Run Go tests | A failure here = real regression. Do NOT re-drive. |
| 9 | Run built-binary smoke tests | `scripts/verify.sh` against the built `./bin/da`. Failure = real regression. |
| 10 | Set up Java (Sonar scanner JRE) | Provides JRE locally so Sonar does not download one (avoids transient 403). |
| 11 | SonarQube Scan | Pinned `uses:` (SHA). Transient gate flake = re-drive class. |
| 12 | **Install Cosign** | Pinned `uses:` SHA (cosign-installer). |
| 13 | Install GoReleaser | Pinned `uses:` SHA. `install-only`. |
| 14 | Azure login (OIDC) | Gated on `vars.TRUSTED_SIGNING_PROFILE != ''`. Dormant until configured. |
| 15 | **Install jsign** | curl|sha256sum-verify of `jsign-${JSIGN_VERSION}.jar` (`JSIGN_VERSION` + `JSIGN_SHA256`). THIS is the kernel32-regression replacement — never a dotnet `sign` step. **Pin-check target.** |
| 16 | Acquire Trusted Signing access token | `az account get-access-token` → masked `TRUSTED_SIGNING_TOKEN`. |
| 17 | **Install quill** (macOS Dev ID) | curl|sha256sum-verify of `quill_${QUILL_VERSION}_linux_amd64.tar.gz` (`QUILL_VERSION` + `QUILL_SHA256`). Gated on `vars.MACOS_SIGNING_ENABLED == 'true'`. **Pin-check target.** |
| 18 | Import Homebrew tap signing key | GPG import into a temp GNUPGHOME. |
| 19 | **Run GoReleaser** | `git tag -a v<version>` + `git push origin v<version>` (THIS creates the tag — exit 128 source on a stale-tag re-run), then `goreleaser release --clean`. Windows/macOS signing hooks run here. |
| 20 | **Verify Cosign signature on checksums.txt** | Re-runs the consumer-facing `cosign verify-blob` recipe against `dist/`. This passing is the real done-signal. |
| 21 | Cleanup smoke home (`if: always()`) | Removes the smoke HOME. |

## Pin-check targets (the CI guard in test.yml)

The pin-check inspects the **curl|install signing-tool steps** — steps 15
(`jsign`), 17 (`quill`), and any cosign install via curl — for a `*_VERSION` +
`*_SHA256` pair, and fails on a `--prerelease`/`@latest` signing-tool install or a
reintroduced dotnet `sign`. It does NOT police the SHA-pinned `uses:` actions
(steps 11–13), which carry their pin in the `uses:` ref itself.
