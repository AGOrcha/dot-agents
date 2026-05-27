#!/bin/sh
# loop-worker-gate/gate.sh
#
# Loop-discipline loop-worker gate.
#
# Enforces R3.1-R3.10 of .agents/workflow/specs/loop-discipline-stop-hooks/design.md:
#   subagent_start : R3.8 - bootstrap reminder (advisory).
#   pre_tool_use   : R3.9 - hard block on `workflow advance|orient|next|status`
#                    while a loop-worker sentinel is active.
#   pre_compact    : R3.10 - continuity advice; no block.
#   subagent_stop  : R3.1 - hard remediation when changed files escape the
#                    sentinel's write_scope.
#                    R3.4 - hard remediation when expected_artifacts include
#                    a merge-back path that is missing.
#                    R3.2/R3.3/R3.5/R3.6 trace-dependent or smell rules emit
#                    soft coverage advisories per D7.
#
# Self-filter per D6: if the sentinel's agent_type is not "loop-worker", the
# gate exits 0 immediately so subagent_stop hooks on Codex/Copilot/Cursor
# (which fire for every subagent) do not block unrelated agents.

set -eu

# ---------- canonical event resolution ----------

vendor_to_canonical() {
  vendor="$1"
  case "$vendor" in
    SubagentStart|subagentStart|subagent_start) printf 'subagent_start\n' ;;
    PreToolUse|pre_tool_use)                     printf 'pre_tool_use\n' ;;
    PreCompact|preCompact|pre_compact)           printf 'pre_compact\n' ;;
    SubagentStop|subagentStop|subagent_stop)     printf 'subagent_stop\n' ;;
    *) printf '%s\n' "$vendor" ;;
  esac
}

WHEN_RAW="${1:-${DA_HOOK_WHEN:-${HOOK_EVENT_NAME:-subagent_stop}}}"
WHEN="$(vendor_to_canonical "$WHEN_RAW")"
PLATFORM="${DA_HOOK_PLATFORM:-claude}"

PAYLOAD="$(cat || true)"

# ---------- sentinel lookup ----------

SENTINEL_RAW=""
SENTINEL_JSON=""
SENTINEL_ID=""
RUN_ID=""
PLAN_ID=""
TASK_ID=""
AGENT_TYPE=""
EXPECTED_ARTIFACTS_LIST=""
WRITE_SCOPE_LIST=""
read_sentinel() {
  if ! SENTINEL_RAW="$(da workflow hook-sentinel read loop-worker --latest 2>/dev/null)"; then
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
  SENTINEL_ID="loop-worker-$RUN_ID"
  # JSON mode is needed for write_scope (text mode does not include
  # context.*). The CLI prints SetIndent("", "  ") so each array member
  # appears one-per-line at four-space indent: portable awk + sed extract.
  SENTINEL_JSON="$(da workflow hook-sentinel read loop-worker --run-id "$RUN_ID" --json 2>/dev/null || true)"
  WRITE_SCOPE_LIST="$(printf '%s\n' "$SENTINEL_JSON" | awk '
    /"write_scope"[[:space:]]*:[[:space:]]*\[/ { in_ws=1; next }
    in_ws && /\]/                              { in_ws=0; next }
    in_ws                                       { print }
  ' | sed -e 's/^[[:space:]]*"//' -e 's/",\{0,1\}[[:space:]]*$//')"
  return 0
}

# ---------- outcome emission ----------

emit_outcome() {
  result="$1"
  rule_id="$2"
  lifecycle_point="$3"
  intervention_class="$4"
  sid="${SENTINEL_ID:-loop-worker-unknown}"
  da workflow hook-outcome write \
    --sentinel-id "$sid" \
    --skill "loop-worker" \
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
  printf 'loop-worker-gate: %s\n' "$reason" >&2
  exit 2
}

emit_advisory() {
  message="$1"
  printf 'loop-worker-gate (advisory): %s\n' "$message" >&2
}

# ---------- rule helpers ----------

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

# path_matches_scope returns 0 (true) when $1 starts with any entry in
# WRITE_SCOPE_LIST. Bare entries match an exact path; trailing-slash entries
# match any descendant.
path_matches_scope() {
  path="$1"
  if [ -z "$WRITE_SCOPE_LIST" ]; then
    # No declared scope means the gate cannot evaluate R3.1; treat every
    # path as out-of-scope to fail closed per the contract's "malformed
    # input must not silently approve a portable hard violation" rule.
    return 1
  fi
  IFS_BACKUP="$IFS"
  IFS='
'
  for scope in $WRITE_SCOPE_LIST; do
    if [ -z "$scope" ]; then
      continue
    fi
    case "$path" in
      "$scope") IFS="$IFS_BACKUP"; return 0 ;;
      "$scope"*) IFS="$IFS_BACKUP"; return 0 ;;
      *) ;;  # no match for this scope entry; loop tries the next one
    esac
  done
  IFS="$IFS_BACKUP"
  return 1
}

