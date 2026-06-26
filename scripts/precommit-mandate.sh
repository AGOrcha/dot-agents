#!/usr/bin/env bash
# precommit-mandate.sh — pre-push checks, dispatched by prek
# (see .pre-commit-config.yaml). Subcommands:
#
#   fmt         gofmt -l check (pre-commit stage)
#   gate        the fast merge-blocking mandate — delegates to `make gate`
#               (build + vet (POSIX + windows) + gofmt + per-file coverage
#               ENFORCE on changed files). This is the SINGLE SOURCE OF TRUTH;
#               build-vet/coverage below are thin back-compat shims that call it
#               so there is no parallel gate definition (spec D1).
#   build-vet   (deprecated shim) → `make gate`
#   coverage    (deprecated shim) → `make gate`
#   sonar       native (or containerized) SonarCloud analysis when a token is
#               present; loud actionable skip otherwise
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

# cmd_gate — the fast merge-blocking mandate. Single source of truth lives in
# scripts/gate.sh (`make gate`); this just delegates so the hook and any direct
# caller share ONE definition (spec D1).
cmd_gate() {
  say gate "make gate (build+vet+cross + per-file coverage of changed files)"
  make -C "$repo_root" gate || fail "make gate failed (see output above)"
}

# Deprecated back-compat shims: the build-vet and per-package coverage steps are
# now folded into `make gate`. Kept so anything still invoking these subcommands
# routes to the single definition instead of a divergent copy.
cmd_build_vet() { cmd_gate; }
cmd_coverage()  { cmd_gate; }

# _sonar_scan_native runs the native sonar-scanner CLI (no container) from the
# repo root, with the same SonarCloud-CE-flake retry as the containerized path.
# A genuine QUALITY GATE FAILED is surfaced (not retried). Native scans are
# independent JVM processes, so concurrent pre-push runs do not contend on one
# Docker daemon/container — which is what wedged parallel branch pushes.
_sonar_scan_native() {
  local logf attempt=0 max=3 rc
  logf="$(mktemp -t sonar-scan.XXXXXX)"
  # Keep the working tree pristine so prek does not abort the push with "files
  # were modified by this hook" even on a PASSED gate. Two sources of in-tree
  # churn the containerized path already guards against, but the native path did
  # not:
  #   1. A live git fsmonitor daemon (e.g. an editor's git integration keeps one
  #      alive) reports phantom tracked-file changes when the scanner walks the
  #      tree. Stop it and drop its socket first. fsmonitor is a perf cache (off
  #      by default here) and won't auto-restart mid-scan; .git SCM data is
  #      untouched.
  #   2. The scanner writes .scannerwork/ into repo_root; remove it afterwards.
  git fsmonitor--daemon stop >/dev/null 2>&1 || true
  rm -f "$repo_root/.git/fsmonitor--daemon.ipc" 2>/dev/null || true
  while :; do
    attempt=$((attempt + 1))
    ( cd "$repo_root" && sonar-scanner \
        -Dsonar.token="$SONAR_TOKEN" \
        -Dsonar.host.url="$SONAR_HOST_URL" \
        -Dsonar.qualitygate.wait=true ) 2>&1 | tee "$logf"
    rc=${PIPESTATUS[0]}
    [[ "$rc" -eq 0 ]] && break
    if grep -q 'CE Task finished abnormally' "$logf" && [[ "$attempt" -lt "$max" ]]; then
      say sonar "SonarCloud CE processing flaked (attempt ${attempt}/${max}) — retrying"
      continue
    fi
    rm -f "$logf"
    rm -rf "$repo_root/.scannerwork" 2>/dev/null || true
    sonar_gate_diagnostics
    fail "sonar-scanner: SonarCloud quality gate failed (see conditions/hotspots above)"
  done
  rm -f "$logf"
  rm -rf "$repo_root/.scannerwork" 2>/dev/null || true
}

