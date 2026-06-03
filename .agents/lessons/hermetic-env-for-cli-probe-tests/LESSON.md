# Lesson: unit tests must not probe the real host environment

## What happened

`git push` of a fully-green release branch was rejected by the pre-push gate
with an opaque `[mandate] BLOCKED: go test failed (coverage profile not
produced)` — the bare `FAIL` had no package name (the push output is
`tail`-truncated). A plain local `go test ./...` passed, so it looked flaky.

It was not flaky. Reproducing the *exact* gate step with full output —
`bash scripts/precommit-mandate.sh coverage > /tmp/log 2>&1` — showed
`commands/internal/lifecycle` hit `panic: test timed out after 5m0s` at
**301.7s**, one second over the gate's `-timeout=300s`. The panic's running
goroutine was in `internal/platform/cliprobe.go` → `os/exec` (a
`copilot --version` probe). The parent `commands` package was at 253.9s — the
same latent bomb.

## Root cause

The install/doctor/status/refresh tests exercise code that probes every
platform's CLI for a `--version` string (`internal/platform/cliprobe.go`). The
test helpers seed fake shims but **prepend** them to the inherited PATH, and
most of the ~100 `runInit`/detect call sites seed nothing — so every probe for
an unstubbed platform fell through to a **real agent CLI on the dev machine**
(claude/codex/copilot in `/opt/homebrew/bin`, cursor in `~/.local/bin`), each
spawning a real subprocess (~1s+ apiece). Compounded, the packages crept to
~250–300s.

This is **machine-dependent and invisible on CI**: CI has no agent CLIs
installed, so every probe is an instant LookPath miss and the suite is fast and
green. The failure only appears on a dev machine that has the CLIs — i.e. the
local pre-push gate is effectively *stricter* than CI here, the inverse of
[[match-ci-test-flags-locally]]. A package that sits at ~300s also passes once
unloaded and fails under the concurrent gate load (sonar container) — boundary
hugging reads as "flaky" but is deterministic environment leak.

## Rule

1. **A unit test must not depend on what is installed on the host.** Any test
   that drives code which probes CLIs on PATH (or app bundles, or other host
   state) must neutralize the environment, not inherit it.
2. **Pin a hermetic base PATH in the package `TestMain`** — the Go toolchain dir
   (`runtime.GOROOT()/bin`, kept so tests that `go build` a fake binary still
   work) + `/usr/bin:/bin:/usr/sbin:/sbin`, and *deliberately drop* the dirs
   that hold the real agent CLIs. Unstubbed probes become fast LookPath misses;
   tests that want a platform present still prepend a fake version-returning
   shim, so the version-parse path stays exercised against a deterministic mock
   (don't just hide the CLIs — that lowers what you test). Guard `GOOS=windows`.
3. **Prepending fakes to the real PATH is not isolation** — it only covers the
   platforms a test bothered to stub; the rest leak. Hermeticity has to be
   package-wide (`TestMain`), not per-helper.

## How to apply

- Diagnosing an opaque pre-push `go test failed`: run
  `bash scripts/precommit-mandate.sh coverage > /tmp/log 2>&1` (full output, the
  push tail hides the package), then grep the log for `FAIL`/`panic:` and read
  the panic's running goroutine for the blocking syscall.
- Time a suspect package in isolation: `/usr/bin/time -p go test ./<pkg> -count=1
  -timeout=300s`. Anything within ~2× of the gate timeout is a latent failure.
- When adding tests that touch CLI detection / `internal/platform`, confirm the
  package has a hermetic `TestMain` (see `commands/testmain_test.go` and
  `commands/internal/lifecycle/testmain_test.go`).
- Root-cause sibling: code that *needs* a real external binary in a unit test
  (e.g. shelling out to `git`) is the deeper smell — prefer the in-process
  library (go-git) so there is no PATH/subprocess dependency at all. Tracked for
  the install clone path under `config-v2-migration/p5-source-types-http-oci`.
