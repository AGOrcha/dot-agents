#!/usr/bin/env bash
# Blocking cross-family adversarial gate (RULE 7) — two-phase, blind pre-registration.
# GPT/Codex reviewer vs Claude-family executors. Read-only. No commits (verdict left for review).
# Runnable anytime the codex GPT quota is available. Safe to re-run (idempotent outputs).
#
# Exit codes: 0 verdict produced | 2 BLOCKED (quota) | 3 cd failed | 4 phase1 parse miss |
#             5 phase2 parse miss.
# IMPORTANT: parse FIRST, only check the quota-cap phrase on a parse MISS. The reviewer cats
# artifacts (preregistered-hypotheses.json contains "resource_exhausted", the decision record
# contains "usage_limit_reached"), so those phrases legitimately appear in the session log of a
# VALID run — never treat their presence as a cap when a verdict parsed cleanly.
set -u
REPO="/Users/nikashp/proj-docs/dot-agents"
GATE="$REPO/.agents/workflow/plans/transcript-analysis-and-pipeline-craft/reviews/cross-family-gate"
OUT="$REPO/.agents/workflow/plans/transcript-analysis-and-pipeline-craft/reviews"
cd "$REPO" || exit 3

# Quota-cap phrasing — checked ONLY on a parse miss, on the CLI's own error stream first.
capped() { grep -qiE "usage limit|usage_limit_reached|resource_exhausted|rate.?limit|quota exceeded|hit your (chatgpt|usage)|try again (at|in)" "$1" 2>/dev/null; }

# --- Phase 1: blind pre-registration (no repo working root) ---
codex exec -s read-only -C /tmp --skip-git-repo-check \
  --output-schema "$GATE/phase1-schema.json" - \
  < "$GATE/phase1-brief.md" > "$GATE/phase1.out" 2> "$GATE/phase1.err"
python3 - "$GATE/phase1.out" "$GATE/phase1-hypotheses.json" <<'PY'
import sys, json, re
raw = open(sys.argv[1]).read()
objs = []
for m in re.finditer(r'\{', raw):
    depth=0
    for i in range(m.start(), len(raw)):
        if raw[i]=='{': depth+=1
        elif raw[i]=='}':
            depth-=1
            if depth==0:
                try:
                    o=json.loads(raw[m.start():i+1])
                    if isinstance(o,dict) and 'hypotheses' in o: objs.append(o)
                except Exception: pass
                break
if not objs: sys.exit(1)
json.dump(objs[-1], open(sys.argv[2],'w'), indent=2)
print("phase1 hypotheses:", len(objs[-1]['hypotheses']))
PY
if [ $? -ne 0 ]; then
  if capped "$GATE/phase1.err" || capped "$GATE/phase1.out"; then
    echo "BLOCKED: codex GPT quota capped at phase 1 ($(date -u +%FT%TZ))"; exit 2
  fi
  echo "phase1 parse miss (no hypotheses JSON, no cap phrase) ($(date -u +%FT%TZ))"; exit 4
fi

# --- Build phase 2 brief: head + frozen hypotheses ---
cp "$GATE/phase2-brief-head.md" "$GATE/phase2-brief.md"
cat "$GATE/phase1-hypotheses.json" >> "$GATE/phase2-brief.md"

# --- Phase 2: execute frozen hypotheses against the repo (read-only) ---
codex exec -s read-only -C "$REPO" \
  --output-schema "$GATE/verdict-schema.json" - \
  < "$GATE/phase2-brief.md" > "$GATE/phase2.out" 2> "$GATE/phase2.err"
python3 - "$GATE/phase2.out" "$OUT/cross-family-review.json" <<'PY'
import sys, json, re
raw = open(sys.argv[1]).read()
objs=[]
for m in re.finditer(r'\{', raw):
    depth=0
    for i in range(m.start(), len(raw)):
        if raw[i]=='{': depth+=1
        elif raw[i]=='}':
            depth-=1
            if depth==0:
                try:
                    o=json.loads(raw[m.start():i+1])
                    if isinstance(o,dict) and 'verdict' in o and 'hypotheses' in o: objs.append(o)
                except Exception: pass
                break
if not objs: sys.exit(1)
v=objs[-1]
json.dump(v, open(sys.argv[2],'w'), indent=2)
print("VERDICT:", v.get('verdict'))
ref=[h for h in v.get('hypotheses',[]) if h.get('outcome')=='refuted-the-work']
print("refuted-the-work:", len(ref))
for h in ref: print("  -", h.get('statement','')[:100])
PY
if [ $? -ne 0 ]; then
  if capped "$GATE/phase2.err" || capped "$GATE/phase2.out"; then
    echo "BLOCKED: codex GPT quota capped at phase 2 ($(date -u +%FT%TZ))"; exit 2
  fi
  echo "phase2 verdict parse miss (no cross-family-review.json written) ($(date -u +%FT%TZ))"; exit 5
fi
echo "cross-family gate complete -> $OUT/cross-family-review.json ($(date -u +%FT%TZ))"
