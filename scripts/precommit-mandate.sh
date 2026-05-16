#!/usr/bin/env bash
# precommit-mandate.sh — heavy pre-push checks, dispatched by prek
# (see .pre-commit-config.yaml). Subcommands:
#
#   build-vet   POSIX + GOOS=windows `go build` and `go vet ./...`
#   coverage    regenerate a fresh profile and enforce the 95%-per-package
#               gate (scripts/coverage-gate.sh)
#   sonar       containerized SonarCloud analysis when Docker + SONAR_TOKEN
#               are present; loud actionable skip otherwise
#
# These run at the pre-push stage, NOT per commit: they are slow and the
# coverage step runs the full test suite. Running them per commit also
# caused a repo-corruption incident — git exports GIT_DIR/GIT_INDEX_FILE
# into hook env, which leaked into test git subprocesses and flipped
# core.bare on the shared config. We strip that env defensively below;
# prek additionally isolates hook execution.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# Defense-in-depth: never let git's hook env reach `go test`'s git
# subprocesses (internal/testutil et al. spawn git in temp dirs).
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_EXEC_PATH \
      GIT_REFLOG_ACTION GIT_AUTHOR_DATE GIT_COMMITTER_DATE 2>/dev/null || true

say()  { printf '\n\033[1m[mandate:%s] %s\033[0m\n' "${1:-?}" "${2:-}"; }
fail() { printf '\n\033[31m[mandate] BLOCKED: %s\033[0m\n' "$1" >&2; exit 1; }

cmd_fmt() {
  say fmt "gofmt"
  u="$(gofmt -l ./cmd ./commands ./internal 2>/dev/null || true)"
  if [ -n "$u" ]; then
    printf '%s\n' "$u"
    fail "gofmt: run gofmt -w on the files above"
  fi
}

cmd_build_vet() {
  say build-vet "go build (POSIX + windows) + go vet"
  go build ./...               || fail "go build failed"
  GOOS=windows go build ./...  || fail "GOOS=windows go build failed"
  go vet ./...                 || fail "go vet reported findings"
}

cmd_coverage() {
  say coverage "95%-per-package gate (fresh profile)"
  # Plain covermode=atomic — coverage % is identical to the -race CI
  # profile; -race only adds the race detector, not coverage.
  go test -count=1 -timeout=300s -covermode=atomic \
      -coverprofile=coverage.out ./... \
    || fail "go test failed (coverage profile not produced)"
  COVERAGE_FILE=coverage.out COVERAGE_THRESHOLD=95 \
    bash scripts/coverage-gate.sh \
    || fail "coverage gate: a package is below 95%"
}

cmd_sonar() {
  say sonar "containerized sonar-scanner"
  if ! command -v docker >/dev/null 2>&1; then
    printf '\033[33m[mandate:sonar] SKIPPED: docker not found.\n'
    printf '  Install Docker + set SONAR_TOKEN to enable the Sonar mandate.\033[0m\n'
    return 0
  fi
  if [ -z "${SONAR_TOKEN:-}" ]; then
    printf '\033[33m[mandate:sonar] SKIPPED: SONAR_TOKEN not set.\n'
    printf '  export SONAR_TOKEN=<token> (SonarCloud > Account > Security).\033[0m\n'
    return 0
  fi
  docker run --rm \
    -e SONAR_TOKEN="$SONAR_TOKEN" \
    -e SONAR_HOST_URL="${SONAR_HOST_URL:-https://sonarcloud.io}" \
    -v "$repo_root:/usr/src" \
    sonarsource/sonar-scanner-cli:latest \
    || fail "sonar-scanner analysis failed"
}

case "${1:-}" in
  fmt)       cmd_fmt ;;
  build-vet) cmd_build_vet ;;
  coverage)  cmd_coverage ;;
  sonar)     cmd_sonar ;;
  *) echo "usage: precommit-mandate.sh {fmt|build-vet|coverage|sonar}" >&2; exit 2 ;;
esac
