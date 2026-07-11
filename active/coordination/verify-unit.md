PASS

# Unit verify stage — wire-managed-gitignore-autofill (D14/R8)

Stage: verify (unit) (`swarm-da-inner-loop-d14-verify_unit-0`).
Upstream: impl=DONE (commit `2bba96819c020234792cf6c7e9ce7728252c6876`).
Worktree: `.agents/worktrees/d14`, branch `swarm/managed-gitignore-autofill`.
Resolved prompt: `verifiers/unit.project.md` (repo-local overlay).

## Scoped test run (as tasked)
`(cd .agents/worktrees/d14 && go test -race -count=1 ./internal/links/... ./internal/platform/... ./commands/...)`

Result: **16 packages ok** (71.6s wall). Digest:
- internal/links ok (1.3s)
- internal/platform ok (5.3s)
- commands ok (18.2s) + all commands/* subpackages ok (agents, config, eval,
  hooks, internal/cmdutil, internal/lifecycle, internal/mcp, internal/rules,
  internal/settings, kg, skills, sync, workflow — every one `ok`).
- Race detector clean, no data races, no cache reuse (`-count=1`).

New tests exercised (both PASS in the green run):
- `commands.TestRunRefresh_WritesManagedGitignoreBlock`
- `commands.TestCollectManagedOutputs_CopilotDynamicAndStaticPlatforms`

## Mutation-verify judgment: ACCEPT (tests are fault-sensitive, not tautological)
Ran two real reverts against HEAD, tests re-run, then `git checkout --` restored
(worktree confirmed clean at `2bba968`).

1. **Broke the per-platform seam** — dropped the `ManagedOutputReporter`
   branch in `platform.CollectManagedOutputs` (copilot falls back to the empty
   static entry):
   - `TestCollectManagedOutputs_...` FAILED: `missing ".github/hooks/*.json"` and
     `".github/copilot-instructions.md"` (got only the static claude set).
   - `TestRunRefresh_...` FAILED: managed block `missing ".github/hooks/*.json"`.
   → both tests genuinely assert copilot's DYNAMIC surface reaches the block.

2. **Unwired the call** — deleted the `ensureManagedGitignoreForRefresh(...)`
   invocation from `refreshOneProject`:
   - `TestRunRefresh_...` FAILED: `managed markers missing/malformed` — the
     `.gitignore` contained only the user line `my-secret-notes/`.
   → the end-to-end test drives the real production wiring
     (runRefresh → refreshOneProject → ensureManagedGitignoreForRefresh →
     CollectManagedOutputs → EnsureManagedGitignore), not a hand-rolled call.

Non-tautology checks: assertions are behavioral (block markers present, copilot
hook glob + `.agentsrc.local.json` inside, committed `.agentsrc.lock`/
`.agentsrc.json` filtered OUT via neverIgnored, user ignore preserved outside
markers, byte-stable across a second refresh). No line-executing/`_ =`-style or
"call it and assert no panic" tests. Negative surface covered (forbidden
contract entries asserted absent). Byte-stability is a real idempotency
invariant, not incidental.

## Coverage note
Cross-package coverage (`-coverpkg` over commands+platform+links) of the new
symbols:
- `platform.CollectManagedOutputs` — 100.0%
- `(*copilot).ManagedOutputs` — 100.0%
- `links.EnsureManagedGitignore` — 90.9% (uncovered: empty-repoRoot error +
  mkdir-error defensive branches)
- `commands.ensureManagedGitignoreForRefresh` — 77.8% (uncovered: `Flags.DryRun`
  preview branch + the write-error warn branch; happy path fully covered)
- links gitignore helpers (strip/render/normalize/join/split/read) — 100.0%.

The two below-95% functions miss only defensive/dry-run branches, not the
config-distribution contract behavior. D12 per-file 95% gate is enforced by
`scripts/verify.sh`/coverage-exceptions at release; flagged here for the impl
owner but not verdict-blocking (behavior is proven and mutation-sensitive).

Verdict: **PASS**.
