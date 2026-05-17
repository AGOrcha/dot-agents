#!/usr/bin/env python3
"""t3 migrator: relocate grab-bag test funcs into source-mirror / feature
files (same package). Pure relocation — declaration text is moved verbatim;
imports re-resolved afterward by goimports. Aborts on any ambiguity
(duplicate symbol with differing body) rather than risk corruption.
"""
import os, re, sys, collections

PKG = sys.argv[1]                       # .../commands/workflow
GRABBAG = re.compile(r'^(coverage_push|integration_harness)[0-9]*_test\.go$')

# ---- destination resolver (mirrors map_tests.py + curated UNMAPPED rules) --
src_files = [f for f in os.listdir(PKG) if f.endswith(".go") and not f.endswith("_test.go")]
sym2file, lc2file = {}, {}
defre = re.compile(r'^func (?:\([^)]*\)\s*)?([A-Za-z_]\w*)|^type ([A-Za-z_]\w*)\b')
for sf in src_files:
    for ln in open(os.path.join(PKG, sf), encoding="utf-8", errors="replace"):
        m = defre.match(ln)
        if m:
            n = m.group(1) or m.group(2)
            if n:
                sym2file.setdefault(n, sf); lc2file.setdefault(n.lower(), sf)

def by_name(base):
    base = base.split("_")[0]
    cands = [base]
    for pre in ("Run","New","Dispatch","Render","Collect","Resolve"):
        if base.startswith(pre) and len(base) > len(pre):
            cands.append(base[len(pre):])
    for c in list(cands):
        parts = re.findall(r'[A-Z][a-z0-9]*', c)
        for k in range(len(parts), 0, -1):
            cands.append("".join(parts[:k]))
    seen=set()
    for c in cands:
        if not c or c in seen: continue
        seen.add(c)
        if c in sym2file: return sym2file[c]
        if c.lower() in lc2file: return lc2file[c.lower()]
    return None

CURATED = [  # (regex on test func name, destination basename)
    (re.compile(r'^Test(Cobra_|Cmd_|NewWorkflow)'), "cmd.go"),
    (re.compile(r'^TestFanout_'),                   "delegation_fanout.go"),
    (re.compile(r'^Test(MergeBack_|DelegationCloseout|WorkflowDelegationGate_)'), "delegation.go"),
    (re.compile(r'^TestFoldBack(Create|List|Update)_'), "foldback.go"),
    (re.compile(r'^TestVerifyRecord'),              "verification.go"),
]
def dest_for_test(name):
    f = by_name(name)
    if f: return f[:-3] + "_test.go"
    for rx, d in CURATED:
        if rx.match(name): return d[:-3] + "_test.go"
    return None

# ---- split a file into top-level blocks (with leading doc comments) --------
def blocks(path):
    lines = open(path, encoding="utf-8", errors="replace").read().split("\n")
    i = 0
    # skip header comment + package + import block
    while i < len(lines) and not lines[i].startswith("package "): i += 1
    i += 1
    while i < len(lines):
        s = lines[i]
        if s.startswith("import ("):
            while i < len(lines) and lines[i] != ")": i += 1
            i += 1; continue
        if s.startswith("import "): i += 1; continue
        break
    out = []
    while i < len(lines):
        if lines[i].strip() == "":
            i += 1; continue
        start = i
        # absorb leading // or /* */ comment lines as part of next decl
        while i < len(lines) and (lines[i].startswith("//") or lines[i].startswith("/*")):
            i += 1
        if i >= len(lines): break
        head = lines[i]
        kind = None
        m = re.match(r'^func (?:\([^)]*\)\s*)?([A-Za-z_]\w*)', head)
        if m: kind, name = "func", m.group(1)
        else:
            m2 = re.match(r'^(type|var|const) (?:\(|([A-Za-z_]\w*))', head)
            if not m2:
                # unknown top-level line; treat as its own 1-line block
                out.append(("misc", None, "\n".join(lines[start:i+1]))); i += 1; continue
            kind = m2.group(1); name = m2.group(2)
        # find end of decl by brace/paren balance from head line
        depth = 0; j = i; opened = False
        while j < len(lines):
            for ch in lines[j]:
                if ch in "{(": depth += 1; opened = True
                elif ch in "})": depth -= 1
            if opened and depth <= 0: break
            if not opened and (lines[j].rstrip().endswith(("=", "(", "{")) is False) and j == i and kind in ("var","const","type") and "(" not in lines[j] and "{" not in lines[j]:
                break  # single-line var/const/type
            j += 1
        body = "\n".join(lines[start:j+1])
        out.append((kind, name, body))
        i = j + 1
    return out

# ---- collect ---------------------------------------------------------------
grab = sorted(f for f in os.listdir(PKG) if GRABBAG.match(f))
moves = collections.defaultdict(list)     # dest filename -> [block text]
helpers = {}                              # name -> (body, origin) for testutil
test_names = {}                           # Test name -> origin (dup guard)
errors = []
for g in grab:
    for kind, name, body in blocks(os.path.join(PKG, g)):
        if kind == "func" and name and re.match(r'^(Test|Benchmark)', name):
            if name in test_names:
                errors.append(f"DUP test {name}: {test_names[name]} & {g}")
            test_names[name] = g
            d = dest_for_test(name)
            if not d:
                errors.append(f"UNRESOLVED dest for {name} ({g})")
                continue
            moves[d].append(body)
        else:
            # helper/type/var/const -> shared testutil_test.go (dedupe)
            key = name or body.strip()[:40]
            if key in helpers:
                if helpers[key][0].strip() != body.strip():
                    errors.append(f"HELPER COLLISION {key}: {helpers[key][1]} vs {g} (differs)")
                continue
            helpers[key] = (body, g)

if errors:
    print("ABORT — ambiguities:\n" + "\n".join(errors)); sys.exit(2)

# guard: moved test name vs names already in destination/other test files
existing = {}
for tf in [f for f in os.listdir(PKG) if f.endswith("_test.go") and not GRABBAG.match(f)]:
    for ln in open(os.path.join(PKG, tf), encoding="utf-8", errors="replace"):
        m = re.match(r'^func (?:\([^)]*\)\s*)?([A-Za-z_]\w*)', ln)
        if m: existing[m.group(1)] = tf
for t in test_names:
    if t in existing:
        errors.append(f"DUP test {t}: grab-bag vs existing {existing[t]}")
for h in helpers:
    if h in existing:
        errors.append(f"HELPER {h} already in {existing[h]} — skip-move")
if errors:
    print("ABORT — collisions with existing files:\n" + "\n".join(errors)); sys.exit(2)

if "--dry-run" in sys.argv:
    print(f"grab-bag files: {len(grab)}  tests: {len(test_names)}  helpers: {len(helpers)}")
    for d in sorted(moves): print(f"  {d:<42} += {len(moves[d])}")
    print(f"  testutil_test.go (helpers)               += {len(helpers)}")
    sys.exit(0)

# ---- write -----------------------------------------------------------------
def append(dest, chunks):
    p = os.path.join(PKG, dest)
    if not os.path.exists(p):
        open(p, "w").write("package workflow\n")
    with open(p, "a") as fh:
        for c in chunks:
            fh.write("\n" + c.rstrip() + "\n")

for d, chunks in moves.items():
    append(d, chunks)
append("testutil_test.go", [b for b, _ in helpers.values()])
for g in grab:
    os.remove(os.path.join(PKG, g))
print(f"moved {len(test_names)} tests + {len(helpers)} helpers into "
      f"{len(moves)+1} files; deleted {len(grab)} grab-bag files")
