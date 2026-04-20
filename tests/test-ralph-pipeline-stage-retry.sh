#!/usr/bin/env bash

set -euo pipefail

REPO_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/ralph-pipeline-stage-retry.XXXXXX")"
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

setup_common_snapshot() {
  local dir="$1"
  mkdir -p "$dir/repo" "$dir/snapshot"
  cd "$dir/repo"
  git init -q
  cp "$REPO_SOURCE/bin/tests/ralph-pipeline" "$dir/snapshot/ralph-pipeline"
  chmod +x "$dir/snapshot/ralph-pipeline"
}

write_orchestrate() {
  local snapshot_dir="$1" verifier_sequence="$2"
  cat >"$snapshot_dir/ralph-orchestrate" <<EOF
#!/usr/bin/env bash
set -euo pipefail
repo_root="\$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
bundle="\$repo_root/.agents/active/delegation-bundles/del-t1.yaml"
mkdir -p "\$(dirname "\$bundle")"
cat >"\$bundle" <<'YAML'
schema_version: 1
delegation_id: del-t1
plan_id: p1
task_id: t1
verification:
  verifier_sequence: [${verifier_sequence}]
YAML
printf 'RALPH_BUNDLE: %s\n' "\$bundle"
EOF
  chmod +x "$snapshot_dir/ralph-orchestrate"
}

scenario_retry_recovery() {
  local dir="$TMPDIR_ROOT/retry-recovery"
  setup_common_snapshot "$dir"
  local repo="$dir/repo"
  local snapshot="$dir/snapshot"
  local fake_da="$dir/fake-dot-agents"
  local out_file="$dir/pipeline.out"
  local log_dir="$dir/logs"

  write_orchestrate "$snapshot" "'unit'"

  cat >"$snapshot/ralph-worker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
bundle=""
stage=""
verifier_type=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bundle) bundle="$2"; shift 2 ;;
    --stage) stage="$2"; shift 2 ;;
    --verifier-type) verifier_type="$2"; shift 2 ;;
    *) shift ;;
  esac
done
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
task_id="$(awk -F': *' '/^task_id:/ {print $2}' "$bundle" | tr -d '"')"
mkdir -p "$repo_root/.agents/active/verification/$task_id" "$repo_root/.agents/active/merge-back"
case "$stage" in
  impl)
    count_file="$repo_root/impl-count"
    count=0
    [[ -f "$count_file" ]] && count="$(cat "$count_file")"
    count=$((count + 1))
    printf '%s' "$count" >"$count_file"
    cat >"$repo_root/.agents/active/verification/$task_id/impl-handoff.yaml" <<YAML
task_id: $task_id
commit_sha: test-sha
write_scope_touched:
  - bin/tests/ralph-pipeline
ready_for_verification: true
impl_notes: retry recovery test
YAML
    ;;
  verifier)
    count_file="$repo_root/verifier-count"
    count=0
    [[ -f "$count_file" ]] && count="$(cat "$count_file")"
    count=$((count + 1))
    printf '%s' "$count" >"$count_file"
    if [[ "${RALPH_WORKER_AGENT_BIN_OVERRIDE:-}" == "binA" ]]; then
      echo "hit your usage limit" >&2
      exit 32
    fi
    cat >"$repo_root/.agents/active/verification/$task_id/${verifier_type}.result.yaml" <<YAML
schema_version: 1
task_id: $task_id
parent_plan_id: p1
verifier_type: ${verifier_type}
status: pass
summary: recovered on fallback bin
recorded_at: 2026-04-20T00:00:00Z
commands:
  - verifier fallback success
YAML
    ;;
  review)
    count_file="$repo_root/review-count"
    count=0
    [[ -f "$count_file" ]] && count="$(cat "$count_file")"
    count=$((count + 1))
    printf '%s' "$count" >"$count_file"
    cat >"$repo_root/.agents/active/merge-back/$task_id.md" <<YAML
task_id: $task_id
parent_plan_id: p1
summary: done
YAML
    ;;
esac
EOF

  cat >"$snapshot/ralph-review-gate" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF

  cat >"$snapshot/ralph-closeout" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
touch "$repo_root/closeout-ran"
exit 0
EOF

  chmod +x "$snapshot/ralph-worker" "$snapshot/ralph-review-gate" "$snapshot/ralph-closeout"

  cat >"$fake_da" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "workflow" && "${2:-}" == "bundle" && "${3:-}" == "stages" ]]; then
  printf 'impl\nverifier:unit\nreview\n'
  exit 0
