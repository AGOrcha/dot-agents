#!/usr/bin/env bash
# pr-gate-wait.sh — watch a PR's checks to completion; on failure, auto-fetch
# the failing job log so you don't have to chase it by hand.
#
# WHY this exists: the standard "wait for the PR gate" pattern is `gh pr checks
# <pr> --watch`, but on a red gate that leaves you to manually find the failing
# run and re-run `gh run view --log-failed`. This wraps both steps so a single
# command both waits and, on failure, dumps the failed-step logs. It replaces
# the ad-hoc poll loops scattered across sessions.
#
# Usage:
#   scripts/pr-gate-wait.sh <pr-number> [--repo OWNER/REPO] [--interval SECS]
#
# Arguments:
#   <pr-number>        PR number to watch (required).
#   --repo OWNER/REPO  Target repo (default: gh's current-directory repo).
#   --interval SECS    Poll interval passed to `gh pr checks --watch` (default 15).
#
# Exit status:
#   0  all checks passed.
#   1  one or more checks failed (failing logs are printed first).
#   2  usage / precondition error.

set -euo pipefail

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-2}"
}

PR=""
REPO=""
INTERVAL=15

while [ $# -gt 0 ]; do
  case "$1" in
    -h|--help) usage 0 ;;
    --repo)
      [ $# -ge 2 ] || { echo "error: --repo needs a value" >&2; usage 2; }
      REPO="$2"; shift 2 ;;
    --interval)
      [ $# -ge 2 ] || { echo "error: --interval needs a value" >&2; usage 2; }
      INTERVAL="$2"; shift 2 ;;
    --) shift; break ;;
    -*) echo "error: unknown option: $1" >&2; usage 2 ;;
    *)
      if [ -z "$PR" ]; then PR="$1"; shift
      else echo "error: unexpected argument: $1" >&2; usage 2; fi ;;
  esac
done

[ -n "$PR" ] || { echo "error: PR number is required" >&2; usage 2; }
command -v gh >/dev/null 2>&1 || { echo "error: gh CLI not found on PATH" >&2; exit 2; }

# Build the shared --repo flag once (empty array when not given).
repo_args=()
[ -n "$REPO" ] && repo_args=(--repo "$REPO")

echo "Watching checks for PR #${PR}${REPO:+ (${REPO})}..." >&2

# `gh pr checks --watch` blocks until all checks complete and exits non-zero
# when any check failed. Capture that without tripping `set -e`.
gate_rc=0
gh pr checks "$PR" "${repo_args[@]}" --watch --interval "$INTERVAL" || gate_rc=$?

if [ "$gate_rc" -eq 0 ]; then
  echo "All checks passed for PR #${PR}." >&2
  exit 0
fi

echo "" >&2
echo "PR #${PR} gate failed (rc=${gate_rc}). Fetching failing job logs..." >&2

# Resolve the head SHA of the PR, then the most recent workflow run for it, and
# dump only the failed steps. Best-effort: if any lookup fails we still surface
# the original failure so the caller can investigate manually.
head_sha="$(gh pr view "$PR" "${repo_args[@]}" --json headRefOid \
  --jq '.headRefOid' 2>/dev/null || true)"

run_id=""
if [ -n "$head_sha" ]; then
  run_id="$(gh run list "${repo_args[@]}" --commit "$head_sha" \
    --json databaseId,status,conclusion \
    --jq 'map(select(.conclusion=="failure")) | .[0].databaseId' \
    2>/dev/null || true)"
fi

if [ -n "$run_id" ] && [ "$run_id" != "null" ]; then
  echo "--- failing run ${run_id} (--log-failed) ---" >&2
  gh run view "$run_id" "${repo_args[@]}" --log-failed || true
else
  echo "Could not auto-resolve the failing run; inspect manually with:" >&2
  echo "  gh pr checks ${PR}${REPO:+ --repo ${REPO}}" >&2
fi

exit 1
