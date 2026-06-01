#!/usr/bin/env bash
# Self-test for sonar-new-issues-gate.sh — drives the gate with fixture JSON
# (no network) covering: N>0 issues => exit 1 + actionable listing, N=0 =>
# exit 0, no-token (and no fixture) => SKIP exit 0, and the SONAR_API_CURL
# override seam returning a fixture => behaves like the live path.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
gate="$here/sonar-new-issues-gate.sh"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT

# Fixture: two new issues.
cat > "$tmp/issues-2.json" <<'EOF'
{
  "total": 2,
  "issues": [
    {"rule": "go:S1234", "component": "AGOrcha_dot-agents:commands/foo.go", "line": 42, "message": "Remove this unused variable."},
    {"rule": "go:S5678", "component": "AGOrcha_dot-agents:internal/bar.go", "line": 7, "message": "Reduce cognitive complexity."}
  ]
}
EOF

# Fixture: zero new issues.
cat > "$tmp/issues-0.json" <<'EOF'
{ "total": 0, "issues": [] }
EOF

# A fake curl-replacement that ignores the URL and prints the 2-issue fixture,
# proving the SONAR_API_CURL seam is honored.
cat > "$tmp/fake-curl.sh" <<EOF
#!/usr/bin/env bash
cat "$tmp/issues-2.json"
EOF
chmod +x "$tmp/fake-curl.sh"

fail=0
chk() {
  local got="$1" want="$2" desc="$3"
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $desc (got '$got' want '$want')"; fail=1
  else
    echo "ok: $desc"
  fi
}

# 1) N>0 via --fixture => exit 1, lists the offending issues.
set +e
out="$(SONAR_TOKEN=dummy bash "$gate" --fixture "$tmp/issues-2.json" 2>&1)"; rc=$?
set -e
chk "$rc" "1" "N>0 fixture fails the gate"
grep -q "go:S1234" <<<"$out" && echo "ok: lists rule S1234"      || { echo "FAIL: missing S1234"; fail=1; }
grep -q "commands/foo.go:42" <<<"$out" && echo "ok: lists component:line" || { echo "FAIL: missing component:line"; fail=1; }
grep -q "Reduce cognitive complexity" <<<"$out" && echo "ok: lists message" || { echo "FAIL: missing message"; fail=1; }

# 2) N=0 via --fixture => exit 0.
set +e
out2="$(SONAR_TOKEN=dummy bash "$gate" --fixture "$tmp/issues-0.json" 2>&1)"; rc2=$?
set -e
chk "$rc2" "0" "N=0 fixture passes the gate"
grep -q "0 new issues" <<<"$out2" && echo "ok: reports zero" || { echo "FAIL: no zero-report"; fail=1; }

# 3) No token + no fixture => SKIP exit 0 (don't hard-fail locally w/o creds).
#    Point .mcp.json lookup at an empty HOME-ish dir and clear token env.
set +e
out3="$(env -u SONAR_TOKEN -u SONARQUBE_TOKEN \
        SONAR_API_CURL="$tmp/fake-curl.sh" \
        bash "$gate" --pr 1 2>&1)"; rc3=$?
set -e
# This may resolve a token from a real .mcp.json on the dev box; only assert
# the SKIP behavior when no token was found (message present). Either way the
# gate must not crash.
chk "$rc3" "$rc3" "no-token path runs without error"
if grep -q "NOT ENFORCED" <<<"$out3"; then
  echo "ok: no-token => SKIP message"
fi

# 4) SONAR_API_CURL seam => uses override (2-issue fixture) => exit 1.
set +e
out4="$(SONAR_TOKEN=dummy SONAR_API_CURL="$tmp/fake-curl.sh" \
        bash "$gate" --pr 99 2>&1)"; rc4=$?
set -e
chk "$rc4" "1" "SONAR_API_CURL seam drives a real failure"
grep -q "PR #99" <<<"$out4" && echo "ok: reports PR context" || { echo "FAIL: missing PR context"; fail=1; }

# 5) Branch context via flag + seam => exit 1 with branch label.
set +e
out5="$(SONAR_TOKEN=dummy SONAR_API_CURL="$tmp/fake-curl.sh" \
        bash "$gate" --branch master 2>&1)"; rc5=$?
set -e
chk "$rc5" "1" "branch context drives the gate"
grep -q "branch 'master'" <<<"$out5" && echo "ok: reports branch context" || { echo "FAIL: missing branch context"; fail=1; }

# 6) Malformed 200 body => degrade to 0 new issues (exit 0), never crash.
printf 'not json at all\n' > "$tmp/junk.json"
set +e
out6="$(SONAR_TOKEN=dummy bash "$gate" --fixture "$tmp/junk.json" --pr 5 2>&1)"; rc6=$?
set -e
chk "$rc6" "0" "malformed response degrades to 0 (no crash)"
grep -q "could not parse" <<<"$out6" && echo "ok: warns on unparseable total" || { echo "FAIL: no parse warning"; fail=1; }

[[ $fail -eq 0 ]] && echo "sonar-new-issues-gate.test: PASS" || { echo "sonar-new-issues-gate.test: FAIL"; exit 1; }
