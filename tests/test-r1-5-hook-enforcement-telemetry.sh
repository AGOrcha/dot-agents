#!/usr/bin/env bash
# Smoke test: R1.5 hook-enforcement telemetry end-to-end chain.
#
# Exercises the full producer→consumer path that the r1-5-hook-enforcement-
# telemetry plan delivers:
#
#   sentinel  -> gate              `da workflow hook-outcome write` appends one
#                                  outcome record anchored to a sentinel id
#   gate      -> sidecar           record lands in the active iteration's
#                                  .agents/active/iteration-log/iter-N.hook-outcomes.yaml
#   sidecar   -> score             `da score iteration N --recompute` reads the
#                                  sidecar, folds it into the `hook_outcomes`
#                                  sub-score, and renders the readback
#
# Assertions prove hook_outcomes flow through to the score readback:
#   1. a `remediate_at_stop` / `remediate` write produces a present
#      hook_outcomes row with SubScore 0.000 (R1.5 D3 remediate band) and the
#      rule_id surfaces in the "Hook outcome sources:" readback block.
#   2. the shipped companion-gate fixtures
#      (tests/fixtures/r1-5-hook-outcomes/companion-gates/*.yaml) score through
#      the same readback unchanged: the all-remediate fixture collapses to
#      SubScore 0.000 and the mixed-advise fixture to SubScore 0.600.
#
# No transcript content is read or printed — only sentinel_id / rule_id
# attribution, per the R1.5 readback contract.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DA="${DA:-${REPO_ROOT}/bin/da}"
FIXTURES="${REPO_ROOT}/tests/fixtures/r1-5-hook-outcomes/companion-gates"

if [[ ! -x "$DA" ]]; then
  echo "SKIP: da binary not found at $DA (run 'make build' or set DA= to override)" >&2
  exit 0
fi

for f in delegation-closeout-remediate.yaml companion-gates-mixed-advise.yaml; do
  if [[ ! -f "$FIXTURES/$f" ]]; then
    echo "FAIL: required fixture missing: $FIXTURES/$f" >&2
    exit 1
  fi
done

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-r1-5-hook-telemetry.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

SANDBOX="$WORK/repo"
ILD="$SANDBOX/.agents/active/iteration-log"
mkdir -p "$ILD"
FAKE_HOME="$WORK/agents-home"
mkdir -p "$FAKE_HOME"

# Isolate prefs/context from previous runs and the real home.
export AGENTS_HOME="$FAKE_HOME"

# `da score iteration --recompute` derives git-topology signals, so the
# sandbox must be a real repo with a seed commit. The canonical iter-1.yaml is
# the iteration record `hook-outcome write` resolves N from and `score` folds.
seed_sandbox() {
  (
    cd "$SANDBOX"
    git init -q
    git config user.email "e2e@example.com"
    git config user.name "r1-5-e2e"
    cat > "$ILD/iter-1.yaml" <<'YAML'
schema_version: 1
iteration: 1
date: "2026-05-29"
commit: "r15seed1"
scope_note: "on-target"
tests_total_pass: true
YAML
    git add -A
    git commit -qm "seed iteration record" >/dev/null
  )
}

# Render the recomputed iteration-1 score for the current sidecar on disk.
score_iter1() {
  (cd "$SANDBOX" && "$DA" score iteration 1 --recompute \
    --iter-log-dir "$ILD" --repo-dir "$SANDBOX" 2>/dev/null)
}

# Extract the hook_outcomes breakdown row from a score render.
hook_row() {
  grep -E '^hook_outcomes[[:space:]]' || true
}

seed_sandbox

# ── step 1: sentinel -> gate -> sidecar ──────────────────────────────────────
# A live gate firing: write one remediate-at-stop outcome anchored to a
# sentinel. This is the production write path the lifecycle hooks invoke.
(cd "$SANDBOX" && "$DA" workflow hook-outcome write \
  --sentinel-id iteration-close-r15-e2e-001 --skill iteration-close \
  --lifecycle-point stop --intervention-class remediate_at_stop \
  --result remediate --rule-id iteration-close.R1.1 --platform claude \
  >/dev/null 2>&1) \
  || { echo "FAIL: hook-outcome write returned non-zero" >&2; exit 1; }

