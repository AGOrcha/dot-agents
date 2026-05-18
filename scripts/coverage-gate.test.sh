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
chk() { if [[ "$1" != "$2" ]]; then echo "FAIL: $3 (got '$1' want '$2')"; fail=1; else echo "ok: $3"; fi; }

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

[[ $fail -eq 0 ]] && echo "coverage-gate.test: PASS" || { echo "coverage-gate.test: FAIL"; exit 1; }
