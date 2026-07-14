#!/usr/bin/env bash
# gate-cross.sh — the HEAVY, pre-merge cross-OS tier (companion to the fast
# `make gate`). This is what a maintainer runs before merging a PR to prove the
# changed packages pass on BOTH this OS and Windows with a TRUE multi-OS
# per-file coverage profile — the same shape CI's post-matrix coverage-gate job
# gates on, but run locally against the live Windows box so drift is caught
# before the PR ever reaches CI. Run it directly any time:
#
#     make gate-cross           # preferred
#     bash scripts/gate-cross.sh
#
# It is DETERMINISTIC and never mutates the local working tree (coverage
# profiles are temp files; the only push is to the REMOTE box's clone).
#
# WHAT IT DOES (heavy tier only — the fast build/vet/gofmt/full-enforce tier is
# `make gate`; native sonar is the sibling task t4, NOT here):
#   1. Derive the changed Go PACKAGES vs `git merge-base origin/master HEAD`
#      (the shared diff-base convention from gate.sh; FAILS LOUD if origin/master
#      is absent — never a silent HEAD~1 fallback).
#   2. LOCAL tier: run the changed-package tests with coverage on THIS OS.
#   3. WINDOWS tier: sync the current ref to pap-h@pap-home.local over BatchMode
#      key-only ssh, run the SAME changed-package tests with coverage on the box,
#      and pull the Windows profile back.
#   4. MERGE local + Windows into a true multi-OS per-file profile (gocovmerge —
#      the exact primitive CI uses) and run scripts/coverage-gate.sh over the
#      MERGED profile (COVERAGE_FILE_MODE=enforce, COVERAGE_PKG_MODE=warn, the
#      shared coverage-exceptions allowlist + threshold). This closes the one
#      residual gap the single-OS `make gate` cannot see: a NEW platform-only
#      file (e.g. a brand-new *_windows.go) that has zero coverage rows locally
#      is credited from its own OS run in the merged profile.
#
# --- SYNC MECHANISM: git (not rsync) -----------------------------------------
# We SYNC by pushing the current ref into a persistent clone on the box, then
# checking it out there — deliberately NOT rsync. The box keeps its own Go build
# cache and module cache warm across runs, so a git checkout of the same commit
# re-tests in seconds; an rsync of the worktree would either drag build
# artifacts around or blow the cache locality. Git also transfers only the delta
# objects the box is missing. The box clone path + host are env-overridable.
#
# --- OQ3: UNREACHABLE-BOX SEMANTICS (pending owner ratification) --------------
# DEFAULT = LOUD-SKIP. If the box does not answer a fast BatchMode ssh probe,
# gate-cross prints an unmistakable warning and SKIPS the cross-OS tier
# DETERMINISTICALLY with exit 0 — because CI remains the AUTHORITATIVE multi-OS
# merge gate (spec cg2): a developer without the box on their network must not be
# hard-blocked from a local pre-merge check that CI will re-run anyway.
# Set GATE_CROSS_STRICT=1 to HARD-FAIL (non-zero) on an unreachable box instead
# (CI / owner use, where the box MUST be reachable). This default is the proposed
# OQ3 resolution and is surfaced for owner ratification.
#
# --- COMMAND SEAMS (for scripts/gate-cross.test.sh; mirror gate.sh's
#     `enforce-coverage` subcommand) ------------------------------------------
# Env overrides (production defaults shown): PROBE_CMD, SSH_CMD, SYNC_CMD,
# COVMERGE_CMD, GATE_CROSS_LOCAL_PROFILE, GATE_CROSS_PKGS. Subcommands `covmerge` and
# `merge-enforce` expose the merge + per-file enforce logic so the unit tests
# drive the EXACT production path against synthetic profiles with no live box.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# Never let git's hook env (set when invoked from a pre-push hook) leak into the
# `go test` / `git` subprocesses — same defense gate.sh and the mandate apply.
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_PREFIX GIT_EXEC_PATH \
      GIT_REFLOG_ACTION GIT_AUTHOR_DATE GIT_COMMITTER_DATE 2>/dev/null || true

