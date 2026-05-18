#!/usr/bin/env bash
# coverage-gate.sh — enforce minimum Go statement coverage.
#
# Two gates over one coverage profile:
#   * PER-PACKAGE (legacy, default enforce) — package avg >= threshold.
#   * PER-FILE    (new, default warn)        — every file >= threshold,
#     unless pattern-excluded or in the rationale'd exceptions allowlist.
#
# Per-file is sound ONLY on a profile that already merges every OS's
# coverage (platform build-tagged files like *_windows.go are 0% on the
# other OS's run). CI feeds the merged multi-OS profile; per-package
# aggregation hid this, per-file would false-fail on a single-OS profile.
#
# Usage:
#   COVERAGE_FILE=coverage.out scripts/coverage-gate.sh
#
# Env vars:
#   COVERAGE_FILE        profile path (default: coverage.out)
#   COVERAGE_THRESHOLD   minimum % (default: 95)
#   COVERAGE_EXCLUDE     regex of paths to skip (package- and file-level)
#   COVERAGE_PKG_MODE    enforce|warn|off  (default: enforce)
#   COVERAGE_FILE_MODE   enforce|warn|off  (default: warn  — Phase 1)
#   COVERAGE_EXCEPTIONS  allowlist path (default: scripts/coverage-exceptions.txt)
#
# Project mandate: every file exhaustively tested. A file below threshold
# must either be brought up, or added to the exceptions allowlist with a
# one-line rationale (unlisted + sub-threshold = per-file violation).

set -euo pipefail

COVERAGE_FILE="${COVERAGE_FILE:-coverage.out}"
THRESHOLD="${COVERAGE_THRESHOLD:-95}"
PKG_MODE="${COVERAGE_PKG_MODE:-enforce}"
FILE_MODE="${COVERAGE_FILE_MODE:-warn}"
EXCEPTIONS_FILE="${COVERAGE_EXCEPTIONS:-scripts/coverage-exceptions.txt}"

# Default exclusions (apply to BOTH gates):
# - cmd/* — main entrypoints; no business logic.
# - internal/storetest — graph-store integration harness; covered via KG.
# - internal/testutil, internal/linktest — *testing.T-wrapping scaffolding;
#   foreign-T failure branches are unreachable from a Go test.
# - internal/scaffold/{home,hooks,templates} — embed.FS copy helpers whose
#   residual uncovered lines are unreachable defensive embed-read errors.
# - scripts/cov*.go — //go:build ignore dev tools, never compiled normally.
# - vendor/.
# Matched against the REPO-RELATIVE path (module prefix stripped), as
# path-segment patterns (not $-anchored package names).
EXCLUDE_RE="${COVERAGE_EXCLUDE:-(^|/)(cmd/[^/]+/|internal/storetest/|internal/testutil/|internal/linktest/|internal/scaffold/(home|hooks|templates)/|vendor/)|(^|/)scripts/cov[a-z]*\.go$}"

if [[ ! -f "$COVERAGE_FILE" ]]; then
  echo "coverage-gate: $COVERAGE_FILE not found" >&2
  exit 1
fi

