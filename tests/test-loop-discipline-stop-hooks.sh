#!/usr/bin/env bash
# Smoke test: loop-discipline stop-hooks end-to-end native remediation and
# advisory behavior.
#
# Exercises the three shipped loop-discipline hook bundles
# (internal/scaffold/hooks/global/{iteration-close-gate,isp-gate,
# loop-worker-gate}) against the real `da workflow hook-sentinel` CLI, proving
# the wired gates perform the intended native remediation / advisory behavior
# described in the P2 + P4 contracts and the plan's P5 required cases:
#
#   1. loop-worker-gate / subagent_stop : out-of-scope edit => hard block
#      (R3.1) naming the scope violation, with the native block JSON for
#      Claude/Codex/Copilot and the `followup_message` shape for Cursor.
#   2. iteration-close-gate / stop       : missing expected artifact => hard
#      block (R1.1). Trace-backed rules (R1.4/R1.6) are NOT enforced here;
#      the gate emits a documented coverage advisory and the test asserts the
#      advisory text rather than labeling an inferred transcript rule as a
#      block (P5 case 2 "no portable trace consumption" branch).
#   3. loop-worker-gate / subagent_stop : dirty-but-in-scope state => success
#      (exit 0) with an explanatory advisory on stderr (soft condition).
#   4. PreToolUse forbidden orchestrator command => hard block before the
#      action (iteration-close.R1.8 `workflow advance`,
#      loop-worker.R3.9 `workflow advance|orient|next|status`).
#   5. SubagentStart bootstrap + PreCompact continuity => non-blocking
#      advisory output (exit 0), no false completed-work claim, no compaction
#      block.
#   6. `da init` into a sandbox AGENTS_HOME materializes the three skill
#      bundles, the loop-worker agent + profile, and the three hook bundles
#      without directory-replacement warnings.
#
# Native remediation output is asserted per platform: Claude/Codex/Copilot
# emit {"decision":"block",...}; Cursor emits {"followup_message":...}.
# Advisories are stderr text with a zero exit. The test distinguishes
# proof-backed blocking from advisory-only missing-trace coverage.
#
# Harness: every filesystem effect is rooted under a single mktemp dir that is
# removed on exit; the real $HOME / ~/.agents is never touched (AGENTS_HOME is
# redirected into the sandbox). No jq/python3 dependency — the gates and this
# test use portable shell + git only.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DA="${DA:-${REPO_ROOT}/bin/da}"

HOOKS_SRC="${REPO_ROOT}/internal/scaffold/hooks/global"
ICG="${HOOKS_SRC}/iteration-close-gate/gate.sh"
ISPG="${HOOKS_SRC}/isp-gate/gate.sh"
LWG="${HOOKS_SRC}/loop-worker-gate/gate.sh"

# Native hard-block marker emitted by the Claude/Codex/Copilot platforms.
readonly BLOCK_JSON='"decision":"block"'

if [[ ! -x "$DA" ]]; then
  echo "SKIP: da binary not found at $DA (run 'make build' or set DA= to override)" >&2
  exit 0
fi
for g in "$ICG" "$ISPG" "$LWG"; do
  if [[ ! -f "$g" ]]; then
    echo "FAIL: required gate script missing: $g" >&2
    exit 1
  fi
done

WORK="$(mktemp -d "${TMPDIR:-/tmp}/test-loop-discipline.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# Isolate config/prefs and the materialized home from the real $HOME and
# ~/.agents. The gates resolve `da` from PATH; expose the built binary's dir.
export AGENTS_HOME="$WORK/agents-home"
DA_DIR="$(dirname "$DA")"
export PATH="$DA_DIR:$PATH"

PASS=0
fail() { echo "FAIL: $*" >&2; exit 1; }
ok() { echo "PASS: $*"; PASS=$((PASS + 1)); }

# fresh_repo creates an isolated git sandbox under $WORK and echoes its path.
# Each scenario gets its own repo so dirty-state assertions do not leak.
fresh_repo() {
  local name="$1"
  local dir="$WORK/$name"
  mkdir -p "$dir"
  (
    cd "$dir"
    git init -q
    git config user.email "loopdisc@example.com"
    git config user.name "loop-discipline-e2e"
  )
  printf '%s\n' "$dir"
}