# Building inside a git worktree can trip VCS stamping (`exit status 128`);
# -buildvcs=false is safe here (the gate does not ship a versioned binary).
export GOFLAGS="${GOFLAGS:--buildvcs=false}"

say()  { printf '\n\033[1m[gate-cross:%s] %s\033[0m\n' "${1:-?}" "${2:-}"; }
fail() { printf '\n\033[31m[gate-cross] BLOCKED: %s\033[0m\n' "${1:-}" >&2; exit 1; }

# --- config (all env-overridable) --------------------------------------------
BOX="${GATE_CROSS_BOX:-pap-h@pap-home.local}"
# Path to the persistent clone on the box (relative to the box's login home, or
# absolute). Its warm Go build/module cache is the reason we git-sync not rsync.
BOX_REPO="${GATE_CROSS_BOX_REPO:-dot-agents}"
# Non-checked-out branch we push the ref to on the box, then check out remotely.
SYNC_BRANCH="${GATE_CROSS_SYNC_BRANCH:-gate-cross-sync}"
STRICT="${GATE_CROSS_STRICT:-0}"

# BatchMode = key-only, non-interactive; ConnectTimeout/ServerAlive keep an
# unreachable box a FAST, deterministic failure (never an interactive hang).
SSH_BASE="ssh -o BatchMode=yes -o ConnectTimeout=8 -o ServerAliveInterval=15 -o ServerAliveCountMax=4"
: "${PROBE_CMD:=$SSH_BASE $BOX true}"
: "${SSH_CMD:=$SSH_BASE $BOX}"

# loud_skip — the OQ3 default when the box is unreachable: an unmistakable banner
# then a deterministic exit 0 (CI is the authoritative multi-OS gate).
loud_skip() {
  {
    printf '\n\033[33m'
    printf '========================================================================\n'
    printf '  gate-cross: CROSS-OS BOX %s IS UNREACHABLE — SKIPPING WINDOWS TIER\n' "$BOX"
    printf '  The changed-package Windows run + merged multi-OS coverage were NOT\n'
    printf '  executed. CI is the AUTHORITATIVE multi-OS gate and WILL re-run them.\n'
    printf '  Re-run on a network with the box up, or set GATE_CROSS_STRICT=1 to\n'
    printf '  make an unreachable box a HARD failure instead of this loud-skip.\n'
    printf '========================================================================\n'
    printf '\033[0m\n'
  } >&2
}

# covmerge <profiles...> — merge coverage profiles into a true multi-OS profile
# on stdout. Defaults to gocovmerge (the EXACT tool + behavior CI's coverage-gate
# job uses: identical blocks have their hit counts summed, so a *_windows.go file
# 0% on the POSIX run is credited from the Windows run). Resolution order:
# COVMERGE_CMD override -> gocovmerge on PATH -> $GOPATH/bin/gocovmerge -> the
# pinned module via `go run` (same commit CI pins).
covmerge() {
  if [[ -n "${COVMERGE_CMD:-}" ]]; then
    # shellcheck disable=SC2086
    $COVMERGE_CMD "$@"
    return
  fi
  local bin=""
  if command -v gocovmerge >/dev/null 2>&1; then
    bin="$(command -v gocovmerge)"
  elif [[ -x "$(go env GOPATH)/bin/gocovmerge" ]]; then
    bin="$(go env GOPATH)/bin/gocovmerge"
  fi
  if [[ -n "$bin" ]]; then
    "$bin" "$@"
  else
    go run github.com/wadey/gocovmerge@b5bfa59ec0adc420475f97f89b58045c721d761c "$@"
  fi
}

