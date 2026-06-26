#!/usr/bin/env bash
# Self-test for scripts/gate.sh — drives the REAL gate coverage code path via
# the GATE_COVERAGE_PROFILE seam against synthetic full-suite profiles, proving
# `make gate` enforces per-file >=95% over the WHOLE profile (CI parity), not a
# diff-scoped subset. Covers the two pass-local-fail-CI holes the diff-scoping
# pivot closes (a cross-harness review found these):
#
#   1. #173 replay        — a sub-95% production file FAILS the gate.
#   2. test-only weakening — a production file dropped <95% by a *test-only*
#      change (the production .go itself did NOT change) still FAILS, because
#      full enforce checks every file, not just the changed set.
#   3. clean profile       — all files >=95% (or allowlisted) PASSES (the
#      mutation control: the gate is not vacuously failing).
#
# build/vet/gofmt run first in gate.sh and are fast on this repo; the seam skips
# the multi-minute `go test`, so this stays a tight unit test of the gate's
# coverage enforcement.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/.." && pwd)"
gate="$here/gate.sh"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

fail=0
chk() {
  local got="$1" want="$2" desc="$3"
  if [[ "$got" != "$want" ]]; then echo "FAIL: $desc (got '$got' want '$want')"; fail=1
  else echo "ok: $desc"; fi
}

# Empty exceptions allowlist so the fixtures are judged purely on coverage
# (the real allowlist is exercised by coverage-gate.test.sh).
: > "$tmp/exc.txt"

# Fixture 1 — a production file at 5/9 = ~55.6% (well below 95%, not
# allowlisted). Mirrors #173: passed the old per-package hook, failed CI.
cat > "$tmp/cov-weak.out" <<'EOF'
mode: atomic
github.com/o/repo/internal/widget/widget.go:1.1,5.2 5 1
github.com/o/repo/internal/widget/widget.go:6.1,12.2 4 0
EOF

# Fixture 2 — same shape, framed as a TEST-ONLY weakening: the production file
# widget.go is unchanged in this branch; only widget_test.go was edited to stop
# covering the second block, dropping widget.go below 95%. A diff-scoped gate
# (changed *.go = {widget_test.go}) would SKIP widget.go and miss this. Full
# enforce must still FAIL. (Identical profile content; the distinction is which
# files "changed", which full enforce ignores by design.)
cp "$tmp/cov-weak.out" "$tmp/cov-testonly.out"

# Fixture 3 — clean: every production file >=95%.
cat > "$tmp/cov-clean.out" <<'EOF'
mode: atomic
github.com/o/repo/internal/widget/widget.go:1.1,5.2 5 1
github.com/o/repo/internal/widget/widget.go:6.1,12.2 4 1
github.com/o/repo/internal/widget/other.go:1.1,3.2 6 1
EOF

run_gate() {  # $1 = profile fixture
  ( cd "$repo_root" && \
    GATE_COVERAGE_PROFILE="$1" COVERAGE_EXCEPTIONS="$tmp/exc.txt" \
    bash "$gate" 2>&1 )
}

echo "== case 1: #173 replay (sub-95% production file) =="
set +e
out1="$(run_gate "$tmp/cov-weak.out")"; rc1=$?
set -e
chk "$rc1" "1" "#173 replay: sub-95% file fails make gate"
grep -q "internal/widget/widget.go .* FAIL" <<<"$out1" \
  && echo "ok: weak widget.go reported FAIL" \
  || { echo "FAIL: widget.go not reported FAIL"; fail=1; }

echo "== case 2: test-only weakening (unchanged production file drops <95%) =="
set +e
out2="$(run_gate "$tmp/cov-testonly.out")"; rc2=$?
set -e
chk "$rc2" "1" "test-only weakening: full enforce still fails the gate"
grep -q "internal/widget/widget.go .* FAIL" <<<"$out2" \
  && echo "ok: test-only-dropped file still caught" \
  || { echo "FAIL: test-only-dropped file not caught"; fail=1; }

echo "== case 3: clean profile (mutation control — gate must PASS) =="
set +e
out3="$(run_gate "$tmp/cov-clean.out")"; rc3=$?
set -e
chk "$rc3" "0" "clean profile: make gate passes"
grep -q "gate PASS" <<<"$out3" \
  && echo "ok: gate reports PASS" \
  || { echo "FAIL: gate did not report PASS"; fail=1; }

[[ $fail -eq 0 ]] && echo "gate.test: PASS" || { echo "gate.test: FAIL"; exit 1; }
