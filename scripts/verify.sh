#!/bin/bash
# dot-agents verification script
# Quick smoke test of all CLI commands

set -uo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

# Find dot-agents binary
if [ -x "./bin/dot-agents" ]; then
  DOT_AGENTS="./bin/dot-agents"
elif [ -f "./src/bin/dot-agents" ]; then
  DOT_AGENTS="./src/bin/dot-agents"
elif command -v dot-agents >/dev/null 2>&1; then
  DOT_AGENTS="dot-agents"
else
  echo -e "${RED}Error: dot-agents not found${NC}"
  exit 1
fi

echo -e "${BOLD}dot-agents Verification Script${NC}"
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
    if [ "$expect_success" = "true" ]; then
      echo -e "${GREEN}✓${NC}"
      passed=$((passed + 1))
    else
      echo -e "${RED}✗ (should have failed)${NC}"
      failed=$((failed + 1))
    fi
  else
    if [ "$expect_success" = "false" ]; then
      echo -e "${GREEN}✓ (expected failure)${NC}"
      passed=$((passed + 1))
    else
      echo -e "${RED}✗${NC}"
      failed=$((failed + 1))
    fi
  fi
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

if [ $failed -gt 0 ]; then
  echo -e "${RED}Some tests failed!${NC}"
  exit 1
else
  echo -e "${GREEN}All tests passed!${NC}"
  exit 0
fi
