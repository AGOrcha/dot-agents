#!/usr/bin/env bash
# gate.sh — the FAST, every-push merge-blocking mandate, single source of truth.
#
# This is what `make gate` runs and what the prek pre-push hook invokes (per the
# local-gate-ci-parity spec, decisions D1/D2/D4). Run it directly any time:
#
#     make gate           # preferred
#     bash scripts/gate.sh
#
# It is DETERMINISTIC: two runs on an unchanged tree produce identical PASS and
# never mutate the working tree. Steps (fast tier only — gate-cross/sonar are
# owned by sibling tasks and slot in later):
#
#   1. gofmt -l        (cmd/ commands/ internal/ — same scope as CI)
#   2. go build ./...
#   3. go vet ./...
#   4. GOOS=windows go build ./...  +  GOOS=windows go vet ./...  (cross parity)
#   5. per-file coverage ENFORCE 100% over the FULL-suite profile, EXACTLY as CI
#      runs it — COVERAGE_FILE_MODE=enforce with CI's same scripts/
#      coverage-exceptions.txt allowlist + threshold. This is faithful to CI by
#      construction (zero coverage-mode drift between the hook and CI even before
#      t5 wires CI to call `make gate`), and no slower (the full suite already
#      runs to produce the profile).
#
#      NOTE (gap owned by t3/gate-cross): a genuinely NEW platform-only file
#      (e.g. a brand-new *_windows.go) has zero coverage rows on this single-OS
#      local run, so the local per-file enforce cannot see it. The merged
#      multi-OS profile in `make gate-cross`/CI is what closes that residual gap.
#      Existing platform-tagged files (fsops_windows.go, …) are already handled
#      identically to CI because they are in coverage-exceptions.txt.
#
#      An earlier revision scoped per-file enforce to the changed `.go` files; a
#      cross-harness (Codex) adversarial review showed that diff-scoping reopens
#      pass-local-fail-CI holes (zero-row platform files passed locally;
#      test-only changes dropped a production file below 100% but were skipped because
#      no production .go "changed"). The pivot to full enforce closes both.
#
# The fast tier no longer needs a diff base at all (coverage is full-suite, not
# diff-scoped). When t3 adds Windows-package selection / gate-cross it will
# derive the changed set from `git merge-base origin/master HEAD` and FAIL LOUD
# if origin/master is absent (never a silent HEAD~1 fallback).
#
# NO COVERAGE-BYPASS KNOB. There is deliberately no env var that lets a caller
# skip or substitute the real coverage run on the production path — that would be
# the same hole as `--no-verify` (git hooks inherit the environment, so any such
# var would be settable on a `git push`). The coverage-enforce LOGIC is exposed
# as a subcommand (`gate.sh enforce-coverage <profile>`) so scripts/gate.test.sh
# can exercise it against fixture profiles WITHOUT changing what production does:
# `make gate` always generates the real profile via `go test` and enforces on it.
#
# Env knobs (these only tune thresholds/paths to MATCH CI — none can skip a step):
#   COVERAGE_THRESHOLD    per-file threshold (default 100, matches CI).
#   COVERAGE_EXCEPTIONS   allowlist path (default scripts/coverage-exceptions.txt,
#                         same file CI uses).
#   COVERAGE_FLOOR_EXCEPTIONS  retained-95% allowlist (default
#                         scripts/coverage-floor-exceptions.txt).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# Never let git's hook env (set when invoked from a pre-push hook) leak into the
# `go test` git subprocesses — same defense the mandate script applies.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_EXEC_PATH \
      GIT_REFLOG_ACTION GIT_AUTHOR_DATE GIT_COMMITTER_DATE 2>/dev/null || true

say()  { printf '\n\033[1m[gate:%s] %s\033[0m\n' "${1:-?}" "${2:-}"; }
fail() { printf '\n\033[31m[gate] BLOCKED: %s\033[0m\n' "${1:-}" >&2; exit 1; }

