APPROVE

# Adversarial review — wire-managed-gitignore-autofill (D14/R8)

Stage: review (adversarial) (`swarm-da-inner-loop-d14-review_adversarial-0`).
Upstream gate: review-acceptance-invariants=APPROVE (review-architecture-standards=APPROVE,
verify-cli-runner=PASS, verify-unit=PASS, impl=DONE, commit `2bba968`). Gate honored — proceeded.
Resolved prompt: `.agents/prompts/reviewers/adversarial.project.md` (repo-local overlay;
`da --json workflow resolve-prompt --kind reviewer --slug adversarial`; base `reviewers/reviewer.base.md`
+ lens `reviewers/adversarial.md` unresolved/absent — only the dot-agents project overlay exists).
Scope: READ-ONLY. `git -C .agents/worktrees/d14 diff origin/master` (8 files, +283/-16) + full source
of `internal/links/gitignore.go`, `internal/platform/{platform,copilot}.go`, `commands/refresh.go`,
both test files; plus two out-of-tree empirical probes (real `git check-ignore` + faithful strip-logic
replica) — no writes to SLICE.

Lens: adversarial (red-team). Verdict: **APPROVE** (lens: adversarial).
No BLOCKER/HIGH. Every acceptance item holds for the wired callers, proven fault-sensitively upstream.
The findings below are latent / environment-gated hardening gaps (MEDIUM/LOW), none triggered by any
current caller, so per the lens rule (`fail` only on BLOCKER/HIGH) they do not gate — but they are
real holes in the *stated* contracts and should be tracked.

## Attack surface probed — results

### neverIgnored contract CAN be bypassed (MEDIUM, latent) — the "never invisible" claim is overstated
`normalizeIgnoreEntries` (gitignore.go:150-166) filters the contract only by EXACT literal:
`neverIgnored[strings.TrimSuffix(norm, "/")]`. It defends `.agentsrc.lock`, `.agentsrc.lock/`,
`.agentsrc.json` (unit-tested, TestEnsureManagedGitignore_NeverIgnoresCommittedContract). It does NOT
defend any git-equivalent spelling of the same file. Proven with real `git check-ignore -v` in a temp
repo — each of these, if it ever entered the block, makes git ignore the committed contract:
  - `*.lock`         → `.gitignore:2:*.lock  .agentsrc.lock`
  - `/.agentsrc.lock`→ `.gitignore:2:/.agentsrc.lock  .agentsrc.lock`
  - `.agentsrc.*`    → matches BOTH `.agentsrc.lock` AND `.agentsrc.json`
