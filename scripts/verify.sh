#!/bin/bash
# da verification script
# Quick smoke test of all CLI commands

set -uo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

# Repo root (this script lives in scripts/). Used to anchor relative binary
# invocations so they still resolve after a subshell `cd` into a fixture dir.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Find da binary.
# DOT_AGENTS         — invocation used from the repo root (back-compat).
# DOT_AGENTS_ABS     — location-independent invocation, safe to run after a
#                      `cd` into a temp fixture directory (config-v2 live
#                      commands read .agentsrc.json from the working dir).
if [[ -x "./bin/da" ]]; then
  DOT_AGENTS="./bin/da"
  DOT_AGENTS_ABS="${REPO_ROOT}/bin/da"
elif command -v da >/dev/null 2>&1; then
  DOT_AGENTS="da"
  DOT_AGENTS_ABS="da"
elif command -v go >/dev/null 2>&1; then
  DOT_AGENTS="go run ./cmd/da"
  DOT_AGENTS_ABS="go run ${REPO_ROOT}/cmd/da"
else
  echo -e "${RED}Error: da not found${NC}" >&2
  exit 1
fi

echo -e "${BOLD}da Verification Script${NC}"
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
    if [[ "$expect_success" = "true" ]]; then
      echo -e "${GREEN}✓${NC}"
      passed=$((passed + 1))
    else
      echo -e "${RED}✗ (should have failed)${NC}"
      failed=$((failed + 1))
    fi
  else
    if [[ "$expect_success" = "false" ]]; then
      echo -e "${GREEN}✓ (expected failure)${NC}"
      passed=$((passed + 1))
    else
      echo -e "${RED}✗${NC}"
      failed=$((failed + 1))
    fi
  fi

  return 0
}

# assert_contains NAME CMD NEEDLE
# Runs CMD (capturing stdout+stderr) and passes only if the output contains the
# literal NEEDLE. Content-level assertion — proves the command returned the
# expected data, not just exit 0. Updates the shared pass/fail counters.
assert_contains() {
  local name="$1"
  local cmd="$2"
  local needle="$3"
  local out

  echo -n "  Asserting $name... "
  out="$(eval "$cmd" 2>&1)"
  if printf '%s' "$out" | grep -qF -- "$needle"; then
    echo -e "${GREEN}✓${NC}"
    passed=$((passed + 1))
  else
    echo -e "${RED}✗ (missing '${needle}')${NC}"
    failed=$((failed + 1))
  fi

  return 0
}

# assert_json_field NAME CMD PYEXPR
# Runs CMD (expected to emit JSON on stdout) and evaluates PYEXPR in a python3
# snippet where `d` is the parsed JSON. Passes when PYEXPR is truthy. Lets the
# smoke assert structured fields/values rather than raw substrings.
assert_json_field() {
  local name="$1"
  local cmd="$2"
  local pyexpr="$3"

  echo -n "  Asserting $name... "
  if eval "$cmd" 2>/dev/null | python3 -c "import sys,json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(1)
sys.exit(0 if (${pyexpr}) else 1)" 2>/dev/null; then
    echo -e "${GREEN}✓${NC}"
    passed=$((passed + 1))
  else
    echo -e "${RED}✗ (json check failed: ${pyexpr})${NC}"
    failed=$((failed + 1))
  fi

  return 0
}

echo -e "${BOLD}Basic Commands${NC}"
test_command "--version" "$DOT_AGENTS --version"
test_command "--help" "$DOT_AGENTS --help"
test_command "version --json" "$DOT_AGENTS --version --json"

echo ""
echo -e "${BOLD}Core Commands${NC}"
test_command "status" "$DOT_AGENTS status"
test_command "status --json" "$DOT_AGENTS status --json"
test_command "status --audit" "$DOT_AGENTS status --audit"
test_command "doctor" "$DOT_AGENTS doctor"
test_command "doctor --json" "$DOT_AGENTS doctor --json"
test_command "refresh --help" "$DOT_AGENTS refresh --help"
test_command "import --help" "$DOT_AGENTS import --help"
test_command "install --help" "$DOT_AGENTS install --help"
# Content assertion on the reshaped read-only status surface: the JSON payload
# must expose the canonical top-level keys (fleet/link-health view).
assert_json_field "status --json keys" "$DOT_AGENTS status --json" \
  "isinstance(d, dict) and 'canonical_store' in d and 'projects' in d"

