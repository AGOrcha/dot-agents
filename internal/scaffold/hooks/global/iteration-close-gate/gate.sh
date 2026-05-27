#!/bin/sh
# iteration-close-gate/gate.sh
#
# Loop-discipline iteration-close gate.
#
# Enforces R1.1-R1.9 of .agents/workflow/specs/loop-discipline-stop-hooks/design.md:
#   pre_tool_use  : R1.8 - block delegated `workflow advance` before execution.
#   pre_compact   : R1.9 - non-blocking continuity advice while obligations remain.
#   stop / subagent_stop : R1.1/R1.2 - verify expected_artifacts exist; emit
#                  hard remediation when missing. R1.4 trace-dependent rules
#                  are emitted as soft coverage advisories when the platform
#                  did not supply a readable trace (D7).
#
# All decisions are mirrored through `da workflow hook-outcome write` so the
# r1-5 telemetry pipeline can score them. Native remediation output:
#   claude/codex/copilot: {"decision":"block","reason":"..."}
#   cursor              : {"followup_message":"..."}
#
# Inputs:
#   $1                 : optional canonical When override (testing).
#   $HOOK_EVENT_NAME   : Claude/Codex pass the vendor event (PascalCase).
#   $DA_HOOK_PLATFORM  : claude|codex|copilot|cursor (defaults to claude).
#   $DA_HOOK_WHEN      : explicit canonical When (overrides env detection).
#   stdin              : vendor hook JSON payload (drained, not parsed beyond
#                        portable grep - `jq`/`python3` are not declared
#                        dependencies per the P2 contract).
#
# Fail-safe: malformed input never silently approves a portable hard
# violation. Exit codes: 0 for advisory/allow, 2 for hard block (with native
# JSON on stdout where the vendor supports it).

set -eu

# ---------- canonical event resolution ----------

# vendor_to_canonical translates a vendor PascalCase event name to the
# canonical snake_case used by HookSpec.When. Unknown names fall through to
# the input so $DA_HOOK_WHEN can pass canonical values directly.
vendor_to_canonical() {
  case "$1" in
    PreToolUse|pre_tool_use)        printf 'pre_tool_use\n' ;;
    PreCompact|preCompact|pre_compact) printf 'pre_compact\n' ;;
    Stop|stop|agentStop)            printf 'stop\n' ;;
    SubagentStop|subagentStop|subagent_stop) printf 'subagent_stop\n' ;;
    *) printf '%s\n' "$1" ;;
  esac
}

WHEN_RAW="${1:-${DA_HOOK_WHEN:-${HOOK_EVENT_NAME:-stop}}}"
WHEN="$(vendor_to_canonical "$WHEN_RAW")"
PLATFORM="${DA_HOOK_PLATFORM:-claude}"

# Drain stdin so the vendor's hook contract is honoured even when we do not
# parse the payload. Portable shell only - no jq/python3 dependency per the
# input-and-portability rules in p2-hook-scripts.contract.md.
PAYLOAD="$(cat || true)"

# ---------- sentinel lookup ----------

# read_sentinel populates SENTINEL_RAW, SENTINEL_ID, RUN_ID, PLAN_ID,
# TASK_ID, AGENT_TYPE, EXPECTED_ARTIFACTS_LIST when the active sentinel for
# iteration-close exists. Returns non-zero when no sentinel is found.
SENTINEL_RAW=""
SENTINEL_ID=""
RUN_ID=""
PLAN_ID=""
TASK_ID=""
AGENT_TYPE=""
EXPECTED_ARTIFACTS_LIST=""
read_sentinel() {
  # The CLI prints "<path>\n  skill=... run_id=... started_at=...\n
  # plan=... task=... agent_type=...\n  expected_artifacts: a, b\n". On
  # "no sentinels" the CLI errors with a non-zero exit; suppress stderr and
  # propagate that as "no sentinel".
  if ! SENTINEL_RAW="$(da workflow hook-sentinel read iteration-close --latest 2>/dev/null)"; then
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
  SENTINEL_ID="iteration-close-$RUN_ID"
  return 0
}

# ---------- outcome emission ----------

# emit_outcome wraps `da workflow hook-outcome write` so a CLI failure
# (no active iteration, transient FS error, etc.) does not blow up the gate.
# The CLI itself exits 0 silently when no iteration is active per R2.2.
emit_outcome() {
  result="$1"
  rule_id="$2"
  lifecycle_point="$3"
  intervention_class="$4"
  sid="${SENTINEL_ID:-iteration-close-unknown}"
  da workflow hook-outcome write \
    --sentinel-id "$sid" \
    --skill "iteration-close" \
    --lifecycle-point "$lifecycle_point" \
    --intervention-class "$intervention_class" \
    --result "$result" \
    --rule-id "$rule_id" \
    --platform "$PLATFORM" \
    >/dev/null 2>&1 || true
}

