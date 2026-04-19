#!/usr/bin/env bash

set -euo pipefail

REPO_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/ralph-pipeline-gate.XXXXXX")"
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

TEST_REPO="$TMPDIR_ROOT/repo"
SNAPSHOT_DIR="$TMPDIR_ROOT/snapshot"
FAKE_DA="$TMPDIR_ROOT/fake-dot-agents"
OUT_FILE="$TMPDIR_ROOT/pipeline.out"

mkdir -p "$TEST_REPO" "$SNAPSHOT_DIR"
cd "$TEST_REPO"
git init -q

cp "$REPO_SOURCE/bin/tests/ralph-pipeline" "$SNAPSHOT_DIR/ralph-pipeline"

cat >"$SNAPSHOT_DIR/ralph-orchestrate" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
bundle="$repo_root/.agents/active/delegation-bundles/del-t1.yaml"
mkdir -p "$(dirname "$bundle")"
cat >"$bundle" <<'YAML'
schema_version: 1
delegation_id: del-t1
plan_id: p1
task_id: t1
verification:
  verifier_sequence: []
YAML
printf 'RALPH_BUNDLE: %s\n' "$bundle"
EOF

cat >"$SNAPSHOT_DIR/ralph-worker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
bundle=""
stage=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bundle) bundle="$2"; shift 2 ;;
    --stage) stage="$2"; shift 2 ;;
    --verifier-type) shift 2 ;;
    *) shift ;;
  esac
done
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
task_id="$(awk -F': *' '/^task_id:/ {print $2}' "$bundle" | tr -d '"')"
mkdir -p "$repo_root/.agents/active/verification/$task_id" "$repo_root/.agents/active/merge-back"
case "${stage:-}" in
  impl)
    cat >"$repo_root/.agents/active/verification/$task_id/impl-handoff.yaml" <<YAML
task_id: $task_id
ready_for_verification: true
YAML
    ;;
  review|"")
    cat >"$repo_root/.agents/active/merge-back/$task_id.md" <<YAML
task_id: $task_id
parent_plan_id: p1
summary: done
YAML
    ;;
esac
EOF

cat >"$SNAPSHOT_DIR/ralph-review-gate" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "GATE_FAIL"
exit 2
EOF

cat >"$SNAPSHOT_DIR/ralph-closeout" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
touch "$repo_root/closeout-ran"
exit 0
EOF

chmod +x \
  "$SNAPSHOT_DIR/ralph-pipeline" \
  "$SNAPSHOT_DIR/ralph-orchestrate" \
  "$SNAPSHOT_DIR/ralph-worker" \
  "$SNAPSHOT_DIR/ralph-review-gate" \
  "$SNAPSHOT_DIR/ralph-closeout"

cat >"$FAKE_DA" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "workflow" && "${2:-}" == "bundle" && "${3:-}" == "stages" ]]; then
  printf 'impl\nreview\n'
  exit 0
fi
echo "unexpected fake dot-agents invocation: $*" >&2
exit 1
EOF
chmod +x "$FAKE_DA"

set +e
RALPH_PIPELINE_SNAPSHOT_DIR="$SNAPSHOT_DIR" \
DOT_AGENTS="$FAKE_DA" \
RALPH_NO_LOG=1 \
"$SNAPSHOT_DIR/ralph-pipeline" >"$OUT_FILE" 2>&1
rc=$?
set -e

if [[ $rc -eq 0 ]]; then
  echo "expected ralph-pipeline to fail when review gate fails" >&2
  cat "$OUT_FILE" >&2
  exit 1
fi

if [[ -f "$TEST_REPO/closeout-ran" ]]; then
  echo "expected closeout to be skipped after review gate failure" >&2
  cat "$OUT_FILE" >&2
  exit 1
fi

if ! grep -q "review gate blocked closeout" "$OUT_FILE"; then
  echo "expected pipeline output to mention review gate blocking closeout" >&2
  cat "$OUT_FILE" >&2
  exit 1
fi

echo "PASS: review gate failure blocks closeout"