# run_gate <gate> <repo> <when> <payload> <platform> -> writes stdout to
# $G_OUT, stderr to $G_ERR, sets $G_RC. Drains a JSON payload on stdin exactly
# as the platform launcher would.
run_gate() {
  local gate="$1" repo="$2" when="$3" payload="$4" platform="${5:-claude}"
  G_OUT="$WORK/.gate-out"
  G_ERR="$WORK/.gate-err"
  set +e
  (
    cd "$repo"
    printf '%s' "$payload" | DA_HOOK_PLATFORM="$platform" sh "$gate" "$when"
  ) >"$G_OUT" 2>"$G_ERR"
  G_RC=$?
  set -e
}

# ── case 1: loop-worker out-of-scope edit => hard block at subagent_stop ──────
r="$(fresh_repo lw-scope)"
(
  cd "$r"
  "$DA" workflow hook-sentinel write loop-worker \
    --run-id r1 --plan dp --task t1 --agent-type loop-worker \
    --write-scope src/ >/dev/null
  mkdir -p src
)
# An out-of-scope file (outside src/) must trip R3.1.
echo "out of scope" >"$r/escapee.txt"
run_gate "$LWG" "$r" subagent_stop '{}' claude
[[ "$G_RC" -eq 2 ]] || fail "case1: expected hard-block exit 2, got $G_RC (stderr: $(cat "$G_ERR"))"
grep -q "$BLOCK_JSON" "$G_OUT" || fail "case1: missing Claude native block JSON ($(cat "$G_OUT"))"
grep -q 'escapee.txt' "$G_OUT" || fail "case1: block reason did not name the out-of-scope path"
grep -q 'loop-worker.R3.1' "$G_OUT" || fail "case1: block reason did not cite rule loop-worker.R3.1"
ok "case1: loop-worker out-of-scope edit => native block naming the scope violation (R3.1)"

# Same violation, Cursor platform => native followup_message, not decision.
run_gate "$LWG" "$r" subagent_stop '{}' cursor
[[ "$G_RC" -eq 2 ]] || fail "case1b: expected exit 2 for cursor, got $G_RC"
grep -q '"followup_message"' "$G_OUT" || fail "case1b: cursor must emit followup_message ($(cat "$G_OUT"))"
grep -q '"decision"' "$G_OUT" && fail "case1b: cursor must NOT emit a decision block"
ok "case1b: same violation on Cursor emits native followup_message"

# ── case 2: iteration-close missing artifact => block; trace rule = advisory ─
r="$(fresh_repo ic-terminal)"
(
  cd "$r"
  "$DA" workflow hook-sentinel write iteration-close \
    --run-id ic1 --plan dp --task t1 --agent-type main \
    --expect merge-back.md >/dev/null
)
# merge-back.md is intentionally absent => portable hard remediation (R1.1).
run_gate "$ICG" "$r" stop '{}' claude
[[ "$G_RC" -eq 2 ]] || fail "case2: expected hard-block exit 2, got $G_RC"
grep -q "$BLOCK_JSON" "$G_OUT" || fail "case2: missing native block JSON"
grep -q 'merge-back.md' "$G_OUT" || fail "case2: block reason did not name the missing artifact"
grep -q 'iteration-close.R1.1' "$G_OUT" || fail "case2: block reason did not cite rule iteration-close.R1.1"
ok "case2: iteration-close missing artifact => native block (R1.1)"

# Trace-backed coverage: with the artifact present, the terminal gate must
# NOT block on the inferred R1.4/R1.6 transcript rules. No portable trace
# consumption ships, so the gate emits a coverage advisory and exits 0. We
# assert the advisory rather than a block (P5 case 2, hard-trace pending).
echo "merge-back recorded" >"$r/merge-back.md"
(cd "$r" && git add -A && git commit -qm "artifact" >/dev/null)
run_gate "$ICG" "$r" stop '{}' claude
[[ "$G_RC" -eq 0 ]] || fail "case2b: artifact present must exit 0, got $G_RC (out: $(cat "$G_OUT"))"
grep -q "$BLOCK_JSON" "$G_OUT" && fail "case2b: must not block when artifact present (no enforced trace rule)"
grep -qi 'deferred' "$G_ERR" || fail "case2b: expected trace-coverage advisory on stderr ($(cat "$G_ERR"))"
grep -q 'R1.4' "$G_ERR" || fail "case2b: advisory did not name the deferred trace rule R1.4"
ok "case2b: trace-backed rule reported as advisory coverage, not an enforced block (R1.4 deferred)"

