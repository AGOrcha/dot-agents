#!/bin/bash
# da verification script
# Quick smoke test of all CLI commands

set -uo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

# Repo root (this script lives in scripts/). Used to anchor relative binary
# invocations so they still resolve after a subshell `cd` into a fixture dir.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Find da binary.
# DOT_AGENTS         — invocation used from the repo root (back-compat).
# DOT_AGENTS_ABS     — location-independent invocation, safe to run after a
#                      `cd` into a temp fixture directory (config-v2 live
#                      commands read .agentsrc.json from the working dir).
if [[ -x "./bin/da" ]]; then
  DOT_AGENTS="./bin/da"
  DOT_AGENTS_ABS="${REPO_ROOT}/bin/da"
elif command -v da >/dev/null 2>&1; then
  DOT_AGENTS="da"
  DOT_AGENTS_ABS="da"
elif command -v go >/dev/null 2>&1; then
  DOT_AGENTS="go run ./cmd/da"
  DOT_AGENTS_ABS="go run ${REPO_ROOT}/cmd/da"
else
  echo -e "${RED}Error: da not found${NC}" >&2
  exit 1
fi

echo -e "${BOLD}da Verification Script${NC}"
echo -e "Binary: $DOT_AGENTS"
echo ""

passed=0
failed=0

test_command() {
  local name="$1"
  local cmd="$2"
  local expect_success="${3:-true}"

  echo -n "  Testing $name... "
  if eval "$cmd" >/dev/null 2>&1; then
    if [[ "$expect_success" = "true" ]]; then
      echo -e "${GREEN}✓${NC}"
      passed=$((passed + 1))
    else
      echo -e "${RED}✗ (should have failed)${NC}"
      failed=$((failed + 1))
    fi
  else
    if [[ "$expect_success" = "false" ]]; then
      echo -e "${GREEN}✓ (expected failure)${NC}"
      passed=$((passed + 1))
    else
      echo -e "${RED}✗${NC}"
      failed=$((failed + 1))
    fi
  fi

  return 0
}

# assert_contains NAME CMD NEEDLE
# Runs CMD (capturing stdout+stderr) and passes only if the output contains the
# literal NEEDLE. Content-level assertion — proves the command returned the
# expected data, not just exit 0. Updates the shared pass/fail counters.
assert_contains() {
  local name="$1"
  local cmd="$2"
  local needle="$3"
  local out

  echo -n "  Asserting $name... "
  out="$(eval "$cmd" 2>&1)"
  if printf '%s' "$out" | grep -qF -- "$needle"; then
    echo -e "${GREEN}✓${NC}"
    passed=$((passed + 1))
  else
    echo -e "${RED}✗ (missing '${needle}')${NC}"
    failed=$((failed + 1))
  fi

  return 0
}

# assert_json_field NAME CMD PYEXPR
# Runs CMD (expected to emit JSON on stdout) and evaluates PYEXPR in a python3
# snippet where `d` is the parsed JSON. Passes when PYEXPR is truthy. Lets the
# smoke assert structured fields/values rather than raw substrings.
assert_json_field() {
  local name="$1"
  local cmd="$2"
  local pyexpr="$3"

  echo -n "  Asserting $name... "
  if eval "$cmd" 2>/dev/null | python3 -c "import sys,json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(1)
sys.exit(0 if (${pyexpr}) else 1)" 2>/dev/null; then
    echo -e "${GREEN}✓${NC}"
    passed=$((passed + 1))
  else
    echo -e "${RED}✗ (json check failed: ${pyexpr})${NC}"
    failed=$((failed + 1))
  fi

  return 0
}

echo -e "${BOLD}Basic Commands${NC}"
test_command "--version" "$DOT_AGENTS --version"
test_command "--help" "$DOT_AGENTS --help"
test_command "version --json" "$DOT_AGENTS --version --json"

