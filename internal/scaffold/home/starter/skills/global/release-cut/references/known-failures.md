# Reference: Known Release-Failure Classifier

Match the failed `auto-release.yml` step + log signature to a class, then route.
Two top-level classes: **toolchain fault** (re-pin or re-drive — the project did
not change) vs. **real regression** (fix the code; do NOT re-drive).

| Signature in the log | Failing area | Class | Action |
|----------------------|--------------|-------|--------|
| `kernel32.dll` / `DllNotFoundException` during Windows signing | dotnet `sign` running on Linux (dotnet/sign#711) | Toolchain — broken tool | Confirm `auto-release.yml` uses **jsign**, not dotnet `sign`. If `sign` was reintroduced, revert it. Re-pin, then cut. Do NOT just re-drive. |
| `DLSequence` cast / ASN.1 / Bouncy-Castle internal error in timestamp path | unpinned/bumped signing/timestamp lib | Toolchain — version regression | Pin (or re-pin) the tool to a known-good `*_VERSION` + `*_SHA256` in its own PR, then cut. |
| jsign `--storetype TRUSTEDSIGNING` timestamp/`tsmode` error, or transient TSA/CE ("certificate enrollment") timeout | Trusted Signing timestamp authority flake | Infra — transient | Clean stale tag + `workflow_dispatch` re-drive **once**. If it recurs identically, escalate to pin review. |
| Cosign / Fulcio / Rekor / OIDC token network error; Sonar JRE 403; `az account get-access-token` transient | external-service flake | Infra — transient | Clean stale tag + re-drive once. |
| `fatal: tag 'v<x.y.z>' already exists` (git exit 128) on a re-run | leftover tag from prior half-cut | State — stale tag | Delete remote+local tag (and partial release), then re-drive. Not a regression. |
| `CLI reports version X but VERSION file has Y` | `Verify CLI version matches` step | Real regression | Fix VERSION / ldflags / CLI; do NOT re-drive. |
| Go `go test` / `go build` failure, or `scripts/verify.sh` smoke failure | Run Go tests / smoke step | Real regression | Hand back to code owner; release stays uncut. |

## Re-drive discipline

- Re-drive at most **once** per infra-class failure. A second identical signature
  means the toolchain/pin itself needs a fix PR — stop re-driving.
- Always clean the stale tag (and any partial GitHub release) **before** the
  re-drive, or the re-run aborts at the tag-create step with exit 128.
- The `workflow_dispatch` path needs no version bump; the "Check if release
  exists" guard makes the re-run a safe no-op if the version already shipped.

## See also

The `pin-release-toolchain-and-make-releases-retryable` lesson is the *why*
behind this classifier — the kernel32 and DLSequence signatures are its canonical
"toolchain fault, not project regression" examples.
