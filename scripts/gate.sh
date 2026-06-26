#!/usr/bin/env bash
# gate.sh — the FAST, every-push merge-blocking mandate, single source of truth.
#
# This is what `make gate` runs, what the prek pre-push hook invokes, and the
# shared definition CI uses for the fast tier (per the local-gate-ci-parity
# spec, decisions D1/D2/D4). Run it directly any time:
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
#   5. per-file coverage ENFORCE >=95%, SCOPED to the .go files changed vs
#      `git merge-base origin/master HEAD`. Scoping to changed files matches
#      CI's per-file enforce contract for new code while sidestepping the
#      single-OS false-fail on platform-tagged files and pre-existing debt
#      (spec R2/D2/D4). coverage-gate.sh remains the single coverage authority.
#
# Env knobs (mostly for tests / CI):
#   GATE_DIFF_BASE        override the diff base (default: merge-base of
#                         origin/master and HEAD; falls back to origin/master,
#                         then master, then HEAD~1).
#   GATE_SKIP_COVERAGE=1  run build/vet/fmt only (used by the coverage unit
#                         test which drives coverage-gate.sh directly).
#   COVERAGE_THRESHOLD    per-file threshold (default 95, matches CI).
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

# --- 5. per-file coverage ENFORCE on CHANGED .go files -------------------
# Diff base (OQ2): the merge-base of origin/master and HEAD, so the changed set
# is exactly this branch's contribution. Degrade gracefully when the remote ref
# is unavailable (fresh clone without origin, detached state).
diff_base="${GATE_DIFF_BASE:-}"
if [[ -z "$diff_base" ]]; then
  if git rev-parse --verify -q origin/master >/dev/null; then
    diff_base="$(git merge-base origin/master HEAD 2>/dev/null || true)"
  fi
  [[ -z "$diff_base" ]] && diff_base="$(git rev-parse --verify -q master 2>/dev/null || true)"
  [[ -z "$diff_base" ]] && diff_base="$(git rev-parse --verify -q HEAD~1 2>/dev/null || true)"
fi

# Collect changed .go files: committed (base..HEAD) + uncommitted (working tree
# and staged), filter to non-test .go, keep only files that still exist. Test
# files are not coverage targets for their own coverage, so they are excluded
# from the enforce set (they still execute and contribute to other files' %).
collect_changed() {
  {
    if [[ -n "$diff_base" ]]; then
      git diff --name-only --diff-filter=ACMR "$diff_base"...HEAD 2>/dev/null || true
    fi
    git diff --name-only --diff-filter=ACMR HEAD 2>/dev/null || true       # unstaged
    git diff --name-only --diff-filter=ACMR --cached 2>/dev/null || true   # staged
  } | sort -u
}

changed_go=()
while IFS= read -r f; do
  [[ -z "$f" ]] && continue
  [[ "$f" == *.go ]] || continue
  [[ "$f" == *_test.go ]] && continue
  [[ -f "$f" ]] || continue
  changed_go+=("$f")
done < <(collect_changed)

if [[ "${#changed_go[@]}" -eq 0 ]]; then
  say coverage "no changed .go files vs ${diff_base:-<base>} — per-file coverage gate has nothing to enforce"
  say ok "gate PASS (build + vet + cross-compile; no changed Go to cover)"
  exit 0
fi

say coverage "${#changed_go[@]} changed .go file(s); running suite + per-file enforce (>=${COVERAGE_THRESHOLD:-95}%)"
printf '  - %s\n' "${changed_go[@]}"

# Run the suite once with coverage. Plain covermode=atomic — coverage % is
# identical to CI's -race profile (-race only adds the race detector).
# Write the profile to a temp file so `make gate` never mutates the worktree
# (coverage.out is not gitignored as a committed artifact; keep the tree
# pristine for determinism + so prek never sees a hook-modified file).
cov_out="$(mktemp -t gate-cov.XXXXXX)"
trap 'rm -f "$cov_out"' EXIT
go test -count=1 -timeout=300s -covermode=atomic -coverprofile="$cov_out" ./... \
  || fail "go test failed (coverage profile not produced)"

# Enforce per-file >=95% ONLY on the changed files (single-OS profile is faithful
# for changed code; coverage-gate.sh honors the same exceptions allowlist as CI).
COVERAGE_FILE="$cov_out" \
COVERAGE_THRESHOLD="${COVERAGE_THRESHOLD:-95}" \
COVERAGE_PKG_MODE=off \
COVERAGE_FILE_MODE=enforce \
COVERAGE_INCLUDE_FILES="$(printf '%s\n' "${changed_go[@]}")" \
  bash "$repo_root/scripts/coverage-gate.sh" \
  || fail "coverage gate: a changed file is below ${COVERAGE_THRESHOLD:-95}% per-file coverage"

say ok "gate PASS"
