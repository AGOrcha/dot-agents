#!/usr/bin/env bash

set -euo pipefail

REPO_SOURCE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/ralph-pipeline-locked-scope.XXXXXX")"
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

dir="$TMPDIR_ROOT/locked"
mkdir -p "$dir/repo" "$dir/snapshot"
cd "$dir/repo"
git init -q

cp "$REPO_SOURCE/bin/tests/ralph-pipeline" "$dir/snapshot/ralph-pipeline"
chmod +x "$dir/snapshot/ralph-pipeline"

cat >"$dir/snapshot/ralph-orchestrate" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
touch "$repo_root/orchestrate-ran"
exit 0
EOF

cat >"$dir/snapshot/ralph-worker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "worker should not run for locked scoped completion" >&2
exit 1
EOF

cat >"$dir/snapshot/ralph-closeout" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
echo "closeout should not run for locked scoped completion" >&2
exit 1
EOF

chmod +x \
  "$dir/snapshot/ralph-orchestrate" \
  "$dir/snapshot/ralph-worker" \
  "$dir/snapshot/ralph-closeout"

cat >"$dir/fake-dot-agents" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "workflow" && "${2:-}" == "complete" && "${3:-}" == "--json" && "${4:-}" == "--plan" ]]; then
  cat <<'JSON'
{"scope":["p1"],"state":"locked"}
JSON
  exit 0
fi
echo "unexpected fake dot-agents invocation: $*" >&2
exit 1
EOF
chmod +x "$dir/fake-dot-agents"

out_file="$dir/pipeline.out"
set +e
RALPH_PIPELINE_SNAPSHOT_DIR="$dir/snapshot" \
DOT_AGENTS="$dir/fake-dot-agents" \
RALPH_NO_LOG=1 \
RALPH_RUN_PLAN="p1" \
"$dir/snapshot/ralph-pipeline" >"$out_file" 2>&1
rc=$?
set -e

if [[ $rc -ne 0 ]]; then
  echo "FAIL: locked scoped completion should exit 0, got $rc" >&2
  cat "$out_file" >&2
  exit 1
fi

if [[ -f "$dir/repo/orchestrate-ran" ]]; then
  echo "FAIL: orchestrate should not run while scoped completion is locked" >&2
  cat "$out_file" >&2
  exit 1
fi

grep -q "scoped completion locked for plan(s): p1" "$out_file" || {
  echo "FAIL: expected locked scoped completion message" >&2
  cat "$out_file" >&2
  exit 1
}

echo "PASS: locked scoped completion stops before orchestration fanout"
