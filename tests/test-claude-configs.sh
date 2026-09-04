#!/bin/bash
# test-claude-configs.sh
# Run inside Docker container after authentication
#
# This script tests that dot-agents config files actually influence
# Claude Code's behavior - not just that files exist, but that they work.

set -e

# Hermeticity guard: this script writes UNCONDITIONALLY into ~/.agents and
# ~/.claude (mkdir -p ~/.agents/skills/global/docker-verify, overwrites
# ~/.agents/settings/global/claude-code.json) — by design, since it proves
# Claude Code actually reads those real, live config locations, not a
# sandboxed copy. That only stays safe inside the disposable "sandbox" user
# tests/Dockerfile.sandbox creates (USER sandbox, HOME=/home/sandbox, see
# tests/SANDBOX.md). Run this on a developer machine or a shared CI runner by
# mistake and it pollutes/clobbers the real ~/.agents tree — this is the exact
# class of test-isolation defect documented in
# .agents/lessons/hermetic-home-for-state-resolving-tests/LESSON.md (the
# my-skill/idem-skill/extra-skill/demo-skill fixture-skill leaks were the
# unguarded-t.Setenv variant of the same mistake; this script is the
# unguarded-real-binary variant). Refuse to run unless $HOME clearly is the
# dedicated sandbox user's disposable home.
if [[ "$(id -un 2>/dev/null)" != "sandbox" || "$HOME" != "/home/sandbox" ]]; then
  echo "REFUSING TO RUN: this script writes into \$HOME/.agents and \$HOME/.claude" >&2
  echo "unconditionally and must only run as the disposable 'sandbox' user inside" >&2
  echo "tests/Dockerfile.sandbox (see tests/SANDBOX.md), never on a developer or CI" >&2
  echo "host. Current user=$(id -un 2>/dev/null || echo '?') HOME=${HOME:-<unset>}." >&2
  exit 1
fi

PASS=0
FAIL=0

# Separator bar used in test section headers and the summary block
readonly SEP_BAR='━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━'

log_test() {
  local test_name="$1"
  echo ""
  echo "$SEP_BAR"
  echo "TEST: $test_name"
  echo "$SEP_BAR"
}

log_pass() { local msg="$1"; echo "✅ PASS: $msg"; ((PASS++)); }
log_fail() { local msg="$1"; echo "❌ FAIL: $msg"; ((FAIL++)); }

# Setup
echo "Setting up test environment..."
cd /workspace
rm -rf test-project
mkdir -p test-project
cd test-project
git init -q

da init --yes 2>/dev/null || true
da add /workspace/test-project --name test-proj --yes 2>/dev/null || true

###########################################
# TEST 1: CLAUDE.md Rules
###########################################
log_test "CLAUDE.md Rules Apply (uppercase response)"

cat > CLAUDE.md << 'EOF'
CRITICAL: Respond ONLY in UPPERCASE. Never use lowercase.
EOF

response=$(echo "Say the word hello" | timeout 60 claude --print 2>/dev/null || echo "TIMEOUT")
echo "Response: $response"

if [[ "$response" = "TIMEOUT" ]]; then
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

if [[ -f testfile.txt ]]; then
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

da add /workspace/test-project --force --yes 2>/dev/null || true

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

if [[ -f /tmp/hook.log ]]; then
  log_pass "Hook fired"
  cat /tmp/hook.log
else
  log_fail "Hook did not fire"
fi

###########################################
# SUMMARY
###########################################
echo ""
echo "$SEP_BAR"
echo "RESULTS: $PASS passed, $FAIL failed"
echo "$SEP_BAR"

[[ $FAIL -eq 0 ]] && exit 0 || exit 1
