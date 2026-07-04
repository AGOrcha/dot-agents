#!/usr/bin/env bash
# test-da-run.sh — POSIX-portable local acceptance test for `da run`.
#
# Proves R5/D1: a chmod-+x .da recipe file with '#!/usr/bin/env -S da run'
# executes directly on macOS/Linux; on Windows Git Bash (no OS shebang) the
# equivalent 'da run <file>' fallback produces identical output (R4).
#
# Runnable from any directory — the script locates the repo root from its own
# path and builds its own `da` binary so no prior install is required.
set -euo pipefail

PASS=0
FAIL=0

# Expected leading prefix of `da --version` output, shared by all assertions.
readonly EXPECTED_PREFIX="da "

pass() { local msg="$1"; PASS=$((PASS + 1)); echo "PASS: $msg"; }
fail() { local msg="$1"; FAIL=$((FAIL + 1)); echo "FAIL: $msg"; }

# Locate the repo root from this script's own path so the script is invocable
# from any working directory (CI, developer shell, etc.).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "--- Building da binary (from $REPO_ROOT) ---"
(cd "$REPO_ROOT" && go build -buildvcs=false -o "$tmp/da" ./cmd/da)
PATH="$tmp:$PATH"
export PATH

echo "--- Writing recipe.da ---"
recipe="$tmp/recipe.da"
# First line is the D1 shebang; second is a comment that effectiveLines must
# skip; third is the single effective step dispatched as `da --version`.
printf '#!/usr/bin/env -S da run\n# this comment must be skipped\n--version\n' > "$recipe"
chmod +x "$recipe"

platform="$(uname -s)"
echo "--- Platform: $platform ---"

case "$platform" in
    Darwin | Linux)
        echo "--- Shebang direct execution (yq-#1851 path) ---"
        output="$("$recipe")"
        if [[ "$output" == "$EXPECTED_PREFIX"* ]]; then
            pass "shebang direct exec produced: $output"
        else
            fail "shebang direct exec: expected output starting with 'da ', got: $output"
        fi
        ;;
    MINGW* | MSYS* | CYGWIN*)
        # Windows Git Bash: the OS does not honour POSIX shebangs.
        # The 'da run <file>' fallback is the R4-equivalent path.
        echo "NOTE: Windows Git Bash detected (${platform}) — no native OS shebang; using 'da run <file>' fallback"
        output="$(da run "$recipe")"
        if [[ "$output" == "$EXPECTED_PREFIX"* ]]; then
            pass "da run fallback produced: $output"
        else
            fail "da run fallback: expected output starting with 'da ', got: $output"
        fi
        ;;
    *)
        echo "NOTE: Unrecognised platform '${platform}' — using 'da run <file>' fallback"
        output="$(da run "$recipe")"
        if [[ "$output" == "$EXPECTED_PREFIX"* ]]; then
            pass "da run fallback produced: $output"
        else
            fail "da run fallback: expected output starting with 'da ', got: $output"
        fi
        ;;
esac

echo ""
echo "Results: PASS=${PASS} FAIL=${FAIL}"
if [[ "$FAIL" -gt 0 ]]; then
    exit 1
fi
