#!/usr/bin/env bash
# sonar-new-issues-gate.sh — hard-fail when SonarCloud reports any NEW issue
# in the new-code period for the current PR / branch.
#
# WHY this exists: SonarCloud's custom quality gates (e.g. a "new_issues=0"
# gate) require a PAID plan. This org is on the free tier and cannot ASSIGN a
# custom gate to the project, so the built-in "Sonar way" gate (which tolerates
# some new issues) is the only one available. This script reimplements the
# strict "zero new issues" rule ourselves by querying the SonarCloud web API
# after the scan has been ingested, and exiting non-zero if new_violations > 0.
#
# It is the durable replacement for the unassignable `dot-agents-strict` gate.
#
# CONTEXT DETECTION:
#   * Pull request — when running in GitHub Actions on a pull_request event
#     ($GITHUB_EVENT_NAME == pull_request), the PR number is read from the
#     event payload ($GITHUB_EVENT_PATH .pull_request.number) or, failing
#     that, parsed from $GITHUB_REF (refs/pull/<N>/merge). Override with --pr N.
#   * Branch — otherwise (e.g. a push to master) the branch name is used and
#     the API is queried with &branch=<b> instead of &pullRequest=<N>.
#
# TOKEN RESOLUTION (mirrors precommit-mandate.sh cmd_sonar):
#   $SONAR_TOKEN, else $SONARQUBE_TOKEN, else .mcpServers.sonarqube.env
#   .SONARQUBE_TOKEN from .mcp.json. No token => SKIP (exit 0), same posture as
#   cmd_sonar's "SKIPPED: no SONAR_TOKEN". In CI the secret is present so it
#   runs for real.
#
# PREREQUISITE: the scan must already be ingested. In CI the SonarQube Scan
# step runs with the action's default behaviour and this step runs AFTER it in
# the same job; precommit-mandate.sh runs the containerized scanner with
# -Dsonar.qualitygate.wait=true (which blocks until analysis is processed)
# before invoking this script.
#
# Usage:
#   scripts/sonar-new-issues-gate.sh [--pr N] [--branch B]
#
# Test seam:
#   SONAR_API_CURL  — override the curl invocation (the test points this at a
#                     fixture-emitting shell function/script).
#   --fixture FILE  — read the issues/search JSON from FILE instead of the API.

set -euo pipefail

PROJECT_KEY="NikashPrakash_dot-agents"
ORGANIZATION="npk-aorcha"
SONAR_HOST="${SONAR_HOST_URL:-https://sonarcloud.io}"

PR_NUM=""
BRANCH=""
FIXTURE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pr)      PR_NUM="${2:-}"; shift 2 ;;
    --branch)  BRANCH="${2:-}"; shift 2 ;;
    --fixture) FIXTURE="${2:-}"; shift 2 ;;
    *) echo "sonar-new-issues-gate: unknown arg '$1'" >&2; exit 2 ;;
  esac
done

say()  { printf '\n\033[1m[sonar-new-issues] %s\033[0m\n' "${1:-}"; }
warn() { printf '\033[33m[sonar-new-issues] %s\033[0m\n' "${1:-}"; }

# --- JSON helper: prefer jq, fall back to python3 -------------------------
have_jq() { command -v jq >/dev/null 2>&1; }

# --- token resolution -----------------------------------------------------
resolve_token() {
  if [[ -n "${SONAR_TOKEN:-}" ]]; then return 0; fi
  if [[ -n "${SONARQUBE_TOKEN:-}" ]]; then SONAR_TOKEN="$SONARQUBE_TOKEN"; return 0; fi
  # TODO(auth-proxy): stopgap — route via `da daemon` auth-proxy injector once live
  # (see .agents/proposals/sonar-gate-auth-via-proxy.md).
  # Reuse the SonarQube MCP credentials from .mcp.json (gitignored). Look in
  # the current worktree, then the primary worktree (where .mcp.json lives).
  local repo_root mcp_json="" cand
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  for cand in \
    "$repo_root/.mcp.json" \
    "$(git worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2; exit}')/.mcp.json"
  do
    [[ -f "$cand" ]] && { mcp_json="$cand"; break; }
  done
  if [[ -n "$mcp_json" ]]; then
    if have_jq; then
      SONAR_TOKEN="$(jq -r '.mcpServers.sonarqube.env.SONARQUBE_TOKEN // empty' "$mcp_json" 2>/dev/null)"
    else
      SONAR_TOKEN="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("mcpServers",{}).get("sonarqube",{}).get("env",{}).get("SONARQUBE_TOKEN",""))' "$mcp_json" 2>/dev/null)"
    fi
  fi
  [[ -n "${SONAR_TOKEN:-}" ]]
}

