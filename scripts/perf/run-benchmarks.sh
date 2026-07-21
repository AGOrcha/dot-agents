#!/bin/bash
# scripts/perf/run-benchmarks.sh — package-artifact-install t9 regression
# guard. Runs the full Go benchmark suite for the perf-sensitive `da`
# command-chain flows (two-pass resolution, shared-target projection, pass-2
# packages hydrate, the .agentsrc.lock read-modify-write) and writes a
# timestamped report under scripts/perf/reports/, so a later run can be
# diffed against a prior one to catch a silent regression.
#
# Usage:
#   scripts/perf/run-benchmarks.sh                # default (moderate) run
#   scripts/perf/run-benchmarks.sh -benchtime=50x  # any extra `go test` flags
#
# The benchmarks themselves live beside the code they measure:
#   internal/config/resolver_bench_test.go        — pass-1 (extends/layers)
#   internal/config/ensure_resolved_bench_test.go — §7A.5 auto-sync dispatch
#   internal/platform/resource_plan_bench_test.go — shared-target projection
#   commands/internal/lifecycle/install_bench_test.go — pass-2 packages hydrate
#   commands/workflow/perf_bench_test.go              — workflow git hot paths + state-ref CAS (git-spawn counted)
#   internal/graphstore/graphstore_bench_test.go      — KG impact/stats/search/edges @5k
#   internal/dashboard/store/store_bench_test.go      — dashboard projection (sessions() rebuild)
#   internal/adapters/builtin/crg/postprocess_bench_test.go — CRG derived views
#   internal/platform/pipeline_projection_bench_test.go + resources_bench_test.go — pipeline emit + resource scan
#   internal/config/agentsrc_bench_test.go            — manifest load/generate
#
# See docs/PERF_BUDGET.md for the documented baseline numbers and the
# rationale behind each optimization this suite guards against regressing.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPORT_DIR="${REPO_ROOT}/scripts/perf/reports"
mkdir -p "${REPORT_DIR}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
REPORT="${REPORT_DIR}/bench-${STAMP}.txt"

BENCH_PACKAGES=(
  ./internal/config/...
  ./internal/platform/...
  ./commands/internal/lifecycle/...
  ./commands/workflow/...
  ./internal/graphstore/...
  ./internal/dashboard/store/...
  ./internal/adapters/builtin/crg/...
)

echo "package-artifact-install t9 perf regression guard — $(date -u)" | tee "${REPORT}"
echo "commit: $(git -C "${REPO_ROOT}" rev-parse --short HEAD 2>/dev/null || echo unknown)" | tee -a "${REPORT}"
echo "---" | tee -a "${REPORT}"

cd "${REPO_ROOT}"
for pkg in "${BENCH_PACKAGES[@]}"; do
  echo "" | tee -a "${REPORT}"
  echo "=== ${pkg} ===" | tee -a "${REPORT}"
  # shellcheck disable=SC2068
  go test "${pkg}" -run xxx -bench . -benchmem "$@" 2>&1 | tee -a "${REPORT}"
done

echo "" | tee -a "${REPORT}"
echo "Report written to ${REPORT}" | tee -a "${REPORT}"
echo "Compare against a prior report with: benchstat <old> <new>  (go install golang.org/x/perf/cmd/benchstat@latest)"
