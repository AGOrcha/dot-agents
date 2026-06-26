# Spec: Local gate ↔ CI parity (`make gate` single-source, kill the `--no-verify` loop)

**Spec id:** `local-gate-ci-parity`
**Status:** draft (for review) — design stage 2026-06-25 (owner-ratified decisions below).
**Source:** the 2026-06-25 graph-chain wave — workers passed the local prek pre-push hook, then failed CI ~6 times across #168/#170/#173/#174 on per-file coverage, Windows runtime, and sonar-new-issues; and (per owner) workers had started `--no-verify`-ing the hook due to residual flakiness.
**Grounds in:** [[gates-must-be-locally-reproducible]], [[tests-must-drive-the-production-path]], the `prek` pre-push mandate (`scripts/precommit-mandate.sh`), `scripts/coverage-gate.sh`, `scripts/sonar-new-issues-gate.sh`, the CI `test.yml` Coverage-gate (merged multi-OS) job, and the native-sonar work (#16/#147).

---

## 1. Problem & goal

A worker's local pre-push gate does **not** faithfully reproduce the gates that block merge, so two failure modes recur:

1. **Pass-local-fail-CI.** The worker proves the tree green locally, pushes, and CI fails on a gate the local hook didn't run — burning a multi-minute pipeline per round.
2. **Bypass.** When the local hook flakes (non-deterministic failure unrelated to the change), the worker reaches for `--no-verify` — which skips *every* gate, not just the flaky one, shipping unverified work.

**Goal:** make the local gate **identical to CI by construction** and **deterministic**, so the worker's loop is tight and there is never a reason to bypass.

### Root causes (grounded)

- **R1 — coverage granularity mismatch.** The hook (`precommit-mandate.sh cmd_coverage`) enforces **≥95% per-*package*** (per-file defaults to `warn`); CI's Coverage-gate runs **≥95% per-*file*** (`COVERAGE_FILE_MODE=enforce`). Workers pass the hook, fail CI (#173: 5 files <95%; #174: 1 file).
- **R2 — single-OS vs merged-multi-OS profile.** The hook produces a macOS-only `coverage.out` (`go test ./...`); CI merges darwin+linux+windows. Per-file enforce on a single-OS profile *false-fails* on platform-specific files (e.g. `fsops_windows.go`) — so naive local per-file is noisy — and a single-OS run also misses **Windows runtime** bugs that cross-compile cannot catch (`os.ReadDir`-on-file is a no-op on Windows → #174).
- **R3 — residual flakiness drives bypass.** The pre-push sonar step (native scanner, post-#16/#147) and/or `.scannerwork`/fsmonitor churn intermittently aborts the push ("files were modified by this hook"), training workers to `--no-verify`. Per [[gates-must-be-locally-reproducible]], a non-deterministic gate is the defect.
- **R4 — no single source of truth.** Gate logic lives in `precommit-mandate.sh` (hook) and `test.yml` (CI) independently; they drift (R1 is an instance). There is no `make gate` a human/worker/hook/CI all share.

## 2. Decisions (owner-ratified 2026-06-25)

- **D1 — `make gate` is the single source of truth.** One target defines the merge-blocking mandate. The prek pre-push hook calls `make gate`; humans run `make gate`; **CI runs `make gate` too**, so local == CI by construction (closes R4). `precommit-mandate.sh` becomes an implementation detail invoked by `make gate`, not a parallel definition.
- **D2 — two tiers: fast `make gate` (every push) + `make gate-cross` (pre-merge).**
  - **`make gate`** (every push, deterministic, seconds-to-low-minutes): build + vet (incl. `GOOS=windows` cross-compile), **per-file** coverage enforced **scoped to changed files** (single-OS, faithful for new cross-platform code, sidesteps R2 false-fails + pre-existing debt), native sonar (D3).
  - **`make gate-cross`** (pre-merge, heavier): real **Windows tests via `ssh pap-h@pap-home.local`** for changed packages (closes the runtime gap cross-compile misses), and a **merged local+Windows coverage profile** → true multi-OS per-file enforce == CI. Driven by the orchestrator (or CI) before merge, not on every push.
- **D3 — sonar is tiered: offline proxies fast, full native sonar pre-merge (owner-ratified 2026-06-25, resolves OQ1).** The **fast every-push tier uses offline proxies** for the only new-issue rules that blocked this wave: `gocognit -over 15` (S3776) + a duplicated-literal scan (S1192). These are instant, offline, deterministic — keeping every-push fast so there is no bypass incentive (closes R3 without a server round-trip on every push). The **full native sonar-scanner** (Java 25 + sonar-scanner 8.1, already present; all rules + true new-issues-vs-baseline) runs in **`make gate-cross` (pre-merge) + CI** as the authoritative backstop. The native run must keep the worktree pristine (`.scannerwork`/`dist` excluded + gitignored, extending the #147 fix) so prek never sees a hook-modified file.
- **D4 — CI per-file coverage is the contract; the local gate matches it, never weaker.** The local gate must be **≥** CI in strictness on changed code (R1 fixed): per-file enforce on changed files locally; full merged multi-OS per-file in `gate-cross`/CI.
- **D5 — Windows parity is achieved by execution, not simulation.** Because the Windows box is reachable, the design *runs* Windows rather than skipping it — `gate-cross` ssh-runs the changed packages' tests on Windows. OS-skips in tests remain a last resort (per the cross-platform-test-skips direction); a portable trigger is preferred.

## 3. Requirements

1. `make gate` exits non-zero iff a merge-blocking CI gate would fail for the **changed** code; deterministic across repeated runs on an unchanged tree (no flake).
2. The prek pre-push hook invokes `make gate` (no parallel gate logic in `precommit-mandate.sh`).
3. CI's Coverage-gate job invokes `make gate` / `make gate-cross` so the same definition runs in both places.
4. `make gate` enforces **per-file** ≥95% on changed `.go` files (matching CI's enforce mode), not per-package.
5. `make gate-cross` produces a **merged multi-OS** coverage profile (local + Windows-over-ssh) and runs changed-package tests on Windows; its per-file enforce result equals CI's.
6. The native sonar step leaves the working tree pristine (no prek "files modified by this hook"); `.scannerwork`/`dist` are gitignored and scanner-excluded.
7. A worker brief / loop-worker self-gate references `make gate` as the pre-push step; `--no-verify` is documented as forbidden (the hook is reliable, so there is no reason).

## 4. Open questions (resolve in the plan)

- **OQ1 — RESOLVED (D3): proxies fast, full native sonar in `gate-cross`/CI.** Plan must define the dup-literal proxy precisely (the S1192 threshold sonar uses — same literal ≥N times, non-trivial length) so the proxy doesn't diverge from the server rule.
- **OQ2 — "changed files" derivation.** Diff base for scoping per-file coverage + Windows package selection: `merge-base origin/master HEAD`? changed `.go` files → owning packages? Define precisely.
- **OQ3 — Windows-over-ssh orchestration.** Sync mechanism (rsync/git push-to-box/clone), Go cache on the box, parallel-push safety, and failure semantics if the box is unreachable (skip-with-loud-warning vs hard-fail). The box requires a non-batch ssh in general; confirm key-only `BatchMode` works for automation.
- **OQ4 — CI invocation shape.** Does CI literally call `make gate-cross`, or call the same underlying scripts with CI-provided multi-OS artifacts (CI already has real Windows runners — it should feed its native Windows coverage into the merge rather than ssh)? Likely: `make gate` shares the *scripts*; CI assembles the multi-OS profile from its matrix.

## 5. Done criteria

- A change that fails CI's per-file coverage, Windows tests, or sonar-new-issues **also fails `make gate`/`make gate-cross` locally** (demonstrated by replaying #173 and #174 pre-fix: both must fail the local gate).
- Running `make gate` twice on an unchanged tree yields identical PASS with no working-tree mutation (determinism / R3).
- The prek pre-push hook and CI both run `make gate*` (one definition; grep shows no divergent coverage-mode constant).
- A worker following its brief runs `make gate` and does not need `--no-verify`.

## 6. Deferred / out of scope

- A typed `da verify` command (owner chose `make gate`); revisit only if the shell target proves limiting.
- Hardening the *containerized* sonar path (superseded by native, D3).
- Generalizing the Windows-over-ssh runner to other repos/platforms (design for dot-agents first).
