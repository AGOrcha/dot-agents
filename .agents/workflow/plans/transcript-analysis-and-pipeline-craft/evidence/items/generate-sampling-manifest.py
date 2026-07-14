#!/usr/bin/env python3
"""Sampling-manifest generator (pre-registered; seed 20260712).

Reproducibility note: the FROZEN sampling-manifest.json committed beside this script is
authoritative — it was drawn before the R3 evidence-id normalization, whose slug rewrite
changed lexicographic sort order, so re-running on the normalized inventory can produce a
(recorded) selection delta. This script pins the exact algorithm: CPython `random.Random`
(Mersenne Twister), stable sort by evidence_id, `rng.sample` draw order, dict-insertion
duplicate handling (a session selected by an earlier rule is not re-drawn by a later one;
rule4 draws only from the unselected pool).
"""
import json, math, random, collections, sys, os

SEED = 20260712
WF = ("payout", "dot-agents", "provadm", "roos", "agent-conf")

def bucket(row):  # project-bucket construction
    slug = (row.get("project_slug") or "").lower()
    return "workflow" if any(k in slug for k in WF) else "other"

def generate(inventory_path):
    rows = [json.loads(l) for l in open(inventory_path)]
    rng = random.Random(SEED)
    selected = {}
    # rule1/rule2: exhaustive strata
    for r in rows:
        if r["harness"] in ("omp", "copilot"):
            selected[r["evidence_id"]] = "rule1-all-" + r["harness"]
        elif r["harness"] == "claude-code":
            selected[r["evidence_id"]] = "rule2-all-cc"
    # rule3: codex/cursor stratified 20%/cell, floor 2, cells iterated in sorted key order
    cells = collections.defaultdict(list)
    for r in rows:
        if r["harness"] in ("codex", "cursor"):
            cells[(r["harness"], bucket(r), r["status"], bool(r.get("has_tokens")))].append(r)
    for key, members in sorted(cells.items()):
        members.sort(key=lambda r: r["evidence_id"])
        k = max(2, math.ceil(0.2 * len(members))) if len(members) >= 2 else len(members)
        for r in rng.sample(members, min(k, len(members))):
            selected[r["evidence_id"]] = f"rule3-stratum-{key[0]}/{key[1]}/{key[2]}/tok={key[3]}"
    # rule4: negative controls from the unselected pool, sorted, single draw
    pool = sorted((r for r in rows if r["evidence_id"] not in selected), key=lambda r: r["evidence_id"])
    for r in rng.sample(pool, min(15, len(pool))):
        selected[r["evidence_id"]] = "rule4-negative-control"
    return selected

if __name__ == "__main__":
    inv = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), "..", "inventory", "inventory.jsonl")
    sel = generate(inv)
    frozen = json.load(open(os.path.join(os.path.dirname(__file__), "sampling-manifest.json")))
    fset = {s["evidence_id"] for s in frozen["selections"]}
    print(f"generated={len(sel)} frozen={len(fset)} overlap={len(fset & set(sel))}")
