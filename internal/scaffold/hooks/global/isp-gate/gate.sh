#!/bin/sh
# isp-gate/gate.sh
#
# Loop-discipline ISP (staged-runtime) gate.
#
# Enforces R2.1-R2.6 of .agents/workflow/specs/loop-discipline-stop-hooks/design.md:
#   pre_compact : R2.6 - continuity context naming the selected task/bundle
#                  and next unresolved stage; no block.
#   stop        : R2.1 - hard remediation when expected_artifacts (the
#                  parent-gate iter-log entry and the bundle file) are
#                  missing.
#                  R2.2/R2.3 trace-dependent rules emitted as soft coverage
#                  advisories when no readable transcript was supplied (D7).
#                  R2.4 (max_batch>1 but only one bundle materialized) is a
#                  soft advisory.
#
# All decisions are mirrored through `da workflow hook-outcome write` so the
# r1-5 telemetry pipeline can score them. Native remediation output:
#   claude/codex/copilot: {"decision":"block","reason":"..."}
#   cursor              : {"followup_message":"..."}
#
# Inputs: same as iteration-close-gate/gate.sh. Portability: no jq/python3.

set -eu

# ---------- canonical event resolution ----------

vendor_to_canonical() {
  vendor="$1"
  case "$vendor" in
    PreCompact|preCompact|pre_compact) printf 'pre_compact\n' ;;
    Stop|stop|agentStop)               printf 'stop\n' ;;
    *) printf '%s\n' "$vendor" ;;
  esac
}

WHEN_RAW="${1:-${DA_HOOK_WHEN:-${HOOK_EVENT_NAME:-stop}}}"
WHEN="$(vendor_to_canonical "$WHEN_RAW")"
PLATFORM="${DA_HOOK_PLATFORM:-claude}"

PAYLOAD="$(cat || true)"
: "$PAYLOAD"

# ---------- sentinel lookup ----------

SENTINEL_RAW=""
SENTINEL_ID=""
RUN_ID=""
PLAN_ID=""
TASK_ID=""
AGENT_TYPE=""
EXPECTED_ARTIFACTS_LIST=""
read_sentinel() {
  if ! SENTINEL_RAW="$(da workflow hook-sentinel read isp --latest 2>/dev/null)"; then
    return 1
  fi
  if [ -z "$SENTINEL_RAW" ]; then
    return 1
  fi
  RUN_ID="$(printf '%s\n' "$SENTINEL_RAW" | sed -n 's/.*run_id=\([A-Za-z0-9._-]*\).*/\1/p' | head -n1)"
  PLAN_ID="$(printf '%s\n' "$SENTINEL_RAW" | sed -n 's/.*plan=\([^ ]*\).*/\1/p' | head -n1)"
  TASK_ID="$(printf '%s\n' "$SENTINEL_RAW" | sed -n 's/.*task=\([^ ]*\).*/\1/p' | head -n1)"
  AGENT_TYPE="$(printf '%s\n' "$SENTINEL_RAW" | sed -n 's/.*agent_type=\([^ ]*\).*/\1/p' | head -n1)"
  EXPECTED_ARTIFACTS_LIST="$(printf '%s\n' "$SENTINEL_RAW" | sed -n 's/.*expected_artifacts: //p' | head -n1)"
  if [ -z "$RUN_ID" ]; then
    return 1
  fi
  SENTINEL_ID="isp-$RUN_ID"
  return 0
}

# ---------- outcome emission ----------

emit_outcome() {
  result="$1"
  rule_id="$2"
  lifecycle_point="$3"
  intervention_class="$4"
  sid="${SENTINEL_ID:-isp-unknown}"
  da workflow hook-outcome write \
    --sentinel-id "$sid" \
    --skill "isp" \
    --lifecycle-point "$lifecycle_point" \
    --intervention-class "$intervention_class" \
    --result "$result" \
    --rule-id "$rule_id" \
    --platform "$PLATFORM" \
    >/dev/null 2>&1 || true
}

json_escape() {
  raw="$1"
  printf '%s' "$raw" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

emit_hard_block() {
  reason="$1"
  escaped="$(json_escape "$reason")"
  case "$PLATFORM" in
    cursor)
      printf '{"followup_message":"%s"}\n' "$escaped"
      ;;
    claude|codex|copilot|*)
      printf '{"decision":"block","reason":"%s"}\n' "$escaped"
      ;;
  esac
  printf 'isp-gate: %s\n' "$reason" >&2
  exit 2
}

emit_advisory() {
  message="$1"
  printf 'isp-gate (advisory): %s\n' "$message" >&2
}

missing_expected_artifacts() {
  missing=""
  if [ -z "$EXPECTED_ARTIFACTS_LIST" ]; then
    printf ''
    return 0
  fi
  list="$(printf '%s' "$EXPECTED_ARTIFACTS_LIST" | tr ',' '\n')"
  IFS_BACKUP="$IFS"
  IFS='
'
  for raw in $list; do
    artifact="$(printf '%s' "$raw" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    if [ -z "$artifact" ]; then
      continue
    fi
    if [ ! -e "$artifact" ]; then
      if [ -z "$missing" ]; then
        missing="$artifact"
      else
        missing="$missing, $artifact"
      fi
    fi
  done
  IFS="$IFS_BACKUP"
  printf '%s' "$missing"
}

# ---------- dispatch ----------

if ! read_sentinel; then
  exit 0
fi

case "$WHEN" in
  pre_compact)
    emit_advisory "isp sentinel active (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID). Next unresolved stage requires the parent-gate iter-log entry and merge-back review before fanout. Capture decision text and bundle paths before compaction (rule isp.R2.6)."
    emit_outcome advise isp.R2.6 pre_compact continuity_advice
    exit 0
    ;;
  stop)
    missing="$(missing_expected_artifacts)"
    if [ -n "$missing" ]; then
      emit_outcome remediate isp.R2.1 stop remediate_at_stop
      emit_hard_block "isp staged-runtime did not produce all expected artifacts. Missing: $missing. Append the parent-gate entry to the active iter-log and persist the bundle before stopping (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID; rule isp.R2.1)."
    fi
    # R2.2/R2.3 trace-dependent: emit soft coverage advisory per D7.
    emit_advisory "isp artifacts present; trace-backed rules (R2.2 eligible-snapshot orient re-run, R2.3 direct-vs-fanout decision text) deferred - platform did not supply a readable transcript path (rule isp.R2.2)."
    emit_outcome allow isp.R2.1 stop remediate_at_stop
    emit_outcome advise isp.R2.2 stop remediate_at_stop
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