# merge_enforce <local-profile> <windows-profile> — merge the two OS profiles and
# run the per-file coverage gate over the MERGED profile. This is the SAME
# invocation CI's coverage-gate job uses (per-file ENFORCE, per-package warn, the
# shared coverage-exceptions allowlist + threshold), so there is zero
# coverage-mode drift between this pre-merge tier and CI. Both the production run
# AND gate-cross.test.sh call THIS function — production passes the real `go test`
# profiles, the test passes fixtures — so the test exercises production logic.
merge_enforce() {
  local lp="$1" wp="$2" merged
  merged="$(mktemp -t gate-cross-merged.XXXXXX)"
  # shellcheck disable=SC2064
  trap "rm -f '$merged'" RETURN
  covmerge "$lp" "$wp" > "$merged" || fail "coverage merge failed (gocovmerge)"
  COVERAGE_FILE="$merged" \
  COVERAGE_THRESHOLD=95 \
  COVERAGE_PKG_MODE=warn \
  COVERAGE_FILE_MODE=enforce \
  COVERAGE_EXCEPTIONS="${COVERAGE_FLOOR_EXCEPTIONS:-scripts/coverage-floor-exceptions.txt}" \
    bash "$repo_root/scripts/coverage-gate.sh" \
    || fail "cross coverage floor: a changed-package file regressed below 95% (not floor-allowlisted)"

  say merge "merged local + Windows -> multi-OS per-file profile; enforcing >=${COVERAGE_THRESHOLD:-100}%"
  COVERAGE_FILE="$merged" \
  COVERAGE_THRESHOLD="${COVERAGE_THRESHOLD:-100}" \
  COVERAGE_PKG_MODE=warn \
  COVERAGE_FILE_MODE=enforce \
    bash "$repo_root/scripts/coverage-gate.sh" \
    || fail "cross coverage gate: a changed-package file is below ${COVERAGE_THRESHOLD:-100}% on the merged multi-OS profile (not allowlisted)"
}

# changed_packages — echo the unique, still-existing Go package dirs (as ./import
# paths) that changed vs the merge-base with origin/master. FAILS LOUD if
# origin/master is absent (never a silent HEAD~1 fallback), per the gate.sh
# diff-base convention.
changed_packages() {
  git rev-parse --verify --quiet origin/master >/dev/null \
    || fail "origin/master not found — fetch it first (refusing a silent HEAD~1 fallback)"
  local base
  base="$(git merge-base origin/master HEAD)" || fail "no merge-base with origin/master"
  local f d p seen=""
  while IFS= read -r f; do
    [[ -n "$f" ]] || continue
    d="$(dirname "$f")"
    [[ -d "$d" ]] || continue   # dir removed by a deletion — nothing to test
    if [[ "$d" == "." ]]; then p="."; else p="./$d"; fi
    case " $seen " in *" $p "*) continue ;; esac
    seen="$seen $p"
    printf '%s\n' "$p"
  done < <(git diff --name-only "$base" HEAD -- '*.go')
}

# local_profile <out> <pkgs...> — changed-package tests + coverage on THIS OS.
# GATE_CROSS_LOCAL_PROFILE injects a fixture profile for the unit tests.
local_profile() {
  local out="$1"; shift
  if [[ -n "${GATE_CROSS_LOCAL_PROFILE:-}" ]]; then
    cp "$GATE_CROSS_LOCAL_PROFILE" "$out"
    return
  fi
  go test -count=1 -timeout=300s -covermode=atomic -coverprofile="$out" "$@" \
    || fail "local changed-package go test failed (coverage profile not produced)"
}

# sync_ref — git-sync the current ref into the box's persistent clone (reuses its
# warm Go cache). SYNC_CMD overrides the whole step (the unit tests inject a
# no-op). We push HEAD to a NON-checked-out branch so receive.denyCurrentBranch
# never trips; the remote checkout happens inside the windows-run command.
sync_ref() {
  if [[ -n "${SYNC_CMD:-}" ]]; then
    # shellcheck disable=SC2086
    $SYNC_CMD || fail "SYNC_CMD failed (sync to $BOX)"
    return
  fi
  git push --force "$BOX:$BOX_REPO" "HEAD:refs/heads/$SYNC_BRANCH" \
    || fail "git sync push to $BOX:$BOX_REPO failed"
}