# ── case 3: loop-worker dirty-but-in-scope => success with explanatory stderr ─
r="$(fresh_repo lw-soft)"
(
  cd "$r"
  # Scope covers both the in-scope edit dir and the sentinel dir so the only
  # dirty paths are in scope; this is the soft (advisory) condition.
  "$DA" workflow hook-sentinel write loop-worker \
    --run-id r3 --plan dp --task t1 --agent-type loop-worker \
    --write-scope src/ --write-scope .agents/ >/dev/null
  mkdir -p src
)
echo "in scope change" >"$r/src/feature.txt"
run_gate "$LWG" "$r" subagent_stop '{}' claude
[[ "$G_RC" -eq 0 ]] || fail "case3: in-scope dirty state must exit 0, got $G_RC (out: $(cat "$G_OUT"))"
grep -q "$BLOCK_JSON" "$G_OUT" && fail "case3: in-scope change must not block"
grep -qi 'advisory' "$G_ERR" || fail "case3: expected explanatory advisory on stderr ($(cat "$G_ERR"))"
ok "case3: loop-worker dirty-but-in-scope => success with explanatory stderr advisory"

# ── case 4: PreToolUse forbidden command => prevention before action ─────────
r="$(fresh_repo pre-tool)"
(
  cd "$r"
  "$DA" workflow hook-sentinel write loop-worker \
    --run-id r4 --plan dp --task t1 --agent-type loop-worker \
    --write-scope src/ >/dev/null
  "$DA" workflow hook-sentinel write iteration-close \
    --run-id ic4 --plan dp --task t1 --agent-type main \
    --expect merge-back.md >/dev/null
)
# loop-worker may not run any orchestrator command (R3.9).
run_gate "$LWG" "$r" pre_tool_use '{"command":"da workflow advance"}' claude
[[ "$G_RC" -eq 2 ]] || fail "case4a: loop-worker PreToolUse must block, got $G_RC"
grep -q "$BLOCK_JSON" "$G_OUT" || fail "case4a: missing native block JSON"
grep -q 'loop-worker.R3.9' "$G_OUT" || fail "case4a: block did not cite rule loop-worker.R3.9"
ok "case4a: loop-worker PreToolUse 'workflow advance' => native prevention (R3.9)"

# A non-orchestrator command must NOT be prevented (no false positive).
run_gate "$LWG" "$r" pre_tool_use '{"command":"git status"}' claude
[[ "$G_RC" -eq 0 ]] || fail "case4b: benign PreToolUse command must exit 0, got $G_RC"
grep -q "$BLOCK_JSON" "$G_OUT" && fail "case4b: benign command must not be blocked"
ok "case4b: loop-worker PreToolUse benign command => allowed (no false prevention)"

# Delegated iteration-close may not call `workflow advance` (R1.8).
run_gate "$ICG" "$r" pre_tool_use '{"command":"da workflow advance"}' claude
[[ "$G_RC" -eq 2 ]] || fail "case4c: iteration-close PreToolUse advance must block, got $G_RC"
grep -q "$BLOCK_JSON" "$G_OUT" || fail "case4c: missing native block JSON"
grep -q 'iteration-close.R1.8' "$G_OUT" || fail "case4c: block did not cite rule iteration-close.R1.8"
ok "case4c: delegated iteration-close PreToolUse 'workflow advance' => native prevention (R1.8)"

