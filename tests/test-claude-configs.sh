#!/bin/bash
# test-claude-configs.sh
# Run inside Docker container after authentication
#
# This script tests that dot-agents config files actually influence
# Claude Code's behavior - not just that files exist, but that they work.

set -e

PASS=0
FAIL=0

log_test() {
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "TEST: $1"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

log_pass() { echo "✅ PASS: $1"; ((PASS++)); }
log_fail() { echo "❌ FAIL: $1"; ((FAIL++)); }

# Setup
echo "Setting up test environment..."
cd /workspace
rm -rf test-project
mkdir -p test-project
cd test-project
git init -q

dot-agents init --yes 2>/dev/null || true
dot-agents add /workspace/test-project --name test-proj --yes 2>/dev/null || true

###########################################
# TEST 1: CLAUDE.md Rules
###########################################
log_test "CLAUDE.md Rules Apply (uppercase response)"

cat > CLAUDE.md << 'EOF'
CRITICAL: Respond ONLY in UPPERCASE. Never use lowercase.
EOF

response=$(echo "Say the word hello" | timeout 60 claude --print 2>/dev/null || echo "TIMEOUT")
echo "Response: $response"

if [ "$response" = "TIMEOUT" ]; then
  log_fail "Claude timed out"
elif echo "$response" | grep -qE '^[^a-z]*$'; then
  log_pass "Response appears uppercase"
else
  log_fail "Response contains lowercase"
fi

###########################################
# TEST 2: Permissions Deny
###########################################
log_test "Permissions Deny (block rm command)"

mkdir -p .claude
cat > .claude/settings.local.json << 'EOF'
{"$schema":"https://json.schemastore.org/claude-code-settings.json","permissions":{"deny":["Bash(rm:*)"]}}
EOF

echo "protected" > testfile.txt
echo "Delete testfile.txt with rm" | timeout 60 claude --print 2>/dev/null || true

if [ -f testfile.txt ]; then
  log_pass "File protected - rm was denied"
else
  log_fail "File was deleted - permission not enforced"
fi

###########################################
# TEST 3: Skills
###########################################
log_test "Skills Load (/docker-verify)"

mkdir -p ~/.agents/skills/global/docker-verify
cat > ~/.agents/skills/global/docker-verify/SKILL.md << 'EOF'
---
name: Docker Verify
---
Respond with exactly: SKILL_VERIFIED_OK
EOF

dot-agents add /workspace/test-project --force --yes 2>/dev/null || true

response=$(echo "Run /docker-verify skill" | timeout 60 claude --print 2>/dev/null || echo "")
echo "Response: $response"

if echo "$response" | grep -q "SKILL_VERIFIED_OK"; then
  log_pass "Skill executed correctly"
else
  log_fail "Skill not found or wrong output"
fi

###########################################
# TEST 4: Hooks
###########################################
log_test "Hooks Fire (PreToolUse)"

cat > ~/.agents/settings/global/claude-code.json << 'EOF'
{"$schema":"https://json.schemastore.org/claude-code-settings.json","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo HOOK_OK >> /tmp/hook.log"}]}]}}
EOF

rm -f /tmp/hook.log
echo "Run: echo test" | timeout 60 claude --print 2>/dev/null || true

if [ -f /tmp/hook.log ]; then
  log_pass "Hook fired"
  cat /tmp/hook.log
else
  log_fail "Hook did not fire"
fi

###########################################
# SUMMARY
###########################################
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "RESULTS: $PASS passed, $FAIL failed"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

[ $FAIL -eq 0 ] && exit 0 || exit 1