fi
echo "unexpected fake dot-agents invocation: $*" >&2
exit 1
EOF
  chmod +x "$fake_da"

  set +e
  RALPH_PIPELINE_SNAPSHOT_DIR="$snapshot" \
  DOT_AGENTS="$fake_da" \
  RALPH_NO_LOG=0 \
  RALPH_LOG_DIR="$log_dir" \
  RALPH_STAGE_RETRY_MAX=1 \
  RALPH_VERIFIER_WORKER_AGENT_BIN="binA,binB" \
  "$snapshot/ralph-pipeline" >"$out_file" 2>&1
  rc=$?
  set -e

  if [[ $rc -ne 0 ]]; then
    echo "FAIL: retry recovery scenario should exit 0, got $rc" >&2
    cat "$out_file" >&2
    exit 1
  fi
  [[ "$(cat "$repo/impl-count")" == "1" ]] || { echo "FAIL: impl should run once" >&2; exit 1; }
  [[ "$(cat "$repo/verifier-count")" == "2" ]] || { echo "FAIL: verifier should retry once on fallback" >&2; exit 1; }
  [[ "$(cat "$repo/review-count")" == "1" ]] || { echo "FAIL: review should run once" >&2; exit 1; }
  [[ -f "$repo/closeout-ran" ]] || { echo "FAIL: closeout should run after recovered retry" >&2; exit 1; }
  grep -q "replacement retry 1/1 using fallback agent_bin=binB" "$out_file" || { echo "FAIL: missing retry log line" >&2; cat "$out_file" >&2; exit 1; }
  python3 - <<'PY' "$log_dir/metrics.json"
import json, sys
with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
rr = data.get("replacement_retry", {})
assert rr.get("max_per_stage") == 1, rr
assert rr.get("total_attempts") == 1, rr
assert rr.get("recovered") == 1, rr
assert rr.get("task_attempts", {}).get("t1") == 1, rr
assert rr.get("task_recovered", {}).get("t1") == 1, rr
assert rr.get("task_last_failure", {}).get("t1") == "usage_limit", rr
sm = data.get("stage_metrics", {}).get("t1", {})
stages = {entry.get("stage"): entry for entry in sm.get("stages", [])}
assert stages.get("impl", {}).get("totals", {}).get("attempt_count") == 1, stages
assert stages.get("verifier:unit", {}).get("totals", {}).get("attempt_count") == 2, stages
assert stages.get("review", {}).get("totals", {}).get("attempt_count") == 1, stages
assert stages.get("verifier:unit", {}).get("final_exit_label") == "success", stages
PY
  echo "PASS: usage_limit retry recovers with fallback bin and preserves prior stage artifacts"
}

scenario_non_resumable_hard_stop() {
  local dir="$TMPDIR_ROOT/non-resumable"
  setup_common_snapshot "$dir"
  local repo="$dir/repo"
  local snapshot="$dir/snapshot"
  local fake_da="$dir/fake-dot-agents"
  local out_file="$dir/pipeline.out"

  write_orchestrate "$snapshot" "''"

  cat >"$snapshot/ralph-worker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "denies writes under .agents/active" >&2
exit 33
EOF

  cat >"$snapshot/ralph-review-gate" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
exit 0
EOF

  cat >"$snapshot/ralph-closeout" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
touch "$repo_root/closeout-ran"
exit 0
EOF

  chmod +x "$snapshot/ralph-worker" "$snapshot/ralph-review-gate" "$snapshot/ralph-closeout"

  cat >"$fake_da" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "workflow" && "${2:-}" == "bundle" && "${3:-}" == "stages" ]]; then
  printf 'impl\nreview\n'
  exit 0
fi
echo "unexpected fake dot-agents invocation: $*" >&2
exit 1
EOF
  chmod +x "$fake_da"

  set +e
  RALPH_PIPELINE_SNAPSHOT_DIR="$snapshot" \
  DOT_AGENTS="$fake_da" \
  RALPH_NO_LOG=1 \
  RALPH_STAGE_RETRY_MAX=1 \
  "$snapshot/ralph-pipeline" >"$out_file" 2>&1
  rc=$?
  set -e

  if [[ $rc -eq 0 ]]; then
    echo "FAIL: non-resumable workspace_permissions scenario should fail" >&2
    cat "$out_file" >&2
    exit 1
  fi
  [[ ! -f "$repo/closeout-ran" ]] || { echo "FAIL: closeout must not run on hard stop" >&2; exit 1; }
  grep -q "workspace_permissions" "$out_file" || { echo "FAIL: expected workspace_permissions hard-stop output" >&2; cat "$out_file" >&2; exit 1; }
  echo "PASS: non-resumable workspace_permissions stops without closeout"
}

scenario_retry_recovery
scenario_non_resumable_hard_stop

echo "ALL PASS"
