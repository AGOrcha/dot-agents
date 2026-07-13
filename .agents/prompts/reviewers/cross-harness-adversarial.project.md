# Cross-harness adversarial lens — dot-agents repo overlay

Repo-local committed layer. Composes **after** `reviewers/reviewer.base.md` (the contract) and
`reviewers/cross-harness-adversarial.md` (the lens: route the adversarial pass to a different brain).
This file adds **only** the concrete commands for the harnesses dot-agents hosts commonly have, each
behind a detection guard so an absent tool is a **skip, not a failure**.

## Detection — probe, then exclude the running engine

dot-agents detects the same five platforms its `internal/platform` layer does. Probe PATH (mirrors
`cliprobe.go`'s `exec.LookPath`); the running engine is excluded from selection.

```bash
running_engine=claude
[ -n "$CURSOR_SESSION_ID${CURSOR_TRACE_ID:-}" ] && running_engine=cursor
[ -n "${CODEX_SESSION_ID:-}" ]    && running_engine=codex
[ -n "${OPENCODE_SESSION_ID:-}" ] && running_engine=opencode
# CLAUDECODE set => Claude Code host (default above)

avail=()
command -v codex        >/dev/null 2>&1 && avail+=(codex)
command -v cursor        >/dev/null 2>&1 && avail+=(cursor)   # or: command -v agent
command -v agent         >/dev/null 2>&1 && avail+=(cursor)
command -v opencode      >/dev/null 2>&1 && avail+=(opencode)
command -v copilot       >/dev/null 2>&1 && avail+=(copilot)
command -v claude        >/dev/null 2>&1 && avail+=(claude)

# candidate set = available minus running_engine; pick the first.
```

## Concrete dispatch (the harnesses dot-agents commonly has: codex, cursor-agent)

Each is guarded: if the binary is absent, the branch is skipped, not failed.

```bash
brief='Read-only adversarial review of this diff. Do NOT edit, commit, or run anything.
Apply the adversarial checklist (security, broken invariants, concurrency, swallowed errors,
data-loss/clobber, POSIX/Windows divergence). Emit findings in:
- severity / location / scenario / suggested_fix.  Target diff follows:'
diff="$(git diff origin/master...HEAD)"   # or the PR diff

out=.agents/active/review/${task_id}-cross-harness-adversarial.engine.txt

# Prefer codex (a different brain than a Claude host)
if [ "$review_engine" = codex ] && command -v codex >/dev/null 2>&1; then
  printf '%s\n\n%s\n' "$brief" "$diff" | codex exec - > "$out"

# else cursor-agent
elif [ "$review_engine" = cursor ] && { command -v cursor >/dev/null 2>&1 || command -v agent >/dev/null 2>&1; }; then
  bin=$(command -v cursor >/dev/null 2>&1 && echo cursor || echo agent)
  printf '%s\n\n%s\n' "$brief" "$diff" | "$bin" agent --print --output-format text > "$out"

else
  echo "no alternate harness available; skipping (see lens template Graceful skip)"
fi
```

## Output-to-PR-comment (engine-prefixed)

```bash
{ printf '### Cross-harness adversarial review — engine: %s (host: %s)\n\n' "$review_engine" "$running_engine"; cat "$out"; } > "${out%.txt}.body.md"
gh pr comment "$pr" --body-file "${out%.txt}.body.md"
```

If `$pr` is empty (bare branch/commit target), leave the engine-prefixed body at
`.agents/active/review/${task_id}-cross-harness-adversarial.md` for the orchestrator to read.

## dot-agents hotspots to red-team (same as the `adversarial` overlay)

The alternate brain should be briefed on dot-agents' specific attack surface:

- **PATH / exec hardening:** `exec.Command` taking a caller-influenced binary name (SonarCloud
  go:S4036) — absolute / `execabs`-checked spawns only.
- **POSIX/Windows divergence:** raw `os.*` / `runtime.GOOS` branches that should route through
  `internal/fsops` (mutations) / `internal/testutil.Make*Unreadable` (forced errors).
- **Config-layer trust:** inherited org/team layer injecting a malicious source/package ref/protected
  override; secret leakage into the lock, generated outputs, or logs.
- **Swallowed results / clobber:** discarded `err` / `_ =` on `os.Stat`/`os.Remove`/link ops hiding a
  managed-output clobber; `da sync` pruning that deletes outside the resolved managed set.

Read-only always. Verdict line `(lens: cross-harness-adversarial)`; `fail` on any confirmed
BLOCKER/HIGH. Absent alternate harness => graceful skip (`pass` + `[SKIPPED: no alternate harness]`),
never a hard fail.
