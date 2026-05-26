#!/bin/sh
# loop-worker-gate/gate.sh
#
# Minimal scaffold provided by r1-5-hook-enforcement-telemetry/t1 to satisfy
# R2.1: the loop-worker gate MUST emit its outcome through
# `da workflow hook-outcome write`. loop-discipline-stop-hooks/p2 will
# replace the placeholder decision block below with the real bootstrap /
# PreToolUse prevention / PreCompact advice / SubagentStop verification
# logic per its contract.
#
# Contract surface mirrors iteration-close-gate/gate.sh. The loop-worker
# gate evaluates four lifecycle points: subagent_start (bootstrap),
# pre_tool_use (forbidden workflow-command prevention), pre_compact
# (continuity advice), and subagent_stop (terminal scope/handoff check).

set -eu

WHEN="${1:-${HOOK_EVENT_NAME:-subagent_stop}}"
PLATFORM="${DA_HOOK_PLATFORM:-claude}"
SENTINEL_ID="${DA_HOOK_SENTINEL_ID:-loop-worker-unknown}"

PAYLOAD="$(cat || true)"
: "$PAYLOAD"

case "$WHEN" in
  subagent_start)
    LIFECYCLE_POINT="subagent_start"
    INTERVENTION_CLASS="continuity_advice"
    RULE_ID="loop-worker.R3.0"
    ;;
  pre_tool_use)
    LIFECYCLE_POINT="pre_tool_use"
    INTERVENTION_CLASS="prevent_before_action"
    RULE_ID="loop-worker.R3.9"
    ;;
  pre_compact)
    LIFECYCLE_POINT="pre_compact"
    INTERVENTION_CLASS="continuity_advice"
    RULE_ID="loop-worker.R3.5"
    ;;
  subagent_stop|*)
    LIFECYCLE_POINT="subagent_stop"
    INTERVENTION_CLASS="remediate_at_stop"
    RULE_ID="loop-worker.R3.1"
    ;;
esac

# Placeholder decision until p2 lands the real evidence check.
RESULT="allow"

# Emit the outcome through the CLI per R2.1 (silent exit 0 when no
# iteration is active per R2.2).
da workflow hook-outcome write \
  --sentinel-id "$SENTINEL_ID" \
  --skill "loop-worker" \
  --lifecycle-point "$LIFECYCLE_POINT" \
  --intervention-class "$INTERVENTION_CLASS" \
  --result "$RESULT" \
  --rule-id "$RULE_ID" \
  --platform "$PLATFORM" \
  >/dev/null || true

exit 0