echo ""
echo -e "${BOLD}Info Commands${NC}"
test_command "explain" "$DOT_AGENTS explain"
test_command "explain --help" "$DOT_AGENTS explain --help"
test_command "workflow --help" "$DOT_AGENTS workflow --help"
test_command "review --help" "$DOT_AGENTS review --help"
test_command "kg --help" "$DOT_AGENTS kg --help"

echo ""
echo -e "${BOLD}Config-v2 Help Commands${NC}"
# Help paths run anywhere (no .agentsrc.json required) and prove the
# config-v2 subtree is wired into the built binary's command surface.
test_command "config --help" "$DOT_AGENTS config --help"
test_command "config explain --help" "$DOT_AGENTS config explain --help"
test_command "config sync --help" "$DOT_AGENTS config sync --help"
test_command "config lint --help" "$DOT_AGENTS config lint --help"
test_command "config verify --help" "$DOT_AGENTS config verify --help"

echo ""
echo -e "${BOLD}Config-v2 + Mutations (isolated managed home)${NC}"
# The mutating config-v2 + install + refresh paths need a real managed
# ~/.agents and a working-directory manifest. Build a fully isolated home
# under a temp dir (own HOME/AGENTS_HOME/KG_HOME) so nothing touches the
# caller's real config and the steps stay hermetic + cross-OS. The manifest
# declares a LOCAL extends layer so lint/sync/explain have real content to act
# on. The binary is resolved to DOT_AGENTS_ABS so a relative ./bin/da survives
# the cd into the project dir.
SMOKE_ROOT="$(mktemp -d 2>/dev/null || mktemp -d -t da-smoke)"
export HOME="${SMOKE_ROOT}/home"
export AGENTS_HOME="${SMOKE_ROOT}/home/.agents"
export KG_HOME="${SMOKE_ROOT}/home/.kg"
mkdir -p "${HOME}"
PROJ="${SMOKE_ROOT}/proj"
mkdir -p "${PROJ}/layers"

# Base layer: declares project name, a skill, and two feature flags. The repo
# layer below OVERRIDES `project` and feature `alpha`, leaving `skills` and
# feature `beta` to win from the base — so provenance has something real to
# resolve across layers.
cat > "${PROJ}/layers/base.json" <<'JSON'
{
  "version": 1,
  "project": "base-project",
  "skills": ["base-skill"],
  "features": { "alpha": "from-base", "beta": "base-only" }
}
JSON
# Repo-local manifest: declares the local source + extends the base layer, and
# overrides `project` + feature `alpha`.
write_manifest() {
  cat > "${PROJ}/.agentsrc.json" <<'JSON'
{
  "version": 1,
  "sources": [ { "type": "local", "id": "localbase", "path": "./layers" } ],
  "extends": ["localbase:base.json"],
  "project": "override-project",
  "features": { "alpha": "from-repo" }
}
JSON
  return 0
}
write_manifest

# Bootstrap the managed home, then run the config-v2 lifecycle in the project.
test_command "init --yes (isolated)" "$DOT_AGENTS_ABS init --yes"