# windows_profile <out> <pkgs...> — sync the ref, run the SAME changed-package
# tests with coverage on the box over BatchMode ssh, and stream the Windows
# profile back to <out>. SSH_CMD is the overridable command seam (the unit tests
# inject a fake that emits a fixture Windows profile).
windows_profile() {
  local out="$1"; shift
  local pkgs="$*" remote
  sync_ref
  # Remote: check out the pushed ref in the box's warm clone, run the changed
  # packages with coverage to a temp file, cat it to stdout (captured locally),
  # then clean up. GOFLAGS mirrors the local buildvcs guard for the box's build.
  # Single-quoted ON PURPOSE (SC2016): $win / $(mktemp) must expand on the BOX,
  # not locally — only %q-substituted path/branch/pkgs are filled in here.
  # shellcheck disable=SC2016
  remote="$(printf 'set -e; export GOFLAGS=-buildvcs=false; cd %q; git checkout --force --quiet %q; win=$(mktemp); go test -count=1 -timeout=300s -covermode=atomic -coverprofile="$win" %s 1>&2; cat "$win"; rm -f "$win"' \
    "$BOX_REPO" "$SYNC_BRANCH" "$pkgs")"
  # shellcheck disable=SC2086
  $SSH_CMD "$remote" > "$out" || fail "Windows changed-package go test over ssh to $BOX failed"
}

# probe_box — fast, non-interactive reachability check. Exit 0 = reachable.
probe_box() {
  # shellcheck disable=SC2086
  $PROBE_CMD >/dev/null 2>&1
}

run_gate_cross() {
  # --- 1. changed packages ----------------------------------------------
  # GATE_CROSS_PKGS forces an explicit package list (space-separated), skipping
  # git derivation — used by the unit tests and available to a maintainer who
  # wants to cross-test a specific set.
  say diff "changed Go packages vs merge-base(origin/master, HEAD)"
  local pkgs=() p
  if [[ -n "${GATE_CROSS_PKGS:-}" ]]; then
    # shellcheck disable=SC2086
    for p in $GATE_CROSS_PKGS; do pkgs+=("$p"); done
  else
    while IFS= read -r p; do [[ -n "$p" ]] && pkgs+=("$p"); done < <(changed_packages)
  fi
  if [[ ${#pkgs[@]} -eq 0 ]]; then
    say ok "no changed Go packages vs origin/master — nothing to cross-test"
    return 0
  fi
  printf '  %s\n' "${pkgs[@]}"

  # --- 2. probe the cross-OS box (OQ3) ----------------------------------
  say probe "cross-OS box $BOX (BatchMode key-only, fast-fail)"
  if ! probe_box; then
    if [[ "$STRICT" == "1" ]]; then
      fail "GATE_CROSS_STRICT=1 and cross-OS box $BOX is unreachable — hard-fail"
    fi
    loud_skip
    return 0
  fi

  # --- 3. local + Windows changed-package coverage ----------------------
  local lp wp
  lp="$(mktemp -t gate-cross-local.XXXXXX)"
  wp="$(mktemp -t gate-cross-win.XXXXXX)"
  # shellcheck disable=SC2064
  trap "rm -f '$lp' '$wp'" RETURN
  say local "changed-package tests + coverage on $(uname -s)"
  local_profile "$lp" "${pkgs[@]}"
  say windows "sync ref + changed-package tests + coverage on $BOX"
  windows_profile "$wp" "${pkgs[@]}"

  # --- 4. merge + per-file enforce over the multi-OS profile ------------
  merge_enforce "$lp" "$wp"

  say ok "gate-cross PASS"
}

# Dispatch. Default (no args) runs the full cross-OS gate. `covmerge` and
# `merge-enforce` expose the merge + enforce logic for scripts/gate-cross.test.sh
# — they are the same functions production calls (production feeds real profiles).
case "${1:-}" in
  "")            run_gate_cross ;;
  covmerge)      shift; covmerge "$@" ;;
  merge-enforce) shift
                 [[ $# -eq 2 ]] || fail "merge-enforce needs <local-profile> <windows-profile>"
                 merge_enforce "$1" "$2" ;;
  *) echo "usage: gate-cross.sh [covmerge <profiles...> | merge-enforce <local> <windows>]" >&2; exit 2 ;;
esac