echo ""
echo -e "${BOLD}Core Commands${NC}"
test_command "status" "$DOT_AGENTS status"
test_command "status --json" "$DOT_AGENTS status --json"
test_command "status --audit" "$DOT_AGENTS status --audit"
test_command "doctor" "$DOT_AGENTS doctor"
test_command "doctor --json" "$DOT_AGENTS doctor --json"
test_command "refresh --help" "$DOT_AGENTS refresh --help"
test_command "import --help" "$DOT_AGENTS import --help"
test_command "install --help" "$DOT_AGENTS install --help"
# Content assertion on the reshaped read-only status surface: the JSON payload
# must expose the canonical top-level keys (fleet/link-health view).
assert_json_field "status --json keys" "$DOT_AGENTS status --json" \
  "isinstance(d, dict) and 'canonical_store' in d and 'projects' in d"

echo ""
echo -e "${BOLD}Info Commands${NC}"
test_command "explain" "$DOT_AGENTS explain"
test_command "explain --help" "$DOT_AGENTS explain --help"
test_command "workflow --help" "$DOT_AGENTS workflow --help"
test_command "review --help" "$DOT_AGENTS review --help"
test_command "kg --help" "$DOT_AGENTS kg --help"

echo ""
echo -e "${BOLD}Config-v2 Help Commands${NC}"
# Help paths run anywhere (no .agentsrc.json required) and prove the
# config-v2 subtree is wired into the built binary's command surface.
test_command "config --help" "$DOT_AGENTS config --help"
test_command "config explain --help" "$DOT_AGENTS config explain --help"
test_command "config sync --help" "$DOT_AGENTS config sync --help"
test_command "config lint --help" "$DOT_AGENTS config lint --help"
test_command "config verify --help" "$DOT_AGENTS config verify --help"

echo ""
echo -e "${BOLD}Config-v2 + Mutations (isolated managed home)${NC}"
# The mutating config-v2 + install + refresh paths need a real managed
# ~/.agents and a working-directory manifest. Build a fully isolated home
# under a temp dir (own HOME/AGENTS_HOME/KG_HOME) so nothing touches the
# caller's real config and the steps stay hermetic + cross-OS. The manifest
# declares a LOCAL extends layer so lint/sync/explain have real content to act
# on. The binary is resolved to DOT_AGENTS_ABS so a relative ./bin/da survives
# the cd into the project dir.
SMOKE_ROOT="$(mktemp -d 2>/dev/null || mktemp -d -t da-smoke)"
export HOME="${SMOKE_ROOT}/home"
export AGENTS_HOME="${SMOKE_ROOT}/home/.agents"
export KG_HOME="${SMOKE_ROOT}/home/.kg"
mkdir -p "${HOME}"
PROJ="${SMOKE_ROOT}/proj"
mkdir -p "${PROJ}/layers"

# Base layer: declares project name, a skill, and two feature flags. The repo
# layer below OVERRIDES `project` and feature `alpha`, leaving `skills` and
# feature `beta` to win from the base — so provenance has something real to
# resolve across layers.
cat > "${PROJ}/layers/base.json" <<'JSON'
{
  "version": 1,
  "project": "base-project",
  "skills": ["base-skill"],
  "features": { "alpha": "from-base", "beta": "base-only" }
}
JSON
# Repo-local manifest: declares the local source + extends the base layer, and
# overrides `project` + feature `alpha`.
write_manifest() {
  cat > "${PROJ}/.agentsrc.json" <<'JSON'
{
  "version": 1,
  "sources": [ { "type": "local", "id": "localbase", "path": "./layers" } ],
  "extends": ["localbase:base.json"],
  "project": "override-project",
  "features": { "alpha": "from-repo" }
}
JSON
  return 0
}
write_manifest

# Bootstrap the managed home, then run the config-v2 lifecycle in the project.
test_command "init --yes (isolated)" "$DOT_AGENTS_ABS init --yes"

# ── Layer resolution + provenance combinations ───────────────────────────────
# Valid rc lints clean.
assert_contains "config lint (valid rc) -> OK" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "OK"

# Overridden field: value comes from repo-local, NOT the base layer.
assert_json_field "explain project (overridden) value+origin" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain project --json)" \
  "d.get('value') == 'override-project' and d.get('active_layer') == 'repo-local'"
assert_contains "explain project --origin-only -> repo-local" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain project --origin-only)" "repo-local"

# Base-only field: value + winning layer come from the extended base layer.
assert_json_field "explain skills (base-only) value+origin" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain skills --json)" \
  "d.get('value') == ['base-skill'] and d.get('active_layer') == 'localbase:base.json'"
