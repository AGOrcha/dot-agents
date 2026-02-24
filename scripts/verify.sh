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
if [ -f "./src/bin/dot-agents" ]; then
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
test_command "doctor" "$DOT_AGENTS doctor"
test_command "doctor --json" "$DOT_AGENTS doctor --json"
test_command "audit" "$DOT_AGENTS audit"
test_command "audit --json" "$DOT_AGENTS audit --json"
test_command "audit --agent github-copilot" "$DOT_AGENTS status --audit --agent github-copilot"

echo ""
echo -e "${BOLD}Info Commands${NC}"
test_command "context" "$DOT_AGENTS context"
test_command "context --compact" "$DOT_AGENTS context --compact"
test_command "context --full" "$DOT_AGENTS context --full"
test_command "explain" "$DOT_AGENTS explain"
test_command "explain rules" "$DOT_AGENTS explain rules"
test_command "explain --agent github-copilot" "$DOT_AGENTS explain --agent github-copilot"
test_command "internal" "$DOT_AGENTS internal"
test_command "internal --json" "$DOT_AGENTS internal --json"

echo ""
echo -e "${BOLD}Feature Commands${NC}"
test_command "features" "$DOT_AGENTS features"
test_command "features --json" "$DOT_AGENTS features --json"
test_command "migrate detect" "$DOT_AGENTS migrate detect"
test_command "redundancy --global-only" "$DOT_AGENTS redundancy --global-only"

echo ""
echo -e "${BOLD}Dry-run Commands${NC}"
test_command "init --dry-run" "$DOT_AGENTS init --dry-run"
test_command "add /tmp --dry-run" "$DOT_AGENTS add /tmp --dry-run"

echo ""
echo -e "${BOLD}Help Commands${NC}"
test_command "init --help" "$DOT_AGENTS init --help"
test_command "add --help" "$DOT_AGENTS add --help"
test_command "remove --help" "$DOT_AGENTS remove --help"
test_command "migrate --help" "$DOT_AGENTS migrate --help"
test_command "sync --help" "$DOT_AGENTS sync --help"
test_command "features --help" "$DOT_AGENTS features --help"
test_command "context --help" "$DOT_AGENTS context --help"
test_command "explain --help" "$DOT_AGENTS explain --help"
test_command "internal --help" "$DOT_AGENTS internal --help"
test_command "redundancy --help" "$DOT_AGENTS redundancy --help"

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