# ── P0: first-run lock smoke — lock parent does NOT pre-exist (#148 regression) ─
# RCA #147 found the existing config-v2 lane MASKED the Windows agentslock bug:
# it `mkdir -p`s ${PROJ} (the .agentsrc.lock's parent) before any command runs,
# so by the time a command acquires the lock the parent already exists. The
# field bug (#148) was the OPPOSITE precondition: a lock-acquiring command runs
# when the lockfile's parent directory has NOT yet been materialized. The shared
# agentslock writer took the sidecar dir-lock with a bare os.Mkdir(path+".lock"),
# which — unlike MkdirAll — does NOT create intermediate components, so the
# acquire failed (ENOENT on unix, ERROR_FILE_NOT_FOUND on Windows) before any
# sibling writer had created the directory. #148 fixed acquireFileLock to
# MkdirAll the lock dir's parent first.
#
# This is the LIVE-BINARY regression gate for that escape: drive a real
# lock-acquiring command (`config explain --all` auto-locks; it re-resolves and
# rewrites the units lock when the lock is absent) in a brand-new project whose
# .agentsrc.lock parent is created by the acquire itself — NOT pre-created with a
# `mkdir -p`. The resolver's lock write (config.WriteConfigLock → agentslock
# Flush → acquireFileLock) is the SOLE creator of that parent on this path, so a
# pre-#148 binary acquires before the dir exists. To exercise the MkdirAll-parent
# fix end-to-end the project sits under an intermediate directory that is NEVER
# created here; only the leaf carrying the manifest is materialized, leaving the
# lock-dir's nested parent for the acquire to create. Runs on EVERY OS leg (no
# unix guard) — it is specifically the Windows-relevant path that escaped before.
FR_INTERMEDIATE="${SMOKE_ROOT}/first-run-no-premade"
FR_PROJ="${FR_INTERMEDIATE}/firstrun-proj"
# Materialize ONLY the leaf project dir + its manifest/layer. The intermediate
# ${FR_INTERMEDIATE} is created here only incidentally by mkdir -p of the leaf;
# the lock-dir's parent that the acquire must create is the nested
# `.agentsrc.lock.lock` sidecar path, which agentslock builds through filepath
# and MkdirAll's (the #148 fix). No prior install/sync/lint runs in this project,
# so `config explain --all` is the FIRST writer and the .agentsrc.lock parent is
# not pre-staged by any earlier command.
mkdir -p "${FR_PROJ}/layers"
cat > "${FR_PROJ}/layers/base.json" <<'JSON'
{ "version": 1, "project": "base-project", "skills": ["base-skill"] }
JSON
cat > "${FR_PROJ}/.agentsrc.json" <<'JSON'
{
  "version": 1,
  "sources": [ { "type": "local", "id": "localbase", "path": "./layers" } ],
  "extends": ["localbase:base.json"],
  "project": "firstrun-project"
}
JSON
# Precondition: no lock and no sidecar exist yet.
test_command "first-run: lock absent before acquire" "test ! -e '${FR_PROJ}/.agentsrc.lock'"
test_command "first-run: sidecar absent before acquire" "test ! -e '${FR_PROJ}/.agentsrc.lock.lock'"
# The lock-acquiring command MUST succeed (it failed pre-#148 on the absent-parent
# path). `config explain --all` auto-locks: it writes the units lock when absent.
test_command "first-run: lock-acquiring command succeeds (#148)" \
  "(cd '${FR_PROJ}' && $DOT_AGENTS_ABS config explain --all)"
# Assert the lock was actually written by the acquire (parent materialized).
test_command "first-run: lock written by acquire" "test -f '${FR_PROJ}/.agentsrc.lock'"
assert_contains "first-run: lock carries the resolved units" \
  "cat '${FR_PROJ}/.agentsrc.lock'" "localbase:base.json"
# The sidecar dir-lock must be released, not leaked, after a clean acquire.
test_command "first-run: sidecar dir-lock released (not leaked)" \
  "test ! -e '${FR_PROJ}/.agentsrc.lock.lock'"
# A second lock-acquiring command (install --yes resolves + writes the lock) must
# also succeed against the now-existing project, proving acquire/release is
# idempotent across the first-run boundary.
test_command "first-run: second acquire (install --yes) succeeds" \
  "(cd '${FR_PROJ}' && $DOT_AGENTS_ABS install --yes)"
rm -rf "${FR_INTERMEDIATE}"

# ── GIT-SOURCE first-run smoke — install/explain/sync against a bare-repo source ─
# The owner's Windows work PC cannot run `da install` or `da config explain`:
# its manifest extends a GIT source. Yet this smoke (and CI) only ever exercised
# a LOCAL extends layer — the entire git lane (clone into the layer cache,
# cache-integrity locks, Windows path handling) had ZERO live-binary coverage on
# any OS. That is the same structural blind-spot class as the #147 ESCAPED
# agentslock bug (.agents/history/rca-windows-agentslock-escape.md): the
# field-failing surface had no net. This lane is the net for git sources, and it
# is hermetic + offline: the "remote" is a local BARE repo inside the smoke
# tree, declared via a file:// URL — the identical go-git clone path a network
# remote takes (internal/config fetcher accepts file:// as a legitimate clone
# source), minus the network. FIRST-RUN shape throughout: no pre-created lock,
# no warmed cache — matching the owner's failing machine.
GS_WORK="${SMOKE_ROOT}/git-source-work"
GS_BARE="${SMOKE_ROOT}/git-source-layer.git"
GS_PROJ="${SMOKE_ROOT}/gitsrc-proj"
mkdir -p "${GS_WORK}"
# Source-layer content mirrors the local-extends fixture shape (project + a
# skill + a feature) so the explain/lock assertions have real fields to win.
cat > "${GS_WORK}/base.json" <<'JSON'
{
  "version": 1,
  "project": "git-base-project",
  "skills": ["git-source-skill"],
  "features": { "gamma": "from-git" }
}
JSON
git init -q "${GS_WORK}"
# Pin the branch to `main` regardless of the runner's init.defaultBranch (the
# git fetcher's default ref is main). Re-pointing the unborn HEAD via
# symbolic-ref is portable to every git version — no `init -b` requirement.
git -C "${GS_WORK}" symbolic-ref HEAD refs/heads/main
git -C "${GS_WORK}" add base.json
# Inline identity so the commit works on clean runners with no git config.
git -C "${GS_WORK}" -c user.name=smoke -c user.email=smoke@local commit -q -m "git-source layer"
git clone -q --bare "${GS_WORK}" "${GS_BARE}"
GS_SHA="$(git --git-dir="${GS_BARE}" rev-parse HEAD)"
# The clone URL is embedded in .agentsrc.json and read VERBATIM by the binary —
# no shell path conversion applies to file content — so on Windows-bash it must
# be the native form. cygpath -m yields forward-slash native paths (C:/...),
# which are also JSON-safe (no backslash escaping needed).
GS_URL_PATH="${GS_BARE}"
if command -v cygpath >/dev/null 2>&1; then
  GS_URL_PATH="$(cygpath -m "${GS_BARE}")"