assert_contains "explain skills --origin-only -> base layer" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain skills --origin-only)" "localbase:base.json"

# Full effective config (--all --json) merges the layers: project overridden,
# skills from base, and features merged (alpha overridden, beta from base).
assert_json_field "explain --all --json merges layers" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain --all --json)" \
  "d['effective']['project'] == 'override-project' and d['effective']['skills'] == ['base-skill'] and d['effective']['features'] == {'alpha': 'from-repo', 'beta': 'base-only'}"

# ── lint failure on a broken layer (then restore) ────────────────────────────
# Point extends at a missing layer file → lint must FAIL with the expected
# error text and a non-zero exit.
cat > "${PROJ}/.agentsrc.json" <<'JSON'
{
  "version": 1,
  "sources": [ { "type": "local", "id": "localbase", "path": "./layers" } ],
  "extends": ["localbase:missing.json"]
}
JSON
assert_contains "lint (missing layer) reports file not found" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "file not found"
assert_contains "lint (missing layer) verdict FAILED" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "FAILED"
test_command "lint (missing layer) exits non-zero" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "false"
# Malformed layer JSON → lint must report an invalid-JSON error.
write_manifest
printf '{ this is not valid json\n' > "${PROJ}/layers/base.json"
assert_contains "lint (malformed layer JSON) reports invalid JSON" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "invalid JSON"
test_command "lint (malformed layer) exits non-zero" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "false"
# Restore the valid base layer + manifest for the mutation lane below.
cat > "${PROJ}/layers/base.json" <<'JSON'
{
  "version": 1,
  "project": "base-project",
  "skills": ["base-skill"],
  "features": { "alpha": "from-base", "beta": "base-only" }
}
JSON
write_manifest
assert_contains "lint (restored rc) -> OK" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "OK"

# ── install materializes managed state ───────────────────────────────────────
# Which platform link trees (.claude/.cursor/AGENTS.md) install writes depends
# on platform DETECTION on the runner, so the robust cross-OS/cross-runner
# proof that install ran its full pass is the install STAMP it records in the
# lockfile (project name + stamped_at).
test_command "install --yes (isolated)" "(cd '${PROJ}' && $DOT_AGENTS_ABS install --yes)"
test_command "install wrote lock" "test -f '${PROJ}/.agentsrc.lock'"
assert_contains "install stamped the lock" "cat '${PROJ}/.agentsrc.lock'" '"install"'
assert_contains "install stamp names the project" "cat '${PROJ}/.agentsrc.lock'" "override-project"
# Re-running install (default EXACT) and install --inexact both converge
# cleanly and idempotently — the documented exact-vs-additive link passes.
# NOTE: asserting a specific managed-link is PRUNED vs KEPT requires a
# source-provided resource whose platform link type differs per OS (POSIX
# symlink vs Windows hard link), which is not hermetically stable in a
# cross-OS shell smoke; that prune/keep matrix is covered by Go unit tests
# (internal/links/managed_link_branches2_test.go). Here we assert both passes
# run clean on the managed project.
test_command "install (re-run, default exact)" "(cd '${PROJ}' && $DOT_AGENTS_ABS install --yes)"
test_command "install --inexact (additive)" "(cd '${PROJ}' && $DOT_AGENTS_ABS install --inexact --yes)"

# ── config sync writes/updates the lock; --dry-run must not (#105) ───────────
test_command "config sync (isolated)" "(cd '${PROJ}' && $DOT_AGENTS_ABS config sync)"
test_command "config sync wrote lock" "test -f '${PROJ}/.agentsrc.lock'"
assert_contains "lock has inputs_digest" "cat '${PROJ}/.agentsrc.lock'" "inputs_digest"
assert_contains "lock has the local layer unit" "cat '${PROJ}/.agentsrc.lock'" "localbase:base.json"

# verify's full contract-check verdict is OK once the lock is in sync.
assert_contains "config verify -> OK" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config verify)" "OK"

