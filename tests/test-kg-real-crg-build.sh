#!/usr/bin/env bash
# Integration test: full CLI build path with the real code-review-graph binary.
#
# Skipped when CRG is not available (no .venv and not on PATH).
# Guards the nested-transaction defect path that stub tests cannot reach:
# commandWithSQLiteAutocommit must patch isolation_level before CRG opens
# SQLite, otherwise explicit BEGIN/COMMIT collides with Python's implicit
# transaction management on a fresh database.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST_GOMODCACHE="$(go env GOMODCACHE)"
HOST_GOCACHE="$(go env GOCACHE)"

# Skip if real CRG is not available in the repo .venv or on PATH.
CRG_BIN=""
if [[ -x "$REPO_ROOT/.venv/bin/code-review-graph" ]]; then
    CRG_BIN="$REPO_ROOT/.venv/bin/code-review-graph"
elif command -v code-review-graph &>/dev/null; then
    CRG_BIN="$(command -v code-review-graph)"
fi
if [[ -z "$CRG_BIN" ]]; then
    echo "SKIP: real code-review-graph not available (set up .venv or install on PATH)"
    exit 0
fi

TMPDIR_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kg-real-crg-build.XXXXXX")"
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

TMP_HOME="$TMPDIR_ROOT/home"
TMP_KG_HOME="$TMPDIR_ROOT/kg-home"
TMP_AGENTS_HOME="$TMP_HOME/.agents"
BUILD_JSON="$TMPDIR_ROOT/build.json"
STATUS_JSON="$TMPDIR_ROOT/status.json"

mkdir -p "$TMP_HOME" "$TMP_KG_HOME" "$TMP_AGENTS_HOME"

# Build the current repo with a fresh isolated HOME/AGENTS_HOME/KG_HOME.
# --repo points at the repo root so the real .venv CRG is discovered.
(
    cd "$REPO_ROOT"
    env HOME="$TMP_HOME" AGENTS_HOME="$TMP_AGENTS_HOME" KG_HOME="$TMP_KG_HOME" \
        GOMODCACHE="$HOST_GOMODCACHE" GOCACHE="$HOST_GOCACHE" \
        go run ./cmd/da --json kg build --repo "$REPO_ROOT" \
        --skip-flows --skip-postprocess >"$BUILD_JSON"
    env HOME="$TMP_HOME" AGENTS_HOME="$TMP_AGENTS_HOME" KG_HOME="$TMP_KG_HOME" \
        GOMODCACHE="$HOST_GOMODCACHE" GOCACHE="$HOST_GOCACHE" \
        go run ./cmd/da --json kg code-status --repo "$REPO_ROOT" >"$STATUS_JSON"
)

# Validate JSON output and graph.db.
python3 - <<'PY' "$BUILD_JSON" "$STATUS_JSON" "$REPO_ROOT"
import json, sqlite3, sys
from pathlib import Path

with open(sys.argv[1]) as f:
    build = json.load(f)
with open(sys.argv[2]) as f:
    status = json.load(f)

assert build["outcome"] == "ready", f"unexpected build outcome: {build}"
assert build["status"]["nodes"] > 0, f"expected non-zero nodes: {build}"

assert status["state"] == "ready", f"unexpected code-status: {status}"
assert status["ready"] is True, status
assert status["nodes"] > 0, status

db_path = Path(sys.argv[3]) / ".code-review-graph" / "graph.db"
assert db_path.exists(), f"graph.db not found at {db_path}"

conn = sqlite3.connect(str(db_path))
(count,) = conn.execute("SELECT COUNT(*) FROM nodes").fetchone()
assert count > 0, f"graph.db has zero rows in nodes table"
conn.close()

print(f"Build: {build['summary']}")
print(f"Status: nodes={status['nodes']}, files={status['files']}, state={status['state']}")
print(f"graph.db: {count} rows in nodes table")
PY

echo "PASS: real CRG build succeeded with fresh isolated HOME/AGENTS_HOME/KG_HOME"