fi
mkdir -p "${GS_PROJ}"
cat > "${GS_PROJ}/.agentsrc.json" <<JSON
{
  "version": 1,
  "sources": [ { "type": "git", "id": "gitbase", "url": "file://${GS_URL_PATH}", "ref": "main" } ],
  "extends": ["gitbase:base.json"],
  "project": "gitsrc-project"
}
JSON
# FIRST-RUN preconditions: no lock, and the layer cache for this source is cold.
test_command "git-source: lock absent before first run" "test ! -e '${GS_PROJ}/.agentsrc.lock'"
test_command "git-source: layer cache cold before first run" "test ! -e '${AGENTS_HOME}/cache/config/gitbase'"
# `da install` is the owner's first failing surface: it resolves the git layer
# (clone → cache → lock) as part of materializing the project.
test_command "git-source: install --yes (first run)" "(cd '${GS_PROJ}' && $DOT_AGENTS_ABS install --yes)"
test_command "git-source: install wrote lock" "test -f '${GS_PROJ}/.agentsrc.lock'"
# The units entry pins the git layer to the EXACT commit the bare repo serves —
# git layers record the resolved commit SHA as the lock digest (the sha256:…
# form is the http/local/oci content-hash variant of the same field).
assert_contains "git-source: lock has the git layer unit" \
  "cat '${GS_PROJ}/.agentsrc.lock'" "gitbase:base.json"
assert_contains "git-source: lock pins the resolved commit digest" \
  "cat '${GS_PROJ}/.agentsrc.lock'" "${GS_SHA}"
# The fetched layer landed in the content-addressed cache at the resolved SHA
# (~/.agents/cache/config/<source-id>/<layer-path>/<sha>/layer.json).
test_command "git-source: layer cached content-addressed by SHA" \
  "test -f '${AGENTS_HOME}/cache/config/gitbase/base.json/${GS_SHA}/layer.json'"
# `da config explain` is the owner's second failing surface. It must name the
# git layer as the WINNING source for a field only that layer sets.
assert_json_field "git-source: explain skills wins from the git layer" \
  "(cd '${GS_PROJ}' && $DOT_AGENTS_ABS config explain skills --json)" \
  "d.get('value') == ['git-source-skill'] and d.get('active_layer') == 'gitbase:base.json'"
assert_json_field "git-source: explain --all merges git layer + repo override" \
  "(cd '${GS_PROJ}' && $DOT_AGENTS_ABS config explain --all --json)" \
  "d['effective']['project'] == 'gitsrc-project' and d['effective']['skills'] == ['git-source-skill'] and d['effective']['features'] == {'gamma': 'from-git'}"
# sync + verify complete the lifecycle against the git-sourced lock.
test_command "git-source: config sync" "(cd '${GS_PROJ}' && $DOT_AGENTS_ABS config sync)"
assert_contains "git-source: config verify -> OK" \
  "(cd '${GS_PROJ}' && $DOT_AGENTS_ABS config verify)" "OK"
# A SECOND resolve with a warm cache must also succeed (the SHA-addressed
# cache-serve path), and the sidecar dir-lock must not leak across the runs.
test_command "git-source: second explain (cache warm) succeeds" \
  "(cd '${GS_PROJ}' && $DOT_AGENTS_ABS config explain --all)"
