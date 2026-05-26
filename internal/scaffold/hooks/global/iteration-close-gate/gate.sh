#!/bin/sh
# iteration-close-gate/gate.sh
#
# Minimal scaffold provided by r1-5-hook-enforcement-telemetry/t1 to satisfy
# R2.1: every gate MUST emit its outcome through `da workflow hook-outcome write`
# rather than writing the iter-N.hook-outcomes.yaml sidecar from the gate itself.
# loop-discipline-stop-hooks/p2 will replace the placeholder decision block
# below with the real terminal/PreToolUse/PreCompact gate logic per its
# contract; the outcome-emit call site already lives in this file so r1-5
# does not have to revisit it post-p2.
#
# Contract surface:
#   - $1 (optional): the canonical When value (pre_tool_use, stop, etc.) when
#       the platform launcher passes it positionally. Falls back to
#       $HOOK_EVENT_NAME (Claude Code, Codex), then to "stop".
#   - stdin: the vendor's per-platform hook JSON payload. Echoed through so
#       p2 can attach parsers without restructuring this stub.
#   - environment:
#       DA_HOOK_PLATFORM    one of claude|codex|copilot|cursor (required by R1.1)
#       DA_HOOK_SENTINEL_ID <skill>-<run-id>; resolved by p2 from
#                           `da workflow hook-sentinel read iteration-close --latest`
#
# Until p2 lands, the default decision is `allow` so the call site exercises
# the CLI without inventing remediation reasons.

set -eu

WHEN="${1:-${HOOK_EVENT_NAME:-stop}}"
PLATFORM="${DA_HOOK_PLATFORM:-claude}"
SENTINEL_ID="${DA_HOOK_SENTINEL_ID:-iteration-close-unknown}"

# Drain stdin so the vendor's hook contract is honoured even though the
# placeholder does not parse it yet (p2 will).
PAYLOAD="$(cat || true)"
: "$PAYLOAD"

case "$WHEN" in
  pre_tool_use)
    LIFECYCLE_POINT="pre_tool_use"
    INTERVENTION_CLASS="prevent_before_action"
    RULE_ID="iteration-close.R1.8"
    ;;
  pre_compact)
    LIFECYCLE_POINT="pre_compact"
    INTERVENTION_CLASS="continuity_advice"
    RULE_ID="iteration-close.R1.4"
    ;;
  subagent_stop)
    LIFECYCLE_POINT="subagent_stop"
    INTERVENTION_CLASS="remediate_at_stop"
    RULE_ID="iteration-close.R1.1"
    ;;
  stop|*)
    LIFECYCLE_POINT="stop"
    INTERVENTION_CLASS="remediate_at_stop"
    RULE_ID="iteration-close.R1.1"
    ;;
esac

# Placeholder decision until p2 lands the real evidence check.
RESULT="allow"

# Emit the outcome through the CLI per R2.1. The CLI exits 0 silently with
# an stderr advisory when no iteration is active (R2.2), so an early-session
# gate run does not fail the hook chain.
da workflow hook-outcome write \
  --sentinel-id "$SENTINEL_ID" \
  --skill "iteration-close" \
  --lifecycle-point "$LIFECYCLE_POINT" \
  --intervention-class "$INTERVENTION_CLASS" \
  --result "$RESULT" \
  --rule-id "$RULE_ID" \
  --platform "$PLATFORM" \
  >/dev/null || true

exit 0