None is caught by `neverIgnored` (lookups miss: `"*.lock"`, `"/.agentsrc.lock"`, `".agentsrc.*"` are
not keys). The docstrings ("filtered out even if a caller passes them by mistake", "the block must
never make the lock or manifest invisible to git") therefore over-promise: the safety net has holes
exactly where a caller mistake is plausible — a future `ManagedOutputReporter` that emits a broad
pattern (`*.lock`, `.agentsrc.*`) or an anchored form silently defeats the whole contract, with no
test to catch it. NOT a defect today: the static table (platform.go:118-130) and copilot.ManagedOutputs
(copilot.go:642-650) emit only narrow plain/dir/`*.json` patterns — none covers the contract files.
  Recommend (non-blocking): either narrow the promise in the docstrings to "exact-literal only", or
  harden `normalizeIgnoreEntries` to drop any entry whose git match would cover a neverIgnored file,
  and add a regression test feeding `*.lock`/`.agentsrc.*`/`/.agentsrc.lock` and asserting the lock
  stays trackable.

### CRLF line endings break idempotency + byte-stability on Windows (MEDIUM, env-gated) — defeats acceptance #3
Marker recognition is bare-line equality: `line == managedGitignoreBegin` (stripManagedGitignoreBlock,
gitignore.go:116-128), where `line` comes from `strings.Split(strings.TrimRight(content,"\n"),"\n")`
(splitGitignoreLines) — internal `\r` is never trimmed. Proven with a faithful replica of the exact
strip logic: a block written LF then checked out under `core.autocrlf=true` (Windows default) becomes
`# >>> … >>>\r` etc. The begin/end lines no longer equal the markers, so `strip` does NOT recognize
the block → the entire stale managed block is kept as "user content" and a fresh LF block is appended.
Consequence: duplicate managed blocks accumulate and the file is NOT byte-stable across refreshes on
Windows — directly violating acceptance criterion #3 (byte-stable refresh). All idempotency/convergence
tests (refresh_test.go, gitignore_test.go) use LF only, so this is an untested cross-OS gap — precisely
the "POSIX/Windows divergence a skipped test won't catch" the adversarial overlay flags. Not hitting the
primary dev platform (dot-agents is developed on macOS/Linux) and requires autocrlf, hence MEDIUM not HIGH.
  Recommend (non-blocking): `strings.TrimRight(line,"\r")` before the marker compare (or normalize CRLF
  in splitGitignoreLines) and add a CRLF-input idempotency test.

### Zero-enabled-platforms: the "block always present" guarantee is not honored by the wiring (LOW)
The library keeps its promise (EnsureManagedGitignore with empty ignorePaths still emits the
`.agentsrc.local.json` overlay block — TestEnsureManagedGitignore_AlwaysIgnoresOverlayEvenWithNoOutputs).
But the refresh WIRING never reaches it when no platform is enabled: runRefresh early-returns at
refresh.go:85-88 ("No enabled platforms … Nothing to refresh") BEFORE refreshOneProject →
ensureManagedGitignoreForRefresh. So for a zero-platform config the overlay is not ignored, and if a
project already has a managed block and the user later disables every platform, refresh leaves the OLD
block stale (never shrinks/regenerates). The docstring "the block is present (not empty) for any
resolved project" thus holds only at the library layer, not the command layer. Degenerate config, low
impact; noted as a spec/wiring-consistency + test gap (no refresh-level zero-platform overlay test).

### Not defects — verified clean
- **Clobber / partial write:** whole-file regenerate via `fsops.WriteFileAtomic` (gitignore.go:93);
  a non-IsNotExist read error aborts BEFORE any write (readManagedGitignore:100-107), so an unreadable
  `.gitignore` is never clobbered. Outside-marker user content is preserved (tested).
- **Swallowed results:** `ensureManagedGitignoreForRefresh` surfaces the error as a warning AND returns
  true → `projectFailed=true` → success stamp withheld (refresh.go:267-269, 292-295). No `_ =` / dropped err.
- **exec/PATH, config-layer trust, secret leakage:** none in scope — no `exec.Command`, no new source/
  layer parsing, no secrets touch this path. `runtime.GOOS` absent; slash-normalization is unconditional
  `ReplaceAll(…,"\\","/")` (correct, git-portable; tested).
- **Duplicate/self-heal:** repeated begin markers or two full blocks collapse back to one (strip toggles
  inBlock); truncated-block recovery tested.
- **Over-ignore (OBSERVATION, pre-existing model):** whole-dir patterns (`.claude/`, `.cursor/`, `.codex/`,
  `.opencode/`, `.antigravity/`) render any user-authored file under those dirs git-invisible. This is the
  established managed-link ownership model, NOT introduced by D14 (D14 only wires the ignore surface).

## Test gaps (all LOW, none gating)
1. No test feeding a covering-glob / anchored contract spelling (`*.lock`, `.agentsrc.*`, `/.agentsrc.lock`)
   to assert the lock stays trackable (mirrors the M1 bypass).
2. No CRLF-input idempotency/byte-stability test (mirrors M2).
3. No refresh-level zero-enabled-platform test asserting the overlay-present guarantee (mirrors L1).

Verdict: **APPROVE** (lens: adversarial).