test_command "git-source: sidecar dir-lock released (not leaked)" \
  "test ! -e '${GS_PROJ}/.agentsrc.lock.lock'"

# ── GIT-SOURCE CONTENT-INSTALL smoke — packages materialize+lock+project (t5 dogfood, DC2) ─
# package-artifact-install spec DC2 extends the git-source-smoke fixture above
# from LAYER-only to CONTENT install: a bare-repo "tree" source (the same
# file:// clone path a network remote takes) ships a skill + two agents laid
# out exactly like the live AGorcha/da-agc source dot-agents' own
# .agentsrc.json now declares as packages[] (skill/<name>/, agent/<name>/).
# This proves the REAL end-to-end mechanism — fetch -> materialize -> lock
# (kind:artifact + the artifact-content integrity anchor, H10) -> project
# (H13) -> offline verify (H7) — hermetically and offline, no network
# dependency in CI.
PKG_WORK="${SMOKE_ROOT}/pkg-source-work"
PKG_BARE="${SMOKE_ROOT}/pkg-source-layer.git"
PKG_PROJ="${SMOKE_ROOT}/pkgsrc-proj"
mkdir -p "${PKG_WORK}/skill/release-docs-refresh" "${PKG_WORK}/agent/platform-dirs-change-analyst" "${PKG_WORK}/agent/promise-gap-analyst"
echo '# release-docs-refresh' > "${PKG_WORK}/skill/release-docs-refresh/SKILL.md"
echo '# platform-dirs-change-analyst' > "${PKG_WORK}/agent/platform-dirs-change-analyst/AGENT.md"
echo '# promise-gap-analyst' > "${PKG_WORK}/agent/promise-gap-analyst/AGENT.md"
git init -q "${PKG_WORK}"
git -C "${PKG_WORK}" symbolic-ref HEAD refs/heads/main
git -C "${PKG_WORK}" add skill agent
git -C "${PKG_WORK}" -c user.name=smoke -c user.email=smoke@local commit -q -m "da-agc mirror fixture"
git clone -q --bare "${PKG_WORK}" "${PKG_BARE}"
PKG_URL_PATH="${PKG_BARE}"
if command -v cygpath >/dev/null 2>&1; then
  PKG_URL_PATH="$(cygpath -m "${PKG_BARE}")"
fi
mkdir -p "${PKG_PROJ}"
cat > "${PKG_PROJ}/.agentsrc.json" <<JSON
{
  "version": 1,
  "project": "pkgsrc-project",
  "sources": [ { "type": "git", "id": "da-agc", "url": "file://${PKG_URL_PATH}", "ref": "main" } ],
  "packages": [
    "da-agc:skill/release-docs-refresh@main",
    "da-agc:agent/platform-dirs-change-analyst@main",
    "da-agc:agent/promise-gap-analyst@main"
  ]
}
JSON

# FIRST-RUN: no lock, cold CAS.
test_command "git-source content-install: lock absent before first run" "test ! -e '${PKG_PROJ}/.agentsrc.lock'"
test_command "git-source content-install: install --yes (first run)" "(cd '${PKG_PROJ}' && $DOT_AGENTS_ABS install --yes)"

# (a) the skill materialized + projected + present at its invocable path.
SKILL_PROJECTED="${PKG_PROJ}/.claude/skills/release-docs-refresh/SKILL.md"
assert_contains "git-source content-install: skill projected+invocable" \
  "cat '${SKILL_PROJECTED}'" "release-docs-refresh"

# (b) both agents present.
assert_contains "git-source content-install: agent 1 projected" \
  "cat '${PKG_PROJ}/.claude/agents/platform-dirs-change-analyst/AGENT.md'" "platform-dirs-change-analyst"
assert_contains "git-source content-install: agent 2 projected" \
  "cat '${PKG_PROJ}/.claude/agents/promise-gap-analyst/AGENT.md'" "promise-gap-analyst"

# (c) the artifact-content lock section recorded with the content digest, plus
# a kind:artifact units entry per ref (H10).
assert_contains "git-source content-install: lock has artifact-content section" \
  "cat '${PKG_PROJ}/.agentsrc.lock'" "artifact-content"
assert_contains "git-source content-install: lock has the skill artifact unit" \
  "cat '${PKG_PROJ}/.agentsrc.lock'" "da-agc:skill/release-docs-refresh@main"