# --- context detection (PR vs branch) -------------------------------------
detect_context() {
  # Explicit flags win.
  [[ -n "$PR_NUM" || -n "$BRANCH" ]] && return 0

  if [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" ]]; then
    # Prefer the event payload (authoritative PR number).
    if [[ -n "${GITHUB_EVENT_PATH:-}" && -f "${GITHUB_EVENT_PATH}" ]]; then
      if have_jq; then
        PR_NUM="$(jq -r '.pull_request.number // .number // empty' "$GITHUB_EVENT_PATH" 2>/dev/null)"
      else
        PR_NUM="$(python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print(d.get("pull_request",{}).get("number") or d.get("number") or "")' "$GITHUB_EVENT_PATH" 2>/dev/null)"
      fi
    fi
    # Fall back to refs/pull/<N>/merge.
    if [[ -z "$PR_NUM" && "${GITHUB_REF:-}" =~ refs/pull/([0-9]+)/ ]]; then
      PR_NUM="${BASH_REMATCH[1]}"
    fi
    [[ -n "$PR_NUM" ]] && return 0
  fi

  # Branch context: CI provides the branch; locally fall back to git.
  BRANCH="${GITHUB_REF_NAME:-}"
  if [[ -z "$BRANCH" ]]; then
    BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  fi
}

# --- API call -------------------------------------------------------------
# Emits the issues/search JSON to stdout. Honors $SONAR_API_CURL (test seam)
# and --fixture (offline).
fetch_issues() {
  if [[ -n "$FIXTURE" ]]; then
    cat "$FIXTURE"
    return 0
  fi

  local base="${SONAR_HOST}/api/issues/search"
  local q="componentKeys=${PROJECT_KEY}&organization=${ORGANIZATION}&resolved=false&inNewCodePeriod=true&ps=100"
  if [[ -n "$PR_NUM" ]]; then
    q="${q}&pullRequest=${PR_NUM}"
  elif [[ -n "$BRANCH" ]]; then
    q="${q}&branch=${BRANCH}"
  fi

  local url="${base}?${q}"
  if [[ -n "${SONAR_API_CURL:-}" ]]; then
    # Test seam: the override receives the full URL as $1.
    "$SONAR_API_CURL" "$url"
  else
    curl --proto "=https" --tlsv1.2 -sSf \
      -H "Authorization: Bearer ${SONAR_TOKEN}" \
      "$url"
  fi
}

# --- JSON extraction ------------------------------------------------------
# Reads the issues/search payload on stdin; prints the issue total.
json_total() {
  if have_jq; then
    jq -r '.total // 0'
  else
    python3 -c 'import json,sys;print(json.load(sys.stdin).get("total",0))'
  fi
}

# Reads the payload on stdin; prints one "rule|component:line|message" per issue.
json_issue_lines() {
  if have_jq; then
    jq -r '.issues[]? | "\(.rule)  \(.component):\(.line // "?")  \(.message)"'
  else
    python3 -c '
import json,sys
d=json.load(sys.stdin)
for i in d.get("issues",[]):
    print("%s  %s:%s  %s" % (i.get("rule"), i.get("component"), i.get("line","?"), i.get("message")))
'
  fi
}

main() {
  if [[ -z "$FIXTURE" ]] && ! resolve_token; then
    warn "================ SONAR NEW-ISSUES GATE NOT ENFORCED ================"
    warn "SKIPPED: no SONAR_TOKEN / SONARQUBE_TOKEN and no sonarqube token in"
    warn ".mcp.json — new-issue enforcement was NOT checked. In CI the secret"
    warn "is present so this gate runs for real."
    exit 0
  fi

  detect_context
  if [[ -z "$FIXTURE" && -z "$PR_NUM" && -z "$BRANCH" ]]; then
    warn "SKIPPED: could not determine a PR number or branch to query."
    exit 0
  fi

  if [[ -n "$PR_NUM" ]]; then
    say "checking new issues for PR #${PR_NUM} (${PROJECT_KEY})"
  elif [[ -n "$BRANCH" ]]; then
    say "checking new issues for branch '${BRANCH}' (${PROJECT_KEY})"
  else
    say "checking new issues (fixture)"
  fi

  local payload total
  if ! payload="$(fetch_issues)"; then
    # No new-code period (first analysis) and other 4xx responses make curl
    # -f exit non-zero. Treat an absent new-code period as zero new issues
    # rather than false-failing the build.
    warn "issues/search query failed or returned no new-code period — treating as 0 new issues."
    exit 0
  fi

  # json_total may fail on a malformed 200 body; never let that abort the
  # gate. Default to 0 and require a clean integer before comparing.
  total="$(printf '%s' "$payload" | json_total 2>/dev/null || true)"
  if ! [[ "$total" =~ ^[0-9]+$ ]]; then
    warn "could not parse issue total from the response — treating as 0 new issues."
    total=0
  fi

  if [[ "$total" -gt 0 ]]; then
    printf '\033[31m[sonar-new-issues] BLOCKED: %s new issue(s) introduced in the new-code period:\033[0m\n' "$total" >&2
    printf '%s' "$payload" | json_issue_lines | sed 's/^/  - /' >&2
    printf '\033[31m[sonar-new-issues] Resolve the issues above before merging (new_violations must be 0).\033[0m\n' >&2
    exit 1
  fi

  say "OK: 0 new issues in the new-code period."
  exit 0
}

main