# ── case 5: SubagentStart bootstrap + PreCompact continuity (non-blocking) ───
r="$(fresh_repo continuity)"
(
  cd "$r"
  "$DA" workflow hook-sentinel write loop-worker \
    --run-id r5 --plan dp --task t1 --agent-type loop-worker \
    --write-scope src/ >/dev/null
  "$DA" workflow hook-sentinel write iteration-close \
    --run-id ic5 --plan dp --task t1 --agent-type main \
    --expect merge-back.md >/dev/null
  "$DA" workflow hook-sentinel write isp \
    --run-id isp5 --plan dp --task t1 --agent-type main \
    --expect bundle.yaml >/dev/null
)
# SubagentStart: bootstrap advice only, never a completed-work claim or block.
run_gate "$LWG" "$r" subagent_start '{}' claude
[[ "$G_RC" -eq 0 ]] || fail "case5a: SubagentStart must be non-blocking (exit 0), got $G_RC"
[[ -s "$G_OUT" ]] && fail "case5a: SubagentStart must emit no native block payload on stdout"
grep -qi 'bootstrap' "$G_ERR" || fail "case5a: SubagentStart should emit bootstrap context on stderr"
grep -qi 'verified at SubagentStop' "$G_ERR" || fail "case5a: SubagentStart must not claim completed work (defers verification to SubagentStop)"
ok "case5a: loop-worker SubagentStart => bootstrap advisory only, no block, no completed-work claim"

# PreCompact during active sentinels: continuity advice, never a block.
check_precompact() {
  local gate="$1" label="$2"
  run_gate "$gate" "$r" pre_compact '{}' claude
  [[ "$G_RC" -eq 0 ]] || fail "case5b: PreCompact ($label) must not block (exit 0), got $G_RC"
  [[ -s "$G_OUT" ]] && fail "case5b: PreCompact ($label) must emit no block payload on stdout"
  grep -qi 'advisory' "$G_ERR" || fail "case5b: PreCompact ($label) expected continuity advisory ($(cat "$G_ERR"))"
}
check_precompact "$LWG" loop-worker
check_precompact "$ICG" iteration-close
check_precompact "$ISPG" isp
ok "case5b: PreCompact on all three gates => continuity advisory only, no compaction block"

# ── case 6: da init materializes the shipped bundles into a sandbox home ─────
INIT_LOG="$WORK/.init-log"
set +e
"$DA" init --yes >"$INIT_LOG" 2>&1
INIT_RC=$?
set -e
[[ "$INIT_RC" -eq 0 ]] || fail "case6: da init returned $INIT_RC (log: $(cat "$INIT_LOG"))"

# No bundle/scaffold directory-replacement or overwrite warnings during a
# clean scaffold into an empty AGENTS_HOME. Scope the scan to the
# scaffold/bundle/skill/hook surface so the unrelated ~/.claude global
# settings-symlink notice (which is about $HOME, not the materialized bundles)
# does not produce a false failure.
if grep -iE 'replac|overwrit' "$INIT_LOG" \
  | grep -iE 'bundle|skill|hook|scaffold|starter|agent|profile' \
  | grep -q .; then
  fail "case6: da init emitted a bundle/scaffold replacement warning ($(grep -iE 'replac|overwrit' "$INIT_LOG" | grep -iE 'bundle|skill|hook|scaffold|starter|agent|profile'))"
fi

for hb in iteration-close-gate isp-gate loop-worker-gate; do
  [[ -f "$AGENTS_HOME/hooks/global/$hb/HOOK.yaml" ]] || fail "case6: missing hook bundle manifest $hb/HOOK.yaml"
  [[ -f "$AGENTS_HOME/hooks/global/$hb/gate.sh" ]] || fail "case6: missing hook bundle script $hb/gate.sh"
done
for sk in iteration-close isp loop-worker; do
  [[ -f "$AGENTS_HOME/skills/global/$sk/SKILL.md" ]] || fail "case6: missing skill bundle $sk/SKILL.md"
done
[[ -d "$AGENTS_HOME/agents/global/loop-worker" ]] || fail "case6: missing loop-worker agent bundle"
[[ -f "$AGENTS_HOME/profiles/loop-worker.md" ]] || fail "case6: missing loop-worker profile"
ok "case6: da init materialized 3 hook bundles + 3 skills + loop-worker agent/profile (no replacement warnings)"

# P4 wiring sanity: the sentinel-write entry call is present in the promoted
# starter skills (proves the gates have a sentinel to read at runtime).
for sk in iteration-close isp loop-worker; do
  if ! grep -rq "hook-sentinel write" "$AGENTS_HOME/skills/global/$sk/"; then
    fail "case6b: starter skill $sk missing 'hook-sentinel write' wiring (P4)"
  fi
done
ok "case6b: promoted starter skills wire 'da workflow hook-sentinel write' at entry (P4)"

echo "PASS: loop-discipline stop-hooks end-to-end ($PASS assertions across native remediation, prevention, and advisory paths)"