assert_json_field "git-source content-install: artifact-content anchors all 3 refs" \
  "cat '${PKG_PROJ}/.agentsrc.lock'" \
  "set(d['artifact-content'].keys()) == {'da-agc:skill/release-docs-refresh@main', 'da-agc:agent/platform-dirs-change-analyst@main', 'da-agc:agent/promise-gap-analyst@main'} and all(v.startswith('sha256:') for v in d['artifact-content'].values())"

# (e) the offline H7 primitive (VerifyStoreContentDigest, wired into `config
# verify`) passes over every projected ref.
assert_contains "git-source content-install: config verify -> OK" \
  "(cd '${PKG_PROJ}' && $DOT_AGENTS_ABS config verify)" "OK"

# (d) a SECOND run with the lock present is a no-op: the units + artifact-
# content sections are byte-unchanged (only the install-stamp timestamp
# moves), and the projected files are byte-identical (H9 frozen = no rewrite).
cp "${PKG_PROJ}/.agentsrc.lock" "${SMOKE_ROOT}/pkg-lock-before.json"
cp "${SKILL_PROJECTED}" "${SMOKE_ROOT}/pkg-skill-before.md"
test_command "git-source content-install: install --yes (second run, frozen no-op)" \
  "(cd '${PKG_PROJ}' && $DOT_AGENTS_ABS install --yes)"
assert_json_field "git-source content-install: units+artifact-content unchanged across the no-op re-run" \
  "python3 -c \"import json,sys; a=json.load(open('${SMOKE_ROOT}/pkg-lock-before.json')); b=json.load(open('${PKG_PROJ}/.agentsrc.lock')); print(json.dumps({'ok': a['units']==b['units'] and a['artifact-content']==b['artifact-content']}))\"" \
  "d['ok'] is True"
test_command "git-source content-install: projected skill byte-identical after the no-op re-run" \
  "diff -q '${SMOKE_ROOT}/pkg-skill-before.md' '${SKILL_PROJECTED}'"

# ── Adversarial: a tampered CAS entry fails verify, and a normal re-install
# self-heals it (H16 quarantine + re-extract) ─────────────────────────────
PKG_SKILL_DIGEST="$(python3 -c "import json; print(json.load(open('${PKG_PROJ}/.agentsrc.lock'))['units']['da-agc:skill/release-docs-refresh@main']['digest'])")"
PKG_CAS_FILE="${AGENTS_HOME}/cache/artifacts/skills/${PKG_SKILL_DIGEST#sha256:}/SKILL.md"
test_command "git-source content-install: skill CAS entry exists before tamper" "test -f '${PKG_CAS_FILE}'"
chmod +w "${PKG_CAS_FILE}"
echo 'TAMPERED' > "${PKG_CAS_FILE}"
assert_contains "git-source content-install: tampered CAS fails config verify" \
  "(cd '${PKG_PROJ}' && $DOT_AGENTS_ABS config verify)" "FAIL"
test_command "git-source content-install: re-install self-heals the tampered CAS entry" \
  "(cd '${PKG_PROJ}' && $DOT_AGENTS_ABS install --yes)"
assert_contains "git-source content-install: config verify -> OK after self-heal" \
  "(cd '${PKG_PROJ}' && $DOT_AGENTS_ABS config verify)" "OK"
assert_contains "git-source content-install: self-healed skill content restored" \
  "cat '${SKILL_PROJECTED}'" "release-docs-refresh"

# ── Adversarial: deleting the lock forces a re-fetch on the next install ────
rm -f "${PKG_PROJ}/.agentsrc.lock"
test_command "git-source content-install: lock absent after deletion" "test ! -e '${PKG_PROJ}/.agentsrc.lock'"
test_command "git-source content-install: install --yes re-fetches after lock deletion" \
  "(cd '${PKG_PROJ}' && $DOT_AGENTS_ABS install --yes)"
assert_contains "git-source content-install: re-fetched lock re-anchors the skill" \
  "cat '${PKG_PROJ}/.agentsrc.lock'" "da-agc:skill/release-docs-refresh@main"
assert_contains "git-source content-install: re-fetched projection still invocable" \
  "cat '${SKILL_PROJECTED}'" "release-docs-refresh"

# ── Layer resolution + provenance combinations ───────────────────────────────
# Valid rc lints clean.
assert_contains "config lint (valid rc) -> OK" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "OK"

