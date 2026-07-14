#!/usr/bin/env bash
# crg-bridge-consumer-audit.sh — reproducible consumer audit for the CRG bridge.
#
# Regenerates, purely from repo content, the list of things that still depend on
# the CRG "bridge" and derives a READY / NOT-READY verdict for its deletion
# (plan graph-backend-adapter-contract, task t6d). It is the deterministic,
# re-runnable form of the §11.4-gate-condition-4 readiness check that
# `workflow drift` is specified to consume.
#
#   bash scripts/crg-bridge-consumer-audit.sh            # print the audit report
#   bash scripts/crg-bridge-consumer-audit.sh --check F  # verify F embeds this output
#   make ... (not wired) — invoke directly or from CI
#
# DETERMINISTIC: two runs on an unchanged tree produce byte-identical stdout and
# the script never mutates the working tree. The audited commit SHA + timestamp
# go to STDERR only, so stdout stays stable for --check.
#
# TWO DISTINCT SURFACES are audited, because t6d removes BOTH together (§11.4):
#   [A] the legacy Python CRG subprocess bridge — internal/graphstore/crg.go
#       (type CRGBridge / NewCRGBridge), which shells out to the `code-review-graph`
#       Python CLI. This is the load-bearing runtime; `da kg` + the MCP server use it.
#   [B] the migration-only `crg-bridge` mirror ADAPTER —
#       internal/adapters/builtin/crg-bridge/, exposing kg_crg-bridge.* read-only
#       for parity. Its consumers are materialized views declaring
#       reads_from:[crg-bridge] (registry.EnforceReadsFrom / graphstore.BridgeConsumers).

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT" || exit 1

# Search roots for Go source. Kept explicit so adding this script/doc never
# perturbs the counts (scripts/ and docs/ are intentionally excluded).
GO_ROOTS=(internal commands cmd)

# ── grep/find helpers (portable across BSD grep on macOS and GNU grep in CI) ──

# Files (relative paths) under GO_ROOTS matching the pattern, non-test only.
prod_go_files() {
  grep -rlE "$1" --include='*.go' "${GO_ROOTS[@]}" 2>/dev/null \
    | grep -v '_test\.go$' | LC_ALL=C sort -u
}
# Test-only files under GO_ROOTS matching the pattern.
test_go_files() {
  grep -rlE "$1" --include='*.go' "${GO_ROOTS[@]}" 2>/dev/null \
    | grep '_test\.go$' | LC_ALL=C sort -u
}
# Count non-empty lines on stdin.
nlines() { grep -c . || true; }
# Matching lines within one file (0 if none / absent).
file_hits() { grep -cE "$2" "$1" 2>/dev/null || echo 0; }
# "present"/"absent" for a fixed string in a file.
present_if() { if grep -qF "$2" "$1" 2>/dev/null; then echo present; else echo absent; fi; }

# ── [A] Python CRG subprocess bridge (internal/graphstore/crg.go) ──
# Consumer = constructs a CRGBridge (NewCRGBridge(...) or &CRGBridge{...}),
# excluding crg.go itself (the definition/self-use site).
A_PAT='NewCRGBridge\(|CRGBridge\{'
A_PROD="$(prod_go_files "$A_PAT" | grep -v '^internal/graphstore/crg\.go$' || true)"
A_TEST="$(test_go_files  "$A_PAT" | grep -v '^internal/graphstore/crg\.go$' || true)"
A_PROD_N="$(printf '%s\n' "$A_PROD" | nlines)"
A_TEST_N="$(printf '%s\n' "$A_TEST" | nlines)"

# ── [B] crg-bridge mirror adapter ──
# B1: production wiring — anything outside the adapter package that calls
#     RegisterCRGFamily or imports the crg-bridge adapter (non-test).
B_REG="$(prod_go_files 'RegisterCRGFamily\(|/builtin/crg-bridge"' \
  | grep -v '^internal/adapters/builtin/crg-bridge/' || true)"
B_REG_N="$(printf '%s\n' "$B_REG" | nlines)"
# B2: real reads_from:[crg-bridge] declarations in adapter schemas or lockfiles.
B_READS="$( { grep -rlE 'reads_from:[^#]*crg-bridge' --include='*.yaml' --include='*.yml' \
                internal/adapters 2>/dev/null;
             find . -name '*.agentsrc.lock' -exec grep -l 'crg-bridge' {} + 2>/dev/null; } \
           | LC_ALL=C sort -u || true)"
B_READS_N="$(printf '%s\n' "$B_READS" | nlines)"
# B3: MirrorSnapshot production callers (definition lives in the adapter package).
B_MIRROR="$(prod_go_files 'MirrorSnapshot\(' \
  | grep -v '^internal/adapters/builtin/crg-bridge/' || true)"
B_MIRROR_N="$(printf '%s\n' "$B_MIRROR" | nlines)"

# ── [C] graph_backend profile references selecting the mirror ──
C_PROFILES="$(grep -rlE 'graph_backend:[^#]*crg-bridge' \
  --include='*.yaml' --include='*.yml' --include='*.md' .agents/workflow/specs 2>/dev/null \
  | LC_ALL=C sort -u || true)"
C_N="$(printf '%s\n' "$C_PROFILES" | nlines)"

# ── [D] named / out-of-tree consumers of the CRG graph (via the Python CLI) ──
D_CI="$(present_if .github/workflows/test.yml 'code-review-graph')"
D_HOOK="$(present_if commands/kg/sync_code_warm_link.go 'code-review-graph not installed')"
D_MCP="$(present_if commands/kg/kg.go 'NewMCPServer')"