# enforce_coverage_profile <profile> — the per-file coverage ENFORCE step, given
# a coverage profile. This is the SAME invocation CI's coverage-gate job uses:
# per-file ENFORCE, per-package warn, the shared coverage-exceptions allowlist +
# threshold. coverage-gate.sh is the single coverage authority, so there is zero
# coverage-mode drift between the hook and CI even before t5 wires CI to call
# `make gate`. Both the production path AND gate.test.sh call THIS function — the
# test passes a fixture profile, production passes the real `go test` profile, so
# the test exercises the exact production enforce logic with no bypass.
enforce_coverage_profile() {
  local profile="$1"
  COVERAGE_FILE="$profile" \
  COVERAGE_THRESHOLD=95 \
  COVERAGE_PKG_MODE=warn \
  COVERAGE_FILE_MODE=enforce \
  COVERAGE_EXCEPTIONS="${COVERAGE_FLOOR_EXCEPTIONS:-scripts/coverage-floor-exceptions.txt}" \
    bash "$repo_root/scripts/coverage-gate.sh" \
    || fail "coverage floor: a file regressed below 95% per-file coverage (not floor-allowlisted)"

  COVERAGE_FILE="$profile" \
  COVERAGE_THRESHOLD="${COVERAGE_THRESHOLD:-100}" \
  COVERAGE_PKG_MODE=warn \
  COVERAGE_FILE_MODE=enforce \
    bash "$repo_root/scripts/coverage-gate.sh" \
    || fail "coverage gate: a file is below ${COVERAGE_THRESHOLD:-100}% per-file coverage (not allowlisted)"
}

# run_coverage — PRODUCTION coverage step: always generate the real full-suite
# profile, then enforce on it. No knob can skip or substitute the `go test` run.
# Plain covermode=atomic — coverage % is identical to CI's -race profile (-race
# only adds the race detector). The profile is a temp file so `make gate` never
# mutates the worktree (determinism + prek never sees a hook-modified file).
run_coverage() {
  say coverage "full-suite per-file enforce (>=${COVERAGE_THRESHOLD:-100}%, CI's coverage-exceptions allowlist)"
  local cov_out
  cov_out="$(mktemp -t gate-cov.XXXXXX)"
  trap 'rm -f "$cov_out"' EXIT
  go test -count=1 -timeout=300s -covermode=atomic -coverprofile="$cov_out" ./... \
    || fail "go test failed (coverage profile not produced)"
  enforce_coverage_profile "$cov_out"
}

# run_gate — the full fast-tier mandate (what `make gate` runs).
run_gate() {
  # --- 1. gofmt ----------------------------------------------------------
  say fmt "gofmt -l (cmd commands internal)"
  local unformatted
  unformatted="$(gofmt -l ./cmd ./commands ./internal 2>/dev/null || true)"
  if [[ -n "$unformatted" ]]; then
    printf '%s\n' "$unformatted"
    fail "gofmt: run 'gofmt -w' on the files above"
  fi

  # --- 2/3. build + vet (POSIX) -----------------------------------------
  say build-vet "go build ./... + go vet ./..."
  go build ./... || fail "go build failed"
  go vet ./...   || fail "go vet reported findings"

  # --- 4. cross-compile parity (windows build + vet) --------------------
  say cross "GOOS=windows go build ./... + go vet ./..."
  GOOS=windows go build ./... || fail "GOOS=windows go build failed"
  GOOS=windows go vet ./...   || fail "GOOS=windows go vet reported findings"

  # --- 5. per-file coverage ENFORCE over the FULL profile (exactly as CI)
  run_coverage

  say ok "gate PASS"
}

# Dispatch. Default (no args) runs the full gate. `enforce-coverage <profile>`
# exposes ONLY the enforce logic for scripts/gate.test.sh — it does NOT generate
# a profile and is not used by production `make gate`.
case "${1:-}" in
  "")               run_gate ;;
  enforce-coverage) shift; [[ -n "${1:-}" ]] || fail "enforce-coverage: profile path required"
                    enforce_coverage_profile "$1" ;;
  *) echo "usage: gate.sh [enforce-coverage <profile>]" >&2; exit 2 ;;
esac