# Overridden field: value comes from repo-local, NOT the base layer.
assert_json_field "explain project (overridden) value+origin" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain project --json)" \
  "d.get('value') == 'override-project' and d.get('active_layer') == 'repo-local'"
assert_contains "explain project --origin-only -> repo-local" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain project --origin-only)" "repo-local"

# Base-only field: value + winning layer come from the extended base layer.
assert_json_field "explain skills (base-only) value+origin" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain skills --json)" \
  "d.get('value') == ['base-skill'] and d.get('active_layer') == 'localbase:base.json'"
assert_contains "explain skills --origin-only -> base layer" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain skills --origin-only)" "localbase:base.json"

# Full effective config (--all --json) merges the layers: project overridden,
# skills from base, and features merged (alpha overridden, beta from base).
assert_json_field "explain --all --json merges layers" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config explain --all --json)" \
  "d['effective']['project'] == 'override-project' and d['effective']['skills'] == ['base-skill'] and d['effective']['features'] == {'alpha': 'from-repo', 'beta': 'base-only'}"

# ── lint failure on a broken layer (then restore) ────────────────────────────
# Point extends at a missing layer file → lint must FAIL with the expected
# error text and a non-zero exit.
cat > "${PROJ}/.agentsrc.json" <<'JSON'
{
  "version": 1,
  "sources": [ { "type": "local", "id": "localbase", "path": "./layers" } ],
  "extends": ["localbase:missing.json"]
}
JSON
assert_contains "lint (missing layer) reports file not found" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "file not found"
assert_contains "lint (missing layer) verdict FAILED" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "FAILED"
test_command "lint (missing layer) exits non-zero" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "false"
# Malformed layer JSON → lint must report an invalid-JSON error.
write_manifest
printf '{ this is not valid json\n' > "${PROJ}/layers/base.json"
assert_contains "lint (malformed layer JSON) reports invalid JSON" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "invalid JSON"
test_command "lint (malformed layer) exits non-zero" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "false"
# Restore the valid base layer + manifest for the mutation lane below.
cat > "${PROJ}/layers/base.json" <<'JSON'
{
  "version": 1,
  "project": "base-project",
  "skills": ["base-skill"],
  "features": { "alpha": "from-base", "beta": "base-only" }
}
JSON
write_manifest
assert_contains "lint (restored rc) -> OK" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config lint)" "OK"

# ── install materializes managed state ───────────────────────────────────────
# Which platform link trees (.claude/.cursor/AGENTS.md) install writes depends
# on platform DETECTION on the runner, so the robust cross-OS/cross-runner
# proof that install ran its full pass is the install STAMP it records in the
# lockfile (project name + stamped_at).
test_command "install --yes (isolated)" "(cd '${PROJ}' && $DOT_AGENTS_ABS install --yes)"
test_command "install wrote lock" "test -f '${PROJ}/.agentsrc.lock'"
assert_contains "install stamped the lock" "cat '${PROJ}/.agentsrc.lock'" '"install"'
assert_contains "install stamp names the project" "cat '${PROJ}/.agentsrc.lock'" "override-project"
# Re-running install (default EXACT) and install --inexact both converge
# cleanly and idempotently — the documented exact-vs-additive link passes.
# NOTE: asserting a specific managed-link is PRUNED vs KEPT requires a
# source-provided resource whose platform link type differs per OS (POSIX
# symlink vs Windows hard link), which is not hermetically stable in a
# cross-OS shell smoke; that prune/keep matrix is covered by Go unit tests
# (internal/links/managed_link_branches2_test.go). Here we assert both passes
# run clean on the managed project.
test_command "install (re-run, default exact)" "(cd '${PROJ}' && $DOT_AGENTS_ABS install --yes)"
test_command "install --inexact (additive)" "(cd '${PROJ}' && $DOT_AGENTS_ABS install --inexact --yes)"

# ── config sync writes/updates the lock; --dry-run must not (#105) ───────────
test_command "config sync (isolated)" "(cd '${PROJ}' && $DOT_AGENTS_ABS config sync)"
test_command "config sync wrote lock" "test -f '${PROJ}/.agentsrc.lock'"
assert_contains "lock has inputs_digest" "cat '${PROJ}/.agentsrc.lock'" "inputs_digest"
assert_contains "lock has the local layer unit" "cat '${PROJ}/.agentsrc.lock'" "localbase:base.json"

# verify's full contract-check verdict is OK once the lock is in sync.
assert_contains "config verify -> OK" \
  "(cd '${PROJ}' && $DOT_AGENTS_ABS config verify)" "OK"