# escaped_paths returns the list of changed files (staged + unstaged +
# untracked) that fall outside the declared write_scope. Uses `git status
# --porcelain` rooted at $PWD which the platform launchers set to the
# worker's CWD.
escaped_paths() {
  changed="$(git status --porcelain 2>/dev/null | awk '{print $2}' | sed '/^$/d' || true)"
  if [ -z "$changed" ]; then
    printf ''
    return 0
  fi
  escaped=""
  IFS_BACKUP="$IFS"
  IFS='
'
  for path in $changed; do
    if ! path_matches_scope "$path"; then
      if [ -z "$escaped" ]; then
        escaped="$path"
      else
        escaped="$escaped, $path"
      fi
    fi
  done
  IFS="$IFS_BACKUP"
  printf '%s' "$escaped"
}

# payload_attempted_orchestrator_command returns 0 when stdin contains any
# of the forbidden orchestrator subcommands. R3.9 deterministic check.
payload_attempted_orchestrator_command() {
  case "$PAYLOAD" in
    *"workflow advance"*) return 0 ;;
    *"workflow orient"*)  return 0 ;;
    *"workflow next"*)    return 0 ;;
    *"workflow status"*)  return 0 ;;
    *) return 1 ;;
  esac
}

# ---------- dispatch ----------

if ! read_sentinel; then
  # No loop-worker sentinel for this turn - R3.7 says exit 0 silently.
  exit 0
fi

# D6 self-filter: if the active sentinel is for some other agent type, do
# not interfere with whatever non-loop-worker subagent is exiting.
if [ -n "$AGENT_TYPE" ] && [ "$AGENT_TYPE" != "loop-worker" ]; then
  exit 0
fi

case "$WHEN" in
  subagent_start)
    # R3.8 - bootstrap reminder. Non-blocking by construction.
    emit_advisory "loop-worker bootstrap (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID). Read the delegation bundle, honor write_scope, and use the delegated iteration-close skill for verify/checkpoint/merge-back. SubagentStart is bootstrap context only - compliance is verified at SubagentStop (rule loop-worker.R3.8)."
    emit_outcome advise loop-worker.R3.8 subagent_start continuity_advice
    exit 0
    ;;
  pre_tool_use)
    if payload_attempted_orchestrator_command; then
      emit_outcome remediate loop-worker.R3.9 pre_tool_use prevent_before_action
      emit_hard_block "loop-worker cannot run orchestrator workflow commands (advance/orient/next/status). Return to the parent via 'workflow merge-back' instead (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID; rule loop-worker.R3.9)."
    fi
    emit_outcome allow loop-worker.R3.9 pre_tool_use prevent_before_action
    exit 0
    ;;
  pre_compact)
    scope_summary="$(printf '%s' "$WRITE_SCOPE_LIST" | tr '\n' ' ')"
    emit_advisory "loop-worker sentinel active (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID). Write scope: ${scope_summary:-(undeclared)}. Required closeout: verify -> checkpoint -> merge-back via delegated iteration-close. Do not compact before merge-back is written (rule loop-worker.R3.10)."
    emit_outcome advise loop-worker.R3.10 pre_compact continuity_advice
    exit 0
    ;;
  subagent_stop)
    # R3.1 - write_scope diff (portable hard check).
    escapees="$(escaped_paths)"
    if [ -n "$escapees" ]; then
      scope_summary="$(printf '%s' "$WRITE_SCOPE_LIST" | tr '\n' ' ')"
      emit_outcome remediate loop-worker.R3.1 subagent_stop remediate_at_stop
      emit_hard_block "loop-worker modified paths outside the declared write_scope. Out-of-scope: $escapees. Allowed scope: ${scope_summary:-(undeclared)}. Revert the offending edits before stopping (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID; rule loop-worker.R3.1)."
    fi
    # R3.4 - missing merge-back artifact.
    missing="$(missing_expected_artifacts)"
    if [ -n "$missing" ]; then
      emit_outcome remediate loop-worker.R3.4 subagent_stop remediate_at_stop
      emit_hard_block "loop-worker stop without required handoff artifacts. Missing: $missing. Write the delegated merge-back before stopping (run_id=$RUN_ID, plan=$PLAN_ID, task=$TASK_ID; rule loop-worker.R3.4)."
    fi
    # R3.2/R3.3/R3.5/R3.6 - soft coverage / smell advisories per D7.
    emit_advisory "loop-worker scope and handoff verified; trace-backed rules (R3.2 orchestrator-command check, R3.3 loop-state edits, R3.5 positive+negative test trace, R3.6 dirty worktree) deferred - platform did not supply a readable transcript path (rule loop-worker.R3.2)."
    emit_outcome allow loop-worker.R3.1 subagent_stop remediate_at_stop
    emit_outcome advise loop-worker.R3.2 subagent_stop remediate_at_stop
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
