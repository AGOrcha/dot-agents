#!/usr/bin/env bash
# Self-test for scripts/gate.sh — drives the REAL coverage-enforce logic via the
# `gate.sh enforce-coverage <profile>` subcommand against synthetic full-suite
# profiles. This entrypoint is the SAME function the production path calls on the
# real `go test` profile (enforce_coverage_profile), so the test exercises
# production logic WITHOUT a bypass: production always generates the real profile;
# the test just hands the enforce step a fixture. There is intentionally no env
# var that lets a caller skip/substitute coverage on the production `make gate`.
#
# Covers the two pass-local-fail-CI holes the full-enforce design must catch (a
# cross-harness review found these):
#   1. #173 replay        — a sub-100% production file FAILS the gate.
#   2. test-only weakening — a production file dropped below 100% by a *test-only*
#      change (the production .go itself did NOT change) still FAILS, because
#      full enforce checks every file, not just the changed set.
#   3. clean profile       — all files at 100% (or allowlisted) PASSES (the
#      mutation control: the gate is not vacuously failing).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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

# Fixture 1 — a production file at 5/9 = ~55.6% (well below 100%, not
# allowlisted). Mirrors #173: passed the old per-package hook, failed CI.
cat > "$tmp/cov-weak.out" <<'EOF'
mode: atomic
github.com/o/repo/internal/widget/widget.go:1.1,5.2 5 1
github.com/o/repo/internal/widget/widget.go:6.1,12.2 4 0
EOF

# Fixture 2 — same shape, framed as a TEST-ONLY weakening: the production file
# widget.go is unchanged in this branch; only widget_test.go was edited to stop
# covering the second block, dropping widget.go below 100%. A diff-scoped gate
# (changed *.go = {widget_test.go}) would SKIP widget.go and miss this. Full
# enforce must still FAIL. (Identical profile content; the distinction is which
# files "changed", which full enforce ignores by design.)
cp "$tmp/cov-weak.out" "$tmp/cov-testonly.out"

# Fixture 3 — clean: every production file is 100% covered.
cat > "$tmp/cov-clean.out" <<'EOF'
mode: atomic
github.com/o/repo/internal/widget/widget.go:1.1,5.2 5 1
github.com/o/repo/internal/widget/widget.go:6.1,12.2 4 1
github.com/o/repo/internal/widget/other.go:1.1,3.2 6 1
EOF

# Drive the production enforce logic directly via the subcommand. COVERAGE_EXCEPTIONS
# points at the empty fixture allowlist; everything else (enforce mode, threshold,
# per-package warn) is fixed inside gate.sh, exactly as production runs it.
enforce() {
  local profile="$1"  # S7679: bind positional to a named local before use
  COVERAGE_EXCEPTIONS="$tmp/exc.txt" bash "$gate" enforce-coverage "$profile" 2>&1
}

echo "== case 1: #173 replay (sub-100% production file) =="
set +e
out1="$(enforce "$tmp/cov-weak.out")"; rc1=$?
set -e
chk "$rc1" "1" "#173 replay: sub-100% file fails the enforce step"
grep -q "internal/widget/widget.go .* FAIL" <<<"$out1" \
  && echo "ok: weak widget.go reported FAIL" \
  || { echo "FAIL: widget.go not reported FAIL"; fail=1; }

echo "== case 2: test-only weakening (unchanged production file drops below 100%) =="
set +e
out2="$(enforce "$tmp/cov-testonly.out")"; rc2=$?
set -e
chk "$rc2" "1" "test-only weakening: full enforce still fails"
grep -q "internal/widget/widget.go .* FAIL" <<<"$out2" \
  && echo "ok: test-only-dropped file still caught" \
  || { echo "FAIL: test-only-dropped file not caught"; fail=1; }

echo "== case 3: clean profile (mutation control — must PASS) =="
set +e
out3="$(enforce "$tmp/cov-clean.out")"; rc3=$?
set -e
chk "$rc3" "0" "clean profile: enforce passes"
grep -q "coverage-gate: PASS" <<<"$out3" \
  && echo "ok: coverage-gate reports PASS" \
  || { echo "FAIL: coverage-gate did not report PASS"; fail=1; }

# Guard: confirm production `make gate` has NO env var that skips/substitutes
# coverage (the regressed bypass this refactor removes). grep gate.sh for the
# old knobs and any skip-shaped var on the production path.
echo "== case 4: no coverage-bypass knob on the production path =="
if grep -Eq 'GATE_SKIP_COVERAGE|GATE_COVERAGE_PROFILE' "$gate"; then
  echo "FAIL: gate.sh references a coverage-skip/substitute env var"; fail=1
else
  echo "ok: no GATE_SKIP_COVERAGE / GATE_COVERAGE_PROFILE in gate.sh"
fi

[[ $fail -eq 0 ]] && echo "gate.test: PASS" || { echo "gate.test: FAIL"; exit 1; }