# Mutation guard (#105): config sync --dry-run must NOT rewrite the lock — the
# lock must be byte-identical before/after (cksum is POSIX + cross-OS).
LOCK_BEFORE="$(cksum "${PROJ}/.agentsrc.lock" | awk '{print $1, $2}')"
test_command "config sync --dry-run (isolated)" "(cd '${PROJ}' && $DOT_AGENTS_ABS config sync --dry-run)"
LOCK_AFTER="$(cksum "${PROJ}/.agentsrc.lock" | awk '{print $1, $2}')"
test_command "dry-run left lock byte-identical (#105)" "[ \"${LOCK_BEFORE}\" = \"${LOCK_AFTER}\" ]"

# da refresh (default exact) and refresh --inexact both exit clean.
test_command "refresh (isolated managed project)" "(cd '${PROJ}' && $DOT_AGENTS_ABS refresh)"
test_command "refresh --inexact (additive)" "(cd '${PROJ}' && $DOT_AGENTS_ABS refresh --inexact)"

echo ""
echo -e "${BOLD}KG note lane (warm + query + bridge, no crg)${NC}"
# The note lane works without code-review-graph: setup -> ingest -> warm ->
# query -> bridge. Assertions are on CONTENT, not just exit 0. Uses the same
# isolated KG_HOME so it stays hermetic and cross-OS.
test_command "kg setup" "$DOT_AGENTS_ABS kg setup"
KG_NOTE="${SMOKE_ROOT}/smoke-note.md"
cat > "${KG_NOTE}" <<'MD'
# Smoke Sentinel Note

This note exists to verify the dot-agents knowledge graph warm + query lane.
Sentinel token: ZEBRAFISH_SMOKE_TOKEN. The repo context is dot-agents.
MD
test_command "kg ingest note" "$DOT_AGENTS_ABS kg ingest '${KG_NOTE}' --type markdown --yes"
assert_contains "kg warm indexes notes" "$DOT_AGENTS_ABS kg warm" "notes indexed"
assert_contains "kg query source_lookup returns the note" \
  "$DOT_AGENTS_ABS kg query --intent source_lookup 'smoke-note'" "src-smoke-note"
assert_contains "kg query graph_health is healthy" \
  "$DOT_AGENTS_ABS kg query --intent graph_health ''" "status=healthy"
assert_contains "kg bridge health is available" \
  "$DOT_AGENTS_ABS kg bridge health" "available"

echo ""
echo -e "${BOLD}KG Help / read-only Commands${NC}"
# Help paths and read-only status paths prove the kg subtree executes on the
# live binary even without a built code graph. The CODE lane (kg build +
# content-asserting code-status/impact) needs code-review-graph and is
# exercised in CI (test.yml sets up a .venv crg), not here.
test_command "kg health --help" "$DOT_AGENTS_ABS kg health --help"
test_command "kg code-status --help" "$DOT_AGENTS_ABS kg code-status --help"
test_command "kg bridge --help" "$DOT_AGENTS_ABS kg bridge --help"
test_command "kg code-status" "$DOT_AGENTS_ABS kg code-status"

# Cleanup the isolated home so repeat local runs start fresh.
rm -rf "${SMOKE_ROOT}"

echo ""
echo -e "${BOLD}Feature Commands${NC}"
test_command "skills --help" "$DOT_AGENTS skills --help"
test_command "agents --help" "$DOT_AGENTS agents --help"
test_command "hooks --help" "$DOT_AGENTS hooks --help"
test_command "sync --help" "$DOT_AGENTS sync --help"

echo ""
echo -e "${BOLD}Dry-run Commands${NC}"
test_command "init --dry-run" "$DOT_AGENTS init --dry-run"
test_command "add /tmp --dry-run" "$DOT_AGENTS add /tmp --dry-run"

echo ""
echo -e "${BOLD}Help Commands${NC}"
test_command "init --help" "$DOT_AGENTS init --help"
test_command "add --help" "$DOT_AGENTS add --help"
test_command "remove --help" "$DOT_AGENTS remove --help"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${BOLD}Results:${NC} ${GREEN}$passed passed${NC}, ${RED}$failed failed${NC}"
echo ""

if [[ $failed -gt 0 ]]; then
  echo -e "${RED}Some tests failed!${NC}"
  exit 1
else
  echo -e "${GREEN}All tests passed!${NC}"
  exit 0
fi
