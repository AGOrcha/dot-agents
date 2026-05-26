#!/bin/sh
# isp-gate/gate.sh
#
# Minimal scaffold provided by r1-5-hook-enforcement-telemetry/t1 to satisfy
# R2.1: the isp staged-runtime gate MUST emit its outcome through
# `da workflow hook-outcome write`. loop-discipline-stop-hooks/p2 will
# replace the placeholder decision block with the real terminal/PreCompact
# logic per its contract.
#
# Contract surface mirrors iteration-close-gate/gate.sh. The isp gate
# evaluates two lifecycle points: pre_compact (continuity_advice) and stop
# (remediate_at_stop).

set -eu

WHEN="${1:-${HOOK_EVENT_NAME:-stop}}"
PLATFORM="${DA_HOOK_PLATFORM:-claude}"
SENTINEL_ID="${DA_HOOK_SENTINEL_ID:-isp-unknown}"

PAYLOAD="$(cat || true)"
: "$PAYLOAD"

case "$WHEN" in
  pre_compact)
    LIFECYCLE_POINT="pre_compact"
    INTERVENTION_CLASS="continuity_advice"
    RULE_ID="isp.R2.1"
    ;;
  stop|*)
    LIFECYCLE_POINT="stop"
    INTERVENTION_CLASS="remediate_at_stop"
    RULE_ID="isp.R2.2"
    ;;
esac

# Placeholder decision until p2 lands the real evidence check.
RESULT="allow"

# Emit the outcome through the CLI per R2.1 (silent exit 0 when no
# iteration is active per R2.2).
da workflow hook-outcome write \
  --sentinel-id "$SENTINEL_ID" \
  --skill "isp" \
  --lifecycle-point "$LIFECYCLE_POINT" \
  --intervention-class "$INTERVENTION_CLASS" \
  --result "$RESULT" \
  --rule-id "$RULE_ID" \
  --platform "$PLATFORM" \
  >/dev/null || true

exit 0