# Mutation guard (#105): config sync --dry-run must NOT rewrite the lock — the
# lock must be byte-identical before/after (cksum is POSIX + cross-OS).
LOCK_BEFORE="$(cksum "${PROJ}/.agentsrc.lock" | awk '{print $1, $2}')"
test_command "config sync --dry-run (isolated)" "(cd '${PROJ}' && $DOT_AGENTS_ABS config sync --dry-run)"
LOCK_AFTER="$(cksum "${PROJ}/.agentsrc.lock" | awk '{print $1, $2}')"
test_command "dry-run left lock byte-identical (#105)" "[ \"${LOCK_BEFORE}\" = \"${LOCK_AFTER}\" ]"

# da refresh (default exact) and refresh --inexact both exit clean.
test_command "refresh (isolated managed project)" "(cd '${PROJ}' && $DOT_AGENTS_ABS refresh)"
test_command "refresh --inexact (additive)" "(cd '${PROJ}' && $DOT_AGENTS_ABS refresh --inexact)"

echo ""
echo -e "${BOLD}KG note lane (warm + query + bridge, no crg)${NC}"
# The note lane works without code-review-graph: setup -> ingest -> warm ->
# query -> bridge. Assertions are on CONTENT, not just exit 0. Uses the same
# isolated KG_HOME so it stays hermetic and cross-OS.
test_command "kg setup" "$DOT_AGENTS_ABS kg setup"
KG_NOTE="${SMOKE_ROOT}/smoke-note.md"
cat > "${KG_NOTE}" <<'MD'
# Smoke Sentinel Note

This note exists to verify the dot-agents knowledge graph warm + query lane.
Sentinel token: ZEBRAFISH_SMOKE_TOKEN. The repo context is dot-agents.
MD
test_command "kg ingest note" "$DOT_AGENTS_ABS kg ingest '${KG_NOTE}' --type markdown --yes"
assert_contains "kg warm indexes notes" "$DOT_AGENTS_ABS kg warm" "notes indexed"
assert_contains "kg query source_lookup returns the note" \
  "$DOT_AGENTS_ABS kg query --intent source_lookup 'smoke-note'" "src-smoke-note"
assert_contains "kg query graph_health is healthy" \
  "$DOT_AGENTS_ABS kg query --intent graph_health ''" "status=healthy"
assert_contains "kg bridge health is available" \
  "$DOT_AGENTS_ABS kg bridge health" "available"

echo ""
echo -e "${BOLD}KG Help / read-only Commands${NC}"
# Help paths and read-only status paths prove the kg subtree executes on the
# live binary even without a built code graph. The CODE lane (kg build +
# content-asserting code-status/impact) needs code-review-graph and is
# exercised in CI (test.yml sets up a .venv crg), not here.
test_command "kg health --help" "$DOT_AGENTS_ABS kg health --help"
test_command "kg code-status --help" "$DOT_AGENTS_ABS kg code-status --help"
test_command "kg bridge --help" "$DOT_AGENTS_ABS kg bridge --help"
test_command "kg code-status" "$DOT_AGENTS_ABS kg code-status"

# Cleanup the isolated home so repeat local runs start fresh.
rm -rf "${SMOKE_ROOT}"

echo ""
echo -e "${BOLD}Feature Commands${NC}"
test_command "skills --help" "$DOT_AGENTS skills --help"
test_command "agents --help" "$DOT_AGENTS agents --help"
test_command "hooks --help" "$DOT_AGENTS hooks --help"
test_command "sync --help" "$DOT_AGENTS sync --help"

echo ""
echo -e "${BOLD}Dry-run Commands${NC}"
test_command "init --dry-run" "$DOT_AGENTS init --dry-run"
test_command "add /tmp --dry-run" "$DOT_AGENTS add /tmp --dry-run"

echo ""
echo -e "${BOLD}Help Commands${NC}"
test_command "init --help" "$DOT_AGENTS init --help"
test_command "add --help" "$DOT_AGENTS add --help"
test_command "remove --help" "$DOT_AGENTS remove --help"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${BOLD}Results:${NC} ${GREEN}$passed passed${NC}, ${RED}$failed failed${NC}"
echo ""

if [[ $failed -gt 0 ]]; then
  echo -e "${RED}Some tests failed!${NC}"
  exit 1
else
  echo -e "${GREEN}All tests passed!${NC}"
  exit 0
fi
