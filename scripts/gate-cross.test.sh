#!/usr/bin/env bash
# Self-test for scripts/gate-cross.sh — drives the REAL production paths with no
# live Windows box, via the same command seams production uses (mirrors how
# gate.test.sh drives gate.sh's `enforce-coverage` subcommand):
#
#   1. covmerge union       — merging a local + Windows profile CREDITS a
#      *_windows.go file that is 0% on the POSIX run (0 + 1 = covered).
#   2. merge-enforce PASS    — the merged multi-OS profile passes coverage-gate;
#      the mutation control (local+local, no Windows credit) FAILS, proving the
#      Windows profile's contribution is what carries the gate.
#   3. unreachable -> loud-skip — a full run against an unreachable box prints a
#      loud banner and exits 0 (OQ3 default: CI is the authoritative gate).
#   4. STRICT + unreachable   — GATE_CROSS_STRICT=1 turns an unreachable box into
#      a hard, non-zero failure instead.
#   5. reachable full run     — with mock PROBE/SYNC/SSH + a fixture local profile
#      the run merges local+Windows and coverage-gate PASSES on the union.
#   6. reachable, Windows gap — same wiring but the Windows profile does NOT cover
#      the *_windows.go file, so the merged profile FAILS: the Windows run really
#      flows into the gate (not a rubber stamp).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
gate="$here/gate-cross.sh"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

# Fictional Go package the gate is pointed at across the reachable/unreachable
# cases; a single constant so the fixture package name lives in one place.
readonly FIXTURE_PKG='./internal/foo'

fail=0
chk() {
  local got="$1" want="$2" desc="$3"
  if [[ "$got" != "$want" ]]; then echo "FAIL: $desc (got '$got' want '$want')"; fail=1
  else echo "ok: $desc"; fi
}

# Empty exceptions allowlist so fixtures are judged purely on coverage (the real
# allowlist is exercised by coverage-gate.test.sh). internal/foo is fictional so
# it is neither excluded nor allowlisted by the real files either.
: > "$tmp/exc.txt"

# Fixture profiles. foo.go is a normal cross-platform file; foo_windows.go is a
# build-tagged Windows-only file that the POSIX run cannot execute (0 hits) but
# the Windows run covers.
cat > "$tmp/local.out" <<'EOF'
mode: atomic
github.com/o/repo/internal/foo/foo.go:1.1,5.2 5 1
github.com/o/repo/internal/foo/foo_windows.go:1.1,6.2 6 0
EOF
# Windows run: foo_windows.go covered.
cat > "$tmp/win-good.out" <<'EOF'
mode: atomic
github.com/o/repo/internal/foo/foo.go:1.1,5.2 5 1
github.com/o/repo/internal/foo/foo_windows.go:1.1,6.2 6 1
EOF
# Windows run with the gap: foo_windows.go still uncovered (mutation control).
cp "$tmp/local.out" "$tmp/win-bad.out"

# Fake ssh: ignore the remote-command arg, emit the Windows fixture named by
# $WINPROF. Fake sync/probe are plain shell builtins injected per case.
cat > "$tmp/fakessh.sh" <<'EOF'
#!/usr/bin/env bash
cat "$WINPROF"
EOF

# ---------------------------------------------------------------------------
echo "== case 1: covmerge credits a Windows-only file (union) =="
set +e
merged="$(bash "$gate" covmerge "$tmp/local.out" "$tmp/win-good.out")"; rc=$?
set -e
chk "$rc" "0" "covmerge exits 0"
grep -q 'foo_windows.go:1.1,6.2 6 [1-9]' <<<"$merged" \
  && echo "ok: foo_windows.go credited on the merged profile" \
  || { echo "FAIL: foo_windows.go not credited in merge"; fail=1; }

echo "== case 2: merge-enforce PASS on the union; local-only FAILS (control) =="
set +e
out2="$(COVERAGE_EXCEPTIONS="$tmp/exc.txt" bash "$gate" merge-enforce "$tmp/local.out" "$tmp/win-good.out" 2>&1)"; rc2=$?
set -e
chk "$rc2" "0" "merged multi-OS profile passes coverage-gate"
grep -q "coverage-gate: PASS" <<<"$out2" \
  && echo "ok: coverage-gate reports PASS on merged profile" \
  || { echo "FAIL: coverage-gate did not PASS on merged profile"; fail=1; }
set +e
COVERAGE_EXCEPTIONS="$tmp/exc.txt" bash "$gate" merge-enforce "$tmp/local.out" "$tmp/local.out" >/dev/null 2>&1; rc2b=$?
set -e
chk "$rc2b" "1" "control: no Windows credit -> foo_windows.go <95% -> FAIL"

echo "== case 3: unreachable box -> loud-skip, exit 0 =="
set +e
out3="$(GATE_CROSS_PKGS="$FIXTURE_PKG" PROBE_CMD="false" bash "$gate" 2>&1)"; rc3=$?
set -e
chk "$rc3" "0" "unreachable box loud-skips deterministically (exit 0)"
grep -q "UNREACHABLE" <<<"$out3" \
  && echo "ok: loud unreachable banner printed" \
  || { echo "FAIL: no loud unreachable banner"; fail=1; }

echo "== case 4: GATE_CROSS_STRICT=1 + unreachable -> hard-fail =="
set +e
GATE_CROSS_PKGS="$FIXTURE_PKG" PROBE_CMD="false" GATE_CROSS_STRICT=1 bash "$gate" >/dev/null 2>&1; rc4=$?
set -e
chk "$rc4" "1" "STRICT turns an unreachable box into a non-zero failure"

echo "== case 5: reachable full run (mock probe/sync/ssh) -> merge + PASS =="
set +e
out5="$(GATE_CROSS_PKGS="$FIXTURE_PKG" \
        PROBE_CMD="true" SYNC_CMD="true" \
        SSH_CMD="bash $tmp/fakessh.sh" WINPROF="$tmp/win-good.out" \
        GATE_CROSS_LOCAL_PROFILE="$tmp/local.out" \
        COVERAGE_EXCEPTIONS="$tmp/exc.txt" \
        bash "$gate" 2>&1)"; rc5=$?
set -e
chk "$rc5" "0" "reachable run merges local+Windows and PASSES"
grep -q "coverage-gate: PASS" <<<"$out5" \
  && echo "ok: coverage-gate ran on the merged union" \
  || { echo "FAIL: coverage-gate did not run/PASS on the union"; fail=1; }
grep -q "gate-cross PASS" <<<"$out5" \
  && echo "ok: gate-cross reports PASS" \
  || { echo "FAIL: gate-cross did not report PASS"; fail=1; }

echo "== case 6: reachable, Windows profile has the gap -> FAIL (not a stamp) =="
set +e
GATE_CROSS_PKGS="$FIXTURE_PKG" \
  PROBE_CMD="true" SYNC_CMD="true" \
  SSH_CMD="bash $tmp/fakessh.sh" WINPROF="$tmp/win-bad.out" \
  GATE_CROSS_LOCAL_PROFILE="$tmp/local.out" \
  COVERAGE_EXCEPTIONS="$tmp/exc.txt" \
  bash "$gate" >/dev/null 2>&1; rc6=$?
set -e
chk "$rc6" "1" "an uncovered Windows-only file fails the merged gate"

[[ $fail -eq 0 ]] && echo "gate-cross.test: PASS" || { echo "gate-cross.test: FAIL"; exit 1; }