# ── [E] does workflow drift actually run the reads_from:[crg-bridge] sweep? ──
E_DRIFT_WIRED="$(if grep -rqF 'BridgeConsumers(' commands/workflow 2>/dev/null; \
  then echo wired; else echo UNWIRED; fi)"

# ── Verdict (derivable half of §11.4): bridge is deletable only when the
# Python machinery has zero live consumers AND no view reads the mirror. The
# CI-soak halves (§11.4 conditions 1-2) are external facts, reported not derived.
if [ "$A_PROD_N" -gt 0 ] || [ "$B_READS_N" -gt 0 ]; then
  VERDICT="NOT-READY — KEEP THE BRIDGE"
else
  VERDICT="IN-REPO-CLEAR (bridge deletable pending external §11.4 CI soak)"
fi

emit_report() {
  cat <<EOF
CRG BRIDGE CONSUMER AUDIT
=========================
Scope: dot-agents repo, content-derived (branch-independent). Two surfaces t6d
removes together: [A] Python subprocess bridge, [B] crg-bridge mirror adapter.

[A] Legacy Python CRG subprocess bridge  (internal/graphstore/crg.go: CRGBridge)
    Definition site      : internal/graphstore/crg.go (+ crg_venv_unix.go / crg_venv_windows.go)
    Production consumers  : ${A_PROD_N} file(s)
$(printf '%s\n' "$A_PROD" | sed '/^$/d' | while read -r f; do printf '      %s  (%s ref-lines)\n' "$f" "$(file_hits "$f" "$A_PAT")"; done)
    Test-only consumers  : ${A_TEST_N} file(s)
    STATUS: $( [ "$A_PROD_N" -gt 0 ] && echo 'LIVE / load-bearing — da kg code ops + MCP server route here.' || echo 'no live consumers.' )

[B] Migration-only crg-bridge mirror adapter  (internal/adapters/builtin/crg-bridge/)
    Production registration (RegisterCRGFamily / adapter import) : ${B_REG_N}
    reads_from:[crg-bridge] declarations (schemas + lockfiles)   : ${B_READS_N}
    MirrorSnapshot production callers                            : ${B_MIRROR_N}
    STATUS: $( { [ "$B_REG_N" -eq 0 ] && [ "$B_READS_N" -eq 0 ] && [ "$B_MIRROR_N" -eq 0 ]; } && echo 'DEAD WEIGHT — registered nowhere; zero consumers.' || echo 'has consumers — see counts.' )

[C] graph_backend profile references selecting crg-bridge : ${C_N}
    (built-in graph backends are crg / none; the migration-only mirror is not selectable)

[D] Named / out-of-tree consumers of the CRG graph (bound to the Python CLI)
    CI KG CODE lane (.github/workflows/test.yml)            : ${D_CI}
    kg update post_tool_use hook (graceful-degrade)         : ${D_HOOK}
    MCP server (da kg serve -> graphstore.NewMCPServer)     : ${D_MCP}
    Plus documented set: da kg {build,update,code-status,impact,flows,communities,
      postprocess,changes}; review skills build-graph / review-delta / review-pr;
      cross-repo sweep target = ~/.agents/config.json managed repos (per-install).

[E] §11.4 decommission gate (all four required before t6d deletes the bridge)
    1. parity matrix 8 rows x 3wk CI      : EXTERNAL — owned by t6a/t6b (t6b pending)
    2. behavior-preservation gate         : EXTERNAL — owned by t6b
    3. out-of-tree consumer migration     : NOT MET — no kg-native replacement wired
                                            (both builtin registries register only 'none';
                                             RegisterCRGFamily prod callers = ${B_REG_N})
    4. zero reads_from:[crg-bridge]        : in-repo count ${B_READS_N}; cross-repo sweep
                                            NOT automated — workflow drift is ${E_DRIFT_WIRED}
                                            for graphstore.BridgeConsumers()

VERDICT: ${VERDICT}
EOF
}

# Extract the fenced audit block embedded between the doc markers.
extract_doc_block() {
  awk '
    /<!-- BEGIN crg-bridge-consumer-audit.sh output -->/ { inblk=1; next }
    /<!-- END crg-bridge-consumer-audit.sh output -->/   { inblk=0 }
    inblk && /^```/ { infence = !infence; next }
    inblk && infence { print }
  ' "$1"
}

case "${1:-}" in
  -h|--help)
    grep -E '^#( |$)' "$0" | sed 's/^# \{0,1\}//'
    exit 0 ;;
  --check)
    doc="${2:?usage: --check <doc.md>}"
    if diff -u <(extract_doc_block "$doc") <(emit_report) >/tmp/crg-audit-check.$$; then
      echo "OK: ${doc} matches scripts/crg-bridge-consumer-audit.sh output" >&2
      rm -f /tmp/crg-audit-check.$$
      exit 0
    else
      echo "DRIFT: ${doc} does not match current audit output:" >&2
      cat /tmp/crg-audit-check.$$ >&2
      rm -f /tmp/crg-audit-check.$$
      exit 1
    fi ;;
  '')
    echo "crg-bridge-consumer-audit: audited $(git rev-parse --short HEAD 2>/dev/null || echo '?') at $(date -u +%FT%TZ)" >&2
    emit_report ;;
  *)
    echo "unknown argument: $1 (try --help)" >&2
    exit 2 ;;
esac
