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
echo -e "${BOLD}Config-v2 Live Commands${NC}"
# config explain/sync/lint/verify read the effective .agentsrc.json from the
# current directory. Run them inside a throwaway fixture dir holding a minimal
# valid manifest ({"version": 1}) so they exercise the real code path and exit
# cleanly without depending on the repo's own (possibly drifted) manifest.
# The fixture dir is created under $TMPDIR (cross-OS; Git Bash maps it) and
# removed afterward, so nothing is written outside a temp location.
CONFIG_FIXTURE="$(mktemp -d 2>/dev/null || mktemp -d -t da-smoke)"
printf '{\n  "version": 1\n}\n' > "${CONFIG_FIXTURE}/.agentsrc.json"
test_command "config lint (fixture)" "(cd '${CONFIG_FIXTURE}' && $DOT_AGENTS_ABS config lint)"
test_command "config explain (fixture)" "(cd '${CONFIG_FIXTURE}' && $DOT_AGENTS_ABS config explain)"
test_command "config explain --all (fixture)" "(cd '${CONFIG_FIXTURE}' && $DOT_AGENTS_ABS config explain --all)"
test_command "config explain --json (fixture)" "(cd '${CONFIG_FIXTURE}' && $DOT_AGENTS_ABS config explain --json)"
test_command "config explain repo_id (fixture)" "(cd '${CONFIG_FIXTURE}' && $DOT_AGENTS_ABS config explain repo_id)"
test_command "config explain --all --json (fixture)" "(cd '${CONFIG_FIXTURE}' && $DOT_AGENTS_ABS config explain --all --json)"
test_command "config sync (fixture)" "(cd '${CONFIG_FIXTURE}' && $DOT_AGENTS_ABS config sync)"
test_command "config verify (fixture)" "(cd '${CONFIG_FIXTURE}' && $DOT_AGENTS_ABS config verify)"
rm -rf "${CONFIG_FIXTURE}"

echo ""
echo -e "${BOLD}KG Commands${NC}"
# Help paths and read-only status paths prove the kg subtree executes on the
# live binary. code-status and bridge health report UNBUILT/empty cleanly
# without a built code graph (no .venv crg tooling required), so they stay
# hermetic and cross-OS. Heavier paths (build/update/query) need real graph
# infra and are intentionally NOT smoked here.
test_command "kg health --help" "$DOT_AGENTS kg health --help"
test_command "kg code-status --help" "$DOT_AGENTS kg code-status --help"
test_command "kg bridge --help" "$DOT_AGENTS kg bridge --help"
test_command "kg code-status" "$DOT_AGENTS kg code-status"
test_command "kg bridge health" "$DOT_AGENTS kg bridge health"

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