# --- exceptions allowlist (rationale mandatory) ----------------------------
# Format, one per line: <file-path><whitespace># <rationale>
# Blank lines and whole-line # comments ignored. A listed path with no
# '# rationale' is a HARD error (rationale is not optional).
ALLOW_TMP="$(mktemp)"
trap 'rm -f "$ALLOW_TMP"' EXIT
if [[ -f "$EXCEPTIONS_FILE" ]]; then
  lineno=0
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    lineno=$((lineno + 1))
    line="${raw%$'\r'}"
    [[ -z "${line//[[:space:]]/}" ]] && continue
    trimmed="${line#"${line%%[![:space:]]*}"}"
    [[ "$trimmed" == \#* ]] && continue
    if [[ "$line" != *"# "* ]]; then
      echo "coverage-gate: $EXCEPTIONS_FILE:$lineno has no '# <rationale>' — rationale is mandatory" >&2
      exit 1
    fi
    path="${line%%#*}"
    path="${path#"${path%%[![:space:]]*}"}"
    path="${path%"${path##*[![:space:]]}"}"
    [[ -z "$path" ]] && continue
    printf '%s\n' "$path" >> "$ALLOW_TMP"
  done < "$EXCEPTIONS_FILE"
fi

awk -v threshold="$THRESHOLD" -v exclude_re="$EXCLUDE_RE" \
    -v pkg_mode="$PKG_MODE" -v file_mode="$FILE_MODE" \
    -v allowfile="$ALLOW_TMP" '
function below(p) { return (p + 0 < (threshold + 0) - 0.05) }   # 0.05pp tol
BEGIN {
  while ((getline ln < allowfile) > 0) if (ln != "") allowed[ln] = 1
  close(allowfile)
}
NR > 1 {
  # $1 = path/file.go:sLine.col,eLine.col   $2 = numStmts   $3 = count
  split($1, parts, ":"); file = parts[1]
  m = split(file, segs, "/"); if (m < 2) next
  # Normalize to repo-relative: strip a leading module prefix
  # (domain/org/repo) so excludes + allowlist use repo-relative paths.
  start = 1
  if (m >= 4 && segs[1] ~ /\./) start = 4
  rel = segs[start]; for (i = start + 1; i <= m; i++) rel = rel "/" segs[i]
  if (exclude_re != "" && rel ~ exclude_re) next
  pkg = segs[start]; for (i = start + 1; i < m; i++) pkg = pkg "/" segs[i]

  pstmts[pkg] += $2 + 0; if ($3 + 0 > 0) pcov[pkg] += $2 + 0
  if (!(pkg in pseen)) { pseen[pkg]=1; porder[++pn]=pkg }
  fstmts[rel] += $2 + 0; if ($3 + 0 > 0) fcov[rel] += $2 + 0
  if (!(rel in fseen)) { fseen[rel]=1; forder[++fn]=rel }
}
END {
  rc = 0

  if (pkg_mode != "off") {
    printf "== PER-PACKAGE (%s) ==\n", pkg_mode
    pf = 0
    for (i = 1; i <= pn; i++) { p = porder[i]
      if (pstmts[p] == 0) continue
      pct = pcov[p]/pstmts[p]*100; st = "ok"
      if (below(pct)) { st = "FAIL"; pf = 1 }
      printf "  %-66s %7.2f%%  %s\n", p, pct, st
    }
    if (pf && pkg_mode == "enforce") rc = 1
    if (pf && pkg_mode == "warn") printf "  (warn: package(s) below %s%%)\n", threshold
  }

  if (file_mode != "off") {
    printf "\n== PER-FILE (%s, threshold %s%%) ==\n", file_mode, threshold
    ff = 0; nlist = 0; stale = 0
    for (i = 1; i <= fn; i++) { f = forder[i]
      if (fstmts[f] == 0) continue
      pct = fcov[f]/fstmts[f]*100
      if (below(pct)) {
        if (f in allowed) { printf "  %-66s %7.2f%%  ALLOWLISTED\n", f, pct }
        else { printf "  %-66s %7.2f%%  FAIL\n", f, pct; ff = 1; nlist++ }
      } else if (f in allowed) {
        printf "  %-66s %7.2f%%  STALE-ALLOWLIST (>=thr, remove entry)\n", f, pct; stale = 1
      }
    }
    if (nlist == 0) printf "  all non-excluded files >= %s%% (or allowlisted)\n", threshold
    else printf "  %d file(s) below %s%% and NOT allowlisted\n", nlist, threshold
    if (stale) printf "  note: stale allowlist entries above should be pruned\n"
    if (ff && file_mode == "enforce") rc = 1
    if (ff && file_mode == "warn") printf "  (warn-only: not failing the build in this phase)\n"
  }

  if (rc) { printf "\ncoverage-gate: FAIL\n"; exit 1 }
  printf "\ncoverage-gate: PASS\n"
}
' "$COVERAGE_FILE"
