#!/usr/bin/env bash
# Self-test for coverage-gate.sh — synthetic merged profile exercising:
# weak file (FAIL), allowlisted file, pattern-excluded cmd/*, a platform
# build-tagged file credited (as it would be on a MERGED multi-OS
# profile), rationale-mandatory allowlist, and warn vs enforce modes.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
gate="$here/coverage-gate.sh"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/cov.out" <<'EOF'
mode: atomic
github.com/o/repo/commands/good.go:1.1,3.2 5 1
github.com/o/repo/commands/weak.go:1.1,10.2 10 1
github.com/o/repo/commands/weak.go:11.1,20.2 10 0
github.com/o/repo/commands/allowme.go:1.1,4.2 8 0
github.com/o/repo/cmd/repo/main.go:1.1,2.2 4 0
github.com/o/repo/internal/links/inode_windows.go:1.1,5.2 6 1
EOF
printf '%s\n' "# hdr" "commands/allowme.go    # untestable defensive branch" > "$tmp/exc.txt"

fail=0
chk() {
  local got="$1" want="$2" desc="$3"
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $desc (got '$got' want '$want')"; fail=1
  else
    echo "ok: $desc"
  fi
}

set +e
out="$(COVERAGE_FILE=$tmp/cov.out COVERAGE_EXCEPTIONS=$tmp/exc.txt \
       COVERAGE_PKG_MODE=off COVERAGE_FILE_MODE=enforce bash "$gate" 2>&1)"; rc=$?
set -e
chk "$rc" "1" "enforce: weak.go fails build"
grep -q "commands/weak.go .* FAIL"        <<<"$out" && echo "ok: weak FAIL"        || { echo "FAIL: weak not FAIL"; fail=1; }
grep -q "commands/allowme.go .* ALLOWLISTED" <<<"$out" && echo "ok: allowlisted"   || { echo "FAIL: allowme not ALLOWLISTED"; fail=1; }
grep -q "cmd/repo/main.go" <<<"$out" && { echo "FAIL: cmd not excluded"; fail=1; } || echo "ok: cmd excluded"
grep -q "inode_windows.go" <<<"$out" && { echo "FAIL: platform file not credited"; fail=1; } || echo "ok: platform file credited(merged)"

set +e
out2="$(COVERAGE_FILE=$tmp/cov.out COVERAGE_EXCEPTIONS=$tmp/exc.txt \
        COVERAGE_PKG_MODE=off COVERAGE_FILE_MODE=warn bash "$gate" 2>&1)"; rc2=$?
set -e
chk "$rc2" "0" "warn: sub-threshold does not fail build"

printf '%s\n' "commands/x.go" > "$tmp/bad.txt"
set +e
COVERAGE_FILE=$tmp/cov.out COVERAGE_EXCEPTIONS=$tmp/bad.txt bash "$gate" >/dev/null 2>&1; rc3=$?
set -e
chk "$rc3" "1" "allowlist entry without rationale is a hard error"

# ── COVERAGE_INCLUDE_FILES scoping (what `make gate` uses) ────────────────
# Scoped to the weak file → enforce FAILS on it (the #173 shape: a changed
# sub-95% file must fail the gate).
set +e
out4="$(COVERAGE_FILE=$tmp/cov.out COVERAGE_EXCEPTIONS=$tmp/exc.txt \
        COVERAGE_PKG_MODE=off COVERAGE_FILE_MODE=enforce \
        COVERAGE_INCLUDE_FILES='commands/weak.go' bash "$gate" 2>&1)"; rc4=$?
set -e
chk "$rc4" "1" "scoped-to-weak: changed sub-95% file fails enforce"
grep -q "scoped to changed files" <<<"$out4" && echo "ok: scoped header shown" || { echo "FAIL: no scoped header"; fail=1; }
grep -q "commands/weak.go .* FAIL" <<<"$out4" && echo "ok: scoped weak FAIL" || { echo "FAIL: scoped weak not FAIL"; fail=1; }

# Scoped to only the good file → weak.go is out of scope, so enforce PASSES
# (proves scoping narrows enforcement to the changed set, not all files).
set +e
out5="$(COVERAGE_FILE=$tmp/cov.out COVERAGE_EXCEPTIONS=$tmp/exc.txt \
        COVERAGE_PKG_MODE=off COVERAGE_FILE_MODE=enforce \
        COVERAGE_INCLUDE_FILES='commands/good.go' bash "$gate" 2>&1)"; rc5=$?
set -e
chk "$rc5" "0" "scoped-to-good: out-of-scope weak file does not fail enforce"
grep -q "commands/weak.go" <<<"$out5" && { echo "FAIL: out-of-scope weak reported"; fail=1; } || echo "ok: out-of-scope weak skipped"

# Scoped to a file with no coverage rows → nothing to enforce, PASS.
set +e
out6="$(COVERAGE_FILE=$tmp/cov.out COVERAGE_EXCEPTIONS=$tmp/exc.txt \
        COVERAGE_PKG_MODE=off COVERAGE_FILE_MODE=enforce \
        COVERAGE_INCLUDE_FILES='commands/absent.go' bash "$gate" 2>&1)"; rc6=$?
set -e
chk "$rc6" "0" "scoped-to-absent: no changed file in profile => PASS"
grep -q "nothing to enforce" <<<"$out6" && echo "ok: nothing-to-enforce message" || { echo "FAIL: no nothing-to-enforce msg"; fail=1; }

[[ $fail -eq 0 ]] && echo "coverage-gate.test: PASS" || { echo "coverage-gate.test: FAIL"; exit 1; }
