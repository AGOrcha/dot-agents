#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMPDIR_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/kg-fresh-build-transaction.XXXXXX")"
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

TMP_REPO="$TMPDIR_ROOT/repo"
TMP_HOME="$TMPDIR_ROOT/home"
TMP_KG_HOME="$TMPDIR_ROOT/kg-home"
TMP_AGENTS_HOME="$TMP_HOME/.agents"
HOST_GOMODCACHE="$(go env GOMODCACHE)"
HOST_GOCACHE="$(go env GOCACHE)"

mkdir -p "$TMP_REPO/.venv/bin" "$TMP_HOME" "$TMP_KG_HOME" "$TMP_AGENTS_HOME"
ln -sf "$(command -v python3)" "$TMP_REPO/.venv/bin/python3"

cat >"$TMP_REPO/.venv/bin/code-review-graph" <<'PY'
#!/usr/bin/env python3
import os
import sqlite3
import sys
from pathlib import Path

probe = sqlite3.connect(":memory:")
if probe.isolation_level is not None:
    raise SystemExit(f"isolation_level={probe.isolation_level!r}")

repo = None
for i, arg in enumerate(sys.argv):
    if arg == "--repo" and i + 1 < len(sys.argv):
        repo = sys.argv[i + 1]
        break
if repo is None:
    raise SystemExit("missing --repo")

db_path = Path(repo) / ".code-review-graph" / "graph.db"
db_path.parent.mkdir(parents=True, exist_ok=True)
conn = sqlite3.connect(db_path)
conn.execute("CREATE TABLE IF NOT EXISTS nodes (file_path TEXT, language TEXT, updated_at TEXT)")
conn.execute("CREATE TABLE IF NOT EXISTS edges (id INTEGER)")
conn.execute("DELETE FROM nodes")
conn.execute("DELETE FROM edges")
conn.execute("INSERT INTO nodes (file_path, language, updated_at) VALUES (?, ?, ?)", ("a.go", "go", "2026-04-20T00:00:00Z"))
conn.execute("INSERT INTO edges (id) VALUES (1)")
conn.commit()
print("build ok")
PY
chmod +x "$TMP_REPO/.venv/bin/code-review-graph"

cat >"$TMP_REPO/a.go" <<'EOF'
package main
func main() {}
EOF

build_json="$TMPDIR_ROOT/build.json"
status_json="$TMPDIR_ROOT/status.json"

(
  cd "$REPO_ROOT"
  env HOME="$TMP_HOME" AGENTS_HOME="$TMP_AGENTS_HOME" KG_HOME="$TMP_KG_HOME" GOMODCACHE="$HOST_GOMODCACHE" GOCACHE="$HOST_GOCACHE" \
    go run ./cmd/dot-agents --json kg build --repo "$TMP_REPO" >"$build_json"
  env HOME="$TMP_HOME" AGENTS_HOME="$TMP_AGENTS_HOME" KG_HOME="$TMP_KG_HOME" GOMODCACHE="$HOST_GOMODCACHE" GOCACHE="$HOST_GOCACHE" \
    go run ./cmd/dot-agents --json kg code-status --repo "$TMP_REPO" >"$status_json"
)

python3 - <<'PY' "$build_json" "$status_json"
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    build = json.load(fh)
with open(sys.argv[2], encoding="utf-8") as fh:
    status = json.load(fh)

assert build["outcome"] == "ready", build
assert status["state"] == "ready", status
assert status["ready"] is True, status
assert status["nodes"] == 1, status
assert status["files"] == 1, status
PY

echo "PASS: fresh HOME/AGENTS_HOME/KG_HOME build succeeds without nested transaction failure"
