#!/usr/bin/env bash
# mutation_check.sh — proves the H1/H7/H8 proofs are mutation-sensitive, i.e.
# breaking the production resolver makes the relevant proof FAIL. This is the
# anti-coverage-theater gate from the tests-must-drive-the-production-path lesson.
#
# For each mutation we: patch resolver.go, run `go test`, assert it FAILS, then
# restore. If any mutation leaves the suite green, the proof is hollow.
set -euo pipefail
cd "$(dirname "$0")"

orig="$(mktemp)"
cp resolver.go "$orig"
restore() { cp "$orig" resolver.go; }
trap restore EXIT

fail=0

run_mutation() {
  local name="$1"; shift
  local sed_expr="$1"; shift
  restore
  # shellcheck disable=SC2001
  perl -0pi -e "$sed_expr" resolver.go
  if go test ./... >/dev/null 2>&1; then
    echo "MUTATION-LEAK [$name]: tests still PASS after breaking production — hollow proof"
    fail=1
  else
    echo "OK [$name]: tests fail when production is broken (mutation-sensitive)"
  fi
  restore
}

# M1: disable lock-deny enforcement (gut applyLockDenies body). H8 must fail.
run_mutation "applyLockDenies-noop" \
  's/func applyLockDenies\(b \*Bundle, policy ResolvedPolicy, ctx Context\) \{/func applyLockDenies(b *Bundle, policy ResolvedPolicy, ctx Context) {\n\treturn\n\t_ = b/'

# M2: disable the additive-path lock filtering (let lower scopes re-grant). H8a must fail.
run_mutation "unionMinusLocked-skip-filter" \
  's/if forbidden\[v\] \{\n\t\t\tcontinue \/\/ locked off — a lower scope cannot re-grant it\n\t\t\}//'

# M3: break determinism — sort refs in reverse so order matters / digest path drifts.
#     Instead, break the precedence ordering tiebreak to be non-deterministic-ish:
#     make orderProfiles a no-op so input order leaks into scalar resolution & union order.
run_mutation "orderProfiles-noop" \
  's/func orderProfiles\(profiles \[\]Profile, policy ResolvedPolicy\) \{/func orderProfiles(profiles []Profile, policy ResolvedPolicy) {\n\treturn\n\t_ = profiles/'

# M4: break precedence-governed scalar resolution (H8c) — ignore policy precedence.
run_mutation "precedenceRanker-ignore-policy" \
  's/if len\(policy.Precedence\) == 0 \{\n\t\treturn authorityRank\n\t\}/return authorityRank\n\tif len(policy.Precedence) == 0 {/'

if [ "$fail" -ne 0 ]; then
  echo "RESULT: at least one proof is NOT mutation-sensitive"
  exit 1
fi
echo "RESULT: all mutations caught — proofs are mutation-sensitive"
