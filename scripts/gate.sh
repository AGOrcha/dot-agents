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
#   5. per-file coverage ENFORCE >=95% over the FULL-suite profile, EXACTLY as CI
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
#      test-only changes dropped a production file <95% but were skipped because
#      no production .go "changed"). The pivot to full enforce closes both.
#
# The fast tier no longer needs a diff base at all (coverage is full-suite, not
# diff-scoped). When t3 adds Windows-package selection / gate-cross it will
# derive the changed set from `git merge-base origin/master HEAD` and FAIL LOUD
# if origin/master is absent (never a silent HEAD~1 fallback).
#
# Env knobs (mostly for tests / CI):
#   GATE_SKIP_COVERAGE=1  run build/vet/fmt only (used by the coverage unit
#                         test which drives coverage-gate.sh directly).
#   COVERAGE_THRESHOLD    per-file threshold (default 95, matches CI).
#   COVERAGE_EXCEPTIONS   allowlist path (default scripts/coverage-exceptions.txt,
#                         same file CI uses).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# Never let git's hook env (set when invoked from a pre-push hook) leak into the
# `go test` git subprocesses — same defense the mandate script applies.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_EXEC_PATH \
      GIT_REFLOG_ACTION GIT_AUTHOR_DATE GIT_COMMITTER_DATE 2>/dev/null || true

say()  { printf '\n\033[1m[gate:%s] %s\033[0m\n' "${1:-?}" "${2:-}"; }
fail() { printf '\n\033[31m[gate] BLOCKED: %s\033[0m\n' "${1:-}" >&2; exit 1; }

# --- 1. gofmt -------------------------------------------------------------
say fmt "gofmt -l (cmd commands internal)"
unformatted="$(gofmt -l ./cmd ./commands ./internal 2>/dev/null || true)"
if [[ -n "$unformatted" ]]; then
  printf '%s\n' "$unformatted"
  fail "gofmt: run 'gofmt -w' on the files above"
fi

# --- 2/3. build + vet (POSIX) --------------------------------------------
say build-vet "go build ./... + go vet ./..."
go build ./... || fail "go build failed"
go vet ./...   || fail "go vet reported findings"

# --- 4. cross-compile parity (windows build + vet) -----------------------
say cross "GOOS=windows go build ./... + go vet ./..."
GOOS=windows go build ./... || fail "GOOS=windows go build failed"
GOOS=windows go vet ./...   || fail "GOOS=windows go vet reported findings"

if [[ "${GATE_SKIP_COVERAGE:-0}" == "1" ]]; then
  say ok "build/vet/fmt PASS (coverage skipped via GATE_SKIP_COVERAGE)"
  exit 0
fi

# --- 5. per-file coverage ENFORCE over the FULL profile (exactly as CI) ---
# Run the suite once with coverage, then enforce per-file >=95% on EVERY
# non-allowlisted file — the same invocation CI's coverage-gate job runs
# (COVERAGE_FILE_MODE=enforce + scripts/coverage-exceptions.txt + threshold 95).
# Full enforce (not diff-scoping) is what makes the local gate faithful to CI:
#   - platform-tagged files are handled by the shared allowlist, not skipped;
#   - a test-only change that drops a *production* file <95% is still caught
#     (diff-scoping skipped it because no production .go "changed").
#
# Plain covermode=atomic — coverage % is identical to CI's -race profile (-race
# only adds the race detector). Write the profile to a temp file so `make gate`
# never mutates the worktree (determinism + prek never sees a hook-modified
# file).
say coverage "full-suite per-file enforce (>=${COVERAGE_THRESHOLD:-95}%, CI's coverage-exceptions allowlist)"
# Test seam: GATE_COVERAGE_PROFILE lets scripts/gate.test.sh drive the SAME
# enforce code path below against a synthetic profile (a real fixture-based
# regression for the #173 sub-95% case and the test-only-weakening case)
# without paying for a full `go test` run. Production never sets it.
if [[ -n "${GATE_COVERAGE_PROFILE:-}" ]]; then
  cov_out="$GATE_COVERAGE_PROFILE"
else
  cov_out="$(mktemp -t gate-cov.XXXXXX)"
  trap 'rm -f "$cov_out"' EXIT
  go test -count=1 -timeout=300s -covermode=atomic -coverprofile="$cov_out" ./... \
    || fail "go test failed (coverage profile not produced)"
fi

# Same coverage-gate.sh invocation CI's coverage-gate job uses: per-file ENFORCE,
# per-package warn, default exceptions allowlist + threshold. coverage-gate.sh is
# the single coverage authority shared with CI, so there is zero coverage-mode
# drift even before t5 wires CI to literally call `make gate`.
COVERAGE_FILE="$cov_out" \
COVERAGE_THRESHOLD="${COVERAGE_THRESHOLD:-95}" \
COVERAGE_PKG_MODE=warn \
COVERAGE_FILE_MODE=enforce \
  bash "$repo_root/scripts/coverage-gate.sh" \
  || fail "coverage gate: a file is below ${COVERAGE_THRESHOLD:-95}% per-file coverage (not allowlisted)"

say ok "gate PASS"