SIDECAR="$ILD/iter-1.hook-outcomes.yaml"
if [[ ! -f "$SIDECAR" ]]; then
  echo "FAIL: hook-outcome write did not create $SIDECAR" >&2
  exit 1
fi
if ! grep -q "rule_id: iteration-close.R1.1" "$SIDECAR"; then
  echo "FAIL: written record missing rule_id in sidecar" >&2
  cat "$SIDECAR" >&2
  exit 1
fi

# ── step 2: sidecar -> score (live write) ────────────────────────────────────
render="$(score_iter1)"
row="$(printf '%s\n' "$render" | hook_row)"

# hook_outcomes must be present (PRESENT=yes) and remediate must collapse the
# sub-score to 0.000 (R1.5 D3 remediate band).
if ! printf '%s\n' "$row" | grep -qE 'hook_outcomes[[:space:]]+yes[[:space:]]+0\.000'; then
  echo "FAIL: live remediate write did not yield present hook_outcomes row at SubScore 0.000" >&2
  printf 'got row: %s\n' "$row" >&2
  exit 1
fi

# The readback must attribute the sub-score to the concrete rule that fired.
if ! printf '%s\n' "$render" | grep -q "Hook outcome sources:"; then
  echo "FAIL: score readback missing 'Hook outcome sources:' block" >&2
  printf '%s\n' "$render" >&2
  exit 1
fi
if ! printf '%s\n' "$render" | grep -q "iteration-close.R1.1"; then
  echo "FAIL: readback did not surface the written rule_id iteration-close.R1.1" >&2
  printf '%s\n' "$render" >&2
  exit 1
fi
# No transcript content should ever appear in the readback.
if printf '%s\n' "$render" | grep -qiE 'transcript[_-]?content'; then
  echo "FAIL: readback leaked transcript content" >&2
  exit 1
fi

echo "PASS: live gate write flows sentinel -> sidecar -> score (remediate, SubScore 0.000)"

# ── step 3: shipped companion-gate fixtures flow through unchanged ────────────
# All-remediate fixture: two distinct remediating rules under one sentinel must
# still collapse to SubScore 0.000 and surface both rule_ids.
cp "$FIXTURES/delegation-closeout-remediate.yaml" "$SIDECAR"
render="$(score_iter1)"
row="$(printf '%s\n' "$render" | hook_row)"
if ! printf '%s\n' "$row" | grep -qE 'hook_outcomes[[:space:]]+yes[[:space:]]+0\.000'; then
  echo "FAIL: all-remediate fixture did not score SubScore 0.000" >&2
  printf 'got row: %s\n' "$row" >&2
  exit 1
fi
for rule in delegation-closeout-gate.R4.1 delegation-closeout-gate.R4.2; do
  if ! printf '%s\n' "$render" | grep -q "$rule"; then
    echo "FAIL: all-remediate fixture readback missing $rule" >&2
    printf '%s\n' "$render" >&2
    exit 1
  fi
done
echo "PASS: delegation-closeout-remediate fixture scores 0.000 with both rules in readback"

# Mixed-advise fixture: no remediate present + at least one advise => middle
# band SubScore 0.600 (R1.5 D3), with both advising rules surfaced.
cp "$FIXTURES/companion-gates-mixed-advise.yaml" "$SIDECAR"
render="$(score_iter1)"
row="$(printf '%s\n' "$render" | hook_row)"
if ! printf '%s\n' "$row" | grep -qE 'hook_outcomes[[:space:]]+yes[[:space:]]+0\.600'; then
  echo "FAIL: mixed-advise fixture did not score SubScore 0.600" >&2
  printf 'got row: %s\n' "$row" >&2
  exit 1
fi
for rule in delegation-closeout-gate.R4.4 orchestrator-handoff-gate.R3.4; do
  if ! printf '%s\n' "$render" | grep -q "$rule"; then
    echo "FAIL: mixed-advise fixture readback missing $rule" >&2
    printf '%s\n' "$render" >&2
    exit 1
  fi
done
echo "PASS: companion-gates-mixed-advise fixture scores 0.600 with both rules in readback"

echo "PASS: R1.5 hook-enforcement telemetry end-to-end (sentinel -> gate -> sidecar -> score)"
