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

# sonar_gate_diagnostics — on a quality-gate failure, print the failing gate
# CONDITIONS and the new-code SECURITY HOTSPOTS to the error log so the block is
# actionable (IntelliJ-style), instead of the opaque "gate failed". The new
# ISSUES detail is already printed by sonar-new-issues-gate.sh, but that runs
# only after a PASS — a hotspot/coverage gate failure exits before it. Best
# effort: needs SONAR_TOKEN (resolved by cmd_sonar) + sonar-project.properties;
# any query hiccup degrades to the dashboard pointer. Queries the main branch
# (free-tier SonarCloud analyzes locally as main — branch analysis is paid).
sonar_gate_diagnostics() {
  local host="${SONAR_HOST_URL:-https://sonarcloud.io}" key org
  key="$(sed -n 's/^sonar\.projectKey=//p' "$repo_root/sonar-project.properties" 2>/dev/null | head -1)"
  org="$(sed -n 's/^sonar\.organization=//p' "$repo_root/sonar-project.properties" 2>/dev/null | head -1)"
  [[ -z "${SONAR_TOKEN:-}" || -z "$key" ]] && return 0

  say sonar "FAILED quality-gate conditions:"
  curl -sSf -H "Authorization: Bearer ${SONAR_TOKEN}" \
    "${host}/api/qualitygates/project_status?projectKey=${key}&organization=${org}" 2>/dev/null \
    | python3 -c 'import json,sys
try: ps=json.load(sys.stdin).get("projectStatus",{})
except Exception: sys.exit(0)
for c in ps.get("conditions",[]):
    if c.get("status")!="OK":
        print("  - %s: actual=%s threshold=%s" % (c.get("metricKey"),c.get("actualValue","?"),c.get("errorThreshold","?")))' 2>/dev/null || true

  say sonar "New-code security hotspots to review (SonarCloud UI → Security Hotspots):"
  curl -sSf -H "Authorization: Bearer ${SONAR_TOKEN}" \
    "${host}/api/hotspots/search?projectKey=${key}&organization=${org}&sinceLeakPeriod=true&ps=50" 2>/dev/null \
    | python3 -c 'import json,sys
try: d=json.load(sys.stdin)
except Exception: sys.exit(0)
for h in d.get("hotspots",[]):
    comp=h.get("component","").split(":")[-1]
    print("  - %s:%s  %s  %s" % (comp,h.get("line","?"),h.get("ruleKey",""),(h.get("message","") or "")[:90]))' 2>/dev/null || true

  printf '  Dashboard: %s/dashboard?id=%s\n' "$host" "$key" >&2
}

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

  # Worktree handling: in a git worktree, $repo_root/.git is a FILE containing
  # `gitdir: <abs-path>/.git/worktrees/<name>` — a host-absolute path. When the
  # scanner container only has $repo_root mounted, JGit (the scanner's SCM
  # probe) follows that pointer, cannot resolve it inside the container, and
  # blows up with `RepositoryNotFoundException: .git/worktrees/<name>`. Every
  # worker session was working around this with SKIP=sonar-scanner.
  #
  # Fix: mount the parent .git/ at the same absolute path the gitfile
  # references, so the pointer resolves identically inside the container. This
  # preserves full SCM data (vs. -Dsonar.scm.disabled=true which would drop
  # blame info and pollute SonarCloud history for the branch).
  worktree_mount_args=()
  git_dir="$(git rev-parse --git-dir 2>/dev/null || true)"
  git_common_dir="$(git rev-parse --git-common-dir 2>/dev/null || true)"
  if [[ -n "$git_dir" && -n "$git_common_dir" && "$git_dir" != "$git_common_dir" ]]; then
    # Resolve to absolute paths (git-common-dir is absolute from a worktree,
    # but normalize defensively in case future git versions change that).
    git_common_abs="$(cd "$git_common_dir" && pwd)"
    say sonar "worktree detected — mounting $git_common_abs into container so JGit can resolve gitfile pointer"
    # Mount the parent .git/ at its host-absolute path inside the container.
    # :ro because the scanner only reads SCM data, never writes.
    worktree_mount_args=(-v "$git_common_abs:$git_common_abs:ro")
  fi

  # NOTE: we deliberately do NOT pass -Dsonar.branch.name. SonarCloud branch
  # analysis is a paid feature; on this free-tier project the scanner only
  # supports the main branch (+ PR analysis in CI). Setting branch.name makes
  # the gate-status check fail with "Project not found". Locally the working
  # tree is therefore analyzed as the main branch, so the gate reflects main's
  # state — keep main green (review/fix its hotspots) and clean branches pass.

  # Retry the intermittent SonarCloud CE "Task finished abnormally" (server-side
  # processing flake, common with the emulated amd64 scanner on arm64 hosts):
  # that is infra, not a quality signal. A genuine QUALITY GATE STATUS: FAILED
  # is NOT retried — it is surfaced with actionable conditions/hotspots.
  local logf attempt=0 max=3 rc
  logf="$(mktemp -t sonar-scan.XXXXXX)"
  while :; do
    attempt=$((attempt + 1))
    docker run --rm \
      -e SONAR_TOKEN \
      -e SONAR_HOST_URL \
      -v "$repo_root:/usr/src" \
      ${worktree_mount_args[@]+"${worktree_mount_args[@]}"} \
      sonarsource/sonar-scanner-cli:latest \
      -Dsonar.qualitygate.wait=true 2>&1 | tee "$logf"
    rc=${PIPESTATUS[0]}
    [[ "$rc" -eq 0 ]] && break
    if grep -q 'CE Task finished abnormally' "$logf" && [[ "$attempt" -lt "$max" ]]; then
      say sonar "SonarCloud CE processing flaked (attempt ${attempt}/${max}) — retrying"
      continue
    fi
    rm -f "$logf"
    sonar_gate_diagnostics
    fail "sonar-scanner: SonarCloud quality gate failed (see conditions/hotspots above)"
  done
  rm -f "$logf"

  # Free-tier strict gate: the built-in "Sonar way" gate tolerates some new
  # issues (a custom new_issues=0 gate needs a paid plan we don't have), so
  # enforce zero new issues ourselves via the API. The scan above ran with
  # -Dsonar.qualitygate.wait=true so the analysis is ingested by now and the
  # gate queries the just-uploaded results. SONAR_TOKEN/SONAR_HOST_URL are
  # already resolved + exported above and reused by the child script.
  say sonar "enforcing zero new SonarCloud issues (new_violations=0)"
  bash "$repo_root/scripts/sonar-new-issues-gate.sh" \
    || fail "sonar: new SonarCloud issues introduced (new_violations>0)"
}

case "${1:-}" in
  fmt)       cmd_fmt ;;
  build-vet) cmd_build_vet ;;
  coverage)  cmd_coverage ;;
  sonar)     cmd_sonar ;;
  *) echo "usage: precommit-mandate.sh {fmt|build-vet|coverage|sonar}" >&2; exit 2 ;;
esac