cmd_sonar() {
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

  # Prefer a NATIVE sonar-scanner when present (parallel-push safe — see helper).
  # The pre-push hook can run with a minimal PATH, so add common user bin dirs
  # before probing, otherwise a brew/manual sonar-scanner is missed and we fall
  # back to the contended container.
  export PATH="/opt/homebrew/bin:/usr/local/bin:$HOME/.local/bin:$PATH"
  if command -v sonar-scanner >/dev/null 2>&1; then
    say sonar "native sonar-scanner (no container)"
    _sonar_scan_native
    say sonar "enforcing zero new SonarCloud issues (new_violations=0)"
    bash "$repo_root/scripts/sonar-new-issues-gate.sh" \
      || fail "sonar: new SonarCloud issues introduced (new_violations>0)"
    return 0
  fi
  if ! command -v docker >/dev/null 2>&1; then
    printf '\033[33m[mandate:sonar] SKIPPED: no native sonar-scanner and no docker.\n'
    printf '  brew install sonar-scanner (or install Docker) to enable the Sonar mandate.\033[0m\n'
    return 0
  fi
  say sonar "containerized sonar-scanner"

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

  # The git fsmonitor daemon keeps a Unix-domain socket at
  # .git/fsmonitor--daemon.ipc and churns it. The containerized scanner walks the
  # bind-mounted repo and stat()s that socket mid-walk, dying with
  # NoSuchFileException and aborting the scan (intermittent — only when a daemon is
  # live, e.g. an editor's git integration keeps one alive, which is why manual
  # `fsmonitor--daemon stop` "fixed" it). The socket cannot be bind-masked — runc
  # can't openat2 a socket as a mount target ("operation not supported") — so stop
  # the daemon and remove the socket before scanning. fsmonitor is a perf cache
  # (off by default in this repo) that won't auto-restart during the scan; .git SCM
  # data is untouched. Covers normal checkouts and the worktree common dir.
  git fsmonitor--daemon stop >/dev/null 2>&1 || true
  rm -f "$repo_root/.git/fsmonitor--daemon.ipc" 2>/dev/null || true
  if [[ -n "${git_common_abs:-}" ]]; then
    rm -f "$git_common_abs/fsmonitor--daemon.ipc" 2>/dev/null || true
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

# cmd_push — safe `git push` wrapper for this repo's slow pre-push hook.
#
# Two failure modes the bare `git push` hits and this guards against (see lesson
# .agents/lessons/ssh-keepalive-for-slow-pre-push-hook):
#
#   1. SSH idle-timeout drop. `git push` negotiates the SSH transport BEFORE
#      running the (multi-minute: build-vet + coverage + sonar) pre-push hook.
#      While the hook runs, the idle connection is closed by GitHub's server-side
#      timeout, so the pack send fails with "Connection ... closed by remote
#      host" AFTER the hooks print all-green. Force keepalives so the connection
#      survives the hook. Respect a caller-supplied GIT_SSH_COMMAND (append our
#      options) rather than clobbering it.
#
#   2. Silent non-landing. The exit code can surface as 0 when piped, masking the
#      drop, so the ref never lands. After a reportedly-successful push, VERIFY
#      the remote ref SHA equals local HEAD and fail loudly otherwise.
#
# Usage: precommit-mandate.sh push [<remote>] [<branch>] [-- <extra git push args>]
# Defaults: remote=origin, branch=current HEAD branch. Extra args after `--` are
# passed through to `git push` (e.g. --force-with-lease).
cmd_push() {
  local remote="origin" branch="" passthru=()
  # Parse leading positional remote/branch, then `--` passthrough.
  while [[ $# -gt 0 ]]; do
    local arg="$1"
    case "$arg" in
      --) shift; passthru=("$@"); break ;;
      -*) passthru+=("$arg"); shift ;;    # a flag with no positional remote/branch
      *)
        if [[ "$remote" == "origin" && -z "$branch" && "${_remote_set:-}" != 1 ]]; then
          remote="$arg"; _remote_set=1
        else
          branch="$arg"
        fi
        shift ;;
    esac
  done
  if [[ -z "$branch" ]]; then
    branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
    [[ -z "$branch" || "$branch" == "HEAD" ]] && fail "push: cannot determine current branch (detached HEAD?) — pass it explicitly"
  fi

  local local_head
  local_head="$(git rev-parse HEAD 2>/dev/null || true)"
  [[ -z "$local_head" ]] && fail "push: cannot resolve local HEAD"

  # Keepalive: send a probe every 15s during the long hook so GitHub never sees
  # the connection as idle; tolerate up to ~30 missed probes before giving up.
  # Append to any caller GIT_SSH_COMMAND so a custom ssh/identity is preserved.
  local ssh_keepalive="ssh -o ServerAliveInterval=15 -o ServerAliveCountMax=30 -o TCPKeepAlive=yes"
  if [[ -n "${GIT_SSH_COMMAND:-}" ]]; then
    GIT_SSH_COMMAND="${GIT_SSH_COMMAND} -o ServerAliveInterval=15 -o ServerAliveCountMax=30 -o TCPKeepAlive=yes"
  else
    GIT_SSH_COMMAND="$ssh_keepalive"
  fi
  export GIT_SSH_COMMAND

  say push "git push ${remote} ${branch} (SSH keepalive: ServerAliveInterval=15)"
  git push "${remote}" "HEAD:${branch}" ${passthru[@]+"${passthru[@]}"} \
    || fail "push: git push failed (see output above)"

  # Ref-land verification: a green hook + exit 0 is NOT proof the pack landed —
  # an idle-dropped connection can still surface as 0 when piped. Confirm the
  # remote ref now points at our local HEAD.
  say push "verifying ref landed on ${remote} (ls-remote SHA == local HEAD)"
  local remote_sha
  remote_sha="$(git ls-remote "${remote}" "refs/heads/${branch}" 2>/dev/null | awk '{print $1}' | head -1)"
  if [[ -z "$remote_sha" ]]; then
    fail "push: ref refs/heads/${branch} not found on ${remote} after push — it did NOT land (likely an SSH idle-drop; re-run, the keepalive is already set)"
  fi
  if [[ "$remote_sha" != "$local_head" ]]; then
    fail "push: ${remote}/${branch} is at ${remote_sha} but local HEAD is ${local_head} — the push did NOT land your commit (idle-drop or stale lease); re-run the push"
  fi
  say push "OK: ${remote}/${branch} landed at ${local_head}"
}

case "${1:-}" in
  fmt)       cmd_fmt ;;
  gate)      cmd_gate ;;
  build-vet) cmd_build_vet ;;
  coverage)  cmd_coverage ;;
  sonar)     cmd_sonar ;;
  push)      shift; cmd_push "$@" ;;
  *) echo "usage: precommit-mandate.sh {fmt|gate|build-vet|coverage|sonar|push}" >&2; exit 2 ;;
esac
