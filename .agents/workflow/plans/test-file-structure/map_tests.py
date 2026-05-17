#!/usr/bin/env python3
"""Map grab-bag test functions to their destination source-mirroring file.

Heuristic, deterministic, reviewable:
  1. Index every top-level func/method + exported type in the package's
     NON-test .go files: symbol -> source basename.
  2. For each Test*/Benchmark* func in a grab-bag file, scan its body for
     identifiers that are indexed symbols; tally hits per source file.
  3. Destination = "<top source file stem>_test.go". Tie/none -> flagged
     UNMAPPED for manual placement (feature/integration file).

Output: a markdown table to stdout. No files are moved.
"""
import os, re, sys, collections

pkg_dir = sys.argv[1]
grabbag_globs = sys.argv[2].split(",")  # e.g. "coverage_push,integration_harness"

src_files = [f for f in os.listdir(pkg_dir)
             if f.endswith(".go") and not f.endswith("_test.go")]
test_files = sorted(
    f for f in os.listdir(pkg_dir)
    if f.endswith("_test.go") and any(re.match(g + r"[0-9]*_test\.go$", f)
                                      for g in grabbag_globs))

# 1. symbol -> source file (first definer wins). Keep a case-insensitive
#    index too: Go unit tests are TestRunFoo for an unexported runFoo.
sym2file = {}
lc2file = {}
def_re = re.compile(r'^func (?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)|'
                    r'^type ([A-Za-z_][A-Za-z0-9_]*)\b')
for sf in src_files:
    for line in open(os.path.join(pkg_dir, sf), encoding="utf-8", errors="replace"):
        m = def_re.match(line)
        if m:
            name = m.group(1) or m.group(2)
            if name:
                sym2file.setdefault(name, sf)
                lc2file.setdefault(name.lower(), sf)

def resolve_by_name(base):
    """Map a test base name (after 'Test'/'Benchmark') to a source file by
    the Go convention: TestXxx exercises Xxx (often unexported). Try the
    full name, common prefixes stripped, then progressively shorter
    CamelCase prefixes — case-insensitively. Returns (file, matched_sym)."""
    base = base.split("_")[0]                      # drop _SubcaseName
    cands = [base]
    for pre in ("Run", "New", "Dispatch", "Render", "Collect", "Resolve"):
        if base.startswith(pre) and len(base) > len(pre):
            cands.append(base[len(pre):])
    # progressive CamelCase prefixes (longest first), e.g.
    # RunWorkflowPlanList -> WorkflowPlanList -> WorkflowPlan -> Workflow
    for c in list(cands):
        parts = re.findall(r'[A-Z][a-z0-9]*', c)
        for k in range(len(parts), 0, -1):
            cands.append("".join(parts[:k]))
    seen = set()
    for c in cands:
        if c in seen or not c:
            continue
        seen.add(c)
        if c in sym2file:
            return sym2file[c], c
        if c.lower() in lc2file:
            return lc2file[c.lower()], c
    return None, None

func_hdr = re.compile(r'^func (Test|Benchmark)([A-Za-z0-9_]+)\s*\(')
ident = re.compile(r'\b([A-Za-z_][A-Za-z0-9_]*)\b')

rows = []           # (test_file, test_func, dest_file, confidence, top_hits)
dest_counts = collections.Counter()
for tf in test_files:
    path = os.path.join(pkg_dir, tf)
    lines = open(path, encoding="utf-8", errors="replace").read().split("\n")
    i = 0
    while i < len(lines):
        m = func_hdr.match(lines[i])
        if not m:
            i += 1
            continue
        fname = m.group(1) + m.group(2)
        # capture body until a line that is exactly "}"
        body = []
        i += 1
        while i < len(lines) and lines[i] != "}":
            body.append(lines[i])
            i += 1
        i += 1
        base = m.group(2)
        blob = "\n".join(body)
        # Primary signal: the Go naming convention (TestXxx -> Xxx's file).
        nm_file, nm_sym = resolve_by_name(base)
        # Corroboration: body symbol tally.
        hits = collections.Counter()
        for tok in ident.findall(blob):
            sf = sym2file.get(tok)
            if sf:
                hits[sf] += 1
        if nm_file:
            dest = nm_file[:-3] + "_test.go"
            # high if the body also predominantly touches that file, or the
            # name match is exact; else medium (name-led, body diffuse).
            if hits:
                top, n = hits.most_common(1)[0]
                conf = "high" if (top == nm_file or n / sum(hits.values()) >= .4) else "medium"
            else:
                conf = "high"
            evid = f"name:{nm_sym} | " + ", ".join(f"{f}:{c}" for f, c in hits.most_common(3))
        elif hits:
            top, n = hits.most_common(1)[0]
            total = sum(hits.values())
            conf = "medium" if n / total >= 0.5 and n >= 3 else "low"
            dest = top[:-3] + "_test.go"
            evid = "body: " + ", ".join(f"{f}:{c}" for f, c in hits.most_common(3))
        else:
            dest, conf, evid = "UNMAPPED", "none", "(no symbol/name match)"
        dest_counts[dest] += 1
        rows.append((tf, fname, dest, conf, evid))

# Curated resolution for cross-cutting tests (mapping.md UNMAPPED rules).
CURATED = [
    (re.compile(r'^Test(Cobra_|Cmd_|NewWorkflow)'), "cmd_test.go"),
    (re.compile(r'^TestFanout_'),                    "delegation_fanout_test.go"),
    (re.compile(r'^Test(MergeBack_|DelegationCloseout|WorkflowDelegationGate_)'), "delegation_test.go"),
    (re.compile(r'^TestFoldBack(Create|List|Update)'), "foldback_test.go"),
    (re.compile(r'^TestVerifyRecord'),               "verification_test.go"),
]
def curated_dest(fn):
    for rx, d in CURATED:
        if rx.match(fn):
            return d
    return None

DEFAULT_DEST = None
for a in sys.argv:
    if a.startswith("--default-dest="):
        DEFAULT_DEST = a.split("=", 1)[1]

if "--tsv" in sys.argv:
    # funcname \t dest \t source-grabbag  (one line per Test/Benchmark func)
    unresolved = []
    for tf, fn, dest, conf, _ in rows:
        if dest == "UNMAPPED":
            c = curated_dest(fn) or DEFAULT_DEST
            if not c:
                unresolved.append(fn)
                continue
            dest = c
        print(f"{fn}\t{dest}\t{tf}")
    if unresolved:
        sys.stderr.write("UNRESOLVED (no rule): " + ", ".join(unresolved) + "\n")
        sys.exit(3)
    sys.exit(0)

print(f"## {pkg_dir}\n")
print(f"- grab-bag files: {len(test_files)}  ·  test funcs: {len(rows)}")
print("- destination spread:")
for d, c in dest_counts.most_common():
    print(f"  - `{d}` ← {c}")
unm = [r for r in rows if r[2] == "UNMAPPED"]
low = [r for r in rows if r[3] == "low"]
print(f"- UNMAPPED (manual/feature placement): {len(unm)}  ·  low-confidence: {len(low)}\n")
print("| src test file | test func | → dest | conf | top symbol-owners |")
print("|---|---|---|---|---|")
for tf, fn, dest, conf, top in sorted(rows):
    print(f"| {tf} | {fn} | {dest} | {conf} | {top} |")
