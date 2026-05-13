#!/usr/bin/env bash
# coverage-gate.sh — enforce per-package minimum statement coverage.
#
# Reads a Go coverage profile (default: coverage.out) and fails if any package's
# statement coverage falls below the threshold (default: 95%).
#
# Usage:
#   COVERAGE_FILE=coverage.out COVERAGE_THRESHOLD=95 scripts/coverage-gate.sh
#
# Env vars:
#   COVERAGE_FILE       path to go test -coverprofile output (default: coverage.out)
#   COVERAGE_THRESHOLD  minimum per-package coverage in % (default: 95)
#   COVERAGE_EXCLUDE    regex of package paths to skip (default skips
#                       generated, vendor, internal/storetest, and main entrypoints)
#
# Project mandate: every package must be exhaustively tested. New packages
# without tests will fail this gate. To exclude a package legitimately
# (e.g. pure entrypoint with no logic), add it to COVERAGE_EXCLUDE.

set -euo pipefail

COVERAGE_FILE="${COVERAGE_FILE:-coverage.out}"
THRESHOLD="${COVERAGE_THRESHOLD:-95}"
#
# Default exclusions:
# - cmd/* — main entrypoints; no business logic worth covering.
# - internal/storetest — graph-store integration harness; coverage measured
#   indirectly through KG suite.
# - vendor/ — third-party.
# - internal/testutil — test-scaffolding helpers that wrap *testing.T;
#   failure branches call t.Fatal on the caller's T and cannot be exercised
#   from a Go test (Go does not permit constructing or recovering from a
#   foreign *testing.T). Coverage is exercised through downstream callers.
# - internal/scaffold/{home,hooks,templates} — embed.FS copy helpers whose
#   remaining uncovered statements are defensive `if err != nil` returns on
#   embedded-fs reads/walks. The embed FS is compile-time validated, so
#   these branches are unreachable from any unit test.
EXCLUDE_RE="${COVERAGE_EXCLUDE:-^(github.com/[^/]+/[^/]+/cmd/[^/]+|.*/internal/storetest|.*/internal/testutil|.*/internal/scaffold/(home|hooks|templates)|.*/vendor/.*)$}"

if [ ! -f "$COVERAGE_FILE" ]; then
  echo "coverage-gate: $COVERAGE_FILE not found" >&2
  exit 1
fi

# Parse coverage profile directly. Lines have the form:
#   path/file.go:startLine.col,endLine.col numStmts count
# Aggregate numStmts (denominator) and count>0 numStmts (numerator) per package
# (= dirname of the file path).

awk -v threshold="$THRESHOLD" -v exclude_re="$EXCLUDE_RE" '
NR > 1 {
  # $1 = path/file.go:startLine.col,endLine.col
  # $2 = numStmts
  # $3 = count
  n = split($1, parts, ":")
  file = parts[1]
  # Package = dirname of file
  m = split(file, segs, "/")
  if (m < 2) next
  pkg = segs[1]
  for (i = 2; i < m; i++) pkg = pkg "/" segs[i]

  if (exclude_re != "" && pkg ~ exclude_re) next

  stmts[pkg] += $2 + 0
  if ($3 + 0 > 0) covered[pkg] += $2 + 0
  if (!(pkg in seen)) {
    seen[pkg] = 1
    order[++norder] = pkg
  }
}
END {
  fail = 0
  # Header
  printf "%-72s %8s  %s\n", "PACKAGE", "COV", "STATUS"
  printf "%-72s %8s  %s\n", "-------", "---", "------"
  for (i = 1; i <= norder; i++) {
    p = order[i]
    if (stmts[p] == 0) continue
    pct = (covered[p] / stmts[p]) * 100
    status = "ok"
    if (pct + 0 < threshold + 0) {
      status = "FAIL (need " threshold "%)"
      fail = 1
    }
    printf "%-72s %7.2f%%  %s\n", p, pct, status
  }
  if (fail) {
    printf "\ncoverage-gate: at least one package is below %s%% threshold\n", threshold
    exit 1
  }
  printf "\ncoverage-gate: all packages >= %s%% threshold\n", threshold
}
' "$COVERAGE_FILE"