# ---------- native remediation output ----------

# json_escape escapes backslashes and double quotes for a JSON string body.
# Portable POSIX sed - no jq dependency.
json_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

# emit_hard_block prints the native block payload for the active platform
# and exits 2. Claude/Codex/Copilot get {"decision":"block","reason":...};
# Cursor gets {"followup_message":...}. R5.1 + design D7.
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
  printf 'iteration-close-gate: %s\n' "$reason" >&2
  exit 2
}

# emit_advisory writes the message to stderr and exits 0 (R5.2).
emit_advisory() {
  printf 'iteration-close-gate (advisory): %s\n' "$1" >&2
}

# ---------- rule helpers ----------

# missing_expected_artifacts lists every entry in EXPECTED_ARTIFACTS_LIST
# that is not present on disk. The CLI emits artifacts as "a, b, c" so we
# split on comma + whitespace. Repo-relative paths are resolved from $PWD
# which the platform launchers set to the project root.
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

# payload_attempted_workflow_advance returns 0 when the buffered stdin
# payload contains a `workflow advance` invocation literal. This is a
# best-effort portable check (case glob) used only when a sentinel-anchored
# delegated-closeout rule fires on pre_tool_use. The sentinel must be
# present (read_sentinel succeeded) before this is consulted; we treat the
# subcommand literal in the pre-tool-use payload command field as
# sufficient deterministic evidence per the contract's "deterministic
# PreToolUse hard outcome" line.
payload_attempted_workflow_advance() {
  case "$PAYLOAD" in
    *"workflow advance"*) return 0 ;;
    *) return 1 ;;
  esac
}

# ---------- dispatch ----------

# Resolve sentinel first; an absent sentinel short-circuits to allow for
# every event (R1.7 - "skill did not run this turn"). The pre_tool_use
# event still consults the sentinel: forbidden commands are only blocked
# when this skill is actively governing.
if ! read_sentinel; then
  exit 0
fi

case "$WHEN" in
  pre_tool_use)
    # R1.8 - delegated closeout attempting `workflow advance`.
    if payload_attempted_workflow_advance; then
      emit_outcome remediate iteration-close.R1.8 pre_tool_use prevent_before_action
      emit_hard_block "Delegated iteration-close cannot call 'workflow advance'. Run 'workflow merge-back' instead (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID; rule iteration-close.R1.8)."
    fi
    emit_outcome allow iteration-close.R1.8 pre_tool_use prevent_before_action
    exit 0
    ;;
  pre_compact)
    # R1.9 - continuity advice naming unresolved obligations.
    missing="$(missing_expected_artifacts)"
    if [ -n "$missing" ]; then
      emit_advisory "iteration-close sentinel active (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID). Outstanding artifacts before compaction: $missing. Complete verify-record/checkpoint/merge-back, then run 'da workflow hook-sentinel clear iteration-close --run-id $RUN_ID' (rule iteration-close.R1.9)."
    else
      emit_advisory "iteration-close sentinel active (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID). Closeout artifacts present; remember to clear the sentinel via 'da workflow hook-sentinel clear iteration-close --run-id $RUN_ID' (rule iteration-close.R1.9)."
    fi
    emit_outcome advise iteration-close.R1.9 pre_compact continuity_advice
    exit 0
    ;;
  stop|subagent_stop)
    lifecycle="$WHEN"
    missing="$(missing_expected_artifacts)"
    if [ -n "$missing" ]; then
      emit_outcome remediate iteration-close.R1.1 "$lifecycle" remediate_at_stop
      emit_hard_block "iteration-close did not produce all expected artifacts. Missing: $missing. Re-run the verify-record/checkpoint/merge-back step (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID; rule iteration-close.R1.1)."
    fi
    # R1.4 trace-dependent: without a verified readable trace we emit a
    # soft coverage advisory per D7 rather than a false-positive block.
    emit_advisory "iteration-close artifacts verified; trace-backed rules (R1.4 workflow-advance check, R1.6 build-prod check) deferred - platform did not supply a readable transcript path (rule iteration-close.R1.4)."
    emit_outcome allow iteration-close.R1.1 "$lifecycle" remediate_at_stop
    emit_outcome advise iteration-close.R1.4 "$lifecycle" remediate_at_stop
    exit 0
    ;;
  *)
    # Unknown event - emit no outcome and exit 0 fail-safe (the manifest
    # whitelist already prevents arrival here in normal operation).
    exit 0
    ;;
esac
