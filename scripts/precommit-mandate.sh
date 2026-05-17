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

say()  { local label="${1:-?}" msg="${2:-}"; printf '\n\033[1m[mandate:%s] %s\033[0m\n' "$label" "$msg"; }
fail() { local reason="${1:-}"; printf '\n\033[31m[mandate] BLOCKED: %s\033[0m\n' "$reason" >&2; exit 1; }

cmd_fmt() {
  say fmt "gofmt"
  u="$(gofmt -l ./cmd ./commands ./internal 2>/dev/null || true)"
  if [[ -n "$u" ]]; then
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
  # Token resolution: explicit $SONAR_TOKEN wins; otherwise reuse the
  # SonarQube MCP server credentials already configured in .mcp.json
  # (gitignored — never committed). Looked up in the current worktree
  # then the main worktree (.mcp.json is gitignored so it only exists in
  # the primary checkout). The token value is never printed.
  if [[ -z "${SONAR_TOKEN:-}" ]]; then
    mcp_json=""
    for cand in \
      "$repo_root/.mcp.json" \
      "$(git worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2; exit}')/.mcp.json"
    do
      [[ -f "$cand" ]] && { mcp_json="$cand"; break; }
    done
    if [[ -n "$mcp_json" ]]; then
      if command -v jq >/dev/null 2>&1; then
        SONAR_TOKEN="$(jq -r '.mcpServers.sonarqube.env.SONARQUBE_TOKEN // empty' "$mcp_json" 2>/dev/null)"
        : "${SONAR_HOST_URL:=$(jq -r '.mcpServers.sonarqube.env.SONARQUBE_CLOUD_URL // empty' "$mcp_json" 2>/dev/null)}"
      else
        SONAR_TOKEN="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("mcpServers",{}).get("sonarqube",{}).get("env",{}).get("SONARQUBE_TOKEN",""))' "$mcp_json" 2>/dev/null)"
        : "${SONAR_HOST_URL:=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("mcpServers",{}).get("sonarqube",{}).get("env",{}).get("SONARQUBE_CLOUD_URL",""))' "$mcp_json" 2>/dev/null)}"
      fi
      export SONAR_TOKEN SONAR_HOST_URL
      [[ -n "$SONAR_TOKEN" ]] && printf '[mandate:sonar] using SonarQube MCP credentials from .mcp.json\n'
    fi
  fi
  if [[ -z "${SONAR_TOKEN:-}" ]]; then
    printf '\033[33m================ SONAR NOT ENFORCED ================\n'
    printf '[mandate:sonar] SKIPPED: no SONAR_TOKEN and no sonarqube token\n'
    printf 'in .mcp.json — the SonarCloud quality gate (incl. new security\n'
    printf 'hotspots) was NOT checked locally; CI is your only gate.\033[0m\n'
    return 0
  fi
  # -Dsonar.qualitygate.wait=true makes the scanner block until SonarCloud
  # computes the gate and exit non-zero if it fails — without this the CLI
  # exits 0 on a successful *upload* regardless of the gate verdict, so a
  # local scan would NOT have caught e.g. unreviewed new security hotspots.
  #
  # Pass secrets by env-var NAME only (`-e SONAR_TOKEN`, no inline value):
  # `-e VAR=value` puts the token in docker's argv where `ps`/runner
  # diagnostics can read it. Export so the child docker inherits it.
  export SONAR_TOKEN
  export SONAR_HOST_URL="${SONAR_HOST_URL:-https://sonarcloud.io}"
  docker run --rm \
    -e SONAR_TOKEN \
    -e SONAR_HOST_URL \
    -v "$repo_root:/usr/src" \
    sonarsource/sonar-scanner-cli:latest \
    -Dsonar.qualitygate.wait=true \
    || fail "sonar-scanner: SonarCloud quality gate failed (or analysis errored)"
}

case "${1:-}" in
  fmt)       cmd_fmt ;;
  build-vet) cmd_build_vet ;;
  coverage)  cmd_coverage ;;
  sonar)     cmd_sonar ;;
  *) echo "usage: precommit-mandate.sh {fmt|build-vet|coverage|sonar}" >&2; exit 2 ;;
esac
